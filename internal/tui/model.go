package tui

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"tui-dl/internal/config"
	"tui-dl/internal/core"
	"tui-dl/internal/downloader"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateList state = iota
	stateChecking
	stateDownloading
	stateFolderSelect
)

type Item struct {
	Source      config.Source
	Category    string
	LocalStatus core.VersionStatus
	Downloaded  int64
	Total       int64
}

func (i Item) Title() string       { return i.Source.Name }
func (i Item) Description() string { return string(i.LocalStatus) }
func (i Item) FilterValue() string { return i.Source.Name }

type Model struct {
	Config     *config.Config
	State      state
	Tabs       []string
	ActiveTab  int
	Lists      []list.Model
	Filepicker filepicker.Model
	Viewport   viewport.Model
	Width      int
	Height     int
}

func NewModel(cfg *config.Config) Model {
	tabs := make([]string, 0, len(cfg.Categories))
	for name := range cfg.Categories {
		tabs = append(tabs, name)
	}
	sort.Strings(tabs)

	// Earthy colors for the list
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(lipgloss.AdaptiveColor{Light: "#2D5A27", Dark: "#78B159"}).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#2D5A27", Dark: "#78B159"})
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(lipgloss.AdaptiveColor{Light: "#5D8A47", Dark: "#A8D199"}).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#2D5A27", Dark: "#78B159"})

	lists := make([]list.Model, len(tabs))
	for i, catName := range tabs {
		cat := cfg.Categories[catName]
		items := []list.Item{}
		for _, src := range cat.Sources {
			items = append(items, Item{
				Source:      src,
				Category:    catName,
				LocalStatus: "Not Checked",
			})
		}
		lists[i] = list.New(items, d, 0, 0)
		lists[i].Title = fmt.Sprintf("Category: %s", catName)
		lists[i].SetShowHelp(false)
		lists[i].Styles.Title = lists[i].Styles.Title.
			Background(lipgloss.AdaptiveColor{Light: "#A0522D", Dark: "#CD853F"}).
			Foreground(lipgloss.Color("#FFFFFF"))
	}

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.CurrentDirectory, _ = os.Getwd()

	return Model{
		Config:     cfg,
		State:      stateList,
		Tabs:       tabs,
		ActiveTab:  0,
		Lists:      lists,
		Filepicker: fp,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

type CheckMsg struct {
	Category string
	Index    int
	Result   core.CheckResult
}

func checkSourceCmd(index int, category string, src config.Source, localPath string) tea.Cmd {
	return func() tea.Msg {
		result := core.CheckVersion(src, localPath)
		return CheckMsg{Category: category, Index: index, Result: result}
	}
}

type DownloadMsg struct {
	Category string
	Index    int
	Err      error
}

type StartDownloadMsg struct {
	Category     string
	Index        int
	ProgressChan chan downloader.Progress
}

type ProgressUpdateMsg struct {
	Category     string
	Index        int
	Progress     downloader.Progress
	ProgressChan chan downloader.Progress
}

func DownloadCmdBatch(index int, category, url, dest string, progressChan chan downloader.Progress) tea.Cmd {
	return func() tea.Msg {
		err := downloader.DownloadFile(url, dest, progressChan)
		return DownloadMsg{Category: category, Index: index, Err: err}
	}
}

func DownloadCmd(index int, category, url, dest string) tea.Cmd {
	return func() tea.Msg {
		progressChan := make(chan downloader.Progress, 10)

		go func() {
			// 1. Log space check
			progressChan <- downloader.Progress{Downloaded: 0, Total: -1} // Custom indicator for "Checking space"

			// 2. Perform HEAD to get size
			resp, err := http.Head(url)
			if err != nil {
				// Not fatal, we'll try to download anyway or it will fail later
			} else {
				defer resp.Body.Close()
				size := resp.ContentLength
				if size > 0 {
					// 3. Check space
					ok, avail, err := downloader.CheckAvailableSpace(dest, size)
					if err != nil {
						// Error checking space
					} else if !ok {
						progressChan <- downloader.Progress{Downloaded: -1, Total: avail} // Custom indicator for "Not enough space"
						close(progressChan)
						return
					} else {
						progressChan <- downloader.Progress{Downloaded: 1, Total: -1} // Custom indicator for "Space OK"
					}
				}
			}

			downloader.DownloadFile(url, dest, progressChan)
		}()

		return StartDownloadMsg{
			Category:     category,
			Index:        index,
			ProgressChan: progressChan,
		}
	}
}

func WaitForProgress(index int, category string, progressChan chan downloader.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-progressChan
		if !ok {
			return nil // DownloadCmdBatch will send the final DownloadMsg
		}
		return ProgressUpdateMsg{Category: category, Index: index, Progress: p, ProgressChan: progressChan}
	}
}

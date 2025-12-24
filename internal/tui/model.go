package tui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Source         config.Source
	Category       string
	LocalStatus    core.VersionStatus
	CurrentVersion string
	LatestVersion  string
	LocalMessage   string // Store error or info messages from checking
	Downloaded     int64
	Total          int64
}

func (i Item) Title() string {
	if i.Source.Name != "" {
		return i.Source.Name
	}
	if i.Source.ID != "" {
		return fmt.Sprintf("ID: %s", i.Source.ID)
	}
	return "Unnamed Source"
}
func (i Item) Description() string {
	status := string(i.LocalStatus)

	// Helper to normalize version strings (strip leading 'v's)
	normalizeVer := func(v string) string {
		return strings.TrimLeft(v, "v")
	}

	if i.LocalStatus == core.StatusError {
		return fmt.Sprintf("Error: %s", i.LocalMessage)
	}

	if i.LocalStatus == core.StatusUpToDate && i.CurrentVersion != "" {
		return fmt.Sprintf("Up to Date [v%s]", normalizeVer(i.CurrentVersion))
	}
	if i.LocalStatus == core.StatusNewer && i.CurrentVersion != "" && i.LatestVersion != "" {
		return fmt.Sprintf("Newer Version Available [v%s -> v%s]", normalizeVer(i.CurrentVersion), normalizeVer(i.LatestVersion))
	}
	if i.LatestVersion != "" && i.LocalStatus != core.StatusUpToDate && i.LocalStatus != core.StatusNewer {
		return fmt.Sprintf("%s [Latest: v%s]", status, normalizeVer(i.LatestVersion))
	}
	return status
}
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

func checkSourceCmd(index int, category string, src config.Source, localPath string, githubToken string) tea.Cmd {
	return func() tea.Msg {
		result := core.CheckVersion(src, localPath, githubToken)
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

func DownloadCmd(index int, category string, src config.Source, dest string, githubToken string) tea.Cmd {
	return func() tea.Msg {
		progressChan := make(chan downloader.Progress, 10)

		go func() {
			url := src.URL
			if url == "" {
				// 0. Auto-resolve
				progressChan <- downloader.Progress{Downloaded: 0, Total: -2} // Special indicator for "Resolving..."
				res := core.CheckVersion(src, dest, githubToken)
				if res.ResolvedURL == "" {
					progressChan <- downloader.Progress{Error: fmt.Errorf("could not resolve download URL: %s", res.Message)}
					close(progressChan)
					return
				}
				url = res.ResolvedURL
				// Recalculate dest if it was missing an extension (because URL was empty)
				if filepath.Base(dest) == src.Name || strings.Contains(filepath.Base(dest), "[") {
					dest = filepath.Join(filepath.Dir(dest), filepath.Base(url))
				}
				// Feedback the resolved info to TUI
				progressChan <- downloader.Progress{
					Downloaded:  1,
					Total:       -2,
					Status:      string(res.Status),
					Current:     res.Current,
					Latest:      res.Latest,
					ResolvedURL: res.ResolvedURL,
				}
			}

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
			return DownloadMsg{Category: category, Index: index, Err: nil}
		}
		if p.Error != nil {
			return DownloadMsg{Category: category, Index: index, Err: p.Error}
		}
		return ProgressUpdateMsg{Category: category, Index: index, Progress: p, ProgressChan: progressChan}
	}
}

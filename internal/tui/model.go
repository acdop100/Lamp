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
	"github.com/charmbracelet/bubbles/table"
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

func (i Item) normalizeVer(v string) string {
	return strings.TrimLeft(v, "v")
}

func (i Item) ToRow() table.Row {
	status := string(i.LocalStatus)
	if i.LocalStatus == core.StatusError {
		status = "Error: " + i.LocalMessage
	}

	current := i.normalizeVer(i.CurrentVersion)
	latest := i.normalizeVer(i.LatestVersion)

	return table.Row{
		i.Source.Name,
		status,
		current,
		latest,
	}
}

type Model struct {
	Config     *config.Config
	State      state
	Tabs       []string
	ActiveTab  int
	Tables     []table.Model
	TableData  [][]Item // Raw data for each table
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

	columns := []table.Column{
		{Title: "NAME", Width: 40},
		{Title: "STATUS", Width: 35},
		{Title: "CURRENT", Width: 15},
		{Title: "LATEST", Width: 15},
	}

	tables := make([]table.Model, len(tabs))
	tableData := make([][]Item, len(tabs))

	for i, catName := range tabs {
		cat := cfg.Categories[catName]
		var rows []table.Row
		var items []Item
		for _, src := range cat.Sources {
			it := Item{
				Source:      src,
				Category:    catName,
				LocalStatus: "Not Checked",
			}
			items = append(items, it)
			rows = append(rows, it.ToRow())
		}
		tableData[i] = items

		t := table.New(
			table.WithColumns(columns),
			table.WithRows(rows),
			table.WithFocused(true),
			table.WithHeight(10),
		)

		s := table.DefaultStyles()
		s.Header = s.Header.
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			BorderBottom(true).
			Bold(false).
			Foreground(lipgloss.AdaptiveColor{Light: "#A0522D", Dark: "#CD853F"})
		s.Selected = s.Selected.
			Foreground(lipgloss.AdaptiveColor{Light: "#2D5A27", Dark: "#78B159"}).
			Background(lipgloss.AdaptiveColor{Light: "#E1C699", Dark: "#2D5A27"}).
			Bold(true)
		t.SetStyles(s)

		tables[i] = t
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
		Tables:     tables,
		TableData:  tableData,
		Filepicker: fp,
	}
}

func (m *Model) resizeTableColumns(width int) {
	// Account for some padding/borders
	usableWidth := width - 10
	if usableWidth < 40 {
		usableWidth = 40
	}

	columns := []table.Column{
		{Title: "NAME", Width: int(float64(usableWidth) * 0.40)},
		{Title: "STATUS", Width: int(float64(usableWidth) * 0.35)},
		{Title: "CURRENT", Width: int(float64(usableWidth) * 0.12)},
		{Title: "LATEST", Width: int(float64(usableWidth) * 0.12)},
	}

	for i := range m.Tables {
		m.Tables[i].SetColumns(columns)
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

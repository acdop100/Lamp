package tui

import (
	"fmt"
	"math"
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

	// Check if this looks like a download status and we have progress info
	// The status string from update.go often starts with "Downloading..." or has "Enough space"
	// But we can rely on Total > 0 and Downloaded to be sure we are tracking progress
	if i.Total > 0 {
		percent := float64(i.Downloaded) / float64(i.Total)
		// Use a fixed width for the bar within the column, e.g. 20 chars
		// Ideally this would be dynamic based on column width, but ToRow doesn't know context width easily
		// We'll trust the renderer to truncate or we use a safe default
		status = progressBar(percent, 20)
	} else if i.LocalStatus == core.StatusError {
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

type QueueItem struct {
	Category string
	Index    int
}

type Model struct {
	Config          *config.Config
	State           state
	Tabs            []string
	ActiveTab       int
	Tables          []table.Model
	TableData       [][]Item // Raw data for each table
	Filepicker      filepicker.Model
	Viewport        viewport.Model
	Width           int
	Height          int
	DownloadQueue   []QueueItem
	ActiveDownloads int
}

func progressBar(percent float64, width int) string {
	if width < 2 {
		return ""
	}
	// Clamp percentage
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	barWidth := width - 8 // Reserve space for percentage text " 100.0%"
	if barWidth < 5 {
		barWidth = 5
	}

	full := int(math.Round(percent * float64(barWidth)))
	empty := barWidth - full

	if empty < 0 {
		empty = 0
	}

	bar := strings.Repeat("█", full) + strings.Repeat("░", empty)
	return fmt.Sprintf("%s %5.1f%%", bar, percent*100)
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

func DownloadCmdBatch(index int, category, url, dest string, threads int, progressChan chan downloader.Progress) tea.Cmd {
	return func() tea.Msg {
		err := downloader.DownloadFile(url, dest, threads, progressChan)
		return DownloadMsg{Category: category, Index: index, Err: err}
	}
}

func DownloadCmd(index int, category string, src config.Source, dest string, githubToken string, threads int) tea.Cmd {
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

			downloader.DownloadFile(url, dest, threads, progressChan)
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

type VerifyMsg struct {
	Category string
	Index    int
	Err      error
}

func VerifyCmd(index int, category, path, checksum string) tea.Cmd {
	return func() tea.Msg {
		err := downloader.VerifyFile(path, checksum)
		return VerifyMsg{Category: category, Index: index, Err: err}
	}
}

func (m *Model) ProcessQueue() tea.Cmd {
	var maxConcurrent = 3
	var cmds []tea.Cmd

	for len(m.DownloadQueue) > 0 && m.ActiveDownloads < maxConcurrent {
		// Pop
		item := m.DownloadQueue[0]
		m.DownloadQueue = m.DownloadQueue[1:]
		m.ActiveDownloads++

		// Get latest item data to ensure correct source/path
		// Find the item in table data
		var src config.Source
		found := false
		for tabIdx, name := range m.Tabs {
			if name == item.Category {
				if item.Index >= 0 && item.Index < len(m.TableData[tabIdx]) {
					src = m.TableData[tabIdx][item.Index].Source
					found = true
				}
				break
			}
		}

		if found {
			target := m.Config.GetTargetPath(item.Category, src)

			// Update status to "Starting..." if not already
			m.updateItemState(item.Category, item.Index, func(it *Item) {
				it.LocalStatus = "Starting download..."
			})

			cmds = append(cmds, DownloadCmd(item.Index, item.Category, src, target, m.Config.General.GitHubToken, m.Config.General.Threads))
		} else {
			m.ActiveDownloads-- // Should not happen, but safety decrement
		}
	}

	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

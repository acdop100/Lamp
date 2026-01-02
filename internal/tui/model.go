package tui

import (
	"fmt"
	"lamp/internal/config"
	"lamp/internal/core"
	"lamp/internal/downloader"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateSplash state = iota
	stateList
	stateChecking
	stateDownloading
	stateFolderSelect
	stateSearch // New state for search input mode
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

// GutenbergItem represents a book in the Gutenberg tab
type GutenbergItem struct {
	Book       core.GutenbergBook
	Downloaded bool
	Status     string // "Available", "Downloaded", "Downloading..."
}

// DynamicCatalog represents an API-driven catalog (Gutenberg, future sources)
type DynamicCatalog struct {
	Items       []GutenbergItem // Books in this catalog
	SearchQuery string          // Current search query (empty = default view)
	Loading     bool            // Loading state
	Error       string          // Error message if fetch fails
	CatalogType string          // "gutenberg", future: "archive_org", etc.
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
	Warnings        []string

	// Dynamic catalog support (Gutenberg, future sources)
	DynamicCatalogs map[string]*DynamicCatalog // Key = tab name
	SearchInput     textinput.Model            // Shared text input for search
	SearchActive    bool                       // Whether search mode is active
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

func NewModel(cfg *config.Config, warnings []string) Model {
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

	// Gutenberg-specific columns (simpler: Title, Author, Status, Downloads)
	gutenbergColumns := []table.Column{
		{Title: "TITLE", Width: 45},
		{Title: "AUTHOR", Width: 25},
		{Title: "STATUS", Width: 15},
		{Title: "DOWNLOADS", Width: 15},
	}

	tables := make([]table.Model, len(tabs))
	tableData := make([][]Item, len(tabs))
	dynamicCatalogs := make(map[string]*DynamicCatalog)

	for i, catName := range tabs {
		cat := cfg.Categories[catName]
		isGutenberg := false
		for _, src := range cat.Sources {
			if src.Strategy == "gutenberg" {
				isGutenberg = true
				break
			}
		}

		if isGutenberg {
			// Initialize empty Gutenberg table (will be populated on Init)
			t := table.New(
				table.WithColumns(gutenbergColumns),
				table.WithRows([]table.Row{}),
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
			tableData[i] = []Item{} // Empty for Gutenberg (uses DynamicCatalogs)

			// Initialize dynamic catalog for Gutenberg
			dynamicCatalogs[catName] = &DynamicCatalog{
				Items:       []GutenbergItem{},
				SearchQuery: "",
				Loading:     true, // Will load on Init
				Error:       "",
				CatalogType: "gutenberg",
			}
		} else {
			// Standard category setup
			cat := cfg.Categories[catName]
			var rows []table.Row
			var items []Item
			for _, src := range cat.Sources {
				path := cfg.GetTargetPath(catName, src)
				res := core.ScanLocalStatus(src, path)

				it := Item{
					Source:         src,
					Category:       catName,
					LocalStatus:    res.Status,
					CurrentVersion: res.Current,
					LatestVersion:  "---",
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
	}

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.CurrentDirectory, _ = os.Getwd()

	// Initialize search input
	ti := textinput.New()
	ti.Placeholder = "Search by title or author..."
	ti.CharLimit = 100
	ti.Width = 40

	initialState := stateList
	if len(warnings) > 0 {
		initialState = stateSplash
	}

	return Model{
		Config:          cfg,
		State:           initialState,
		Tabs:            tabs,
		ActiveTab:       0,
		Tables:          tables,
		TableData:       tableData,
		Filepicker:      fp,
		Warnings:        warnings,
		DynamicCatalogs: dynamicCatalogs,
		SearchInput:     ti,
		SearchActive:    false,
	}
}

func (m *Model) resizeTableColumns(width int) {
	// Account for some padding/borders
	usableWidth := width - 10
	if usableWidth < 40 {
		usableWidth = 40
	}

	for i := range m.Tables {
		if m.Tabs[i] == "Gutenberg" {
			gutenbergColumns := []table.Column{
				{Title: "TITLE", Width: int(float64(usableWidth) * 0.45)},
				{Title: "AUTHOR", Width: int(float64(usableWidth) * 0.25)},
				{Title: "STATUS", Width: int(float64(usableWidth) * 0.15)},
				{Title: "DOWNLOADS", Width: int(float64(usableWidth) * 0.15)},
			}
			m.Tables[i].SetColumns(gutenbergColumns)
		} else {
			columns := []table.Column{
				{Title: "NAME", Width: int(float64(usableWidth) * 0.40)},
				{Title: "STATUS", Width: int(float64(usableWidth) * 0.35)},
				{Title: "CURRENT", Width: int(float64(usableWidth) * 0.12)},
				{Title: "LATEST", Width: int(float64(usableWidth) * 0.12)},
			}
			m.Tables[i].SetColumns(columns)
		}
	}
}

func (m Model) Init() tea.Cmd {
	// If a category contains a Gutenberg source, start fetching books
	for name, cat := range m.Config.Categories {
		for _, src := range cat.Sources {
			if src.Strategy == "gutenberg" {
				lang := src.Params["language"]
				if lang == "" {
					lang = "en"
				}
				return FetchGutenbergCmd(name, lang, m.Config)
			}
		}
	}
	return nil
}

// DynamicCatalogLoadedMsg is sent when dynamic catalog data is fetched
type DynamicCatalogLoadedMsg struct {
	TabName string
	Items   []GutenbergItem
	Err     error
}

// DynamicCatalogSearchMsg is sent when search results are fetched
type DynamicCatalogSearchMsg struct {
	TabName string
	Query   string
	Items   []GutenbergItem
	Err     error
}

// FetchGutenbergCmd fetches top 100 books from Gutendex
func FetchGutenbergCmd(tabName string, language string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		books, err := core.FetchTopBooks(language, 100)
		if err != nil {
			return DynamicCatalogLoadedMsg{
				TabName: tabName,
				Items:   nil,
				Err:     err,
			}
		}

		// Find settings for path and organization
		organization := "by_author"
		path := ""
		if cat, ok := cfg.Categories[tabName]; ok {
			path = cat.Path
			for _, src := range cat.Sources {
				if src.Strategy == "gutenberg" {
					if org, ok := src.Params["organization"]; ok {
						organization = org
					}
					break
				}
			}
		}

		items := make([]GutenbergItem, len(books))
		for i, book := range books {
			downloaded := core.CheckDownloaded(book, path, organization)
			status := "Available"
			if downloaded {
				status = "Downloaded"
			}
			items[i] = GutenbergItem{
				Book:       book,
				Downloaded: downloaded,
				Status:     status,
			}
		}

		return DynamicCatalogLoadedMsg{
			TabName: tabName,
			Items:   items,
			Err:     nil,
		}
	}
}

// SearchGutenbergCmd searches for books matching a query
func SearchGutenbergCmd(tabName string, query string, language string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		books, err := core.SearchBooks(query, language)
		if err != nil {
			return DynamicCatalogSearchMsg{
				TabName: tabName,
				Query:   query,
				Items:   nil,
				Err:     err,
			}
		}

		// Find settings for path and organization
		organization := "by_author"
		path := ""
		if cat, ok := cfg.Categories[tabName]; ok {
			path = cat.Path
			for _, src := range cat.Sources {
				if src.Strategy == "gutenberg" {
					if org, ok := src.Params["organization"]; ok {
						organization = org
					}
					break
				}
			}
		}

		items := make([]GutenbergItem, len(books))
		for i, book := range books {
			downloaded := core.CheckDownloaded(book, path, organization)
			status := "Available"
			if downloaded {
				status = "Downloaded"
			}
			items[i] = GutenbergItem{
				Book:       book,
				Downloaded: downloaded,
				Status:     status,
			}
		}

		return DynamicCatalogSearchMsg{
			TabName: tabName,
			Query:   query,
			Items:   items,
			Err:     nil,
		}
	}
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

package tui

import (
	"fmt"
	"lamp/internal/config"
	"lamp/internal/core"
	"lamp/internal/downloader"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.State == stateSplash {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.State = stateList
			return m, nil
		}

		// Handle search mode input
		if m.State == stateSearch {
			switch msg.String() {
			case "esc":
				m.State = stateList
				m.SearchActive = false
				m.SearchInput.Reset()

				// Handle dynamic catalogs (reload default list)
				if catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]; ok {
					catalog.Loading = true
					catalog.SearchQuery = ""
					if catalog.CatalogType == "gutenberg" {
						cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
						lang := cat.Language
						if lang == "" {
							lang = "en"
						}
						return m, FetchGutenbergCmd(m.Tabs[m.ActiveTab], lang, m.Config)
					} else if catalog.CatalogType == "kiwix" {
						cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
						lang := cat.Language
						if lang == "" {
							lang = "eng"
						}
						category := ""
						for _, src := range cat.Sources {
							if src.Strategy == "kiwix" {
								category = src.Params["category"]
								break
							}
						}
						return m, FetchKiwixCmd(m.Tabs[m.ActiveTab], lang, category, m.Config)
					}
				} else {
					// Static tab - clear filter and restore full table
					m.FilterQuery = ""
					m.syncTableRows(m.ActiveTab)
				}
				return m, nil
			case "enter":
				query := m.SearchInput.Value()
				if query != "" {
					// Handle dynamic catalogs (API search)
					if catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]; ok {
						m.State = stateList
						m.SearchActive = false
						catalog.Loading = true
						catalog.SearchQuery = query
						if catalog.CatalogType == "gutenberg" {
							cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
							lang := cat.Language
							if lang == "" {
								lang = "en"
							}
							return m, SearchGutenbergCmd(m.Tabs[m.ActiveTab], query, lang, m.Config)
						} else if catalog.CatalogType == "kiwix" {
							cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
							lang := cat.Language
							if lang == "" {
								lang = "eng"
							}
							return m, SearchKiwixCmd(m.Tabs[m.ActiveTab], query, lang, m.Config)
						}
					} else {
						// Static tab - apply filter locally (already filtered live, just exit search mode)
						m.State = stateList
						m.SearchActive = false
					}
				}
				return m, nil
			}
			// Update text input and apply live filtering for static tabs
			m.SearchInput, cmd = m.SearchInput.Update(msg)
			// Live filtering for static tabs
			if !m.isDynamicTab(m.ActiveTab) {
				m.FilterQuery = m.SearchInput.Value()
				m.applyTableFilter(m.ActiveTab)
			}
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right", "l", "]":
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, nil
		case "left", "h", "[":
			m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			return m, nil
		case "/", "s":
			// Enter search mode for all tabs
			m.State = stateSearch
			m.SearchActive = true
			m.SearchInput.Focus()
			return m, nil
		case "u":
			// Trigger update check for all items in active category (not for dynamic catalogs)
			if m.isDynamicTab(m.ActiveTab) {
				return m, nil
			}
			var cmds []tea.Cmd
			items := m.TableData[m.ActiveTab]
			for i, it := range items {
				target := m.Config.GetTargetPath(it.Category, it.Source)
				cmds = append(cmds, checkSourceCmd(i, it.Category, it.Source, target, m.Config.General.GitHubToken))
			}
			return m, tea.Batch(cmds...)
		case "d":
			// Download selected item
			if m.isGutenbergTab(m.ActiveTab) {
				return m.handleGutenbergDownload()
			}
			if m.isKiwixTab(m.ActiveTab) {
				return m.handleKiwixDownload()
			}
			idx := m.Tables[m.ActiveTab].Cursor()
			if idx < 0 || idx >= len(m.TableData[m.ActiveTab]) {
				return m, nil
			}
			it := m.TableData[m.ActiveTab][idx]
			target := m.Config.GetTargetPath(it.Category, it.Source)

			it.LocalStatus = "Starting download..."
			m.TableData[m.ActiveTab][idx] = it
			m.syncTableRows(m.ActiveTab)

			m.ActiveDownloads++ // Manual download counts towards concurrency
			version := it.LatestVersion
			if version == "" || version == "---" {
				version = it.CurrentVersion
			}
			return m, DownloadCmd(idx, it.Category, it.Source, target, version, m.Config.General.GitHubToken, m.Config.General.Threads)
		case "D":
			// Download all missing files in current tab (not for dynamic catalogs)
			if m.isDynamicTab(m.ActiveTab) {
				return m, nil
			}
			// Add to queue instead of firing immediately
			items := m.TableData[m.ActiveTab]
			for i, it := range items {
				if it.LocalStatus == "Local File Not Found" || it.LocalStatus == "Not Checked" {
					it.LocalStatus = "Queued"
					m.TableData[m.ActiveTab][i] = it
					m.DownloadQueue = append(m.DownloadQueue, QueueItem{Category: it.Category, Index: i})
				}
			}
			m.syncTableRows(m.ActiveTab)
			return m, m.ProcessQueue()
		case "U":
			// Update all files with newer versions available in current tab (not for dynamic catalogs)
			if m.isDynamicTab(m.ActiveTab) {
				return m, nil
			}
			// Add to queue instead of firing immediately
			items := m.TableData[m.ActiveTab]
			for i, it := range items {
				if it.LocalStatus == core.StatusNewer {
					it.LocalStatus = "Queued"
					m.TableData[m.ActiveTab][i] = it
					m.DownloadQueue = append(m.DownloadQueue, QueueItem{Category: it.Category, Index: i})
				}
			}
			m.syncTableRows(m.ActiveTab)
			return m, m.ProcessQueue()
		case "c":
			// Open config directory
			if dir, err := config.GetConfigDir(); err == nil {
				core.OpenDir(dir)
			}
			return m, nil
		case "f":
			m.State = stateFolderSelect
			return m, m.Filepicker.Init()
		case "esc":
			if m.State == stateFolderSelect {
				m.State = stateList
				return m, nil
			}
			// Reset dynamic catalogs if searching
			if m.isDynamicTab(m.ActiveTab) {
				if catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]; ok && catalog.SearchQuery != "" {
					catalog.Loading = true
					catalog.SearchQuery = ""
					catalogType := m.getCatalogType(m.ActiveTab)
					cat := m.Config.Categories[m.Tabs[m.ActiveTab]]

					if catalogType == "gutenberg" {
						lang := cat.Language
						if lang == "" {
							lang = "en"
						}
						return m, FetchGutenbergCmd(m.Tabs[m.ActiveTab], lang, m.Config)
					} else if catalogType == "kiwix" {
						lang := cat.Language
						if lang == "" {
							lang = "eng"
						}
						category := ""
						for _, src := range cat.Sources {
							if src.Strategy == "kiwix" {
								category = src.Params["category"]
								break
							}
						}
						return m, FetchKiwixCmd(m.Tabs[m.ActiveTab], lang, category, m.Config)
					}
				}
			}
		}

	case DynamicCatalogLoadedMsg:
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			catalog.Loading = false
			if msg.Err != nil {
				catalog.Error = msg.Err.Error()
			} else {
				catalog.GutenbergItems = msg.Items
				catalog.Error = ""
				m.syncGutenbergTable(msg.TabName)
			}
		}
		return m, nil

	case DynamicCatalogSearchMsg:
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			catalog.Loading = false
			catalog.SearchQuery = msg.Query
			if msg.Err != nil {
				catalog.Error = msg.Err.Error()
			} else {
				catalog.GutenbergItems = msg.Items
				catalog.Error = ""
				m.syncGutenbergTable(msg.TabName)
			}
		}
		return m, nil

	case GutenbergDownloadMsg:
		m.ActiveDownloads--
		if m.ActiveDownloads < 0 {
			m.ActiveDownloads = 0
		}
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			if msg.Index >= 0 && msg.Index < len(catalog.GutenbergItems) {
				if msg.Err != nil {
					catalog.GutenbergItems[msg.Index].Status = "Error: " + msg.Err.Error()
				} else {
					catalog.GutenbergItems[msg.Index].Status = "Downloaded"
					catalog.GutenbergItems[msg.Index].Downloaded = true
				}
				m.syncGutenbergTable(msg.TabName)
			}
		}
		return m, nil

	case KiwixCatalogLoadedMsg:
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			catalog.Loading = false
			if msg.Err != nil {
				catalog.Error = msg.Err.Error()
			} else {
				catalog.KiwixItems = msg.Items
				catalog.Error = ""
				m.syncKiwixTable(msg.TabName)
			}
		}
		return m, nil

	case KiwixCatalogSearchMsg:
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			catalog.Loading = false
			catalog.SearchQuery = msg.Query
			if msg.Err != nil {
				catalog.Error = msg.Err.Error()
			} else {
				catalog.KiwixItems = msg.Items
				catalog.Error = ""
				m.syncKiwixTable(msg.TabName)
			}
		}
		return m, nil

	case KiwixDownloadMsg:
		m.ActiveDownloads--
		if m.ActiveDownloads < 0 {
			m.ActiveDownloads = 0
		}
		if catalog, ok := m.DynamicCatalogs[msg.TabName]; ok {
			if msg.Index >= 0 && msg.Index < len(catalog.KiwixItems) {
				if msg.Err != nil {
					catalog.KiwixItems[msg.Index].Status = "Error: " + msg.Err.Error()
				} else {
					catalog.KiwixItems[msg.Index].Status = "Downloaded"
					catalog.KiwixItems[msg.Index].Downloaded = true
				}
				m.syncKiwixTable(msg.TabName)
			}
		}
		return m, nil

	case CheckMsg:
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			it.Total = 0
			it.Downloaded = 0
			it.LocalStatus = msg.Result.Status
			it.CurrentVersion = msg.Result.Current
			it.LatestVersion = msg.Result.Latest
			it.LocalMessage = msg.Result.Message
			if msg.Result.ResolvedURL != "" {
				it.Source.URL = msg.Result.ResolvedURL
			}
		})
		return m, nil

	case StartDownloadMsg:
		return m, WaitForProgress(msg.Index, msg.Category, msg.ProgressChan)

	case ProgressUpdateMsg:
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			it.Downloaded = msg.Progress.Downloaded
			it.Total = msg.Progress.Total

			// Special handling for space check and resolution statuses
			if it.Total == -2 {
				if msg.Progress.Downloaded == 0 {
					it.LocalStatus = "Resolving URL..."
				} else {
					// Feedback from auto-resolution
					it.LocalStatus = core.VersionStatus(msg.Progress.Status)
					it.CurrentVersion = msg.Progress.Current
					it.LatestVersion = msg.Progress.Latest
					if msg.Progress.ResolvedURL != "" {
						it.Source.URL = msg.Progress.ResolvedURL
					}
				}
			} else if it.Total == -1 {
				if it.Downloaded == 0 {
					it.LocalStatus = "Checking available space..."
				} else if it.Downloaded == 1 {
					it.LocalStatus = "Enough space available!"
				}
			} else if it.Downloaded == -1 {
				it.LocalStatus = core.VersionStatus(fmt.Sprintf("Error: Not enough space (%s available)",
					humanize.Bytes(uint64(it.Total))))
			} else if it.Total > 0 {
				percent := float64(it.Downloaded) / float64(it.Total)
				it.LocalStatus = core.VersionStatus(fmt.Sprintf("Downloading... %.1f%% (%s/%s)",
					percent*100,
					humanize.Bytes(uint64(it.Downloaded)),
					humanize.Bytes(uint64(it.Total))))
			} else {
				it.LocalStatus = core.VersionStatus(fmt.Sprintf("Downloading... %s",
					humanize.Bytes(uint64(it.Downloaded))))
			}
		})
		return m, WaitForProgress(msg.Index, msg.Category, msg.ProgressChan)

	case DownloadMsg:
		m.ActiveDownloads--
		if m.ActiveDownloads < 0 {
			m.ActiveDownloads = 0
		}

		var nextCmd tea.Cmd
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			if msg.Err != nil {
				it.LocalStatus = core.VersionStatus("Error: " + msg.Err.Error())
			} else {
				if it.Source.Checksum != "" {
					it.LocalStatus = "Verifying integrity..."
					target := m.Config.GetTargetPath(it.Category, it.Source)
					nextCmd = VerifyCmd(msg.Index, msg.Category, target, it.Source.Checksum)
				} else {
					it.LocalStatus = "Finished"
					it.Downloaded = 0
					it.Total = 0
				}
			}
		})

		// Process queue and batch with nextCmd (verify or none)
		queueCmd := m.ProcessQueue()
		if nextCmd != nil && queueCmd != nil {
			return m, tea.Batch(nextCmd, queueCmd)
		} else if nextCmd != nil {
			return m, nextCmd
		}
		return m, queueCmd

	case VerifyMsg:
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			if msg.Err != nil {
				it.LocalStatus = core.VersionStatus("Checksum Failed")
				it.LocalMessage = msg.Err.Error()
			} else {
				it.LocalStatus = "Verified & Finished"
				it.Downloaded = 0
				it.Total = 0
			}
		})
		return m, nil

	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		m.resizeTableColumns(msg.Width)
		for i := range m.Tables {
			m.Tables[i].SetHeight(msg.Height - 11) // Reserve space for tabs, headers, footer
		}
	}

	switch m.State {
	case stateList:
		m.Tables[m.ActiveTab], cmd = m.Tables[m.ActiveTab].Update(msg)
	case stateFolderSelect:
		m.Filepicker, cmd = m.Filepicker.Update(msg)
		if didSelect, _ := m.Filepicker.DidSelectDisabledFile(msg); didSelect {
			m.State = stateList
		}
		if didSelect, _ := m.Filepicker.DidSelectFile(msg); didSelect {
			m.State = stateList
		}
	case stateSearch:
		m.SearchInput, cmd = m.SearchInput.Update(msg)
	}

	return m, cmd
}

// GutenbergDownloadMsg is sent when a Gutenberg book download completes
type GutenbergDownloadMsg struct {
	TabName string
	Index   int
	Err     error
}

// handleGutenbergDownload handles downloading the selected Gutenberg book
func (m *Model) handleGutenbergDownload() (tea.Model, tea.Cmd) {
	idx := m.Tables[m.ActiveTab].Cursor()
	catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]
	if !ok || idx < 0 || idx >= len(catalog.GutenbergItems) {
		return m, nil
	}

	item := &catalog.GutenbergItems[idx]
	if item.Downloaded || item.Status == "Downloading..." {
		return m, nil
	}

	item.Status = "Downloading..."
	m.syncGutenbergTable(m.Tabs[m.ActiveTab])
	m.ActiveDownloads++

	return m, DownloadGutenbergCmd(m.Tabs[m.ActiveTab], idx, item.Book, m.Config)
}

// DownloadGutenbergCmd downloads a Gutenberg book
func DownloadGutenbergCmd(tabName string, index int, book core.GutenbergBook, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		url := core.GetEPUB3URL(book)
		if url == "" {
			return GutenbergDownloadMsg{TabName: tabName, Index: index, Err: fmt.Errorf("no EPUB format available")}
		}

		// Find settings for path and organization
		cat := cfg.Categories[tabName]
		organization := "by_author"
		path := cat.Path
		for _, src := range cat.Sources {
			if src.Strategy == "gutenberg" {
				if org, ok := src.Params["organization"]; ok {
					organization = org
				}
				break
			}
		}

		dest := core.GetExpectedPath(book, path, organization)

		progressChan := make(chan downloader.Progress, 10)
		go func() {
			downloader.DownloadFile(url, dest, cfg.General.Threads, progressChan)
		}()

		// Drain progress channel (simplified - doesn't show progress bar for Gutenberg)
		for range progressChan {
		}

		// Check if file exists after download
		if core.CheckDownloaded(book, path, organization) {
			return GutenbergDownloadMsg{TabName: tabName, Index: index, Err: nil}
		}
		return GutenbergDownloadMsg{TabName: tabName, Index: index, Err: fmt.Errorf("download failed")}
	}
}

// syncGutenbergTable updates the Gutenberg table rows from DynamicCatalogs
func (m *Model) syncGutenbergTable(tabName string) {
	catalog, ok := m.DynamicCatalogs[tabName]
	if !ok {
		return
	}

	// Find Gutenberg tab index
	tabIdx := -1
	for i, tab := range m.Tabs {
		if tab == tabName {
			tabIdx = i
			break
		}
	}
	if tabIdx < 0 {
		return
	}

	var rows []table.Row
	for _, item := range catalog.GutenbergItems {
		author := core.GetPrimaryAuthor(item.Book)
		downloads := fmt.Sprintf("%d", item.Book.DownloadCount)
		rows = append(rows, table.Row{item.Book.Title, author, item.Status, downloads})
	}
	m.Tables[tabIdx].SetRows(rows)
}

// syncKiwixTable updates the Kiwix table rows from DynamicCatalogs
func (m *Model) syncKiwixTable(tabName string) {
	catalog, ok := m.DynamicCatalogs[tabName]
	if !ok {
		return
	}

	// Find Kiwix tab index
	tabIdx := -1
	for i, tab := range m.Tabs {
		if tab == tabName {
			tabIdx = i
			break
		}
	}
	if tabIdx < 0 {
		return
	}

	var rows []table.Row
	for _, item := range catalog.KiwixItems {
		size := humanize.Bytes(uint64(item.Entry.GetFileSize()))
		date := item.Entry.GetIssuedDate().Format("2006-01")
		rows = append(rows, table.Row{item.Entry.Title, item.Entry.Summary, item.Entry.Language, size, date, item.Status})
	}
	m.Tables[tabIdx].SetRows(rows)
}

// KiwixDownloadMsg is sent when a Kiwix ZIM download completes
type KiwixDownloadMsg struct {
	TabName string
	Index   int
	Err     error
}

// handleKiwixDownload handles downloading the selected Kiwix ZIM file
func (m *Model) handleKiwixDownload() (tea.Model, tea.Cmd) {
	idx := m.Tables[m.ActiveTab].Cursor()
	catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]
	if !ok || idx < 0 || idx >= len(catalog.KiwixItems) {
		return m, nil
	}

	item := &catalog.KiwixItems[idx]
	if item.Downloaded || item.Status == "Downloading..." {
		return m, nil
	}

	item.Status = "Downloading..."
	m.syncKiwixTable(m.Tabs[m.ActiveTab])
	m.ActiveDownloads++

	return m, DownloadKiwixCmd(m.Tabs[m.ActiveTab], idx, item.Entry, m.Config)
}

// DownloadKiwixCmd downloads a Kiwix ZIM file
func DownloadKiwixCmd(tabName string, index int, entry core.KiwixEntry, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		url := entry.GetDownloadURL()
		if url == "" {
			return KiwixDownloadMsg{TabName: tabName, Index: index, Err: fmt.Errorf("no download URL available")}
		}

		// Find settings for path
		cat := cfg.Categories[tabName]
		path := cat.Path

		dest := core.GetExpectedKiwixPath(entry, path)

		progressChan := make(chan downloader.Progress, 10)
		go func() {
			downloader.DownloadFile(url, dest, cfg.General.Threads, progressChan)
		}()

		// Drain progress channel (simplified - doesn't show progress bar for Kiwix)
		for range progressChan {
		}

		// Check if file exists after download
		if core.CheckKiwixDownloaded(entry, path) {
			return KiwixDownloadMsg{TabName: tabName, Index: index, Err: nil}
		}
		return KiwixDownloadMsg{TabName: tabName, Index: index, Err: fmt.Errorf("download failed")}
	}
}

func (m *Model) updateItemState(category string, index int, updateFn func(*Item)) {
	for i, tab := range m.Tabs {
		if tab == category {
			if index >= 0 && index < len(m.TableData[i]) {
				updateFn(&m.TableData[i][index])
				m.syncTableRows(i)
			}
			return
		}
	}
}

func (m *Model) syncTableRows(tabIndex int) {
	var rows []table.Row
	for _, it := range m.TableData[tabIndex] {
		rows = append(rows, it.ToRow())
	}
	m.Tables[tabIndex].SetRows(rows)
}

// applyTableFilter filters the table rows for static tabs based on FilterQuery
func (m *Model) applyTableFilter(tabIndex int) {
	if tabIndex < 0 || tabIndex >= len(m.TableData) {
		return
	}

	query := strings.ToLower(m.FilterQuery)
	var rows []table.Row
	for _, it := range m.TableData[tabIndex] {
		// Filter by Name (case-insensitive)
		if query == "" || strings.Contains(strings.ToLower(it.Source.Name), query) {
			rows = append(rows, it.ToRow())
		}
	}
	m.Tables[tabIndex].SetRows(rows)
}

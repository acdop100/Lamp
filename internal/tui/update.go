package tui

import (
	"fmt"
	"tui-dl/internal/core"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right", "l", "]":
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, nil
		case "left", "h", "[":
			m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			return m, nil
		case "u":
			// Trigger update check for all items in active category
			var cmds []tea.Cmd
			items := m.TableData[m.ActiveTab]
			for i, it := range items {
				target := m.Config.GetTargetPath(it.Category, it.Source)
				cmds = append(cmds, checkSourceCmd(i, it.Category, it.Source, target, m.Config.General.GitHubToken))
			}
			return m, tea.Batch(cmds...)
		case "d":
			// Download selected item
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
			return m, DownloadCmd(idx, it.Category, it.Source, target, m.Config.General.GitHubToken)
		case "D":
			// Download all missing files in current tab
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
		case "f":
			m.State = stateFolderSelect
			return m, m.Filepicker.Init()
		case "esc":
			if m.State == stateFolderSelect {
				m.State = stateList
			}
		}

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
	}

	return m, cmd
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

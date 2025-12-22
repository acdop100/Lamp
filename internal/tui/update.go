package tui

import (
	"fmt"
	"tui-dl/internal/core"

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
			items := m.Lists[m.ActiveTab].Items()
			for i, item := range items {
				it := item.(Item)
				target := m.Config.GetTargetPath(it.Category, it.Source)
				cmds = append(cmds, checkSourceCmd(i, it.Category, it.Source, target))
			}
			return m, tea.Batch(cmds...)
		case "d":
			// Download selected item
			idx := m.Lists[m.ActiveTab].Index()
			it := m.Lists[m.ActiveTab].SelectedItem().(Item)
			target := m.Config.GetTargetPath(it.Category, it.Source)

			it.LocalStatus = "Starting download..."
			m.Lists[m.ActiveTab].SetItem(idx, it)

			return m, DownloadCmd(idx, it.Category, it.Source.URL, target)
		case "D":
			// Download all missing files in current tab
			var cmds []tea.Cmd
			items := m.Lists[m.ActiveTab].Items()
			for i, item := range items {
				it := item.(Item)
				if it.LocalStatus == "Local File Not Found" || it.LocalStatus == "Not Checked" {
					target := m.Config.GetTargetPath(it.Category, it.Source)
					it.LocalStatus = "Queued for download..."
					m.Lists[m.ActiveTab].SetItem(i, it)
					cmds = append(cmds, DownloadCmd(i, it.Category, it.Source.URL, target))
				}
			}
			return m, tea.Batch(cmds...)
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
			it.LocalStatus = msg.Result.Status
		})
		return m, nil

	case StartDownloadMsg:
		return m, WaitForProgress(msg.Index, msg.Category, msg.ProgressChan)

	case ProgressUpdateMsg:
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			it.Downloaded = msg.Progress.Downloaded
			it.Total = msg.Progress.Total

			// Special handling for space check statuses
			if it.Total == -1 {
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
		m.updateItemState(msg.Category, msg.Index, func(it *Item) {
			if msg.Err != nil {
				it.LocalStatus = core.VersionStatus("Error: " + msg.Err.Error())
			} else {
				it.LocalStatus = "Finished"
				it.Downloaded = it.Total
			}
		})
		return m, nil

	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		for i := range m.Lists {
			m.Lists[i].SetSize(msg.Width, msg.Height-5) // Reserve space for tabs
		}
	}

	switch m.State {
	case stateList:
		m.Lists[m.ActiveTab], cmd = m.Lists[m.ActiveTab].Update(msg)
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
			items := m.Lists[i].Items()
			if index >= 0 && index < len(items) {
				it := items[index].(Item)
				updateFn(&it)
				m.Lists[i].SetItem(index, it)
			}
			return
		}
	}
}

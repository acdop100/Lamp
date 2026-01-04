package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle = lipgloss.NewStyle().Margin(1, 2)

	// Earthy Palette
	forestGreen = lipgloss.AdaptiveColor{Light: "#2D5A27", Dark: "#78B159"} // Active accents
	sand        = lipgloss.AdaptiveColor{Light: "#C2B280", Dark: "#E1C699"} // Inactive/Secondary
	clay        = lipgloss.AdaptiveColor{Light: "#A0522D", Dark: "#CD853F"} // Border/Warning

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(forestGreen).
			Padding(0, 1).
			Bold(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(sand).
				Padding(0, 1)

	tabRowStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(clay)
)

func (m Model) View() string {
	switch m.State {
	case stateSplash:
		warnStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")). // Yellow
			Bold(true).
			MarginBottom(1)

		msgStyle := lipgloss.NewStyle().
			Foreground(sand).
			MarginTop(2)

		var warnings string
		for _, w := range m.Warnings {
			warnings += fmt.Sprintf("- %s\n", w)
		}

		content := lipgloss.JoinVertical(lipgloss.Center,
			warnStyle.Render("Configuration Warning:"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(warnings),
			msgStyle.Render("Press any key to continue..."),
		)

		// Center the content
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, docStyle.Render(content))

	case stateList, stateSearch:
		catName := m.Tabs[m.ActiveTab]
		cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
		catalogType := m.getCatalogType(m.ActiveTab)

		// Config Header - different for dynamic catalogs
		var configHeader string
		if catalog, ok := m.DynamicCatalogs[catName]; ok && catalogType != "" {
			if catalog.Loading {
				loadingText := "Loading catalog..."
				if catalogType == "gutenberg" {
					loadingText = "Loading Project Gutenberg books..."
				} else if catalogType == "kiwix" {
					loadingText = "Loading Kiwix library..."
				}
				configHeader = lipgloss.NewStyle().
					Foreground(sand).
					Width(m.Width - 4).
					Align(lipgloss.Center).
					Render(loadingText)
			} else if catalog.Error != "" {
				configHeader = lipgloss.NewStyle().
					Foreground(lipgloss.Color("9")). // Red
					Width(m.Width - 4).
					Align(lipgloss.Center).
					Render(fmt.Sprintf("Error loading: %s", catalog.Error))
			} else if catalog.SearchQuery != "" {
				itemCount := 0
				if catalogType == "gutenberg" {
					itemCount = len(catalog.GutenbergItems)
				} else if catalogType == "kiwix" {
					itemCount = len(catalog.KiwixItems)
				}
				configHeader = lipgloss.NewStyle().
					Foreground(sand).
					Width(m.Width - 4).
					Align(lipgloss.Center).
					Render(fmt.Sprintf("Search results for: \"%s\" (%d items) | Path: %s", catalog.SearchQuery, itemCount, cat.Path))
			} else {
				defaultText := "Catalog | Path: " + cat.Path
				if catalogType == "gutenberg" {
					defaultText = "Top 100 Popular Books | Path: " + cat.Path
				} else if catalogType == "kiwix" {
					defaultText = fmt.Sprintf("Kiwix Library (%d ZIMs) | Path: %s", len(catalog.KiwixItems), cat.Path)
				}
				configHeader = lipgloss.NewStyle().
					Foreground(sand).
					Width(m.Width - 4).
					Align(lipgloss.Center).
					Render(defaultText)
			}
		} else {
			// Standard category (non-dynamic)
			dlPath := m.Config.Categories[catName].Path
			if dlPath == "" {
				dlPath = m.Config.Storage.DefaultRoot
			}
			configHeader = lipgloss.NewStyle().
				Foreground(sand).
				Width(m.Width - 4).
				Align(lipgloss.Center).
				Render(fmt.Sprintf("Targets: OS=%v Arch=%v | Path: %s", m.Config.General.OS, m.Config.General.Arch, dlPath))
		}

		var tabs []string
		for i, t := range m.Tabs {
			if i == m.ActiveTab {
				tabs = append(tabs, activeTabStyle.Render(t))
			} else {
				tabs = append(tabs, inactiveTabStyle.Render(t))
			}
		}

		tabRow := tabRowStyle.
			Width(m.Width - 4).
			Align(lipgloss.Center).
			Render(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))

		tableView := m.Tables[m.ActiveTab].View()

		// Footer - different for dynamic catalogs
		var footer string
		if m.isDynamicTab(m.ActiveTab) {
			if m.State == stateSearch {
				footer = lipgloss.NewStyle().
					Foreground(sand).
					MarginTop(1).
					Render(" Enter: search | Esc: cancel | Type to search...")
			} else {
				footer = lipgloss.NewStyle().
					Foreground(sand).
					MarginTop(1).
					Render(" h/l: tabs | /: search | d: download | Esc: back to list | c: open config | q: quit")
			}
		} else {
			footer = lipgloss.NewStyle().
				Foreground(sand).
				MarginTop(1).
				Render(" h/l: tabs | d: download | shift-d: download all | u: check updates | shift-u: update all | c: open config | q: quit")
		}

		// Search bar - always visible, compact inline style (no border)
		searchPrefix := lipgloss.NewStyle().Foreground(sand).Render("/:")
		if m.State == stateSearch {
			searchPrefix = lipgloss.NewStyle().Foreground(forestGreen).Bold(true).Render("/:")
		}
		searchBar := searchPrefix + " " + m.SearchInput.View()

		// configHeader centered across full width, search bar overlayed on left
		centeredHeader := lipgloss.NewStyle().
			Width(m.Width - 4).
			Align(lipgloss.Center).
			Render(configHeader)
		// Place search bar at position 0, overlaying the left side of the centered header
		topRow := lipgloss.Place(m.Width-4, 1, lipgloss.Left, lipgloss.Top, searchBar,
			lipgloss.WithWhitespaceBackground(lipgloss.NoColor{}),
		)
		// Overlay by combining: take search bar width chars, then the rest from centered header
		searchWidth := lipgloss.Width(searchBar)
		if searchWidth < len(centeredHeader) {
			topRow = searchBar + centeredHeader[searchWidth:]
		} else {
			topRow = searchBar
		}

		// Join everything into one string WITHOUT margins first
		content := lipgloss.JoinVertical(lipgloss.Left,
			topRow,
			tabRow,
			tableView,
			footer,
		)

		return docStyle.Render(content)

	case stateFolderSelect:
		return docStyle.Render(fmt.Sprintf(
			"Select Target Directory (ESC to cancel):\n\n%s",
			m.Filepicker.View(),
		))
	default:
		return "Unknown state"
	}
}

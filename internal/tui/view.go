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
		isGutenberg := false
		for _, src := range cat.Sources {
			if src.Strategy == "gutenberg" {
				isGutenberg = true
				break
			}
		}

		// Config Header - different for Gutenberg
		var configHeader string
		if isGutenberg {
			if catalog, ok := m.DynamicCatalogs[m.Tabs[m.ActiveTab]]; ok {
				cat := m.Config.Categories[m.Tabs[m.ActiveTab]]
				if catalog.Loading {
					configHeader = lipgloss.NewStyle().
						Foreground(sand).
						Width(m.Width - 4).
						Align(lipgloss.Center).
						Render("Loading Project Gutenberg books...")
				} else if catalog.Error != "" {
					configHeader = lipgloss.NewStyle().
						Foreground(lipgloss.Color("9")). // Red
						Width(m.Width - 4).
						Align(lipgloss.Center).
						Render(fmt.Sprintf("Error loading books: %s", catalog.Error))
				} else if catalog.SearchQuery != "" {
					configHeader = lipgloss.NewStyle().
						Foreground(sand).
						Width(m.Width - 4).
						Align(lipgloss.Center).
						Render(fmt.Sprintf("Search results for: \"%s\" (%d books) | Path: %s", catalog.SearchQuery, len(catalog.Items), cat.Path))
				} else {
					configHeader = lipgloss.NewStyle().
						Foreground(sand).
						Width(m.Width - 4).
						Align(lipgloss.Center).
						Render(fmt.Sprintf("Top 100 Popular Books | Path: %s", cat.Path))
				}
			}
		} else {
			// Dynamic Download Path for active tab
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

		// Search input row (only in search mode)
		var searchRow string
		if m.State == stateSearch {
			searchStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(forestGreen).
				Padding(0, 1)
			searchRow = searchStyle.Render(m.SearchInput.View())
		}

		tableView := m.Tables[m.ActiveTab].View()

		// Footer - different for Gutenberg
		var footer string
		if isGutenberg {
			if m.State == stateSearch {
				footer = lipgloss.NewStyle().
					Foreground(sand).
					MarginTop(1).
					Render(" Enter: search • Esc: cancel • Type to search...")
			} else {
				footer = lipgloss.NewStyle().
					Foreground(sand).
					MarginTop(1).
					Render(" h/l: tabs • /: search • d: download • Esc: back to top 100 • c: open config • q: quit")
			}
		} else {
			footer = lipgloss.NewStyle().
				Foreground(sand).
				MarginTop(1).
				Render(" h/l: tabs • d: download • shift-d: download all • u: check updates • shift-u: update all • c: open config • q: quit")
		}

		// Join everything into one string WITHOUT margins first
		var content string
		if m.State == stateSearch {
			content = lipgloss.JoinVertical(lipgloss.Left,
				configHeader,
				tabRow,
				searchRow,
				tableView,
				footer,
			)
		} else {
			content = lipgloss.JoinVertical(lipgloss.Left,
				configHeader,
				tabRow,
				tableView,
				footer,
			)
		}

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

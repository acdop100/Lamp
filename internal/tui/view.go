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
	case stateList:
		// Dynamic Download Path for active tab
		catName := m.Tabs[m.ActiveTab]
		dlPath := m.Config.Categories[catName].Path
		if dlPath == "" {
			dlPath = m.Config.Storage.DefaultRoot
		}

		// Config Header
		configHeader := lipgloss.NewStyle().
			Foreground(sand).
			Width(m.Width - 4).
			Align(lipgloss.Center).
			Render(fmt.Sprintf("Targets: OS=%v Arch=%v | Path: %s", m.Config.General.OS, m.Config.General.Arch, dlPath))

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

		footer := lipgloss.NewStyle().
			Foreground(sand).
			MarginTop(1).
			Render(" h/l: tabs • d: download • D: download all • u: check updates • q: quit")

		// Join everything into one string WITHOUT margins first
		content := lipgloss.JoinVertical(lipgloss.Left,
			configHeader,
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

package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"tui-dl/internal/config"
	"tui-dl/internal/core"
	"tui-dl/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//go:embed config.yaml.example catalogs
var embeddedFiles embed.FS

//go:embed config.yaml.example
var defaultConfig []byte

func main() {
	checkMode := flag.Bool("check", false, "Check status of all monitored applications")
	versionMode := flag.Bool("version", false, "Print version information")
	flag.Parse()

	if *versionMode {
		fmt.Printf("LAMP version %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("build date: %s\n", date)
		os.Exit(0)
	}

	if err := config.EnsureConfigExists(defaultConfig, embeddedFiles); err != nil {
		fmt.Printf("Warning: failed to ensure config exists: %v\n", err)
	}

	// Pass empty string to load from default location
	cfg, err := config.LoadConfig("", defaultConfig, embeddedFiles)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Check system compatibility
	warnings := config.CheckSystemCompatibility(cfg)

	if *checkMode {
		if len(warnings) > 0 {
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
			fmt.Println(warnStyle.Render("Configuration Warning:"))
			for _, w := range warnings {
				fmt.Println(warnStyle.Render("- " + w))
			}
			fmt.Println("") // Spacer
		}

		fmt.Println("Checking status of all monitored applications...")
		fmt.Println("--------------------------------------------------")

		// Define CLI Styles
		red := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		gray := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

		tabs := make([]string, 0, len(cfg.Categories))
		for name := range cfg.Categories {
			tabs = append(tabs, name)
		}
		sort.Strings(tabs)

		for _, catName := range tabs {
			cat := cfg.Categories[catName]
			for _, src := range cat.Sources {
				target := cfg.GetTargetPath(catName, src)
				result := core.CheckVersion(src, target, cfg.General.GitHubToken)

				statusStr := string(result.Status)
				style := gray // Default

				switch result.Status {
				case core.StatusUpToDate:
					statusStr = green.Render(statusStr)
					style = green
				case core.StatusNewer:
					statusStr = yellow.Render(statusStr)
					style = yellow
				case core.StatusNotFound:
					statusStr = red.Render(statusStr)
					style = red
				case core.StatusError:
					statusStr = red.Bold(true).Render(statusStr)
					style = red
				}

				versionInfo := ""
				if result.Current != "" && result.Latest != "" {
					versionInfo = style.Render(fmt.Sprintf(" [%s -> %s]", result.Current, result.Latest))
				} else if result.Latest != "" {
					versionInfo = style.Render(fmt.Sprintf(" [Latest: %s]", result.Latest))
				}

				fmt.Printf("[%s] %s: %s%s\n", catName, src.Name, statusStr, versionInfo)
			}
		}
		os.Exit(0)
	}

	m := tui.NewModel(cfg, warnings)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}
}

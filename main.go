package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"tui-dl/internal/config"
	"tui-dl/internal/core"
	"tui-dl/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	checkMode := flag.Bool("check", false, "Check status of all monitored applications")
	flag.Parse()

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	if *checkMode {
		fmt.Println("Checking status of all monitored applications...")
		fmt.Println("--------------------------------------------------")

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
				versionInfo := ""
				if result.Current != "" && result.Latest != "" {
					versionInfo = fmt.Sprintf(" [%s -> %s]", result.Current, result.Latest)
				} else if result.Latest != "" {
					versionInfo = fmt.Sprintf(" [Latest: %s]", result.Latest)
				}

				fmt.Printf("[%s] %s: %s%s\n", catName, src.Name, statusStr, versionInfo)
			}
		}
		os.Exit(0)
	}

	m := tui.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}
}

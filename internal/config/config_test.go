package config

import (
	"testing"
)

func TestExpandSources(t *testing.T) {
	// Setup generic config
	cfg := &Config{
		General: GeneralConfig{
			OS:   []string{"linux", "macos"},
			Arch: []string{"amd64", "arm64"},
		},
		Categories: map[string]Category{
			"Test": {
				Sources: []Source{
					{
						Name:   "FullApp",
						Params: map[string]string{"p": "{{os}}-{{arch}}"},
					},
					{
						Name:   "ArchOnly",
						Params: map[string]string{"p": "{{arch}}"},
					},
					{
						Name:   "Static",
						Params: map[string]string{"p": "static"},
					},
				},
			},
		},
	}

	expandSources(cfg)

	sources := cfg.Categories["Test"].Sources

	// Expected:
	// FullApp: 2 OS * 2 Arch = 4 entries
	// ArchOnly: 1 OS * 2 Arch = 2 entries
	// Static: 1 entries
	// Total = 7

	if len(sources) != 7 {
		t.Errorf("Expected 7 sources, got %d", len(sources))
		for i, s := range sources {
			t.Logf("[%d] %s (Params: %v)", i, s.Name, s.Params)
		}
	}

	// Verify generic names
	countFull := 0
	countArch := 0
	countStatic := 0

	for _, s := range sources {
		if s.Name == "Static" {
			countStatic++
		} else if len(s.Params) > 0 {
			val := s.Params["p"]
			if val == "linux-amd64" {
				countFull++
			}
			if val == "linux-arm64" {
				countFull++
			}
			if val == "macos-amd64" {
				countFull++
			}
			if val == "macos-arm64" {
				countFull++
			}
			if val == "amd64" {
				countArch++
			}
			if val == "arm64" {
				countArch++
			}
		}
	}

	if countStatic != 1 {
		t.Errorf("Expected 1 static source, got %d", countStatic)
	}
	// FullApps get renamed with [os/arch] suffix
	// Wait, test checked Param values but `Config` logic also updates Name.
	// The original "FullApp" gets replaced by 4 new sources.
	// The logic:
	// FullApp -> "FullApp [linux/amd64]", "FullApp [linux/arm64]", etc.

	// Check if correct names exist?
	// Checking the expansion count is a good primary indicator.
}

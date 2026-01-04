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
}

func TestExpandSourcesWithMaps(t *testing.T) {
	cfg := &Config{
		General: GeneralConfig{
			OS:   []string{"linux", "windows", "macos"},
			Arch: []string{"amd64", "arm64"},
		},
		Categories: map[string]Category{
			"MapTest": {
				Sources: []Source{
					{
						Name: "MappedApp",
						Params: map[string]string{
							"p": "{{os_map}}::{{arch_map}}::{{ext}}",
						},
						OSMap: map[string]string{
							"linux":   "appimage",
							"windows": "win64",
						},
						ArchMap: map[string]string{
							"linux/amd64":   "x64",
							"windows/amd64": "x64",
							// windows arm64 fallback to empty if excluded or just not mapped?
							// If not in map, it returns ""
						},
						ExtMap: map[string]string{
							"windows": "7z",
						},
						Exclude: []string{"macos", "windows/arm64"}, // Exclude macos and win-arm64
					},
				},
			},
		},
	}

	expandSources(cfg)

	sources := cfg.Categories["MapTest"].Sources
	// Expected expansions:
	// linux/amd64 -> p: "appimage::x64::zip" (default ext for linux)
	// linux/arm64 -> p: "appimage::::zip" (arch not mapped)
	// windows/amd64 -> p: "win64::x64::7z" (ext map)
	// windows/arm64 -> Excluded
	// macos -> Excluded

	// Total: 3 (linux/amd64, linux/arm64, windows/amd64)

	if len(sources) != 3 {
		t.Errorf("Expected 3 sources, got %d", len(sources))
		for i, s := range sources {
			t.Logf("[%d] %s OS=%s Arch=%s Params=%v", i, s.Name, s.OS, s.Arch, s.Params)
		}
	}

	foundLinuxAMD64 := false
	foundWinAMD64 := false

	for _, s := range sources {
		if s.Params["p"] == "appimage::x64::zip" {
			foundLinuxAMD64 = true
		}
		if s.Params["p"] == "win64::x64::7z" {
			foundWinAMD64 = true
		}
	}

	if !foundLinuxAMD64 {
		t.Error("Did not find correct Linux AMD64 expansion")
	}
	if !foundWinAMD64 {
		t.Error("Did not find correct Windows AMD64 expansion")
	}
}

func TestGetStandardizedFilename(t *testing.T) {
	tests := []struct {
		name     string
		source   Source
		version  string
		ext      string
		expected string
	}{
		{
			name: "Basic",
			source: Source{
				Name: "Chromium",
				OS:   "windows",
				Arch: "amd64",
			},
			version:  "120.0.6099.109",
			ext:      ".zip",
			expected: "Chromium_windows_amd64_120.0.6099.109.zip",
		},
		{
			name: "Name with spaces and brackets",
			source: Source{
				Name: "Mozilla Firefox [win64]",
				OS:   "windows",
				Arch: "amd64",
			},
			version:  "121.0",
			ext:      "exe",
			expected: "MozillaFirefox_windows_amd64_121.0.exe",
		},
		{
			name: "Empty version",
			source: Source{
				Name: "TestApp",
				OS:   "linux",
				Arch: "arm64",
			},
			version:  "",
			ext:      ".tar.gz",
			expected: "TestApp_linux_arm64_latest.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.source.GetStandardizedFilename(tt.version, tt.ext)
			if got != tt.expected {
				t.Errorf("GetStandardizedFilename() = %v, want %v", got, tt.expected)
			}
		})
	}
}

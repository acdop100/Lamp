package config

import (
	"fmt"
	"os"
	"path/filepath"

	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Storage    Storage             `yaml:"storage"`
	General    GeneralConfig       `yaml:"general"`
	Categories map[string]Category `yaml:"categories"`
}

type GeneralConfig struct {
	OS          []string `yaml:"os"`
	Arch        []string `yaml:"arch"`
	GitHubToken string   `yaml:"github_token"`
}

type Storage struct {
	DefaultRoot string `yaml:"default_root"`
}

type Category struct {
	Path    string   `yaml:"path"`
	Sources []Source `yaml:"sources"`
}

type Source struct {
	ID       string            `yaml:"id,omitempty"`
	Name     string            `yaml:"name,omitempty"`
	Strategy string            `yaml:"strategy,omitempty"`
	Params   map[string]string `yaml:"params,omitempty"`
	OS       string            `yaml:"os,omitempty"`
	Arch     string            `yaml:"arch,omitempty"` // Added to track specific arch of expanded source
	Exclude  []string          `yaml:"exclude,omitempty"`
	// Deprecated: URL is now resolved dynamically, but kept for direct overrides
	URL string `yaml:"url,omitempty"`
}

type Catalog struct {
	Sources []Source `yaml:"sources"`
}

func LoadConfig(configPath string) (*Config, error) {
	// 1. Load User Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Defaults if empty
	if len(cfg.General.OS) == 0 {
		cfg.General.OS = []string{runtime.GOOS}
	}
	if len(cfg.General.Arch) == 0 {
		cfg.General.Arch = []string{runtime.GOARCH}
	}

	// 1.5. Priority: Config > .env > Environment
	loadEnv()
	if cfg.General.GitHubToken == "" {
		cfg.General.GitHubToken = os.Getenv("GITHUB_TOKEN")
	}

	// 2. Load Catalogs
	catalogMap := make(map[string]Source)
	catalogsDir := filepath.Join(filepath.Dir(configPath), "catalogs")
	entries, err := os.ReadDir(catalogsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
				catalogPath := filepath.Join(catalogsDir, entry.Name())
				data, err := os.ReadFile(catalogPath)
				if err != nil {
					continue
				}
				var catalog Catalog
				if err := yaml.Unmarshal(data, &catalog); err == nil {
					for _, s := range catalog.Sources {
						catalogMap[s.ID] = s
					}
				}
			}
		}
	} else {
		// Fallback to legacy catalog.yaml for backward compatibility
		catalogPath := filepath.Join(filepath.Dir(configPath), "catalog.yaml")
		data, err := os.ReadFile(catalogPath)
		if err == nil {
			var catalog Catalog
			if err := yaml.Unmarshal(data, &catalog); err == nil {
				for _, s := range catalog.Sources {
					catalogMap[s.ID] = s
				}
			}
		}
	}

	if len(catalogMap) > 0 {
		// 3. Merge Catalog
		for catName, cat := range cfg.Categories {
			for i, src := range cat.Sources {
				if src.ID != "" {
					if original, ok := catalogMap[src.ID]; ok {
						merged := original
						if src.Name != "" {
							merged.Name = src.Name
						}
						if src.OS != "" {
							merged.OS = src.OS
						}
						if len(src.Exclude) > 0 {
							merged.Exclude = append(merged.Exclude, src.Exclude...)
						}
						cat.Sources[i] = merged
					}
				}
			}
			cfg.Categories[catName] = cat
		}
	}

	// 4. Expand Sources based on General OS/Arch
	expandSources(&cfg)

	// Expand tilde in paths
	cfg.Storage.DefaultRoot = expandTilde(cfg.Storage.DefaultRoot)
	for name, cat := range cfg.Categories {
		cat.Path = expandTilde(cat.Path)
		cfg.Categories[name] = cat
	}

	return &cfg, nil
}

func expandSources(cfg *Config) {
	for catName, cat := range cfg.Categories {
		var expandedSources []Source
		for _, src := range cat.Sources {
			usesOS := false
			usesArch := false
			for _, v := range src.Params {
				if strings.Contains(v, "{{os") || strings.Contains(v, "{{ext") {
					usesOS = true
				}
				if strings.Contains(v, "{{arch") {
					usesArch = true
				}
			}

			// Allow overriding display logic
			if src.Params["force_os_display"] == "true" {
				usesOS = true
			}
			if src.Params["arch_override"] != "" {
				usesArch = true // Force arch usage if override is present
			}

			// Check if exclude list contains OS-specific exclusions (e.g., "linux/amd64")
			// If so, we need to iterate over OS values even if params don't use OS
			needsOSIteration := usesOS
			needsArchIteration := usesArch
			for _, ex := range src.Exclude {
				if strings.Contains(ex, "/") {
					// Exclude contains OS/arch combo, need to iterate both
					needsOSIteration = true
					needsArchIteration = true
				} else if !usesArch && !usesOS {
					// Simple exclusion like "arm64" or "macos"
					// Try to determine if it's an OS or arch
					if ex == "linux" || ex == "macos" || ex == "darwin" || ex == "windows" {
						needsOSIteration = true
					} else {
						needsArchIteration = true
					}
				}
			}

			if !needsOSIteration && !needsArchIteration {
				expandedSources = append(expandedSources, src)
				continue
			}

			// Determine dimensions to iterate
			// If not using OS, treat as single iteration [""]
			// If not using Arch, treat as single iteration [""]

			osList := []string{""}
			if needsOSIteration {
				osList = cfg.General.OS
			}

			archList := []string{""}
			if needsArchIteration {
				archList = cfg.General.Arch
			}

			// Cartesian Product of necessary dimensions
			type expandedKey struct {
				os     string
				params string
			}
			seen := make(map[expandedKey]*Source)
			var keys []expandedKey

			for _, osName := range osList {
				for _, archName := range archList {
					// Check exclusion
					if isExcluded(src.Exclude, osName, archName) {
						continue
					}

					newSrc := src
					newSrc.Params = make(map[string]string)
					for k, v := range src.Params {
						newSrc.Params[k] = v
					}

					substituteParams(&newSrc, osName, archName)

					// De-duplicate based on OS + Params
					paramStr := fmt.Sprintf("%v", newSrc.Params)
					key := expandedKey{os: osName, params: paramStr}

					if existing, ok := seen[key]; ok {
						// If identical, append arch to name if not already there
						// BUT: Don't append if arch_override is set, as it already represents the intended display
						if usesArch && archName != "" && src.Params["arch_override"] == "" {
							if !strings.Contains(existing.Name, archName) {
								// Try to find the closing bracket
								if strings.HasSuffix(existing.Name, "]") {
									existing.Name = existing.Name[:len(existing.Name)-1] + "+" + archName + "]"
								}
							}
						}
						continue
					}

					// Build Name Suffix
					var suffixParts []string
					if usesOS && osName != "" {
						suffixParts = append(suffixParts, osName)
					}
					if usesArch {
						// Use override if present, otherwise use expanded arch name
						if override := src.Params["arch_override"]; override != "" {
							suffixParts = append(suffixParts, override)
						} else if archName != "" {
							suffixParts = append(suffixParts, archName)
						}
					}

					if len(suffixParts) > 0 {
						newSrc.Name = fmt.Sprintf("%s [%s]", src.Name, strings.Join(suffixParts, "/"))
					}

					if usesOS {
						newSrc.OS = osName
					}
					if usesArch {
						if override := src.Params["arch_override"]; override != "" {
							newSrc.Arch = override
						} else {
							newSrc.Arch = archName
						}
					}

					seen[key] = &newSrc
					keys = append(keys, key)
				}
			}

			for _, k := range keys {
				expandedSources = append(expandedSources, *seen[k])
			}
		}
		cat.Sources = expandedSources
		cfg.Categories[catName] = cat
	}
}

func substituteParams(src *Source, osName, archName string) {
	// Mappings
	// OS
	osShort := osName
	if osName == "macos" || osName == "darwin" {
		osShort = "mac"
	}
	ext := "zip" // default
	if osName == "linux" {
		ext = "zip" // balena uses zip for linux
	} else if osName == "macos" || osName == "darwin" {
		ext = "dmg"
	}

	// Arch
	// fedora: amd64->x86_64, arm64->aarch64
	archFedora := archName
	if archName == "amd64" {
		archFedora = "x86_64"
	} else if archName == "arm64" {
		archFedora = "aarch64"
	}

	// electron/github: amd64->x64, arm64->arm64
	archElectron := archName
	if archName == "amd64" {
		archElectron = "x64"
	}

	// VLC: amd64->intel64 (or blank), arm64->arm64
	archVLC := archName
	if archName == "amd64" {
		archVLC = "intel64"
	}

	// mGBA: x64, arm64 (linux), macos/osx (macos)
	archMGBA := "-" + archElectron
	osMGBA := "appimage"
	if osName == "macos" {
		archMGBA = ""
		if archName == "amd64" {
			osMGBA = "osx" // Older Intel builds use osx marker
		} else {
			osMGBA = "macos" // Modern ARM/Universal use macos marker
		}
	}

	// Jellyfin: Intel, AppleSilicon
	archJellyfin := archName
	if osName == "macos" {
		if archName == "amd64" {
			archJellyfin = "Intel"
		} else if archName == "arm64" {
			archJellyfin = "AppleSilicon"
		}
	}

	// BalenaEtcher: New v2.x naming
	// macOS: balenaEtcher-2.1.4-arm64.dmg, balenaEtcher-2.1.4-x64.dmg
	// Linux: balenaEtcher-linux-x64-2.1.4.zip
	osBalena := ""
	archBalena := archElectron
	if osName == "linux" {
		osBalena = "linux-"
	} else if osName == "macos" {
		// For Balena, macOS assets distinguish by arm64 vs x64 directly in the name
		// We'll use archElectron which is already x64/arm64
		archBalena = archElectron
	}
	extBalena := ext

	// OS naming variations
	osProper := "Linux"
	if osName == "macos" {
		osProper = "macOS"
	}

	for k, v := range src.Params {
		v = strings.ReplaceAll(v, "{{os}}", osName)
		v = strings.ReplaceAll(v, "{{os_short}}", osShort)
		v = strings.ReplaceAll(v, "{{os_proper}}", osProper)
		v = strings.ReplaceAll(v, "{{os_mgba}}", osMGBA)
		v = strings.ReplaceAll(v, "{{os_balena}}", osBalena)
		v = strings.ReplaceAll(v, "{{arch}}", archName)
		v = strings.ReplaceAll(v, "{{arch_fedora}}", archFedora)
		v = strings.ReplaceAll(v, "{{arch_electron}}", archElectron)
		v = strings.ReplaceAll(v, "{{arch_vlc}}", archVLC)
		v = strings.ReplaceAll(v, "{{arch_mgba}}", archMGBA)
		v = strings.ReplaceAll(v, "{{arch_balena}}", archBalena)
		v = strings.ReplaceAll(v, "{{arch_jellyfin}}", archJellyfin)
		v = strings.ReplaceAll(v, "{{ext}}", ext)
		v = strings.ReplaceAll(v, "{{ext_balena}}", extBalena)
		src.Params[k] = v
	}
}

func isExcluded(excludeList []string, osName, archName string) bool {
	combo := fmt.Sprintf("%s/%s", osName, archName)
	for _, ex := range excludeList {
		if ex == combo || ex == osName || ex == archName {
			return true
		}
	}
	return false
}

func expandTilde(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// GetTargetPath returns the final download path for a source
func (c *Config) GetTargetPath(categoryName string, src Source) string {
	cat, ok := c.Categories[categoryName]
	if !ok {
		return ""
	}

	basePath := cat.Path
	if basePath == "" {
		basePath = c.Storage.DefaultRoot
	}

	filename := filepath.Base(src.URL)
	if src.URL == "" {
		filename = src.Name
	}

	// Ensure safe filename
	filename = strings.ReplaceAll(filename, "/", "_")

	// Organize by OS if present (now enforced by expansion)
	if src.OS != "" {
		return filepath.Join(basePath, src.OS, filename)
	}

	return filepath.Join(basePath, filename)
}

func loadEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Only set if not already set in environment
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

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
	OS   []string `yaml:"os"`
	Arch []string `yaml:"arch"`
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

	// 2. Load Catalog
	catalogPath := filepath.Join(filepath.Dir(configPath), "catalog.yaml")
	catalogData, err := os.ReadFile(catalogPath)
	if err == nil {
		var catalog Catalog
		if err := yaml.Unmarshal(catalogData, &catalog); err == nil {
			catalogMap := make(map[string]Source)
			for _, s := range catalog.Sources {
				catalogMap[s.ID] = s
			}

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
			// Analyze used variables
			usesOS := false
			usesArch := false
			for _, v := range src.Params {
				if strings.Contains(v, "{{os") {
					usesOS = true
				}
				if strings.Contains(v, "{{arch") {
					usesArch = true
				}
			}

			if !usesOS && !usesArch {
				expandedSources = append(expandedSources, src)
				continue
			}

			// Determine dimensions to iterate
			// If not using OS, treat as single iteration [""]
			// If not using Arch, treat as single iteration [""]

			osList := []string{""}
			if usesOS {
				osList = cfg.General.OS
			}

			archList := []string{""}
			if usesArch {
				archList = cfg.General.Arch
			}

			// Cartesian Product of necessary dimensions
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

					// Build Name Suffix
					var suffixParts []string
					if usesOS {
						suffixParts = append(suffixParts, osName)
					}
					if usesArch {
						suffixParts = append(suffixParts, archName)
					}

					if len(suffixParts) > 0 {
						newSrc.Name = fmt.Sprintf("%s [%s]", src.Name, strings.Join(suffixParts, "/"))
					}

					// Store metadata if used (or just store what we iterated)
					if usesOS {
						newSrc.OS = osName
					}
					// Only set Arch if we actually expanded on it, or if it's relevant
					if usesArch {
						newSrc.Arch = archName
					}

					expandedSources = append(expandedSources, newSrc)
				}
			}
		}
		cat.Sources = expandedSources
		cfg.Categories[catName] = cat
	}
}

func hasTemplateVariables(src Source) bool {
	for _, v := range src.Params {
		if strings.Contains(v, "{{") {
			return true
		}
	}
	return false
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

	for k, v := range src.Params {
		v = strings.ReplaceAll(v, "{{os}}", osName)
		v = strings.ReplaceAll(v, "{{os_short}}", osShort)
		v = strings.ReplaceAll(v, "{{arch}}", archName)
		v = strings.ReplaceAll(v, "{{arch_fedora}}", archFedora)
		v = strings.ReplaceAll(v, "{{arch_electron}}", archElectron)
		v = strings.ReplaceAll(v, "{{ext}}", ext)
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

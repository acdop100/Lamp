package config

import (
	"fmt"
	"io/fs"
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
	OS           []string `yaml:"os"`
	Arch         []string `yaml:"arch"`
	GitHubToken  string   `yaml:"github_token"`
	Threads      int      `yaml:"threads"`        // Number of parallel download segments
	ApiRateLimit float64  `yaml:"api_rate_limit"` // Requests per second
	ApiBurst     int      `yaml:"api_burst"`      // Maximum burst of requests
}

// Category defines a group of download sources
type Category struct {
	Path     string   `yaml:"path"`
	Language string   `yaml:"language,omitempty"` // Default language for dynamic catalogs in this category
	Sources  []Source `yaml:"sources"`
}

type Storage struct {
	DefaultRoot string `yaml:"default_root"`
}

type Source struct {
	ID              string            `yaml:"id,omitempty"`
	Name            string            `yaml:"name,omitempty"`
	Strategy        string            `yaml:"strategy,omitempty"`
	Params          map[string]string `yaml:"params,omitempty"`
	OS              string            `yaml:"os,omitempty"`
	Arch            string            `yaml:"arch,omitempty"` // Added to track specific arch of expanded source
	Exclude         []string          `yaml:"exclude,omitempty"`
	Checksum        string            `yaml:"checksum,omitempty"` // Checksum for integrity verification (e.g. sha256:...)
	URL             string            `yaml:"url,omitempty"`
	StandardizeName bool              `yaml:"standardize_name,omitempty"` // Renames downloaded file to AppName_OS_Arch_Version.ext

	// Configuration Maps
	OSMap   map[string]string `yaml:"os_map,omitempty"`
	ArchMap map[string]string `yaml:"arch_map,omitempty"`
	ExtMap  map[string]string `yaml:"ext_map,omitempty"`
}

type Catalog struct {
	Sources []Source `yaml:"sources"`
}

func GetConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "lamp"), nil
}

func EnsureConfigExists(defaultConfig []byte, catalogFS fs.FS) error {
	configPath, err := GetConfigDir()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write default config.yaml if it doesn't exist
	configFile := filepath.Join(configPath, "config.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := os.WriteFile(configFile, defaultConfig, 0644); err != nil {
			return fmt.Errorf("failed to write default config: %w", err)
		}
	}

	// Write catalogs if they don't exist
	catalogsDir := filepath.Join(configPath, "catalogs")
	if err := os.MkdirAll(catalogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create catalogs directory: %w", err)
	}

	entries, err := fs.ReadDir(catalogFS, "catalogs")
	if err != nil {
		return fmt.Errorf("failed to read embedded catalogs: %w", err)
	}

	for _, entry := range entries {
		// Skip directories, only process files
		if entry.IsDir() {
			continue
		}

		destPath := filepath.Join(catalogsDir, entry.Name())
		// Always overwrite default catalog files to ensure users have latest definitions
		// Users should create new files in the catalogs directory for custom entries
		srcPath := "catalogs/" + entry.Name()
		data, err := fs.ReadFile(catalogFS, srcPath)
		if err != nil {
			fmt.Printf("Warning: failed to read embedded catalog %s: %v\n", entry.Name(), err)
			continue
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			fmt.Printf("Warning: failed to write catalog %s: %v\n", entry.Name(), err)
			continue
		}
	}

	return nil
}

func LoadConfig(configPath string, defaultConfig []byte, catalogFS fs.FS) (*Config, error) {
	// If configPath is empty, try to resolve it from the global directory
	// If configPath is empty, check local then global
	if configPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			configPath = "config.yaml"
		} else {
			dir, err := GetConfigDir()
			if err == nil {
				configPath = filepath.Join(dir, "config.yaml")
			} else {
				// Fallback to local
				configPath = "config.yaml"
			}
		}
	}

	// 1. Load Config
	// Check if file exists, if not and we are in local mode, fallback to default behavior (error)
	// But since we run EnsureConfigExists before this in main, it should exist if we are using global.
	// We allow overriding by passing a specific path (e.g. from flag)

	// If the user manually provided a path (not implementable yet in main but good for future)
	// Or if we resolved it to global config.

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Try local config.yaml as fallback if not absolute path
		if !filepath.IsAbs(configPath) {
			data, err = os.ReadFile("config.yaml")
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
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
	if cfg.General.Threads <= 0 {
		cfg.General.Threads = 4
	}
	if cfg.General.ApiRateLimit <= 0 {
		cfg.General.ApiRateLimit = 1.0
	}
	if cfg.General.ApiBurst <= 0 {
		cfg.General.ApiBurst = 5
	}

	// 1.5. Priority: Config > .env > Environment
	loadEnv()
	if cfg.General.GitHubToken == "" {
		cfg.General.GitHubToken = os.Getenv("GITHUB_TOKEN")
	}

	// 2. Load Catalogs
	catalogMap := make(map[string]Source)

	// Determine where to look for catalogs.
	// If configPath is in global dir, look in global catalogs dir.
	// If configPath is local, look in local catalogs dir.
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
				if err := yaml.Unmarshal(data, &catalog); err != nil {
					return nil, fmt.Errorf("failed to unmarshal catalog %s: %w", entry.Name(), err)
				}
				for _, s := range catalog.Sources {
					catalogMap[s.ID] = s
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
						if src.StandardizeName {
							merged.StandardizeName = true
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

			// Force iteration if maps are present
			if len(src.OSMap) > 0 {
				usesOS = true
			}
			if len(src.ArchMap) > 0 {
				usesArch = true
			}

			// Allow overriding display logic
			if src.Params["force_os_display"] == "true" {
				usesOS = true
			}
			if src.Params["arch_override"] != "" {
				usesArch = true // Force arch usage if override is present
			}

			// Check if exclude list contains OS-specific exclusions (e.g., "linux/amd64")
			needsOSIteration := usesOS
			needsArchIteration := usesArch
			for _, ex := range src.Exclude {
				if strings.Contains(ex, "/") {
					needsOSIteration = true
					needsArchIteration = true
				} else if !usesArch && !usesOS {
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

			osList := []string{""}
			if needsOSIteration {
				osList = cfg.General.OS
			}

			archList := []string{""}
			if needsArchIteration {
				archList = cfg.General.Arch
			}

			// cartesian product
			type expandedKey struct {
				os     string
				arch   string
				params string
			}
			seen := make(map[expandedKey]*Source)
			var keys []expandedKey

			for _, osName := range osList {
				for _, archName := range archList {
					if isExcluded(src.Exclude, osName, archName) {
						continue
					}

					newSrc := src
					newSrc.Params = make(map[string]string)
					for k, v := range src.Params {
						newSrc.Params[k] = v
					}

					if src.OSMap != nil {
						newSrc.OSMap = make(map[string]string)
						for k, v := range src.OSMap {
							newSrc.OSMap[k] = v
						}
					}
					if src.ArchMap != nil {
						newSrc.ArchMap = make(map[string]string)
						for k, v := range src.ArchMap {
							newSrc.ArchMap[k] = v
						}
					}
					if src.ExtMap != nil {
						newSrc.ExtMap = make(map[string]string)
						for k, v := range src.ExtMap {
							newSrc.ExtMap[k] = v
						}
					}

					substituteParams(&newSrc, osName, archName)

					// Determine effective arch for grouping/display
					effectiveArch := archName
					if override := src.Params["arch_override"]; override != "" {
						effectiveArch = override
					}

					paramStr := fmt.Sprintf("%v", newSrc.Params)
					// Key now uses effectiveArch to allow merging Universal binaries (via arch_override)
					// while keeping separate downloads distinct.
					key := expandedKey{os: osName, arch: effectiveArch, params: paramStr}

					if _, ok := seen[key]; ok {
						continue
					}

					// Build Name Suffix
					var suffixParts []string
					if usesOS && osName != "" {
						suffixParts = append(suffixParts, osName)
					}
					if usesArch {
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
	// Standard OS variations
	osShort := osName
	if osName == "macos" || osName == "darwin" {
		osShort = "mac"
	} else if osName == "windows" {
		osShort = "win"
	}

	osProper := "Linux"
	if osName == "macos" {
		osProper = "macOS"
	} else if osName == "windows" {
		osProper = "Windows"
	}

	// 1. Extension Mapping
	ext := "zip" // default
	if osName == "linux" {
		ext = "zip"
	} else if osName == "macos" || osName == "darwin" {
		ext = "dmg"
	} else if osName == "windows" {
		ext = "zip"
	}
	if val, ok := src.ExtMap[osName]; ok {
		ext = val
	}

	// 2. OS Substitution
	mappedOS := ""
	if len(src.OSMap) > 0 {
		if val, ok := src.OSMap[osName]; ok {
			mappedOS = val
		}
	}

	// 3. Arch Substitution
	mappedArch := ""
	if len(src.ArchMap) > 0 {
		compositeKey := fmt.Sprintf("%s/%s", osName, archName)
		if val, ok := src.ArchMap[compositeKey]; ok {
			mappedArch = val
		} else {
			if val, ok := src.ArchMap[archName]; ok {
				mappedArch = val
			}
		}
	}

	for k, v := range src.Params {
		v = strings.ReplaceAll(v, "{{os}}", osName)
		v = strings.ReplaceAll(v, "{{os_short}}", osShort)
		v = strings.ReplaceAll(v, "{{os_proper}}", osProper)
		v = strings.ReplaceAll(v, "{{arch}}", archName)
		v = strings.ReplaceAll(v, "{{ext}}", ext)
		v = strings.ReplaceAll(v, "{{os_map}}", mappedOS)
		v = strings.ReplaceAll(v, "{{arch_map}}", mappedArch)
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

	filename = strings.ReplaceAll(filename, "/", "_")

	if src.OS != "" {
		return filepath.Join(basePath, src.OS, filename)
	}

	return filepath.Join(basePath, filename)
}

func (s Source) GetStandardizedFilename(version, originalExt string) string {
	// Format: AppName_OS_Arch_Version.ext
	name := strings.ReplaceAll(s.Name, " ", "")
	// Clean brackets and extra info
	if idx := strings.Index(name, "["); idx != -1 {
		name = name[:idx]
	}
	name = strings.Trim(name, "_- ")

	osName := s.OS
	if osName == "" {
		osName = runtime.GOOS
	}

	archName := s.Arch
	if archName == "" {
		archName = runtime.GOARCH
	}

	ext := originalExt
	if ext == "" {
		ext = "bin"
	} else {
		ext = strings.TrimPrefix(ext, ".")
	}

	ver := strings.Trim(version, "-_ .")
	if ver == "" {
		ver = "latest"
	}

	return fmt.Sprintf("%s_%s_%s_%s.%s", name, osName, archName, ver, ext)
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
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func CheckSystemCompatibility(cfg *Config) []string {
	var warnings []string
	currentOS := runtime.GOOS
	currentArch := runtime.GOARCH

	osFound := false
	for _, osName := range cfg.General.OS {
		// Handle macOS/darwin alias
		normalizedConfigOS := osName
		if osName == "macos" {
			normalizedConfigOS = "darwin"
		}

		if normalizedConfigOS == currentOS {
			osFound = true
			break
		}
	}

	if !osFound {
		displayOS := currentOS
		if currentOS == "darwin" {
			displayOS = "macos"
		}
		warnings = append(warnings, fmt.Sprintf("Current OS '%s' is not in config.general.os. Add it to receive updates for this machine.", displayOS))
	}

	archFound := false
	for _, archName := range cfg.General.Arch {
		if archName == currentArch {
			archFound = true
			break
		}
	}

	if !archFound {
		warnings = append(warnings, fmt.Sprintf("Current Architecture '%s' is not in config.general.arch. Add it to receive updates for this machine.", currentArch))
	}

	return warnings
}

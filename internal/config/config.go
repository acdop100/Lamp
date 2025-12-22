package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Storage    Storage              `yaml:"storage"`
	Categories map[string]Category `yaml:"categories"`
}

type Storage struct {
	DefaultRoot string `yaml:"default_root"`
}

type Category struct {
	Path    string   `yaml:"path"`
	Sources []Source `yaml:"sources"`
}

type Source struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	CheckType string `yaml:"check_type"`
	OS        string `yaml:"os,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand tilde in paths
	cfg.Storage.DefaultRoot = expandTilde(cfg.Storage.DefaultRoot)
	for name, cat := range cfg.Categories {
		cat.Path = expandTilde(cat.Path)
		cfg.Categories[name] = cat
	}

	return &cfg, nil
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

	if src.OS != "" {
		return filepath.Join(basePath, src.OS, filepath.Base(src.URL))
	}

	return filepath.Join(basePath, filepath.Base(src.URL))
}

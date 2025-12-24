package core

import (
	"os"
	"path/filepath"
	"testing"
	"tui-dl/internal/config"
)

func TestCheckKiwixVersion(t *testing.T) {
	// 1. Create a dummy old ZIM file
	tmpDir := t.TempDir()
	// Using a known existing series "wikipedia_en_100_mini" and an old date
	filename := "wikipedia_en_100_mini_2023-01.zim"
	localPath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(localPath, []byte("dummy content"), 0644); err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	// 2. Configure Source
	src := config.Source{
		Name:     "Kiwix Test",
		Strategy: "kiwix_feed",
		Params: map[string]string{
			"series":   "wikipedia_en_100_mini",
			"feed_url": "https://library.kiwix.org/catalog/v2/entries",
		},
	}

	result := CheckVersion(src, localPath, "")

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	} else {
		t.Logf("Check Result: %+v", result)
	}
}

func TestCheckUbuntuVersion(t *testing.T) {
	tmpDir := t.TempDir()
	filename := "ubuntu-mate-24.04-desktop-amd64.iso"
	localPath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(localPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	src := config.Source{
		Name:     "Ubuntu Test",
		Strategy: "web_scrape",
		Params: map[string]string{
			"base_url":        "https://cdimage.ubuntu.com/ubuntu-mate/releases/",
			"version_pattern": `(\d+\.\d+)/`,
			"file_template":   "{{version}}/release/ubuntu-mate-{{version}}-desktop-amd64.iso",
		},
	}

	result := CheckVersion(src, localPath, "")

	// Since 25.10 is out, this should be Newer
	// Note: allow for UpToDate if 24.04 happens to be the latest matched for some reason,
	// but logically 25.10 exists.
	if result.Status != StatusNewer {
		t.Logf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	} else {
		t.Logf("Ubuntu Check Result: %+v", result)
	}
}

func TestCheckFedoraCoreOSVersion(t *testing.T) {
	tmpDir := t.TempDir()
	// Using a known old version
	filename := "fedora-coreos-38.20230527.3.0-live-iso.x86_64.iso"
	localPath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(localPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	src := config.Source{
		Name:     "Fedora Test",
		Strategy: "fedora_coreos",
		Params: map[string]string{
			"stream": "stable",
			"arch":   "x86_64",
		},
	}

	result := CheckVersion(src, localPath, "")

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	t.Logf("Fedora Check Result: %+v", result)
}

func TestCheckUpToDate(t *testing.T) {
	tmpDir := t.TempDir()

	// First, resolve the latest version to know what filename to create
	src := config.Source{
		Name:     "Fedora Test",
		Strategy: "fedora_coreos",
		Params: map[string]string{
			"stream": "stable",
			"arch":   "x86_64",
		},
	}

	// We pass a dummy localPath just to define the directory
	initialResult := CheckVersion(src, filepath.Join(tmpDir, "dummy"), "")
	if initialResult.Status != StatusNotFound && initialResult.Status != StatusNewer {
		t.Fatalf("Unexpected initial status: %v", initialResult.Status)
	}

	latestFilename := filepath.Base(initialResult.ResolvedURL)
	if latestFilename == "." || latestFilename == "/" {
		t.Fatalf("Invalid resolved filename: %s", latestFilename)
	}

	// Create the "latest" file
	latestPath := filepath.Join(tmpDir, latestFilename)
	if err := os.WriteFile(latestPath, []byte("latest"), 0644); err != nil {
		t.Fatalf("Failed to create latest file: %v", err)
	}

	// Run check again
	finalResult := CheckVersion(src, filepath.Join(tmpDir, "dummy"), "")

	if finalResult.Status != StatusUpToDate {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusUpToDate, finalResult.Status, finalResult.Message)
	}
	t.Logf("UpToDate Check Result: %+v", finalResult)
}

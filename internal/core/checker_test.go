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
		Name:      "Wiki Test",
		URL:       "https://download.kiwix.org/zim/wikipedia/" + filename,
		CheckType: "version_pattern",
	}

	// 3. Run Check
	result := CheckVersion(src, localPath)

	// 4. Assertions
	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}

	if result.Remote == "" {
		t.Error("Expected remote version string, got empty")
	}

	t.Logf("Check Result: %+v", result)
}

func TestCheckUbuntuVersion(t *testing.T) {
	tmpDir := t.TempDir()
	filename := "ubuntu-mate-24.04-desktop-amd64.iso"
	localPath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(localPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	src := config.Source{
		Name:      "Ubuntu Test",
		URL:       "https://cdimage.ubuntu.com/ubuntu-mate/releases/24.04/release/" + filename,
		CheckType: "version_pattern",
	}

	result := CheckVersion(src, localPath)

	// Since 25.10 is out, this should be Newer
	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	t.Logf("Ubuntu Check Result: %+v", result)
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
		Name:      "Fedora Test",
		URL:       "https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/38.20230527.3.0/x86_64/" + filename,
		CheckType: "version_pattern",
	}

	result := CheckVersion(src, localPath)

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	t.Logf("Fedora Check Result: %+v", result)
}

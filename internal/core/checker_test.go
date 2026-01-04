package core

import (
	"bytes"
	"io"
	"lamp/internal/config"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MockHTTPClient allows mocking HTTP responses
type MockHTTPClient struct {
	GetFunc  func(url string) (*http.Response, error)
	HeadFunc func(url string) (*http.Response, error)
	DoFunc   func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Get(url string) (*http.Response, error) {
	if m.GetFunc != nil {
		return m.GetFunc(url)
	}
	return nil, nil // Return what you reasonably expect or error
}

func (m *MockHTTPClient) Head(url string) (*http.Response, error) {
	if m.HeadFunc != nil {
		return m.HeadFunc(url)
	}
	return nil, nil
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}

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

	// 3. Mock Response
	mockXML := `
<feed xmlns="http://www.w3.org/2005/Atom">
    <entry>
        <name>wikipedia_en_100_mini_2023-10</name>
        <issued>2023-10-15T00:00:00Z</issued>
    </entry>
</feed>`

	client := &MockHTTPClient{
		GetFunc: func(url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(mockXML)),
			}, nil
		},
	}

	checker := NewChecker(client, "")
	result := checker.CheckVersion(src, localPath)

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	if result.Latest != "2023-10" {
		t.Errorf("Expected latest %s, got %s", "2023-10", result.Latest)
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

	// Mock logic:
	// 1. GET base_url -> return HTML list
	// 2. HEAD file url -> return OK

	client := &MockHTTPClient{
		GetFunc: func(url string) (*http.Response, error) {
			html := `
<html>
<a href="24.04/">24.04/</a>
<a href="25.10/">25.10/</a>
</html>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(html)),
			}, nil
		},
		HeadFunc: func(url string) (*http.Response, error) {
			// Expect request for 25.10 first (reverse sort)
			if strings.Contains(url, "25.10") {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(nil)}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(nil)}, nil
		},
	}

	checker := NewChecker(client, "")
	result := checker.CheckVersion(src, localPath)

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	if result.Latest != "25.10" {
		t.Errorf("Expected latest 25.10, got %s", result.Latest)
	}
}

func TestCheckFedoraCoreOSVersion(t *testing.T) {
	tmpDir := t.TempDir()
	// Old version
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

	mockJSON := `{
		"architectures": {
			"x86_64": {
				"artifacts": {
					"metal": {
						"release": "39.20231001.3.0",
						"formats": {
							"iso": {
								"disk": { "location": "https://example.com/fedora-coreos-39.iso" }
							}
						}
					}
				}
			}
		}
	}`

	client := &MockHTTPClient{
		GetFunc: func(url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(mockJSON)),
			}, nil
		},
	}

	checker := NewChecker(client, "")
	result := checker.CheckVersion(src, localPath)

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	expectedLatest := "39.20231001.3.0"
	if result.Latest != expectedLatest {
		t.Errorf("Expected latest %s, got %s", expectedLatest, result.Latest)
	}
}

func TestCheckUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	latestFilename := "fedora-coreos-39.iso"
	latestPath := filepath.Join(tmpDir, latestFilename)

	if err := os.WriteFile(latestPath, []byte("latest"), 0644); err != nil {
		t.Fatalf("Failed to create latest file: %v", err)
	}

	src := config.Source{
		Name:     "Fedora Test",
		Strategy: "fedora_coreos",
		Params: map[string]string{
			"stream": "stable",
			"arch":   "x86_64",
		},
	}

	mockJSON := `{
		"architectures": {
			"x86_64": {
				"artifacts": {
					"metal": {
						"release": "39.0.0",
						"formats": {
							"iso": {
								"disk": { "location": "https://example.com/fedora-coreos-39.iso" }
							}
						}
					}
				}
			}
		}
	}`
	client := &MockHTTPClient{
		GetFunc: func(url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(mockJSON)),
			}, nil
		},
	}

	checker := NewChecker(client, "")
	result := checker.CheckVersion(src, latestPath)

	if result.Status != StatusUpToDate {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusUpToDate, result.Status, result.Message)
	}
}

func TestCheckRSSVersion(t *testing.T) {
	tmpDir := t.TempDir()
	filename := "kiwix-desktop_x86_64_2.4.0.appimage"
	localPath := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(localPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	src := config.Source{
		Name:     "RSS Test",
		Strategy: "rss_feed",
		Params: map[string]string{
			"feed_url":        "https://example.com/feed.xml",
			"item_pattern":    `kiwix-desktop_x86_64_.*\.appimage`,
			"version_pattern": `(\d+\.\d+\.\d+)`,
		},
	}

	mockRSS := `
<rss version="2.0">
<channel>
	<item>
		<title>kiwix-desktop_x86_64_2.4.1.appimage</title>
		<link>https://example.com/kiwix-desktop_x86_64_2.4.1.appimage</link>
	</item>
</channel>
</rss>`

	client := &MockHTTPClient{
		GetFunc: func(url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(mockRSS)),
			}, nil
		},
	}

	checker := NewChecker(client, "")
	result := checker.CheckVersion(src, localPath)

	if result.Status != StatusNewer {
		t.Errorf("Expected status %v, got %v (Message: %s)", StatusNewer, result.Status, result.Message)
	}
	if result.Latest != "2.4.1" {
		t.Errorf("Expected latest 2.4.1, got %s", result.Latest)
	}
}

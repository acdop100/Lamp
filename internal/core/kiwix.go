package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	kiwixBaseURL     = "https://library.kiwix.org/catalog/v2/entries"
	kiwixDefaultLang = "eng"
	kiwixCacheTTL    = 24 * time.Hour
)

// KiwixFeed represents the OPDS Atom feed from Kiwix
type KiwixFeed struct {
	XMLName      xml.Name     `xml:"feed"`
	TotalResults int          `xml:"totalResults"`
	StartIndex   int          `xml:"startIndex"`
	ItemsPerPage int          `xml:"itemsPerPage"`
	Entries      []KiwixEntry `xml:"entry"`
}

// KiwixEntry represents a single ZIM file entry in the Kiwix catalog
type KiwixEntry struct {
	ID           string      `xml:"id"`
	Title        string      `xml:"title"`
	Updated      string      `xml:"updated"`
	Summary      string      `xml:"summary"`
	Language     string      `xml:"language"`
	Name         string      `xml:"name"`
	Flavour      string      `xml:"flavour"`
	Category     string      `xml:"category"`
	Tags         string      `xml:"tags"`
	ArticleCount int         `xml:"articleCount"`
	MediaCount   int         `xml:"mediaCount"`
	Author       KiwixAuthor `xml:"author"`
	Publisher    KiwixAuthor `xml:"publisher"`
	Issued       string      `xml:"issued"` // dc:issued
	Links        []KiwixLink `xml:"link"`
}

// KiwixAuthor represents author/publisher info
type KiwixAuthor struct {
	Name string `xml:"name"`
}

// KiwixLink represents a link in the entry (download, thumbnail, etc.)
type KiwixLink struct {
	Rel    string `xml:"rel,attr"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Length int64  `xml:"length,attr"`
}

// kiwixCache represents the structure of the local cache file
type kiwixCache struct {
	Timestamp time.Time    `json:"timestamp"`
	Entries   []KiwixEntry `json:"entries"`
	Language  string       `json:"language"`
	Category  string       `json:"category"`
}

// GetDownloadURL extracts the ZIM download URL from an entry's links
func (e KiwixEntry) GetDownloadURL() string {
	for _, link := range e.Links {
		if link.Rel == "http://opds-spec.org/acquisition/open-access" && link.Type == "application/x-zim" {
			// The href is a .meta4 file, we need to convert to direct .zim link
			href := strings.TrimSuffix(link.Href, ".meta4")
			return href
		}
	}
	return ""
}

// GetFileSize returns the file size in bytes
func (e KiwixEntry) GetFileSize() int64 {
	for _, link := range e.Links {
		if link.Rel == "http://opds-spec.org/acquisition/open-access" {
			return link.Length
		}
	}
	return 0
}

// GetIssuedDate parses the issued date
func (e KiwixEntry) GetIssuedDate() time.Time {
	// Try parsing from Issued first (dc:issued format)
	if e.Issued != "" {
		t, err := time.Parse(time.RFC3339, e.Issued)
		if err == nil {
			return t
		}
	}
	// Fallback to Updated
	if e.Updated != "" {
		t, err := time.Parse(time.RFC3339, e.Updated)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

// FetchKiwixEntries fetches entries from the Kiwix library
func FetchKiwixEntries(language string, category string, limit int) ([]KiwixEntry, error) {
	if language == "" {
		language = kiwixDefaultLang
	}
	if limit <= 0 {
		limit = 100
	}

	// Try loading from cache first
	if cachedEntries, ok := loadKiwixCache(language, category, limit); ok {
		return cachedEntries, nil
	}

	// Build URL with query parameters
	params := url.Values{}
	params.Set("count", fmt.Sprintf("%d", limit))
	if language != "" {
		params.Set("lang", language)
	}
	if category != "" {
		params.Set("category", category)
	}

	apiURL := fmt.Sprintf("%s?%s", kiwixBaseURL, params.Encode())

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "lamp/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Kiwix catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiwix API returned status %d", resp.StatusCode)
	}

	var feed KiwixFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode Kiwix feed: %w", err)
	}

	// Save to cache
	saveKiwixCache(feed.Entries, language, category)

	return feed.Entries, nil
}

// SearchKiwixEntries searches for entries matching a query
func SearchKiwixEntries(query string, language string, limit int) ([]KiwixEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	params := url.Values{}
	params.Set("count", fmt.Sprintf("%d", limit))
	params.Set("q", query)
	if language != "" {
		params.Set("lang", language)
	}

	apiURL := fmt.Sprintf("%s?%s", kiwixBaseURL, params.Encode())

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "lamp/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search Kiwix catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiwix API returned status %d", resp.StatusCode)
	}

	var feed KiwixFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode Kiwix feed: %w", err)
	}

	return feed.Entries, nil
}

// GetKiwixCategories returns a list of known Kiwix categories
func GetKiwixCategories() []string {
	return []string{
		"wikipedia",
		"wiktionary",
		"wikiquote",
		"wikisource",
		"wikibooks",
		"wikinews",
		"wikiversity",
		"wikivoyage",
		"gutenberg",
		"stack_exchange",
		"ted",
		"phet",
		"other",
	}
}

func getKiwixCachePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	lampDir := filepath.Join(configDir, "lamp")
	os.MkdirAll(lampDir, 0755)
	return filepath.Join(lampDir, "kiwix_cache.json")
}

func loadKiwixCache(language string, category string, limit int) ([]KiwixEntry, bool) {
	path := getKiwixCachePath()
	if path == "" {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var cache kiwixCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}

	// Check if cache is expired
	if time.Since(cache.Timestamp) > kiwixCacheTTL {
		return nil, false
	}

	// Check if cache matches the requested filters
	if cache.Language != language || cache.Category != category {
		return nil, false
	}

	// Check if we have enough entries
	if len(cache.Entries) < limit {
		return nil, false
	}

	return cache.Entries[:limit], true
}

func saveKiwixCache(entries []KiwixEntry, language string, category string) {
	path := getKiwixCachePath()
	if path == "" {
		return
	}

	cache := kiwixCache{
		Timestamp: time.Now(),
		Entries:   entries,
		Language:  language,
		Category:  category,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(path, data, 0644)
}

// GetExpectedKiwixPath generates the local file path for a Kiwix ZIM file
func GetExpectedKiwixPath(entry KiwixEntry, basePath string) string {
	if basePath == "" {
		basePath = "Kiwix"
	}

	// Use category as a subdirectory if present
	if entry.Category != "" {
		basePath = filepath.Join(basePath, entry.Category)
	}

	// Use the name field plus date for filename
	issued := entry.GetIssuedDate()
	dateStr := issued.Format("2006-01")

	var filename string
	if entry.Flavour != "" {
		filename = fmt.Sprintf("%s_%s_%s.zim", entry.Name, entry.Flavour, dateStr)
	} else {
		filename = fmt.Sprintf("%s_%s.zim", entry.Name, dateStr)
	}

	return filepath.Join(basePath, filename)
}

// CheckKiwixDownloaded returns true if the ZIM file is already downloaded
func CheckKiwixDownloaded(entry KiwixEntry, basePath string) bool {
	expectedPath := GetExpectedKiwixPath(entry, basePath)
	_, err := os.Stat(expectedPath)
	return err == nil
}

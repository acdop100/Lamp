package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lamp/internal/config"
)

const (
	gutendexBaseURL = "https://gutendex.com/books"
	defaultLimit    = 100
	cacheTTL        = 24 * time.Hour
)

var (
	// Global rate limiter for Gutenberg API: 5 requests burst, refill 1 per second
	gutenbergRateLimiter = NewRateLimiter(5, time.Second)
)

// gutenbergCache represents the structure of the local cache file
type gutenbergCache struct {
	Timestamp time.Time       `json:"timestamp"`
	Books     []GutenbergBook `json:"books"`
}

// GutenbergAuthor represents an author in the Gutendex API response
type GutenbergAuthor struct {
	Name      string `json:"name"`
	BirthYear *int   `json:"birth_year"`
	DeathYear *int   `json:"death_year"`
}

// GutenbergBook represents a book in the Gutendex API response
type GutenbergBook struct {
	ID            int               `json:"id"`
	Title         string            `json:"title"`
	Authors       []GutenbergAuthor `json:"authors"`
	Subjects      []string          `json:"subjects"`
	Bookshelves   []string          `json:"bookshelves"`
	Languages     []string          `json:"languages"`
	Copyright     *bool             `json:"copyright"`
	MediaType     string            `json:"media_type"`
	Formats       map[string]string `json:"formats"`
	DownloadCount int               `json:"download_count"`
}

// GutenbergResponse represents the paginated response from Gutendex API
type GutenbergResponse struct {
	Count    int             `json:"count"`
	Next     *string         `json:"next"`
	Previous *string         `json:"previous"`
	Results  []GutenbergBook `json:"results"`
}

// FetchTopBooks fetches the most popular books from Gutendex (sorted by download count)
func FetchTopBooks(language string, limit int) ([]GutenbergBook, error) {
	if limit <= 0 {
		limit = defaultLimit
	}

	// Try loading from cache first
	if cachedBooks, ok := loadCache(limit); ok {
		return cachedBooks, nil
	}

	// Gutendex returns 32 books per page by default, need multiple requests for 100
	var allBooks []GutenbergBook
	nextURL := fmt.Sprintf("%s?languages=%s&sort=popular", gutendexBaseURL, language)

	client := &http.Client{Timeout: 30 * time.Second}

	for len(allBooks) < limit && nextURL != "" {
		// Rate limit API calls
		gutenbergRateLimiter.Wait()

		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("User-Agent", "lamp/1.0")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch books: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gutendex API returned status %d", resp.StatusCode)
		}

		var gutResp GutenbergResponse
		if err := json.NewDecoder(resp.Body).Decode(&gutResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allBooks = append(allBooks, gutResp.Results...)

		if gutResp.Next != nil {
			nextURL = *gutResp.Next
		} else {
			nextURL = ""
		}
	}

	// Trim to requested limit
	if len(allBooks) > limit {
		allBooks = allBooks[:limit]
	}

	// Save to cache
	saveCache(allBooks)

	return allBooks, nil
}

func getCachePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	lampDir := filepath.Join(configDir, "lamp")
	// Ensure directory exists
	os.MkdirAll(lampDir, 0755)
	return filepath.Join(lampDir, "gutenberg_cache.json")
}

func loadCache(limit int) ([]GutenbergBook, bool) {
	path := getCachePath()
	if path == "" {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var cache gutenbergCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}

	// Check if cache is expired
	if time.Since(cache.Timestamp) > cacheTTL {
		return nil, false
	}

	// Check if we have enough books in cache
	if len(cache.Books) < limit {
		return nil, false
	}

	return cache.Books[:limit], true
}

func saveCache(books []GutenbergBook) {
	path := getCachePath()
	if path == "" {
		return
	}

	cache := gutenbergCache{
		Timestamp: time.Now(),
		Books:     books,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(path, data, 0600)
}

// SearchBooks searches for books by title or author
func SearchBooks(query string, language string) ([]GutenbergBook, error) {
	// Rate limit API calls
	gutenbergRateLimiter.Wait()

	encodedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("%s?search=%s&languages=%s", gutendexBaseURL, encodedQuery, language)

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "lamp/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search books: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gutendex API returned status %d", resp.StatusCode)
	}

	var gutResp GutenbergResponse
	if err := json.NewDecoder(resp.Body).Decode(&gutResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return gutResp.Results, nil
}

// GetEPUB3URL extracts the EPUB3 download URL from a book's formats
func GetEPUB3URL(book GutenbergBook) string {
	// Try EPUB with images first (preferred)
	if url, ok := book.Formats["application/epub+zip"]; ok {
		return url
	}
	return ""
}

// GetPrimaryAuthor returns the primary author name or "Unknown"
func GetPrimaryAuthor(book GutenbergBook) string {
	if len(book.Authors) > 0 {
		return book.Authors[0].Name
	}
	return "Unknown"
}

// slugify converts a string to a filesystem-safe slug
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces and special chars with underscores
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	s = reg.ReplaceAllString(s, "_")
	// Trim leading/trailing underscores
	s = strings.Trim(s, "_")
	// Limit length
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// GetExpectedPath generates the local file path for a book based on organization setting
func GetExpectedPath(book GutenbergBook, basePath string, organization string) string {
	if basePath == "" {
		// This should ideally be passed in, but fallback to a default if absolutely necessary
		// However, with the new design, the TUI should always have a path from the category
		basePath = "Gutenberg"
	}

	titleSlug := slugify(book.Title)
	if titleSlug == "" {
		titleSlug = fmt.Sprintf("book_%d", book.ID)
	}

	switch organization {
	case "by_author":
		authorSlug := slugify(GetPrimaryAuthor(book))
		if authorSlug == "" || authorSlug == "unknown" {
			authorSlug = "unknown_author"
		}
		return filepath.Join(basePath, authorSlug, titleSlug+".epub")
	case "by_id":
		return filepath.Join(basePath, fmt.Sprintf("%d.epub", book.ID))
	case "flat":
		return filepath.Join(basePath, titleSlug+".epub")
	default:
		// Default to by_author
		authorSlug := slugify(GetPrimaryAuthor(book))
		if authorSlug == "" || authorSlug == "unknown" {
			authorSlug = "unknown_author"
		}
		return filepath.Join(basePath, authorSlug, titleSlug+".epub")
	}
}

// CheckDownloaded returns true if the book is already downloaded at expected path
func CheckDownloaded(book GutenbergBook, basePath string, organization string) bool {
	expectedPath := GetExpectedPath(book, basePath, organization)
	_, err := os.Stat(expectedPath)
	return err == nil
}

// BookToSource converts a GutenbergBook to a config.Source for download compatibility
func BookToSource(book GutenbergBook, cfg *config.Config) config.Source {
	return config.Source{
		ID:       fmt.Sprintf("gutenberg-%d", book.ID),
		Name:     book.Title,
		Strategy: "gutenberg",
		URL:      GetEPUB3URL(book),
	}
}

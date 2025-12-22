package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"tui-dl/internal/config"
)

type VersionStatus string

const (
	StatusUpToDate VersionStatus = "Up to Date"
	StatusNewer    VersionStatus = "Newer Version Available"
	StatusNotFound VersionStatus = "Local File Not Found"
	StatusError    VersionStatus = "Error Checking"
)

type CheckResult struct {
	Status  VersionStatus
	Remote  string // ETag, Last-Modified, or Version String
	Message string
}

// Fedora CoreOS Metadata
type FedoraCoreOSStreams struct {
	Architectures map[string]FedoraArch `json:"architectures"`
}

type FedoraArch struct {
	Artifacts map[string]FedoraArtifact `json:"artifacts"`
}

type FedoraArtifact struct {
	Release string `json:"release"`
}

// Kiwix XML Feed Structures
type Feed struct {
	XMLName xml.Name `xml:"feed"`
	Entries []Entry  `xml:"entry"`
}

type Entry struct {
	Name    string `xml:"name"`
	Flavour string `xml:"flavour"`
	Issued  string `xml:"issued"` // Format: 2025-10-16T00:00:00Z
}

func CheckVersion(src config.Source, localPath string) CheckResult {
	info, err := os.Stat(localPath)
	if os.IsNotExist(err) {
		return CheckResult{Status: StatusNotFound}
	} else if err != nil {
		return CheckResult{Status: StatusError, Message: err.Error()}
	}

	switch src.CheckType {
	case "http_header":
		return checkHTTPHeader(src.URL, info)
	case "version_pattern":
		if strings.HasSuffix(localPath, ".iso") {
			return checkLinuxISOVersion(src.URL, localPath)
		}
		if strings.Contains(src.URL, "kiwix.org") {
			return checkKiwixVersion(src.URL, localPath)
		}
		return checkHTTPHeader(src.URL, info)
	default:
		return CheckResult{Status: StatusError, Message: "Unsupported check type: " + src.CheckType}
	}
}

func checkLinuxISOVersion(remoteURL, localPath string) CheckResult {
	filename := filepath.Base(localPath)

	// Fedora CoreOS logic
	if strings.Contains(filename, "fedora-coreos") {
		return checkFedoraCoreOSVersion(remoteURL, localPath)
	}

	// Ubuntu/General logic (directory scraping)
	if strings.Contains(remoteURL, "ubuntu.com") {
		return checkUbuntuVersion(remoteURL, localPath)
	}

	return CheckResult{Status: StatusError, Message: "Unsupported ISO source for version checking"}
}

func checkFedoraCoreOSVersion(remoteURL, localPath string) CheckResult {
	filename := filepath.Base(localPath)
	// Example: fedora-coreos-43.20251120.3.0-live-iso.x86_64.iso
	re := regexp.MustCompile(`fedora-coreos-(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return CheckResult{Status: StatusError, Message: "Could not parse Fedora CoreOS version from filename"}
	}
	localVersion := matches[1]

	// Determine architecture
	arch := "x86_64"
	if strings.Contains(filename, "aarch64") {
		arch = "aarch64"
	}

	// Fetch streams metadata (defaulting to stable)
	resp, err := http.Get("https://builds.coreos.fedoraproject.org/streams/stable.json")
	if err != nil {
		return CheckResult{Status: StatusError, Message: "Failed to fetch Fedora metadata: " + err.Error()}
	}
	defer resp.Body.Close()

	var streams FedoraCoreOSStreams
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		return CheckResult{Status: StatusError, Message: "Failed to parse Fedora metadata: " + err.Error()}
	}

	archData, ok := streams.Architectures[arch]
	if !ok {
		return CheckResult{Status: StatusError, Message: "Architecture not found in metadata: " + arch}
	}

	// Usually 'metal' artifact contains the ISO
	metal, ok := archData.Artifacts["metal"]
	if !ok {
		return CheckResult{Status: StatusError, Message: "Metal artifact not found in metadata"}
	}

	if metal.Release != localVersion {
		// Compare version strings (FCOS versions are lexicographically comparable?)
		// e.g. 43.20251120.3.0 vs 43.20251217.3.0
		if metal.Release > localVersion {
			return CheckResult{
				Status:  StatusNewer,
				Remote:  metal.Release,
				Message: fmt.Sprintf("Remote: %s (Local: %s)", metal.Release, localVersion),
			}
		}
	}

	return CheckResult{Status: StatusUpToDate, Remote: metal.Release}
}

func checkUbuntuVersion(remoteURL, localPath string) CheckResult {
	// Example URL: https://cdimage.ubuntu.com/ubuntu-mate/releases/25.10/release/ubuntu-mate-25.10-desktop-amd64.iso
	filename := filepath.Base(localPath)
	re := regexp.MustCompile(`(\d+\.\d+)`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return CheckResult{Status: StatusError, Message: "Could not parse Ubuntu version from filename"}
	}
	localVersion := matches[1]

	// Navigate up to find the releases base URL
	// From .../releases/25.10/release/... to .../releases/
	parts := strings.Split(remoteURL, "/")
	var baseReleaseURL string
	for i, part := range parts {
		if part == "releases" {
			baseReleaseURL = strings.Join(parts[:i+1], "/") + "/"
			break
		}
	}

	if baseReleaseURL == "" {
		return CheckResult{Status: StatusError, Message: "Could not determine Ubuntu releases base URL"}
	}

	resp, err := http.Get(baseReleaseURL)
	if err != nil {
		return CheckResult{Status: StatusError, Message: "Failed to fetch directory listing: " + err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// Find directories that look like versions: <a href="25.10/">
	reDir := regexp.MustCompile(`href="(\d+\.\d+)/"`)
	dirMatches := reDir.FindAllStringSubmatch(string(body), -1)

	var versions []string
	for _, m := range dirMatches {
		versions = append(versions, m[1])
	}
	sort.Strings(versions)

	if len(versions) > 0 {
		latest := versions[len(versions)-1]
		if latest > localVersion {
			return CheckResult{
				Status:  StatusNewer,
				Remote:  latest,
				Message: fmt.Sprintf("Remote: %s (Local: %s)", latest, localVersion),
			}
		}
	}

	return CheckResult{Status: StatusUpToDate}
}

func checkKiwixVersion(remoteURL, localPath string) CheckResult {
	// 1. Parse local filename to extract series and date
	filename := filepath.Base(localPath)
	// Expected format: series_lang_flavour_YYYY-MM.zim
	// Example: phet_en_all_2025-03.zim

	re := regexp.MustCompile(`(.+)_(\d{4}-\d{2})\.zim$`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) != 3 {
		return CheckResult{Status: StatusError, Message: "Filename format not recognized for versioning"}
	}

	seriesName := matches[1] // e.g., phet_en_all
	localDateStr := matches[2]

	localDate, err := time.Parse("2006-01", localDateStr)
	if err != nil {
		return CheckResult{Status: StatusError, Message: "Invalid local date format: " + err.Error()}
	}

	// 2. Query Kiwix API
	// Use 'q' parameter which seems more robust for OPDS search
	// We implement a fallback strategy: if strict search fails, strip suffix segments (after last _)
	// until we get results or run out of meaningful tokens.
	var feed Feed

	client := &http.Client{Timeout: 10 * time.Second}
	searchName := seriesName
	for {
		query := url.QueryEscape(searchName)
		apiURL := fmt.Sprintf("https://library.kiwix.org/catalog/v2/entries?lang=eng&q=%s", query)

		resp, err := client.Get(apiURL)
		if err != nil {
			return CheckResult{Status: StatusError, Message: "API request failed: " + err.Error()}
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return CheckResult{Status: StatusError, Message: fmt.Sprintf("API returned status: %d", resp.StatusCode)}
		}

		if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
			resp.Body.Close()
			return CheckResult{Status: StatusError, Message: "Failed to parse API response: " + err.Error()}
		}
		resp.Body.Close()

		if len(feed.Entries) > 0 {
			break
		}

		// Strip last segment
		lastUnderscore := strings.LastIndex(searchName, "_")
		if lastUnderscore == -1 {
			// No more underscores, and full search yielded nothing.
			return CheckResult{Status: StatusUpToDate, Message: "No matching remote entry found (checked " + seriesName + ")"}
		}
		searchName = searchName[:lastUnderscore]
	}

	// 3. Find matching entry
	var latestEntry *Entry
	var latestDate time.Time

	for _, entry := range feed.Entries {
		entrySeries := entry.Name
		if entry.Flavour != "" {
			entrySeries += "_" + entry.Flavour
		}

		if entrySeries == seriesName {
			// Parse Issued date
			issuedDate, err := time.Parse(time.RFC3339, entry.Issued)
			if err != nil {
				// Try parsing simplified date if RFC3339 fails (though Kiwix seems consistent)
				issuedDate, err = time.Parse("2006-01-02", entry.Issued)
				if err != nil {
					continue
				}
			}

			// Keep the latest one
			if latestEntry == nil || issuedDate.After(latestDate) {
				latestDate = issuedDate
				entryPtr := entry
				latestEntry = &entryPtr
			}
		}
	}

	if latestEntry == nil {
		return CheckResult{Status: StatusUpToDate, Message: "No matching remote entry found"}
	}

	// 4. Compare Dates
	// Local date is YYYY-MM (resolution: month)
	// Remote is full date.
	// If remote year/month is after local year/month => Newer.
	// If same year/month => Up to date (we assume monthly releases mostly, or we can't distinguish with local filename).

	localYear, localMonth, _ := localDate.Date()
	remoteYear, remoteMonth, _ := latestDate.Date()

	if remoteYear > localYear || (remoteYear == localYear && remoteMonth > localMonth) {
		return CheckResult{
			Status:  StatusNewer,
			Remote:  latestDate.Format("2006-01-02"),
			Message: fmt.Sprintf("Remote: %s (Local: %s)", latestDate.Format("2006-01-02"), localDateStr),
		}
	}

	return CheckResult{
		Status:  StatusUpToDate,
		Remote:  latestDate.Format("2006-01-02"),
		Message: "Local version matches latest remote month",
	}
}

func checkHTTPHeader(url string, localInfo os.FileInfo) CheckResult {
	// ... (rest of checkHTTPHeader as before)
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Head(url)
	if err != nil {
		return CheckResult{Status: StatusError, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CheckResult{Status: StatusError, Message: fmt.Sprintf("HTTP Status: %d", resp.StatusCode)}
	}

	// Check Last-Modified
	remoteLastModStr := resp.Header.Get("Last-Modified")
	if remoteLastModStr != "" {
		remoteLastMod, err := http.ParseTime(remoteLastModStr)
		if err == nil {
			if remoteLastMod.After(localInfo.ModTime()) {
				return CheckResult{
					Status:  StatusNewer,
					Remote:  remoteLastModStr,
					Message: fmt.Sprintf("Remote: %s (Local: %s)", remoteLastModStr, localInfo.ModTime().Format(http.TimeFormat)),
				}
			}
			return CheckResult{
				Status: StatusUpToDate,
				Remote: remoteLastModStr,
			}
		}
	}

	return CheckResult{Status: StatusUpToDate, Message: "No specific version changes detected via headers"}
}

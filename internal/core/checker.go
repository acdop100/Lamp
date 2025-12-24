package core

import (
	"context"
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
	"sync"
	"time"
	"tui-dl/internal/config"

	"github.com/google/go-github/v69/github"
)

var (
	githubCache sync.Map // map[string]*github.RepositoryRelease
	webCache    sync.Map // map[string]string (URL:Body)
)

func parseRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format: %s", repo)
	}
	return parts[0], parts[1], nil
}

type VersionStatus string

const (
	StatusUpToDate VersionStatus = "Up to Date"
	StatusNewer    VersionStatus = "Newer Version Available"
	StatusNotFound VersionStatus = "Local File Not Found"
	StatusError    VersionStatus = "Error Checking"
)

type CheckResult struct {
	Status      VersionStatus
	Current     string // Local version found
	Latest      string // Latest version available
	Message     string
	ResolvedURL string // The dynamic URL found during checking
}

// Fedora CoreOS Metadata
type FedoraCoreOSStreams struct {
	Architectures map[string]FedoraArch `json:"architectures"`
}

type FedoraArch struct {
	Artifacts map[string]FedoraArtifact `json:"artifacts"`
}

type FedoraArtifact struct {
	Release string                 `json:"release"`
	Formats map[string]FedoraImage `json:"formats"`
}

type FedoraImage struct {
	Disk struct {
		Location string `json:"location"`
	} `json:"disk"`
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

func CheckVersion(src config.Source, localPath string, githubToken string) CheckResult {
	info, err := os.Stat(localPath)
	if os.IsNotExist(err) && src.Strategy == "" {
		// Only return NotFound if we have no strategy to verify against (legacy/direct file)
		return CheckResult{Status: StatusNotFound}
	} else if err != nil && !os.IsNotExist(err) {
		return CheckResult{Status: StatusError, Message: err.Error()}
	}

	// Dynamic Resolution Strategies
	switch src.Strategy {
	case "web_scrape":
		return resolveWebScrape(src, localPath)
	case "fedora_coreos":
		return resolveFedoraCoreOS(src, localPath)
	case "kiwix_feed":
		return resolveKiwixFeed(src, localPath)
	case "github_release":
		return resolveGithubRelease(src, localPath, githubToken)
	default:
		// Fallback for direct URLs (legacy behavior)
		if src.URL != "" {
			return checkHTTPHeader(src.URL, info)
		}
		return CheckResult{Status: StatusError, Message: "No strategy or URL provided"}
	}
}

func resolveGithubRelease(src config.Source, localPath string, githubToken string) CheckResult {
	repo := src.Params["repo"]
	assetPattern := src.Params["asset_pattern"]

	if repo == "" || assetPattern == "" {
		return CheckResult{Status: StatusError, Message: "Missing repo or asset_pattern params"}
	}

	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return CheckResult{Status: StatusError, Message: err.Error()}
	}

	var release *github.RepositoryRelease
	if val, ok := githubCache.Load(repo); ok {
		release = val.(*github.RepositoryRelease)
	} else {
		client := github.NewClient(nil)
		if githubToken != "" {
			client = client.WithAuthToken(githubToken)
		} else if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			client = client.WithAuthToken(token)
		}

		var err error
		release, _, err = client.Repositories.GetLatestRelease(context.Background(), owner, repoName)
		if err != nil {
			return CheckResult{Status: StatusError, Message: "GitHub API error: " + err.Error()}
		}
		githubCache.Store(repo, release)
	}

	tagName := release.GetTagName()

	// Find the matching asset
	re, err := regexp.Compile(assetPattern)
	if err != nil {
		return CheckResult{Status: StatusError, Message: "Invalid asset_pattern regex"}
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if re.MatchString(asset.GetName()) {
			downloadURL = asset.GetBrowserDownloadURL()
			break
		}
	}

	if downloadURL == "" {
		return CheckResult{
			Status:  StatusError,
			Message: fmt.Sprintf("No asset found matching pattern '%s' in release %s", assetPattern, tagName),
			Latest:  tagName,
		}
	}

	targetDir := filepath.Dir(localPath)
	remoteFilename := filepath.Base(downloadURL)
	fullLocalPath := filepath.Join(targetDir, remoteFilename)

	var currentVersion string
	entries, _ := os.ReadDir(targetDir)
	for _, entry := range entries {
		if !entry.IsDir() && re.MatchString(entry.Name()) {
			currentVersion = tagName // Best guess
			break
		}
	}

	if _, err := os.Stat(fullLocalPath); err == nil {
		return CheckResult{Status: StatusUpToDate, Current: tagName, Latest: tagName, ResolvedURL: downloadURL}
	}

	if currentVersion != "" {
		return CheckResult{
			Status:      StatusNewer,
			Current:     currentVersion,
			Latest:      tagName,
			Message:     fmt.Sprintf("New release: %s", tagName),
			ResolvedURL: downloadURL,
		}
	}

	return CheckResult{
		Status:      StatusNotFound,
		Latest:      tagName,
		ResolvedURL: downloadURL,
	}
}

func resolveWebScrape(src config.Source, localPath string) CheckResult {
	baseURL := src.Params["base_url"]
	versionPattern := src.Params["version_pattern"]
	fileTemplate := src.Params["file_template"]

	if baseURL == "" || versionPattern == "" || fileTemplate == "" {
		return CheckResult{Status: StatusError, Message: "Missing web_scrape params"}
	}

	// Scrape the directory
	var body []byte
	if val, ok := webCache.Load(baseURL); ok {
		body = val.([]byte)
	} else {
		resp, err := http.Get(baseURL)
		if err != nil {
			return CheckResult{Status: StatusError, Message: "Failed to scrape: " + err.Error()}
		}
		defer resp.Body.Close()

		body, _ = io.ReadAll(resp.Body)
		webCache.Store(baseURL, body)
	}
	reDir := regexp.MustCompile(versionPattern)

	matches := reDir.FindAllStringSubmatch(string(body), -1)
	var versions []string
	for _, m := range matches {
		if len(m) > 1 {
			versions = append(versions, m[1])
		}
	}
	sort.Strings(versions)

	// Step 2: Iterate backwards and verify remote file existence
	var latestVersion string
	var remoteFullURL string
	var remotePath string

	// Regex for file pattern (to extract version from local files too)
	templateFilenamePattern := filepath.Base(fileTemplate)
	regexPattern := strings.ReplaceAll(templateFilenamePattern, "{{version}}", `(\d+\.\d+)`)
	reFile := regexp.MustCompile(regexPattern)

	client := &http.Client{Timeout: 5 * time.Second}

	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		rPath := strings.ReplaceAll(fileTemplate, "{{version}}", v)
		rURL := baseURL + rPath

		resp, err := client.Head(rURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			latestVersion = v
			remoteFullURL = rURL
			remotePath = rPath
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if latestVersion == "" {
		return CheckResult{Status: StatusError, Message: "No valid remote files found for any version"}
	}

	targetDir := filepath.Dir(localPath)
	expectedFilename := filepath.Base(remotePath)
	fullLocalPath := filepath.Join(targetDir, expectedFilename)

	// Local version detection
	var currentVersion string
	entries, _ := os.ReadDir(targetDir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if reFile.MatchString(entry.Name()) {
			m := reFile.FindStringSubmatch(entry.Name())
			if len(m) > 1 {
				currentVersion = m[1]
			}
			break
		}
	}

	if _, err := os.Stat(fullLocalPath); err == nil {
		return CheckResult{Status: StatusUpToDate, Current: latestVersion, Latest: latestVersion, ResolvedURL: remoteFullURL}
	}

	if currentVersion != "" {
		return CheckResult{
			Status:      StatusNewer,
			Current:     currentVersion,
			Latest:      latestVersion,
			ResolvedURL: remoteFullURL,
		}
	}

	return CheckResult{
		Status:      StatusNotFound,
		Latest:      latestVersion,
		ResolvedURL: remoteFullURL,
	}
}

func resolveFedoraCoreOS(src config.Source, localPath string) CheckResult {
	stream := src.Params["stream"]
	arch := src.Params["arch"]

	if stream == "" {
		stream = "stable"
	}
	if arch == "" {
		arch = "x86_64"
	}

	metaURL := fmt.Sprintf("https://builds.coreos.fedoraproject.org/streams/%s.json", stream)

	resp, err := http.Get(metaURL)
	if err != nil {
		return CheckResult{Status: StatusError, Message: "Failed to fetch Fedora metadata: " + err.Error()}
	}
	defer resp.Body.Close()

	var streams FedoraCoreOSStreams
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		return CheckResult{Status: StatusError, Message: "Failed to parse Fedora metadata"}
	}

	archData, ok := streams.Architectures[arch]
	if !ok {
		return CheckResult{Status: StatusError, Message: "Arch not found: " + arch}
	}

	// Usually 'metal' artifact contains the ISO
	// For metal, we might look for 'iso' or 'raw.xz' format?
	// Commonly 'iso' format exists for metal.
	metal, ok := archData.Artifacts["metal"]
	if !ok {
		// Fallback to "live" if "metal" missing? But FCOS usually has metal.
		return CheckResult{Status: StatusError, Message: "Metal artifact not found"}
	}

	// Find the ISO location
	var downloadURL string
	if isoImg, ok := metal.Formats["iso"]; ok {
		downloadURL = isoImg.Disk.Location
	} else if liveIso, ok := metal.Formats["live-iso"]; ok {
		// Sometimes it's called live-iso in formats? Need to check specific stream data.
		// The example log showed "vhd.xz" for azure.
		// For metal, it's typically "iso" or "raw.xz".
		downloadURL = liveIso.Disk.Location
	}

	targetDir := filepath.Dir(localPath)
	remoteFilename := filepath.Base(downloadURL)
	fullLocalPath := filepath.Join(targetDir, remoteFilename)

	// Local version detection
	var currentVersion string
	reVer := regexp.MustCompile(`fedora-coreos-(\d+\.\d+\.\d+\.\d+)`)
	entries, _ := os.ReadDir(targetDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "fedora-coreos-") {
			m := reVer.FindStringSubmatch(entry.Name())
			if len(m) > 1 {
				currentVersion = m[1]
			}
			break
		}
	}

	if _, err := os.Stat(fullLocalPath); err == nil {
		return CheckResult{Status: StatusUpToDate, Current: metal.Release, Latest: metal.Release, ResolvedURL: downloadURL}
	}

	if currentVersion != "" {
		return CheckResult{
			Status:      StatusNewer,
			Current:     currentVersion,
			Latest:      metal.Release,
			ResolvedURL: downloadURL,
		}
	}

	return CheckResult{
		Status:      StatusNotFound,
		Latest:      metal.Release,
		ResolvedURL: downloadURL,
	}
}

func resolveKiwixFeed(src config.Source, localPath string) CheckResult {
	series := src.Params["series"]
	feedURL := src.Params["feed_url"]

	if series == "" || feedURL == "" {
		return CheckResult{Status: StatusError, Message: "Missing series or feed_url params"}
	}

	// Use existing loop fallback logic adapted for params
	// Search API with 'q'
	// Recursive search strategy: strip last segment if not found

	client := &http.Client{Timeout: 10 * time.Second}
	var feed Feed

	searchQuery := series
	found := false

	for {
		searchURL := fmt.Sprintf("%s?q=%s", feedURL, url.QueryEscape(searchQuery))
		resp, err := client.Get(searchURL)
		if err != nil {
			return CheckResult{Status: StatusError, Message: err.Error()}
		}

		if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
			resp.Body.Close()
			return CheckResult{Status: StatusError, Message: "Failed to parse Kiwix feed: " + err.Error()}
		}
		resp.Body.Close()

		if len(feed.Entries) > 0 {
			found = true
			break
		}

		// Strip last segment
		lastUnderscore := strings.LastIndex(searchQuery, "_")
		if lastUnderscore == -1 {
			break
		}
		searchQuery = searchQuery[:lastUnderscore]
	}

	if !found {
		return CheckResult{Status: StatusError, Message: fmt.Sprintf("No entry found for series '%s' (or prefixes)", series)}
	}

	// Find the latest entry for the specified series
	var latestEntry *Entry
	var latestDate time.Time

	for i := range feed.Entries {
		entry := &feed.Entries[i]
		// Check if Name contains series (Entry.Name is typically the ID/Series name)
		// Or contains the *original* series name requested
		if strings.Contains(entry.Name, series) || (searchQuery != series && strings.Contains(entry.Name, searchQuery)) {

			// Parse Issued date
			issuedDate, err := time.Parse(time.RFC3339, entry.Issued)
			if err != nil {
				// Try simplified YYYY-MM-DD
				issuedDate, err = time.Parse("2006-01-02", entry.Issued)
				if err != nil {
					continue
				}
			}

			if latestEntry == nil || issuedDate.After(latestDate) {
				latestDate = issuedDate
				latestEntry = entry
			}
		}
	}

	if latestEntry == nil {
		return CheckResult{Status: StatusError, Message: fmt.Sprintf("No entry found for series '%s'", series)}
	}

	remoteDateShort := latestDate.Format("2006-01")

	targetDir := filepath.Dir(localPath)
	// Expected name pattern: series_remoteDateShort.zim
	expectedFilename := fmt.Sprintf("%s_%s.zim", series, remoteDateShort)
	fullLocalPath := filepath.Join(targetDir, expectedFilename)

	// Local version detection
	var currentVersion string
	reDate := regexp.MustCompile(`_(\d{4}-\d{2})\.zim`)
	entries, _ := os.ReadDir(targetDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), series+"_") {
			m := reDate.FindStringSubmatch(entry.Name())
			if len(m) > 1 {
				currentVersion = m[1]
			}
			break
		}
	}

	if _, err := os.Stat(fullLocalPath); err == nil {
		return CheckResult{Status: StatusUpToDate, Current: remoteDateShort, Latest: remoteDateShort}
	}

	if currentVersion != "" {
		return CheckResult{
			Status:  StatusNewer,
			Current: currentVersion,
			Latest:  remoteDateShort,
		}
	}

	return CheckResult{
		Status: StatusNotFound,
		Latest: remoteDateShort,
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
					Latest:  remoteLastModStr,
					Message: fmt.Sprintf("Remote: %s (Local: %s)", remoteLastModStr, localInfo.ModTime().Format(http.TimeFormat)),
				}
			}
			return CheckResult{
				Status: StatusUpToDate,
				Latest: remoteLastModStr,
			}
		}
	}

	return CheckResult{Status: StatusUpToDate, Message: "No specific version changes detected via headers"}
}

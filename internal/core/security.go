package core

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ValidateRegexPattern checks if a regex pattern is safe to compile and use
// Returns an error if the pattern appears to be potentially dangerous (ReDoS)
func ValidateRegexPattern(pattern string) error {
	if pattern == "" {
		return nil
	}

	// Check pattern length - excessively long patterns can be problematic
	if len(pattern) > 500 {
		return fmt.Errorf("regex pattern too long (%d chars, max 500)", len(pattern))
	}

	// Check for common ReDoS patterns
	// These are simplified checks - a full ReDoS detector would be more complex
	dangerousPatterns := []string{
		// Nested quantifiers like (a+)+ or (a*)*
		`\([^)]*[+*]\)[+*]`,
		// Excessive alternation with overlapping patterns
		`(\|.*){10,}`,
		// Deeply nested groups
		`\(([^()]*\(.*\)){5,}`,
	}

	for _, dangerous := range dangerousPatterns {
		matched, _ := regexp.MatchString(dangerous, pattern)
		if matched {
			return fmt.Errorf("potentially unsafe regex pattern detected (possible ReDoS)")
		}
	}

	// Try to compile the pattern to ensure it's valid
	_, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	return nil
}

// SafeCompileRegex compiles a regex pattern after validating it
func SafeCompileRegex(pattern string) (*regexp.Regexp, error) {
	if err := ValidateRegexPattern(pattern); err != nil {
		return nil, err
	}
	return regexp.Compile(pattern)
}

// SanitizeFilename removes path traversal characters and other unsafe characters
func SanitizeFilename(filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("empty filename")
	}

	// Check for path traversal attempts
	if strings.Contains(filename, "..") {
		return "", fmt.Errorf("path traversal detected in filename: %s", filename)
	}

	// Check for absolute paths
	if strings.HasPrefix(filename, "/") || strings.HasPrefix(filename, "\\") {
		return "", fmt.Errorf("absolute path detected in filename: %s", filename)
	}

	// Check for drive letters (Windows)
	if len(filename) >= 2 && filename[1] == ':' {
		return "", fmt.Errorf("drive letter detected in filename: %s", filename)
	}

	// Remove any directory separators to ensure it's just a filename
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")

	// Remove other potentially dangerous characters
	filename = strings.ReplaceAll(filename, "\x00", "") // null bytes

	if filename == "" || filename == "." || filename == ".." {
		return "", fmt.Errorf("invalid filename after sanitization")
	}

	return filename, nil
}

// ValidateDownloadURL validates that a download URL uses HTTPS
// Returns an error if the URL is not secure (HTTP instead of HTTPS)
// Allows localhost/127.0.0.1 for testing purposes
func ValidateDownloadURL(downloadURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("empty download URL")
	}

	// Parse the URL
	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check if it's HTTPS
	if parsedURL.Scheme == "https" {
		return nil
	}

	// Allow HTTP for localhost/127.0.0.1 (for testing)
	if parsedURL.Scheme == "http" {
		host := parsedURL.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("insecure download URL: HTTP is not allowed (use HTTPS): %s", downloadURL)
	}

	// Reject other schemes
	return fmt.Errorf("invalid URL scheme '%s': only HTTPS is allowed", parsedURL.Scheme)
}

package downloader

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// VerifyFile checks if the file at path matches the expected checksum.
// The expectedChecksum can be prefixed with "sha256:", "md5:", or "sha1:".
// If no prefix is provided, it attempts to guess based on length, defaulting to sha256.
func VerifyFile(path string, expectedChecksum string) error {
	if expectedChecksum == "" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file for verification: %w", err)
	}
	defer f.Close()

	algo := "sha256"
	hashStr := expectedChecksum

	if idx := strings.Index(expectedChecksum, ":"); idx != -1 {
		algo = expectedChecksum[:idx]
		hashStr = expectedChecksum[idx+1:]
	} else {
		// Guess based on length
		l := len(expectedChecksum)
		if l == 32 {
			algo = "md5"
		} else if l == 40 {
			algo = "sha1"
		}
	}

	var hasher hash.Hash
	switch strings.ToLower(algo) {
	case "md5":
		hasher = md5.New()
	case "sha1":
		hasher = sha1.New()
	case "sha256":
		hasher = sha256.New()
	default:
		return fmt.Errorf("unsupported hash algorithm: %s", algo)
	}

	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	calculated := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(calculated, hashStr) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", hashStr, calculated)
	}

	return nil
}

//go:build !windows

package downloader

import (
	"os"
	"path/filepath"
	"syscall"
)

func CheckAvailableSpace(path string, requiredBytes int64) (bool, int64, error) {
	// Ensure the parent directory exists so we can check the partition
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return false, 0, err
		}
	}

	var stat syscall.Statfs_t
	err := syscall.Statfs(dir, &stat)
	if err != nil {
		return false, 0, err
	}

	// Available blocks * size per block
	// Note: Bsize and Bavail types can be different on different architectures (int32 vs uint64)
	// Casting to uint64 is generally safe for calculation
	availableBytes := int64(uint64(stat.Bavail) * uint64(stat.Bsize))
	return availableBytes >= requiredBytes, availableBytes, nil
}

//go:build windows

package downloader

func CheckAvailableSpace(path string, requiredBytes int64) (bool, int64, error) {
	// TODO: Implement proper Windows disk space check using golang.org/x/sys/windows
	// For now, we assume true to allow compilation and execution on Windows without external deps
	return true, 107374182400, nil // Return 100GB dummy available
}

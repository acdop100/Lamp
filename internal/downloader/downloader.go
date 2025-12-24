package downloader

import (
	"fmt"
	"io"
	"net/http"
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
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)
	return availableBytes >= requiredBytes, availableBytes, nil
}

type Progress struct {
	Total      int64
	Downloaded int64
	Error      error

	// Results from auto-resolution
	Status      string
	Current     string
	Latest      string
	ResolvedURL string
}

type ProgressWriter struct {
	Total      int64
	Downloaded int64
	onProgress func(Progress)
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.Downloaded += int64(n)
	pw.onProgress(Progress{Total: pw.Total, Downloaded: pw.Downloaded})
	return n, nil
}

func DownloadFile(url, dest string, progressChan chan<- Progress) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		err = fmt.Errorf("failed to create directory: %w", err)
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}

	if url == "" {
		err := fmt.Errorf("empty download URL")
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}
	req.Header.Set("User-Agent", "tui-dl/1.0 (Bubble Tea Download Manager)")

	resp, err := client.Do(req)
	if err != nil {
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}

	out, err := os.Create(dest)
	if err != nil {
		progressChan <- Progress{Error: err}
		close(progressChan)
		return err
	}
	defer out.Close()

	pw := &ProgressWriter{
		Total: resp.ContentLength,
		onProgress: func(p Progress) {
			// Non-blocking send
			select {
			case progressChan <- p:
			default:
			}
		},
	}

	_, err = io.Copy(out, io.TeeReader(resp.Body, pw))
	if err != nil {
		progressChan <- Progress{Error: err}
	}
	close(progressChan)
	return err
}

package downloader

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

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

// DownloadFile downloads a file from url to dest, supporting resumption.
func DownloadFile(url, dest string, progressChan chan<- Progress) error {
	defer close(progressChan)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		err = fmt.Errorf("failed to create directory: %w", err)
		progressChan <- Progress{Error: err}
		return err
	}

	if url == "" {
		err := fmt.Errorf("empty download URL")
		progressChan <- Progress{Error: err}
		return err
	}

	// Check for partial download (we can use .part extension or just check the dest file)
	// For simplicity, we'll try to resume the dest file directly if it exists,
	// OR we can implement a .part strategy.
	// Let's use the dest file directly for now to be simple, but usually .part is safer.
	// However, the prompt asked specifically for "Resume".

	var startBytes int64 = 0
	if info, err := os.Stat(dest); err == nil {
		startBytes = info.Size()
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		progressChan <- Progress{Error: err}
		return err
	}
	req.Header.Set("User-Agent", "tui-dl/1.0 (Bubble Tea Download Manager)")

	if startBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startBytes))
	}

	resp, err := client.Do(req)
	if err != nil {
		progressChan <- Progress{Error: err}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		// If range request failed (e.g. server doesn't support it, returns 200 OK instead of 206)
		// and we asked for a range, we must restart.
		if startBytes > 0 && resp.StatusCode == http.StatusOK {
			// Server ignored Range header. Accessing full file. Overwrite local.
			startBytes = 0
		} else {
			err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			progressChan <- Progress{Error: err}
			return err
		}
	}

	// If 416 Range Not Satisfiable, maybe we already have the full file?
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// Check if remote size matches local size
		// We can't know remote size easily without a separate HEAD.
		// For now, treat as error or "already done"
		progressChan <- Progress{Downloaded: startBytes, Total: startBytes}
		// Return nil if we assume it's done? Or maybe we should have checked size before.
		// Let's reset download to be safe if this happens, usually means file changed or we are past end.
		startBytes = 0
		// Re-request without range
		req.Header.Del("Range")
		resp, err = client.Do(req)
		if err != nil {
			progressChan <- Progress{Error: err}
			return err
		}
		defer resp.Body.Close()
	}

	var out *os.File
	if startBytes > 0 && resp.StatusCode == http.StatusPartialContent {
		out, err = os.OpenFile(dest, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		out, err = os.Create(dest)
	}

	if err != nil {
		progressChan <- Progress{Error: err}
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength + startBytes
	if resp.ContentLength == -1 {
		totalSize = -1 // Unknown
	}

	// If totalSize is unknown but we have startBytes, we can't report accurate total.

	pw := &ProgressWriter{
		Total:      totalSize,
		Downloaded: startBytes,
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
		return err
	}

	return nil
}

package downloader

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// DownloadFile downloads a file from url to dest, supporting parallel segments and resumption.
func DownloadFile(url, dest string, threads int, progressChan chan<- Progress) error {
	defer close(progressChan)

	if url == "" {
		return fmt.Errorf("empty download URL")
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 1. Get file info and check for range support
	client := &http.Client{}
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tui-dl/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}
	defer resp.Body.Close()

	contentLength := resp.ContentLength
	acceptRanges := resp.Header.Get("Accept-Ranges") == "bytes"

	// Fallback to single-threaded if no range support or unknown size or small file
	if !acceptRanges || contentLength <= 0 || threads <= 1 || contentLength < 1024*1024 {
		return downloadSingle(url, dest, progressChan)
	}

	// 2. Prepare file
	out, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer out.Close()

	if err := out.Truncate(contentLength); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	// 3. Split into segments
	chunkSize := contentLength / int64(threads)
	var wg sync.WaitGroup
	var downloaded int64
	var errOnce sync.Once
	var firstErr error

	for i := 0; i < threads; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == threads-1 {
			end = contentLength - 1
		}

		wg.Add(1)
		go func(s, e int64) {
			defer wg.Done()
			err := downloadSegment(url, out, s, e, &downloaded, contentLength, progressChan)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
				})
			}
		}(start, end)
	}

	wg.Wait()
	return firstErr
}

func downloadSingle(url, dest string, progressChan chan<- Progress) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "tui-dl/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	var downloaded int64
	_ = downloaded // Reserved for future use if we want per-segment in single too
	pw := &ProgressWriter{
		Total:      resp.ContentLength,
		Downloaded: 0,
		onProgress: func(p Progress) {
			select {
			case progressChan <- p:
			default:
			}
		},
	}

	_, err = io.Copy(out, io.TeeReader(resp.Body, pw))
	return err
}

func downloadSegment(url string, out *os.File, start, end int64, totalDownloaded *int64, totalSize int64, progressChan chan<- Progress) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", "tui-dl/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("segment HTTP %d", resp.StatusCode)
	}

	buffer := make([]byte, 32*1024)
	offset := start
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := out.WriteAt(buffer[:n], offset)
			if writeErr != nil {
				return writeErr
			}
			offset += int64(n)
			atomic.AddInt64(totalDownloaded, int64(n))

			// Report progress
			select {
			case progressChan <- Progress{
				Total:      totalSize,
				Downloaded: atomic.LoadInt64(totalDownloaded),
			}:
			default:
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

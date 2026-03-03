package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// MediaFile holds the path to a temporary file containing downloaded media data,
// along with its MIME type and a suggested filename.
// Callers MUST call Cleanup() when done to delete the temp file.
type MediaFile struct {
	Path     string
	MimeType string
	Filename string
	Size     int64
}

// Cleanup removes the underlying temp file.
func (mf *MediaFile) Cleanup() {
	if mf == nil || mf.Path == "" {
		return
	}
	os.Remove(mf.Path)
	mf.Path = ""
}

// ReadData reads the full file contents into memory.
// Intended for single-use: read once, upload, then Cleanup.
func (mf *MediaFile) ReadData() ([]byte, error) {
	if mf.Path == "" {
		return nil, fmt.Errorf("media file already cleaned up")
	}
	return os.ReadFile(mf.Path)
}

// tempDir caches the media temp directory path.
var tempDir string

func getTempDir() (string, error) {
	if tempDir != "" {
		return tempDir, nil
	}
	dir := filepath.Join(os.TempDir(), "mybot-media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tempDir = dir
	return dir, nil
}

// CleanupTempDir removes all leftover temp media files.
// Safe to call on startup to purge stale files from previous runs.
func CleanupTempDir() {
	dir, err := getTempDir()
	if err != nil {
		return
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

// DownloadToFile downloads media from url into a temporary file on disk.
// Returns a MediaFile handle. The caller is responsible for calling Cleanup().
// This keeps only a small buffer (~32KB) in memory during download,
// regardless of file size.
func DownloadToFile(ctx context.Context, url string) (*MediaFile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download media: %s", resp.Status)
	}

	dir, err := getTempDir()
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	f, err := os.CreateTemp(dir, "dl-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := f.Name()

	// Stream from HTTP body to disk with a size limit.
	// io.Copy uses a 32KB buffer internally, so peak memory is ~32KB.
	limited := io.LimitReader(resp.Body, int64(maxDownloadSize)+1)
	n, err := io.Copy(f, limited)
	f.Close() // close before checking error to ensure flush
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	if n > int64(maxDownloadSize) {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("media too large (exceeds %d bytes limit)", maxDownloadSize)
	}

	mimeType := resp.Header.Get("Content-Type")
	return &MediaFile{
		Path:     tmpPath,
		MimeType: mimeType,
		Filename: FilenameFromMIME(mimeType),
		Size:     n,
	}, nil
}

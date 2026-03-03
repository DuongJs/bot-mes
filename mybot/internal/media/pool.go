package media

import (
	"context"
	"sync"
)

// DownloadPool enforces a system-wide limit on the number of concurrent media
// downloads.  Every download (from any user/group) must acquire a slot first.
//
// The limit prevents OOM when many users send media commands at the same time,
// while still letting a single user's 10-image post use all available slots
// for maximum throughput.
type DownloadPool struct {
	sem chan struct{}
}

// NewDownloadPool creates a pool with the given concurrency limit.
// Typical default: 16 (good trade-off between throughput and memory).
func NewDownloadPool(maxConcurrent int) *DownloadPool {
	if maxConcurrent <= 0 {
		maxConcurrent = 16
	}
	return &DownloadPool{
		sem: make(chan struct{}, maxConcurrent),
	}
}

// DownloadToFile acquires a slot from the global pool, downloads the media to
// a temp file, and releases the slot.  The caller owns the returned MediaFile
// and must call Cleanup() when done.
func (p *DownloadPool) DownloadToFile(ctx context.Context, url string) (*MediaFile, error) {
	// Wait for a slot, respecting context cancellation.
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.sem }()

	return DownloadToFile(ctx, url)
}

// DownloadResult holds one item from a batch download.
type DownloadResult struct {
	Index int
	File  *MediaFile
	Err   error
}

// DownloadBatch downloads a list of media items concurrently (up to pool
// capacity) and returns results preserving order.
// On context cancellation, pending downloads are abandoned and their temp
// files cleaned up.
func (p *DownloadPool) DownloadBatch(ctx context.Context, items []MediaItem) []DownloadResult {
	results := make([]DownloadResult, len(items))
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it MediaItem) {
			defer wg.Done()
			mf, err := p.DownloadToFile(ctx, it.URL)
			results[idx] = DownloadResult{Index: idx, File: mf, Err: err}
		}(i, item)
	}
	wg.Wait()
	return results
}

// Capacity returns the maximum concurrent downloads.
func (p *DownloadPool) Capacity() int {
	return cap(p.sem)
}

// InUse returns the number of slots currently occupied.
func (p *DownloadPool) InUse() int {
	return len(p.sem)
}

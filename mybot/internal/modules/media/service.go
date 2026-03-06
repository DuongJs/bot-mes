package media

import (
	"context"

	"mybot/internal/media"
)

// Service wraps the media download pool and platform handlers.
type Service struct {
	Pool *media.DownloadPool
}

// NewService creates a media Service backed by a global download pool.
func NewService(pool *media.DownloadPool) *Service {
	return &Service{Pool: pool}
}

func (s *Service) GetMediaItems(ctx context.Context, url string) (media.MediaResult, error) {
	return media.GetMedia(ctx, url)
}

func (s *Service) Download(ctx context.Context, url string) ([]byte, string, error) {
	return media.DownloadMedia(ctx, url)
}

// DownloadToFile downloads media to a temp file via the global pool.
// Caller is responsible for calling MediaFile.Cleanup().
func (s *Service) DownloadToFile(ctx context.Context, url string) (*media.MediaFile, error) {
	return s.Pool.DownloadToFile(ctx, url)
}

// DownloadBatch downloads all items via the pool, respecting the global
// concurrency limit.  Returns results in order.
func (s *Service) DownloadBatch(ctx context.Context, items []media.MediaItem) []media.DownloadResult {
	return s.Pool.DownloadBatch(ctx, items)
}

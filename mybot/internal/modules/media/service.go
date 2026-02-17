package media

import (
	"context"

	"mybot/internal/media"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) GetMediaItems(ctx context.Context, url string) ([]media.MediaItem, error) {
	return media.GetMedia(ctx, url)
}

func (s *Service) Download(ctx context.Context, url string) ([]byte, string, error) {
	return media.DownloadMedia(ctx, url)
}

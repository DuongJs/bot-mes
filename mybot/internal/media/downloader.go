package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxDownloadSize = 50 * 1024 * 1024 // 50 MB

var httpClient = &http.Client{
	Timeout: FetchTimeout,
}

func GetMedia(ctx context.Context, url string) ([]MediaItem, error) {
	if strings.Contains(url, "instagram.com") {
		return GetInstagramMedia(ctx, url)
	}
	if strings.Contains(url, "tiktok.com") {
		return GetTikTokMedia(ctx, url)
	}
	if strings.Contains(url, "facebook.com") || strings.Contains(url, "fb.watch") {
		item, err := GetFacebookVideo(ctx, url)
		if err != nil {
			return nil, err
		}
		return []MediaItem{*item}, nil
	}
	return nil, fmt.Errorf("unsupported platform")
}

func DownloadMedia(url string) ([]byte, string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to download media: %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
	if err != nil {
		return nil, "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	return data, mimeType, nil
}

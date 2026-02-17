package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxDownloadSize = 25 * 1000 * 1000 // 25 MB – aligned with Facebook's upload limit

var httpClient = &http.Client{
	Timeout: FetchTimeout,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	},
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

// FilenameFromMIME returns a filename with a proper extension based on the MIME type.
func FilenameFromMIME(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "video/"):
		return "media.mp4"
	case strings.Contains(mimeType, "image/gif"):
		return "media.gif"
	case strings.Contains(mimeType, "image/png"):
		return "media.png"
	case strings.Contains(mimeType, "image/webp"):
		return "media.webp"
	case strings.HasPrefix(mimeType, "image/"):
		return "media.jpg"
	case strings.HasPrefix(mimeType, "audio/"):
		return "media.mp3"
	default:
		return "media.bin"
	}
}

func DownloadMedia(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create download request: %w", err)
	}
	resp, err := httpClient.Do(req)
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

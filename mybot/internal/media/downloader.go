package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// PlatformHandler defines a media platform with host-based matching and a handler function.
type PlatformHandler struct {
	Name    string
	Hosts   []string
	Handler func(ctx context.Context, url string) ([]MediaItem, error)
}

// platforms is the ordered registry of supported media platforms.
var platforms = []PlatformHandler{
	{
		Name:    "instagram",
		Hosts:   []string{"instagram.com", "instagr.am"},
		Handler: GetInstagramMedia,
	},
	{
		Name:  "tiktok",
		Hosts: []string{"tiktok.com"},
		Handler: GetTikTokMedia,
	},
	{
		Name:    "douyin",
		Hosts:   []string{"douyin.com", "iesdouyin.com"},
		Handler: GetDouyinMedia,
	},
	{
		Name:  "facebook",
		Hosts: []string{"facebook.com", "fb.watch"},
		Handler: func(ctx context.Context, u string) ([]MediaItem, error) {
			item, err := GetFacebookVideo(ctx, u)
			if err != nil {
				return nil, err
			}
			return []MediaItem{*item}, nil
		},
	},
}

// MatchHost checks whether a raw URL's hostname matches any of the given host suffixes.
func MatchHost(rawURL string, hosts []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(u.Hostname())
	for _, h := range hosts {
		if hostname == h || strings.HasSuffix(hostname, "."+h) {
			return true
		}
	}
	return false
}

func GetMedia(ctx context.Context, rawURL string) ([]MediaItem, error) {
	for _, p := range platforms {
		if MatchHost(rawURL, p.Hosts) {
			return p.Handler(ctx, rawURL)
		}
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

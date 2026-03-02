package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	TikWMAPI = "https://www.tikwm.com/api/"
)

// tikwmResponse represents the tikwm.com API response.
type tikwmResponse struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	ProcessedTime float64 `json:"processed_time"`
	Data          tikwmData `json:"data"`
}

type tikwmData struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Play      string       `json:"play"`       // video download URL (no watermark)
	Hdplay    string       `json:"hdplay"`     // HD video URL
	Images    []string     `json:"images"`     // slideshow image URLs
}

func GetTikTokMedia(ctx context.Context, rawURL string) ([]MediaItem, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			backoff := time.Duration(i) * 500 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		items, err := doTikWMRequest(ctx, rawURL)
		if err != nil {
			lastErr = err
			continue
		}
		return items, nil
	}
	return nil, fmt.Errorf("tikwm api failed after 3 retries: %w", lastErr)
}

func doTikWMRequest(ctx context.Context, tiktokURL string) ([]MediaItem, error) {
	// Build form body
	form := url.Values{}
	form.Set("url", tiktokURL)
	form.Set("hd", "1")

	apiReq, err := http.NewRequestWithContext(ctx, "POST", TikWMAPI, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create tikwm request: %w", err)
	}
	apiReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	apiReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call tikwm api: %w", err)
	}
	defer apiResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(apiResp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read tikwm response: %w", err)
	}

	if apiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tikwm api returned status %d: %s", apiResp.StatusCode, string(body))
	}

	var data tikwmResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to decode tikwm response: %w", err)
	}

	if data.Code != 0 {
		return nil, fmt.Errorf("tikwm api error: %s", data.Msg)
	}

	var items []MediaItem

	// Slideshow (images)
	if len(data.Data.Images) > 0 {
		for _, imgURL := range data.Data.Images {
			if imgURL != "" {
				items = append(items, MediaItem{Type: Image, URL: imgURL})
			}
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	// Video – prefer HD
	videoURL := data.Data.Hdplay
	if videoURL == "" {
		videoURL = data.Data.Play
	}
	if videoURL != "" {
		return []MediaItem{{Type: Video, URL: videoURL}}, nil
	}

	return nil, fmt.Errorf("no video or images found in tikwm response")
}

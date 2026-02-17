package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	DouyinProxyAPI = "https://douyin.cuong.one/api/douyin/detail"
)

type DouyinProxyResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Video   string `json:"video"`
}

func GetDouyinMedia(ctx context.Context, inputURL string) ([]MediaItem, error) {
	apiURL := fmt.Sprintf("%s?url=%s", DouyinProxyAPI, url.QueryEscape(inputURL))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create douyin request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call douyin api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("douyin api returned status: %d", resp.StatusCode)
	}

	var data DouyinProxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode douyin api response: %w", err)
	}

	if data.Status != "ok" || data.Video == "" {
		msg := data.Message
		if msg == "" {
			msg = "Douyin video not found"
		}
		return nil, fmt.Errorf("%s", msg)
	}

	return []MediaItem{{Type: Video, URL: data.Video}}, nil
}

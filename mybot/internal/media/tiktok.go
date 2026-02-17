package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
)

const (
	TikTokAPI = "https://api16-normal-c-useast2a.tiktokv.com/aweme/v1/feed/"
	TikTokUA  = "TikTok 26.2.0 rv:262018 (iPhone; iOS 14.4.2; en_US) Cronet"
)

var (
	awemeIDRegex = regexp.MustCompile(`/video/(\d+)|/photo/(\d+)|/note/(\d+)`)
)

type TikTokResponse struct {
	AwemeList []struct {
		ImagePostInfo *struct {
			Images []struct {
				DisplayImage *struct {
					URLList []string `json:"url_list"`
				} `json:"display_image"`
				URLList []string `json:"url_list"`
			} `json:"images"`
		} `json:"image_post_info"`
		Video *struct {
			PlayAddr *struct {
				URLList []string `json:"url_list"`
			} `json:"play_addr"`
		} `json:"video"`
	} `json:"aweme_list"`
}

// extractAwemeID extracts the aweme/video ID from a TikTok or Douyin URL.
func extractAwemeID(url string) string {
	matches := awemeIDRegex.FindStringSubmatch(url)
	if len(matches) < 2 {
		return ""
	}
	for _, m := range matches[1:] {
		if m != "" {
			return m
		}
	}
	return ""
}

func GetTikTokMedia(ctx context.Context, url string) ([]MediaItem, error) {
	// Resolve short URL
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tiktok request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tiktok url: %w", err)
	}
	finalURL := resp.Request.URL.String()
	resp.Body.Close()

	// Extract Aweme ID
	awemeID := extractAwemeID(finalURL)
	if awemeID == "" {
		return nil, fmt.Errorf("no aweme_id found in %s", finalURL)
	}

	// Fetch from API using OPTIONS method (required by TikTok's API)
	apiURL := fmt.Sprintf("%s?aweme_id=%s", TikTokAPI, awemeID)
	apiReq, err := http.NewRequestWithContext(ctx, "OPTIONS", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create api request: %w", err)
	}
	apiReq.Header.Set("User-Agent", TikTokUA)

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call tiktok api: %w", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tiktok api returned status: %d", apiResp.StatusCode)
	}

	var data TikTokResponse
	if err := json.NewDecoder(apiResp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode tiktok api response: %w", err)
	}

	if len(data.AwemeList) == 0 {
		return nil, fmt.Errorf("no tiktok data found")
	}

	aweme := data.AwemeList[0]
	var items []MediaItem

	// Slideshow (images)
	if aweme.ImagePostInfo != nil && len(aweme.ImagePostInfo.Images) > 0 {
		for _, img := range aweme.ImagePostInfo.Images {
			var url string
			if img.DisplayImage != nil && len(img.DisplayImage.URLList) > 0 {
				url = img.DisplayImage.URLList[0]
			} else if len(img.URLList) > 0 {
				url = img.URLList[0]
			}
			if url != "" {
				items = append(items, MediaItem{Type: Image, URL: url})
			}
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	// Video
	if aweme.Video != nil && aweme.Video.PlayAddr != nil && len(aweme.Video.PlayAddr.URLList) > 0 {
		return []MediaItem{{Type: Video, URL: aweme.Video.PlayAddr.URLList[0]}}, nil
	}

	return nil, fmt.Errorf("no video or images found")
}

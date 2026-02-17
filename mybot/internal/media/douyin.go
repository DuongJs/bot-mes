package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	DouyinAPI = "https://www.iesdouyin.com/aweme/v1/web/aweme/detail/"
)

type DouyinResponse struct {
	AwemeDetail *struct {
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
	} `json:"aweme_detail"`
}

func GetDouyinMedia(ctx context.Context, url string) ([]MediaItem, error) {
	// Resolve short URL (e.g. v.douyin.com/xxx → www.douyin.com/video/xxx)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create douyin request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve douyin url: %w", err)
	}
	finalURL := resp.Request.URL.String()
	resp.Body.Close()

	// Extract video/note ID (shares regex with TikTok)
	awemeID := extractAwemeID(finalURL)
	if awemeID == "" {
		return nil, fmt.Errorf("no video_id found in %s", finalURL)
	}

	// Fetch from Douyin API
	apiURL := fmt.Sprintf("%s?aweme_id=%s&aid=6383&device_platform=webapp", DouyinAPI, awemeID)
	apiReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create douyin api request: %w", err)
	}
	apiReq.Header.Set("User-Agent", UserAgent)
	apiReq.Header.Set("Referer", "https://www.douyin.com/")

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call douyin api: %w", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("douyin api returned status: %d", apiResp.StatusCode)
	}

	var data DouyinResponse
	if err := json.NewDecoder(apiResp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode douyin api response: %w", err)
	}

	if data.AwemeDetail == nil {
		return nil, fmt.Errorf("no douyin data found")
	}

	aweme := data.AwemeDetail
	var items []MediaItem

	// Slideshow (images)
	if aweme.ImagePostInfo != nil && len(aweme.ImagePostInfo.Images) > 0 {
		for _, img := range aweme.ImagePostInfo.Images {
			var imgURL string
			if img.DisplayImage != nil && len(img.DisplayImage.URLList) > 0 {
				imgURL = img.DisplayImage.URLList[0]
			} else if len(img.URLList) > 0 {
				imgURL = img.URLList[0]
			}
			if imgURL != "" {
				items = append(items, MediaItem{Type: Image, URL: imgURL})
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

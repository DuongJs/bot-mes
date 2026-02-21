package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	igGraphqlURL = "https://www.instagram.com/api/graphql"
	igDocID      = "10015901848480474"
	igAppID      = "936619743392459"
	igLSD        = "AdTG3HXj-AuAqL1v6Ppe6xBXk0s"

	igDefaultRetries = 10
)

type InstagramResponse struct {
	Data struct {
		XDTShorcodeMedia *struct {
			Typename              string `json:"__typename"`
			IsVideo               bool   `json:"is_video"`
			VideoURL              string `json:"video_url"`
			DisplayURL            string `json:"display_url"`
			EdgeSidecarToChildren struct {
				Edges []struct {
					Node struct {
						IsVideo    bool   `json:"is_video"`
						VideoURL   string `json:"video_url"`
						DisplayURL string `json:"display_url"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"edge_sidecar_to_children"`
		} `json:"xdt_shortcode_media"`
	} `json:"data"`
}

// extractShortcode extracts the Instagram shortcode from a URL by splitting the
// path segments and looking for known post type markers (p, reel, tv, reels).
func extractShortcode(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	postTags := map[string]bool{"p": true, "reel": true, "tv": true, "reels": true}
	for i, seg := range segments {
		if postTags[seg] && i+1 < len(segments) {
			return segments[i+1]
		}
	}
	return ""
}

// isShareURL checks whether the Instagram URL is a share/redirect link.
func isShareURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, seg := range segments {
		if seg == "share" {
			return true
		}
	}
	return false
}

func GetInstagramMedia(ctx context.Context, inputURL string) ([]MediaItem, error) {
	// Resolve share/redirect links
	if isShareURL(inputURL) {
		req, err := http.NewRequestWithContext(ctx, "GET", inputURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create redirect request: %w", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to check redirect: %w", err)
		}
		inputURL = resp.Request.URL.String()
		resp.Body.Close()
	}

	shortcode := extractShortcode(inputURL)
	if shortcode == "" {
		return nil, fmt.Errorf("invalid instagram url")
	}

	// Query GraphQL with retry
	data, err := igGraphQLRequest(ctx, shortcode, igDefaultRetries)
	if err != nil {
		return nil, err
	}

	media := data.Data.XDTShorcodeMedia
	if media == nil {
		return nil, fmt.Errorf("no media found")
	}

	var items []MediaItem
	if media.Typename == "XDTGraphSidecar" {
		for _, edge := range media.EdgeSidecarToChildren.Edges {
			node := edge.Node
			u := node.DisplayURL
			t := Image
			if node.IsVideo {
				u = node.VideoURL
				t = Video
			}
			items = append(items, MediaItem{Type: t, URL: u})
		}
	} else {
		u := media.DisplayURL
		t := Image
		if media.IsVideo {
			u = media.VideoURL
			t = Video
		}
		items = append(items, MediaItem{Type: t, URL: u})
	}

	return items, nil
}

// igGraphQLRequest calls Instagram's public GraphQL API with hardcoded headers.
// No CSRF token fetch needed — uses static lsd + app-id approach.
func igGraphQLRequest(ctx context.Context, shortcode string, retries int) (*InstagramResponse, error) {
	variables := map[string]interface{}{
		"shortcode":                         shortcode,
		"fetch_comment_count":               0,
		"fetch_related_profile_media_count": 0,
		"parent_comment_count":              0,
		"child_comment_count":               0,
		"fetch_like_count":                  0,
		"fetch_tagged_user_count":           nil,
		"fetch_preview_comment_count":       0,
		"has_threaded_comments":             true,
		"hoisted_comment_id":                nil,
		"hoisted_reply_id":                  nil,
	}
	jsonVars, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}

	form := url.Values{}
	form.Set("lsd", igLSD)
	form.Set("jazoest", "2957")
	form.Set("fb_api_caller_class", "RelayModern")
	form.Set("fb_api_req_friendly_name", "PolarisPostActionLoadPostQueryQuery")
	form.Set("server_timestamps", "true")
	form.Set("doc_id", igDocID)
	form.Set("variables", string(jsonVars))

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		result, err := doIGRequest(ctx, form)
		if err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("instagram graphql failed after %d retries: %w", retries, lastErr)
}

func doIGRequest(ctx context.Context, form url.Values) (*InstagramResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", igGraphqlURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create graphql request: %w", err)
	}

	req.Header.Set("authority", "www.instagram.com")
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "vi-VN,vi;q=0.9,en-US;q=0.6,en;q=0.5")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("dpr", "2.625")
	req.Header.Set("origin", "https://www.instagram.com")
	req.Header.Set("referer", "https://www.instagram.com/")
	req.Header.Set("sec-ch-prefers-color-scheme", "dark")
	req.Header.Set("sec-ch-ua", `"Chromium";v="107", "Not=A?Brand";v="24"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Chromium";v="107.0.5304.74", "Not=A?Brand";v="24.0.0.0"`)
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("sec-ch-ua-model", `"SM-A336E"`)
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("sec-ch-ua-platform-version", `"13.0.0"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (Linux; Android 13; SM-A336E) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Mobile Safari/537.36")
	req.Header.Set("x-asbd-id", "129477")
	req.Header.Set("x-csrftoken", "qPAu6oXeZT5gjItJk4yFAB")
	req.Header.Set("x-fb-friendly-name", "PolarisPostActionLoadPostQueryQuery")
	req.Header.Set("x-fb-lsd", igLSD)
	req.Header.Set("x-ig-app-id", igAppID)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram graphql returned status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var data InstagramResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to decode instagram response: %w", err)
	}

	return &data, nil
}

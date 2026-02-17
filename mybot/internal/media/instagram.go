package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	InstagramURL = "https://www.instagram.com/"
	GraphqlURL   = "https://www.instagram.com/graphql/query"
	DocID        = "9510064595728286"
)

var (
	csrfTokenRegex = regexp.MustCompile(`csrftoken=([^;]+)`)
	shortcodeRegex = regexp.MustCompile(`/(p|reel|tv|reels)/([^/?]+)`)
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

func GetInstagramMedia(ctx context.Context, inputURL string) ([]MediaItem, error) {
	// Check for redirects (e.g. share links)
	if strings.Contains(inputURL, "/share") {
		client := &http.Client{
			Timeout: FetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return nil
			},
		}
		resp, err := client.Get(inputURL)
		if err != nil {
			return nil, fmt.Errorf("failed to check redirect: %w", err)
		}
		inputURL = resp.Request.URL.String()
		resp.Body.Close()
	}

	match := shortcodeRegex.FindStringSubmatch(inputURL)
	if len(match) < 3 {
		return nil, fmt.Errorf("invalid instagram url")
	}
	shortcode := match[2]

	// Get CSRF Token
	req, _ := http.NewRequestWithContext(ctx, "GET", InstagramURL, nil)
	req.Header.Set("User-Agent", UserAgent)
	client := &http.Client{Timeout: FetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch instagram home: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram home returned status: %d", resp.StatusCode)
	}

	var csrfToken string
	// Check headers first (Set-Cookie)
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "csrftoken" {
			csrfToken = cookie.Value
			break
		}
	}

	// Fallback to body regex if not in cookie jar (improbable but safe)
	if csrfToken == "" {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
		bodyStr := string(bodyBytes)
		if match := csrfTokenRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
			csrfToken = match[1]
		}
	}

	if csrfToken == "" {
		return nil, fmt.Errorf("csrf token not found")
	}

	// Query GraphQL
	variables := map[string]interface{}{
		"shortcode":               shortcode,
		"fetch_tagged_user_count": nil,
		"hoisted_comment_id":      nil,
		"hoisted_reply_id":        nil,
	}
	jsonVars, _ := json.Marshal(variables)

	form := url.Values{}
	form.Set("variables", string(jsonVars))
	form.Set("doc_id", DocID)

	req, err = http.NewRequestWithContext(ctx, "POST", GraphqlURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create graphql request: %w", err)
	}
	req.Header.Set("X-CSRFToken", csrfToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call instagram graphql: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram graphql returned status: %d", resp.StatusCode)
	}

	var data InstagramResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode instagram response: %w", err)
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

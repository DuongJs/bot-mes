package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	InstagramURL = "https://www.instagram.com/"
	GraphqlURL   = "https://www.instagram.com/graphql/query"
	DocID        = "9510064595728286"
	IGAppID      = "936619743392459"

	defaultRetries = 5
	defaultDelay   = 1 * time.Second
)

var (
	csrfTokenRegex = regexp.MustCompile(`csrftoken=([^;]+)`)
	shortcodeRegex = regexp.MustCompile(`/(p|reel|tv|reels)/([^/?#]+)`)
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
		// Fallback to regex for non-standard URLs
		match := shortcodeRegex.FindStringSubmatch(inputURL)
		if len(match) < 3 {
			return nil, fmt.Errorf("invalid instagram url")
		}
		shortcode = match[2]
	}

	// Get CSRF Token
	csrfToken, err := getCSRFToken(ctx)
	if err != nil {
		return nil, err
	}

	// Query GraphQL with retry
	data, err := instagramGraphQLRequest(ctx, shortcode, csrfToken, defaultRetries, defaultDelay)
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

func getCSRFToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", InstagramURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create csrf request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch instagram home: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("instagram home returned status: %d", resp.StatusCode)
	}

	// Check Set-Cookie header first
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "csrftoken" {
			return cookie.Value, nil
		}
	}

	// Fallback to body regex
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
	if match := csrfTokenRegex.FindStringSubmatch(string(bodyBytes)); len(match) > 1 {
		return match[1], nil
	}

	return "", fmt.Errorf("csrf token not found")
}

func instagramGraphQLRequest(ctx context.Context, shortcode, csrfToken string, retries int, delay time.Duration) (*InstagramResponse, error) {
	variables := map[string]interface{}{
		"shortcode":               shortcode,
		"fetch_tagged_user_count": nil,
		"hoisted_comment_id":      nil,
		"hoisted_reply_id":        nil,
	}
	jsonVars, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}

	form := url.Values{}
	form.Set("variables", string(jsonVars))
	form.Set("doc_id", DocID)

	req, err := http.NewRequestWithContext(ctx, "POST", GraphqlURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create graphql request: %w", err)
	}
	req.Header.Set("X-CSRFToken", csrfToken)
	req.Header.Set("X-IG-App-ID", IGAppID)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", InstagramURL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)
	req.AddCookie(&http.Cookie{Name: "csrftoken", Value: csrfToken})

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call instagram graphql: %w", err)
	}
	defer resp.Body.Close()

	// Retry on 429 (rate limit) and 403 (forbidden) with exponential backoff
	if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden) && retries > 0 {
		wait := delay
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				wait = time.Duration(secs) * time.Second
			}
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return instagramGraphQLRequest(ctx, shortcode, csrfToken, retries-1, delay*2)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instagram graphql returned status: %d", resp.StatusCode)
	}

	var data InstagramResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode instagram response: %w", err)
	}

	return &data, nil
}

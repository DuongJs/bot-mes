package media

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// instagramGraphQL calls Instagram's GraphQL API to fetch post data by shortcode.
// Logic from: https://huggingface.co/spaces/cuorz/api/raw/main/main.go
func instagramGraphQL(shortcode string) (map[string]interface{}, error) {
	formData := url.Values{}
	formData.Set("lsd", "AdTG3HXj-AuAqL1v6Ppe6xBXk0s")
	formData.Set("jazoest", "2957")
	formData.Set("fb_api_caller_class", "RelayModern")
	formData.Set("fb_api_req_friendly_name", "PolarisPostActionLoadPostQueryQuery")
	formData.Set("server_timestamps", "true")
	formData.Set("doc_id", "10015901848480474")

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
	variablesJSON, _ := json.Marshal(variables)
	formData.Set("variables", string(variablesJSON))

	req, err := http.NewRequest("POST", "https://www.instagram.com/api/graphql", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
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
	req.Header.Set("x-fb-lsd", "AdTG3HXj-AuAqL1v6Ppe6xBXk0s")
	req.Header.Set("x-ig-app-id", "936619743392459")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("JSON decode error: %w\nBody: %s", err, string(body[:min(len(body), 500)]))
	}

	return result, nil
}

// extractMediaURLsFromGraphQL walks the GraphQL response to find video_url or display_url.
func extractMediaURLsFromGraphQL(data map[string]interface{}) []string {
	var urls []string

	dataField, _ := data["data"].(map[string]interface{})
	if dataField == nil {
		return urls
	}
	media, _ := dataField["xdt_shortcode_media"].(map[string]interface{})
	if media == nil {
		return urls
	}

	// Check if it's a single video/image
	if videoURL, ok := media["video_url"].(string); ok && videoURL != "" {
		urls = append(urls, videoURL)
		return urls
	}

	// Check carousel (edge_sidecar_to_children)
	sidecar, _ := media["edge_sidecar_to_children"].(map[string]interface{})
	if sidecar != nil {
		edges, _ := sidecar["edges"].([]interface{})
		for _, edge := range edges {
			edgeMap, _ := edge.(map[string]interface{})
			if edgeMap == nil {
				continue
			}
			node, _ := edgeMap["node"].(map[string]interface{})
			if node == nil {
				continue
			}
			if videoURL, ok := node["video_url"].(string); ok && videoURL != "" {
				urls = append(urls, videoURL)
			} else if displayURL, ok := node["display_url"].(string); ok && displayURL != "" {
				urls = append(urls, displayURL)
			}
		}
		return urls
	}

	// Fallback to display_url for single image
	if displayURL, ok := media["display_url"].(string); ok && displayURL != "" {
		urls = append(urls, displayURL)
	}

	return urls
}

func TestInstagramGraphQLAPI(t *testing.T) {
	testURL := "https://www.instagram.com/reel/DShpzVlE_lO/?utm_source=ig_web_copy_link&igsh=NTc4MTIwNjQ2YQ=="

	// Step 1: Extract shortcode (using existing function from instagram.go)
	shortcode := extractShortcode(testURL)
	if shortcode == "" {
		t.Fatalf("❌ Failed to extract shortcode from URL: %s", testURL)
	}
	t.Logf("✅ Extracted shortcode: %s", shortcode)

	// Step 2: Call Instagram GraphQL API
	result, err := instagramGraphQL(shortcode)
	if err != nil {
		t.Fatalf("❌ GraphQL API call failed: %v", err)
	}
	t.Logf("✅ GraphQL API returned successfully")

	// Step 3: Pretty-print a summary of the response
	if data, ok := result["data"].(map[string]interface{}); ok {
		if media, ok := data["xdt_shortcode_media"].(map[string]interface{}); ok {
			t.Logf("  Media type: %v", media["__typename"])
			if owner, ok := media["owner"].(map[string]interface{}); ok {
				t.Logf("  Owner: %v", owner["username"])
			}
			if caption, ok := media["edge_media_to_caption"].(map[string]interface{}); ok {
				if edges, ok := caption["edges"].([]interface{}); ok && len(edges) > 0 {
					if edge, ok := edges[0].(map[string]interface{}); ok {
						if node, ok := edge["node"].(map[string]interface{}); ok {
							text := fmt.Sprintf("%v", node["text"])
							if len(text) > 100 {
								text = text[:100] + "..."
							}
							t.Logf("  Caption: %s", text)
						}
					}
				}
			}
		} else {
			t.Logf("⚠️  'xdt_shortcode_media' not found in response")
			for k := range data {
				t.Logf("  data key: %s", k)
			}
		}
	} else {
		t.Logf("⚠️  'data' field not found in response")
		for k := range result {
			t.Logf("  top-level key: %s", k)
		}
		raw, _ := json.MarshalIndent(result, "", "  ")
		output := string(raw)
		if len(output) > 2000 {
			output = output[:2000] + "\n... (truncated)"
		}
		t.Logf("Raw response:\n%s", output)
	}

	// Step 4: Extract media URLs
	mediaURLs := extractMediaURLsFromGraphQL(result)
	if len(mediaURLs) == 0 {
		// Dump full response for debugging
		raw, _ := json.MarshalIndent(result, "", "  ")
		output := string(raw)
		if len(output) > 3000 {
			output = output[:3000] + "\n... (truncated)"
		}
		t.Logf("Full response:\n%s", output)
		t.Fatalf("❌ No media URLs found in response")
	}
	for i, u := range mediaURLs {
		truncated := u
		if len(truncated) > 120 {
			truncated = truncated[:120] + "..."
		}
		t.Logf("✅ Media URL #%d: %s", i+1, truncated)
	}

	// Step 5: Verify first URL is downloadable
	headResp, err := http.Head(mediaURLs[0])
	if err != nil {
		t.Fatalf("❌ Failed to HEAD media URL: %v", err)
	}
	defer headResp.Body.Close()

	t.Logf("✅ Media download check: HTTP %d, Content-Type: %s, Content-Length: %s",
		headResp.StatusCode,
		headResp.Header.Get("Content-Type"),
		headResp.Header.Get("Content-Length"),
	)

	if headResp.StatusCode != http.StatusOK {
		t.Errorf("❌ Unexpected status code: %d", headResp.StatusCode)
	}
}

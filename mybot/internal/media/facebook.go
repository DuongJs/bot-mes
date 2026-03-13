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
	"time"

	"github.com/tidwall/gjson"
)

const (
	UserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	FetchTimeout = 30 * time.Second
	MaxHTMLBytes = 5 * 1024 * 1024 // 5 MB

	fbGraphQLURL = "https://graph.facebook.com/graphql"
	fbMobileUA   = "[FBAN/FB4A;FBAV/417.0.0.33.65;FBBV/480085463;FBDM/{density=2.75,width=1080,height=2029};FBLC/vi_VN;FBRV/0;FBCR/VinaPhone;FBMF/Xiaomi;FBBD/Xiaomi;FBPN/com.facebook.katana;FBDV/MI 8 SE;FBSV/9;FBOP/1;FBCA/armeabi-v7a:armeabi;]"
)

var (
	fbAPIToken string // set via SetFacebookToken

	// Regex patterns for URL classification
	fbStoriesRegex   = regexp.MustCompile(`/stories/(\d+)(?:/([^\/?]+))?`)
	fbOnlyVideoRegex = regexp.MustCompile(`^https://(?:www|m|mbasic|mobile|web)\.facebook\.com/(?:watch\?v=\d+|reel/|videos/[^\/?#]+/?[^\/?#]*)$`)
	fbURLRegex       = regexp.MustCompile(`^https?://(?:www\.)?(?:facebook\.com|mbasic\.facebook\.com|m\.facebook\.com|mobile\.facebook\.com|fb\.watch|web\.facebook)`)
	fbIGURLRegex     = regexp.MustCompile(`https://www\.instagram\.com/(stories|p|reel|tv)/[a-zA-Z0-9_\-/?=.]+`)

	// Paths that indicate non-profile content (media, groups, etc.)
	fbNonProfilePaths = map[string]bool{
		"watch": true, "photo": true, "groups": true, "share": true,
		"stories": true, "reel": true, "videos": true, "pages": true,
		"story.php": true, "permalink.php": true, "video.php": true,
	}

	// Legacy regex patterns for HTML scraping fallback
	sdURLRegex         = regexp.MustCompile(`"browser_native_sd_url":"(.*?)"`)
	playableURLRegex   = regexp.MustCompile(`"playable_url":"(.*?)"`)
	sdSrcRegex         = regexp.MustCompile(`sd_src\s*:\s*"([^"]*)"`)
	srcRegex           = regexp.MustCompile(`"src":"[^"]*(https://[^"]*)`)
	hdURLRegex         = regexp.MustCompile(`"browser_native_hd_url":"(.*?)"`)
	playableHDURLRegex = regexp.MustCompile(`"playable_url_quality_hd":"(.*?)"`)
	hdSrcRegex         = regexp.MustCompile(`hd_src\s*:\s*"([^"]*)"`)
	ogImageRegex       = regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`)
	imageURIRegex      = regexp.MustCompile(`"image":\{"uri":"([^"]+)"`)
)

// SetFacebookToken sets the OAuth token used for Facebook GraphQL API calls.
func SetFacebookToken(token string) {
	fbAPIToken = token
}

// isFBProfileURL returns true if the URL is a Facebook profile page (not a post/media).
func isFBProfileURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, "facebook.com") {
		return false
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return true
	}
	firstSeg := strings.SplitN(path, "/", 2)[0]
	if fbNonProfilePaths[firstSeg] {
		return false
	}
	// profile.php?id=... or plain username with no sub-path
	if !strings.Contains(path, "/") || strings.HasPrefix(path, "profile.php") {
		return true
	}
	return false
}

// GetFacebookMedia fetches media from a Facebook URL.
// If an API token is set, uses Facebook's GraphQL API (returns post text + media).
// Otherwise falls back to HTML scraping (media only).
func GetFacebookMedia(ctx context.Context, rawURL string) (MediaResult, error) {
	if fbAPIToken != "" {
		result, err := fbFetchPostMedia(ctx, rawURL)
		if err == nil && len(result.Items) > 0 {
			return result, nil
		}
		// Fall through to legacy on failure
	}
	items, err := legacyFBMedia(ctx, rawURL)
	if err != nil {
		return MediaResult{}, err
	}
	return MediaResult{Items: items}, nil
}

// ── GraphQL API implementation ──────────────────────────────────────────────

// fbGraphqlPost sends a form-encoded POST to Facebook's GraphQL API with OAuth token.
func fbGraphqlPost(ctx context.Context, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", fbGraphQLURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "OAuth "+fbAPIToken)
	req.Header.Set("User-Agent", fbMobileUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("graphql status %d: %s", resp.StatusCode, string(snippet))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return string(body), nil
}

// fbParseResponse handles Facebook's GraphQL responses which may contain
// concatenated JSON objects ({...}{...}) and extracts the one with "data".
func fbParseResponse(body string) gjson.Result {
	body = strings.TrimSpace(body)

	// Try direct parse
	if gjson.Valid(body) {
		r := gjson.Parse(body)
		if r.Get("data").Exists() {
			return r
		}
		// If it's an array, find the element with "data"
		if r.IsArray() {
			for _, item := range r.Array() {
				if item.Get("data").Exists() {
					return item
				}
			}
		}
		return r
	}

	// Handle concatenated JSON objects: {...}{...}
	re := regexp.MustCompile(`\}\s*\{`)
	wrapped := "[" + re.ReplaceAllString(body, "},{") + "]"
	if gjson.Valid(wrapped) {
		arr := gjson.Parse(wrapped)
		for _, item := range arr.Array() {
			if item.Get("data").Exists() {
				return item
			}
		}
		if len(arr.Array()) > 0 {
			return arr.Array()[0]
		}
	}
	return gjson.Result{}
}

// fbFetchStories fetches story media via the StoriesBucketQuery.
func fbFetchStories(ctx context.Context, bucketID, storyID string) (MediaResult, error) {
	vars, _ := json.Marshal(map[string]interface{}{
		"bucketID": bucketID,
		"blur":     10,
		"cursor":   nil,
		"scale":    1,
	})

	form := url.Values{
		"fb_api_caller_class":      {"RelayModern"},
		"fb_api_req_friendly_name": {"StoriesSuspenseContentPaneRootWithEntryPointQuery"},
		"doc_id":                   {"7114359461936746"},
		"variables":                {string(vars)},
	}

	body, err := fbGraphqlPost(ctx, form)
	if err != nil {
		return MediaResult{}, fmt.Errorf("stories query: %w", err)
	}

	data := fbParseResponse(body)
	return fbParseStories(data, storyID), nil
}

// fbParseStories extracts story attachments from the stories query response.
func fbParseStories(data gjson.Result, storyID string) MediaResult {
	var items []MediaItem
	edges := data.Get("data.bucket.unified_stories.edges")
	edges.ForEach(func(_, edge gjson.Result) bool {
		nodeID := edge.Get("node.id").String()
		// If a specific storyID is requested, filter
		if storyID != "" && nodeID != storyID {
			return true
		}
		media := edge.Get("node.attachments.0.media")
		typename := media.Get("__typename").String()
		if typename == "Photo" {
			if uri := media.Get("image.uri").String(); uri != "" {
				items = append(items, MediaItem{Type: Image, URL: uri})
			}
		} else {
			// Video
			u := media.Get("browser_native_hd_url").String()
			if u == "" {
				u = media.Get("browser_native_sd_url").String()
			}
			if u != "" {
				items = append(items, MediaItem{Type: Video, URL: u})
			}
		}
		return true
	})
	return MediaResult{Items: items}
}

// fbFetchPostMedia is the main GraphQL-based fetch function.
// Follows the same logic as the JavaScript FetchStoriesAndMedia.
func fbFetchPostMedia(ctx context.Context, rawURL string) (MediaResult, error) {
	// 1. Stories URL
	if m := fbStoriesRegex.FindStringSubmatch(rawURL); m != nil {
		storyID := ""
		if len(m) > 2 {
			storyID = m[2]
		}
		return fbFetchStories(ctx, m[1], storyID)
	}

	// 2. Validate URL
	if !fbURLRegex.MatchString(rawURL) {
		return MediaResult{}, fmt.Errorf("invalid facebook url")
	}
	if isFBProfileURL(rawURL) {
		return MediaResult{}, fmt.Errorf("profile url not supported")
	}

	// 3. ComposerLinkPreviewQuery — get link preview data
	previewVars, _ := json.Marshal(map[string]interface{}{
		"params": map[string]interface{}{
			"url": rawURL,
		},
	})
	previewForm := url.Values{
		"fb_api_req_friendly_name": {"ComposerLinkPreviewQuery"},
		"client_doc_id":            {"89598650511870084207501691272"},
		"variables":                {string(previewVars)},
	}

	previewBody, err := fbGraphqlPost(ctx, previewForm)
	if err != nil {
		return MediaResult{}, fmt.Errorf("preview query: %w", err)
	}

	preview := fbParseResponse(previewBody)
	if !preview.Get("data").Exists() {
		return MediaResult{}, fmt.Errorf("no data in preview response")
	}

	// 4. If it's a video-only URL or the preview points to a reel/video/IG
	storyURL := preview.Get("data.link_preview.story_attachment.style_infos.0.fb_shorts_story.storyUrl").String()
	isVideoURL := fbOnlyVideoRegex.MatchString(rawURL) ||
		fbOnlyVideoRegex.MatchString(storyURL) ||
		fbIGURLRegex.MatchString(storyURL)

	if isVideoURL {
		return fbParsePreviewMedia(preview), nil
	}

	// 5. Check if share_scrape_data points to a story
	shareScrapeData := preview.Get("data.link_preview.share_scrape_data").String()
	if shareScrapeData != "" {
		shareParams := gjson.Get(shareScrapeData, "share_params")
		canonical := shareParams.Get("urlInfo.canonical").String()
		if canonical != "" {
			if m := fbStoriesRegex.FindStringSubmatch(canonical); m != nil {
				storyID := ""
				if len(m) > 2 {
					storyID = m[2]
				}
				return fbFetchStories(ctx, m[1], storyID)
			}
		}
	}

	// 6. Get post_id and node_id for the full media query
	nodeID := preview.Get("data.link_preview.story.id").String()
	if nodeID == "" {
		return MediaResult{}, fmt.Errorf("no story id in preview response")
	}

	postID := ""
	if shareScrapeData != "" {
		postID = gjson.Get(shareScrapeData, "share_params.0").String()
	}

	// 7. FetchGraphQLStoryAndMediaFromTokenQuery (mobile media)
	mediaVars, _ := json.Marshal(map[string]interface{}{
		"action_location":                         "feed",
		"include_image_ranges":                    true,
		"image_medium_height":                     2048,
		"query_media_type":                        "ALL",
		"automatic_photo_captioning_enabled":      false,
		"image_large_aspect_height":               565,
		"angora_attachment_profile_image_size":    110,
		"profile_pic_media_type":                  "image/x-auto",
		"scale":                                   3,
		"enable_cix_screen_rollout":               true,
		"default_image_scale":                     3,
		"angora_attachment_cover_image_size":      1320,
		"image_low_height":                        2048,
		"image_large_aspect_width":                1080,
		"image_low_width":                         360,
		"image_high_height":                       2048,
		"node_id":                                 nodeID,
		"icon_scale":                              3,
		"can_fetch_suggestion":                    false,
		"profile_image_size":                      110,
		"reading_attachment_profile_image_height": 371,
		"reading_attachment_profile_image_width":  248,
		"fetch_fbc_header":                        true,
		"size_style":                              "contain-fit",
		"photos_feed_reduced_data_fetch":          true,
		"media_paginated_object_first":            200,
		"in_channel_eligibility_experiment":       false,
		"fetch_cix_screen_nt_payload":             true,
		"media_token":                             "pcb." + postID,
		"fetch_heisman_cta":                       true,
		"fix_mediaset_cache_id":                   true,
		"location_suggestion_profile_image_size":  110,
		"image_high_width":                        1080,
		"media_type":                              "image/jpeg",
		"image_medium_width":                      540,
		"nt_context": map[string]interface{}{
			"styles_id":          "e6c6f61b7a86cdf3fa2eaaffa982fbd1",
			"using_white_navbar": true,
			"pixel_ratio":        3,
			"is_push_on":         true,
			"bloks_version":      "c3cc18230235472b54176a5922f9b91d291342c3a276e2644dbdb9760b96deec",
		},
	})
	mediaForm := url.Values{
		"fb_api_req_friendly_name": {"FetchGraphQLStoryAndMediaFromTokenQuery"},
		"client_doc_id":            {"14968485422525517963281561600"},
		"variables":                {string(mediaVars)},
		"fb_api_caller_class":      {"graphservice"},
		"fb_api_analytics_tags":    {`["At_Connection","GraphServices"]`},
	}

	mediaBody, err := fbGraphqlPost(ctx, mediaForm)
	if err != nil {
		return MediaResult{}, fmt.Errorf("media query: %w", err)
	}

	mediaData := fbParseResponse(mediaBody)

	// Check if we got mobile media
	edges := mediaData.Get("data.mediaset.media.edges")
	if edges.Exists() && len(edges.Array()) > 0 {
		return fbParseMobileMedia(mediaData, postID), nil
	}

	// 8. Fallback: CometSinglePostContentQuery (web media)
	webVars, _ := json.Marshal(map[string]interface{}{
		"feedbackSource":                2,
		"feedLocation":                  "PERMALINK",
		"privacySelectorRenderLocation": "COMET_STREAM",
		"renderLocation":                "permalink",
		"scale":                         1.5,
		"storyID":                       nodeID,
		"useDefaultActor":               false,
	})
	webForm := url.Values{
		"fb_api_req_friendly_name": {"CometSinglePostContentQuery"},
		"doc_id":                   {"8362454010438212"},
		"variables":                {string(webVars)},
	}

	webBody, err := fbGraphqlPost(ctx, webForm)
	if err != nil {
		return MediaResult{}, fmt.Errorf("web media query: %w", err)
	}

	webData := fbParseResponse(webBody)
	return fbParseWebMedia(webData, postID), nil
}

// fbParsePreviewMedia extracts video info from the link preview response (reels/shorts).
func fbParsePreviewMedia(data gjson.Result) MediaResult {
	base := "data.link_preview.story_attachment"
	title := data.Get(base + ".title").String()
	videoBase := base + ".style_infos.0.fb_shorts_story.short_form_video_context.video"

	hdURL := data.Get(videoBase + ".original_download_url_hd").String()
	sdURL := data.Get(videoBase + ".original_download_url_sd").String()

	finalURL := hdURL
	if finalURL == "" {
		finalURL = sdURL
	}
	if finalURL == "" {
		return MediaResult{Message: title}
	}

	return MediaResult{
		Message: title,
		Items:   []MediaItem{{Type: Video, URL: finalURL}},
	}
}

// fbParseMobileMedia extracts media items from the mobile media query response.
func fbParseMobileMedia(data gjson.Result, postID string) MediaResult {
	message := data.Get("data.reduced_node.message.text").String()

	var items []MediaItem
	data.Get("data.mediaset.media.edges").ForEach(func(_, edge gjson.Result) bool {
		node := edge.Get("node")
		typename := node.Get("__typename").String()
		if typename == "Photo" {
			if uri := node.Get("image.uri").String(); uri != "" {
				items = append(items, MediaItem{Type: Image, URL: uri})
			}
		} else {
			// Video
			u := node.Get("hd_playable_url").String()
			if u == "" {
				u = node.Get("playable_url").String()
			}
			if u != "" {
				items = append(items, MediaItem{Type: Video, URL: u})
			}
		}
		return true
	})

	return MediaResult{Message: message, Items: items}
}

// fbParseWebMedia extracts media from the CometSinglePostContentQuery response.
func fbParseWebMedia(data gjson.Result, postID string) MediaResult {
	// Navigate to the story content
	content := data.Get("data.node.comet_sections.content.story")
	if !content.Exists() {
		// Try array response
		content = data.Get("0.data.node.comet_sections.content.story")
	}
	if !content.Exists() {
		return MediaResult{}
	}

	message := content.Get("message.text").String()

	// Try multiple attachment paths
	attachment := content.Get("attachments.0.styles.attachment")
	if !attachment.Exists() {
		attachment = content.Get("attached_story.attachments.0.styles.attachment")
	}
	if !attachment.Exists() {
		attachment = content.Get("comet_sections.attached_story.story.attached_story.comet_sections.attached_story_layout.story.attachments.0.styles.attachment")
	}

	if !attachment.Exists() {
		return MediaResult{Message: message}
	}

	var items []MediaItem

	// Case 1: Subattachments (multiple shared media)
	if attachment.Get("subattachments").Exists() {
		attachment.Get("subattachments").ForEach(func(_, sub gjson.Result) bool {
			media := sub.Get("multi_share_media_card_renderer.attachment.media")
			if media.Get("__typename").String() == "GenericAttachmentMedia" {
				return true
			}
			typename := media.Get("__typename").String()
			if typename == "Photo" {
				if uri := media.Get("image.uri").String(); uri != "" {
					items = append(items, MediaItem{Type: Image, URL: uri})
				}
			} else {
				u := media.Get("browser_native_hd_url").String()
				if u == "" {
					u = media.Get("browser_native_sd_url").String()
				}
				if u != "" {
					items = append(items, MediaItem{Type: Video, URL: u})
				}
			}
			return true
		})
		return MediaResult{Message: message, Items: items}
	}

	// Case 2: Single media
	if attachment.Get("media").Exists() {
		media := attachment.Get("media")
		typename := media.Get("__typename").String()
		if typename == "Photo" {
			uri := media.Get("photo_image.uri").String()
			if uri == "" {
				uri = media.Get("image.uri").String()
			}
			if uri != "" {
				items = append(items, MediaItem{Type: Image, URL: uri})
			}
		} else {
			u := media.Get("browser_native_hd_url").String()
			if u == "" {
				u = media.Get("browser_native_sd_url").String()
			}
			if u != "" {
				items = append(items, MediaItem{Type: Video, URL: u})
			}
		}
		return MediaResult{Message: message, Items: items}
	}

	// Case 3: Style infos (shorts/reels)
	if attachment.Get("style_infos").Exists() {
		videoBase := "style_infos.0.fb_shorts_story.short_form_video_context.playback_video"
		u := attachment.Get(videoBase + ".browser_native_hd_url").String()
		if u == "" {
			u = attachment.Get(videoBase + ".browser_native_sd_url").String()
		}
		if u != "" {
			items = append(items, MediaItem{Type: Video, URL: u})
		}
		if message == "" {
			message = attachment.Get("style_infos.0.fb_shorts_story.message.text").String()
		}
		return MediaResult{Message: message, Items: items}
	}

	return MediaResult{Message: message}
}

// ── Legacy HTML scraping fallback ───────────────────────────────────────────

var legacyFBHeaders = map[string]string{
	"sec-fetch-user":            "?1",
	"sec-ch-ua-mobile":          "?0",
	"sec-fetch-site":            "none",
	"sec-fetch-dest":            "document",
	"sec-fetch-mode":            "navigate",
	"cache-control":             "max-age=0",
	"upgrade-insecure-requests": "1",
	"accept-language":           "en-GB,en;q=0.9",
	"user-agent":                UserAgent,
	"accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
}

func legacyFBMedia(ctx context.Context, rawURL string) ([]MediaItem, error) {
	// Resolve share links
	if strings.Contains(rawURL, "/share/") {
		resolveReq, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create redirect request: %w", err)
		}
		resolveReq.Header.Set("User-Agent", UserAgent)
		resolveResp, err := httpClient.Do(resolveReq)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve share url: %w", err)
		}
		io.Copy(io.Discard, resolveResp.Body)
		resolveResp.Body.Close()
		rawURL = resolveResp.Request.URL.String()
	}

	var lastErr error
	for i := 0; i < 10; i++ {
		if i > 0 {
			backoff := time.Duration(i) * 200 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		items, err := doLegacyFBRequest(ctx, rawURL)
		if err != nil {
			lastErr = err
			continue
		}
		return items, nil
	}
	return nil, fmt.Errorf("facebook media failed after 10 retries: %w", lastErr)
}

func doLegacyFBRequest(ctx context.Context, rawURL string) ([]MediaItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range legacyFBHeaders {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch facebook url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("facebook returned status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	data := string(bodyBytes)
	bodyBytes = nil //nolint:ineffassign

	data = strings.ReplaceAll(data, "&quot;", "\"")
	data = strings.ReplaceAll(data, "&amp;", "&")

	parseStr := func(s string) string {
		return strings.ReplaceAll(s, `\/`, `/`)
	}

	// Try video URLs
	var sdURL string
	if match := sdURLRegex.FindStringSubmatch(data); len(match) > 1 {
		sdURL = match[1]
	} else if match := playableURLRegex.FindStringSubmatch(data); len(match) > 1 {
		sdURL = match[1]
	} else if match := sdSrcRegex.FindStringSubmatch(data); len(match) > 1 {
		sdURL = match[1]
	} else if match := srcRegex.FindStringSubmatch(data); len(match) > 1 {
		sdURL = match[1]
	}

	var hdURL string
	if match := hdURLRegex.FindStringSubmatch(data); len(match) > 1 {
		hdURL = match[1]
	} else if match := playableHDURLRegex.FindStringSubmatch(data); len(match) > 1 {
		hdURL = match[1]
	} else if match := hdSrcRegex.FindStringSubmatch(data); len(match) > 1 {
		hdURL = match[1]
	}

	if sdURL != "" || hdURL != "" {
		finalURL := sdURL
		if hdURL != "" {
			finalURL = hdURL
		}
		return []MediaItem{{Type: Video, URL: parseStr(finalURL)}}, nil
	}

	// Fallback: post images
	return extractFacebookPostImages(data, parseStr)
}

func extractFacebookPostImages(data string, parseStr func(string) string) ([]MediaItem, error) {
	seen := make(map[string]bool)
	var items []MediaItem
	for _, re := range []*regexp.Regexp{imageURIRegex, ogImageRegex} {
		for _, match := range re.FindAllStringSubmatch(data, -1) {
			if len(match) > 1 {
				imgURL := parseStr(match[1])
				if !seen[imgURL] {
					seen[imgURL] = true
					items = append(items, MediaItem{Type: Image, URL: imgURL})
				}
			}
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no media found in facebook post")
	}
	return items, nil
}

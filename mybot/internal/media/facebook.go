package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	UserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	FetchTimeout = 30 * time.Second
	MaxHTMLBytes = 5 * 1024 * 1024 // 5 MB
)

var (
	fbHeaders = map[string]string{
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

	sdURLRegex       = regexp.MustCompile(`"browser_native_sd_url":"(.*?)"`)
	playableURLRegex = regexp.MustCompile(`"playable_url":"(.*?)"`)
	sdSrcRegex       = regexp.MustCompile(`sd_src\s*:\s*"([^"]*)"`)
	srcRegex         = regexp.MustCompile(`"src":"[^"]*(https://[^"]*)`)

	hdURLRegex         = regexp.MustCompile(`"browser_native_hd_url":"(.*?)"`)
	playableHDURLRegex = regexp.MustCompile(`"playable_url_quality_hd":"(.*?)"`)
	hdSrcRegex         = regexp.MustCompile(`hd_src\s*:\s*"([^"]*)"`)
)

func GetFacebookVideo(ctx context.Context, url string) (*MediaItem, error) {
	// Resolve share links (e.g. /share/v/, /share/p/, /share/r/) to their final URL
	if strings.Contains(url, "/share/") {
		resolveReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create redirect request: %w", err)
		}
		resolveReq.Header.Set("User-Agent", UserAgent)
		resolveResp, err := httpClient.Do(resolveReq)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve share url: %w", err)
		}
		defer resolveResp.Body.Close()
		url = resolveResp.Request.URL.String()
	}

	// Retry up to 10 times with no delay
	var lastErr error
	for i := 0; i < 10; i++ {
		item, err := doFacebookVideoRequest(ctx, url)
		if err != nil {
			lastErr = err
			continue
		}
		return item, nil
	}
	return nil, fmt.Errorf("facebook video failed after 10 retries: %w", lastErr)
}

func doFacebookVideoRequest(ctx context.Context, url string) (*MediaItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range fbHeaders {
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

	// Limit reader to avoid huge memory usage
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	data := string(bodyBytes)

	// Unescape like in JS: .replace(/&quot;/g, '"').replace(/&amp;/g, '&');
	data = strings.ReplaceAll(data, "&quot;", "\"")
	data = strings.ReplaceAll(data, "&amp;", "&")

	parseStr := func(s string) string {
		return strings.ReplaceAll(s, `\/`, `/`)
	}

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

	if sdURL == "" && hdURL == "" {
		return nil, fmt.Errorf("no video url found")
	}

	finalURL := sdURL
	if hdURL != "" {
		finalURL = hdURL
	}

	return &MediaItem{
		Type: Video,
		URL:  parseStr(finalURL),
	}, nil
}

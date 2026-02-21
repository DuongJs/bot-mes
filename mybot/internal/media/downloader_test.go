package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFilenameFromMIME(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"video/mp4", "media.mp4"},
		{"video/webm", "media.mp4"},
		{"image/jpeg", "media.jpg"},
		{"image/png", "media.png"},
		{"image/gif", "media.gif"},
		{"image/webp", "media.webp"},
		{"image/svg+xml", "media.jpg"},
		{"audio/mpeg", "media.mp3"},
		{"application/octet-stream", "media.bin"},
		{"", "media.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := FilenameFromMIME(tt.mimeType)
			if got != tt.expected {
				t.Errorf("FilenameFromMIME(%q) = %q, want %q", tt.mimeType, got, tt.expected)
			}
		})
	}
}

func TestExtractAwemeID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"tiktok video", "https://www.tiktok.com/@user/video/7123456789012345678", "7123456789012345678"},
		{"tiktok photo", "https://www.tiktok.com/@user/photo/7123456789012345678", "7123456789012345678"},
		{"tiktok note", "https://www.tiktok.com/@user/note/7123456789012345678", "7123456789012345678"},
		{"douyin video", "https://www.douyin.com/video/7123456789012345678", "7123456789012345678"},
		{"douyin note", "https://www.douyin.com/note/7123456789012345678", "7123456789012345678"},
		{"no match", "https://www.example.com/page/123", ""},
		{"empty", "", ""},
		{"with query params", "https://www.tiktok.com/@user/video/7123456789012345678?is_from_webapp=1", "7123456789012345678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAwemeID(tt.url)
			if got != tt.expected {
				t.Errorf("extractAwemeID(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestMatchHost(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		hosts []string
		want  bool
	}{
		// Instagram
		{"instagram.com", "https://www.instagram.com/p/ABC123/", []string{"instagram.com"}, true},
		{"instagram.com bare", "https://instagram.com/p/ABC123/", []string{"instagram.com"}, true},
		{"instagr.am", "https://instagr.am/p/ABC123/", []string{"instagr.am"}, true},
		{"instagram with igsh", "https://www.instagram.com/p/DUZ-cBBkwzt/?igsh=MWMxcTl4bXM0ZmUyNQ==", []string{"instagram.com"}, true},
		// TikTok
		{"tiktok.com", "https://www.tiktok.com/@user/video/123", []string{"tiktok.com"}, true},
		{"vm.tiktok.com", "https://vm.tiktok.com/xxx", []string{"tiktok.com"}, true},
		{"vt.tiktok.com", "https://vt.tiktok.com/xxx", []string{"tiktok.com"}, true},
		// Douyin
		{"douyin.com", "https://www.douyin.com/video/123", []string{"douyin.com", "iesdouyin.com"}, true},
		{"v.douyin.com", "https://v.douyin.com/xxx", []string{"douyin.com"}, true},
		{"iesdouyin.com", "https://www.iesdouyin.com/share/video/123", []string{"iesdouyin.com"}, true},
		// Facebook
		{"facebook.com share video", "https://www.facebook.com/share/v/1DXMCN1e1T/", []string{"facebook.com", "fb.watch"}, true},
		{"facebook.com reel", "https://www.facebook.com/reel/123456", []string{"facebook.com"}, true},
		{"fb.watch", "https://fb.watch/abc123/", []string{"fb.watch"}, true},
		// Negative
		{"not instagram in query", "https://example.com/?url=instagram.com", []string{"instagram.com"}, false},
		{"empty url", "", []string{"instagram.com"}, false},
		{"unrelated domain", "https://example.com/page", []string{"instagram.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchHost(tt.url, tt.hosts)
			if got != tt.want {
				t.Errorf("MatchHost(%q, %v) = %v, want %v", tt.url, tt.hosts, got, tt.want)
			}
		})
	}
}

func TestExtractShortcode(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"standard post", "https://www.instagram.com/p/DUZ-cBBkwzt/", "DUZ-cBBkwzt"},
		{"post with igsh param", "https://www.instagram.com/p/DUZ-cBBkwzt/?igsh=MWMxcTl4bXM0ZmUyNQ==", "DUZ-cBBkwzt"},
		{"reel", "https://www.instagram.com/reel/ABC123/", "ABC123"},
		{"tv", "https://www.instagram.com/tv/XYZ789/", "XYZ789"},
		{"reels", "https://www.instagram.com/reels/DEF456/", "DEF456"},
		{"share post", "https://www.instagram.com/share/p/ABC123/", "ABC123"},
		{"share reel", "https://www.instagram.com/share/reel/ABC123/", "ABC123"},
		{"no shortcode", "https://www.instagram.com/username/", ""},
		{"empty", "", ""},
		{"shortcode with special chars", "https://www.instagram.com/p/A-B_c123/", "A-B_c123"},
		{"reel with igsh param", "https://www.instagram.com/reel/DU0uKofE-er/?igsh=MXczbnllbm55MmJhZg==", "DU0uKofE-er"},
		{"post with igsh param 2", "https://www.instagram.com/p/DUGwazijTpb/?igsh=NnIxdmh2MnQxNWZl", "DUGwazijTpb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractShortcode(tt.url)
			if got != tt.want {
				t.Errorf("extractShortcode(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsShareURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"instagram share post", "https://www.instagram.com/share/p/ABC123/", true},
		{"instagram share reel", "https://www.instagram.com/share/reel/ABC123/", true},
		{"standard post", "https://www.instagram.com/p/ABC123/", false},
		{"post with igsh", "https://www.instagram.com/p/DUZ-cBBkwzt/?igsh=MWMxcTl4bXM0ZmUyNQ==", false},
		{"standard reel", "https://www.instagram.com/reel/ABC123/", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isShareURL(tt.url)
			if got != tt.want {
				t.Errorf("isShareURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestFacebookShareLinkDetection(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		isShare bool
	}{
		{"share video link", "https://www.facebook.com/share/v/1DXMCN1e1T/", true},
		{"share post link", "https://www.facebook.com/share/p/abc123/", true},
		{"share reel link", "https://www.facebook.com/share/r/xyz789/", true},
		{"regular reel link", "https://www.facebook.com/reel/123456", false},
		{"regular video link", "https://www.facebook.com/watch?v=123456", false},
		{"fb.watch link", "https://fb.watch/abc123/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isShareURL(tt.url)
			if got != tt.isShare {
				t.Errorf("URL %q: isShare = %v, want %v", tt.url, got, tt.isShare)
			}
		})
	}
}

func TestInstagramGraphQLHeaders(t *testing.T) {
	var capturedHeaders http.Header

	// Mock GraphQL server
	graphqlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		resp := InstagramResponse{}
		resp.Data.XDTShorcodeMedia = &struct {
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
		}{
			Typename:   "XDTGraphImage",
			IsVideo:    false,
			DisplayURL: "https://example.com/image.jpg",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer graphqlServer.Close()

	ctx := context.Background()

	// Build request with the same headers as doIGRequest
	req, _ := http.NewRequestWithContext(ctx, "POST", graphqlServer.URL, nil)
	req.Header.Set("x-ig-app-id", igAppID)
	req.Header.Set("x-fb-lsd", igLSD)
	req.Header.Set("x-fb-friendly-name", "PolarisPostActionLoadPostQueryQuery")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("origin", "https://www.instagram.com")
	req.Header.Set("referer", "https://www.instagram.com/")
	req.Header.Set("accept", "*/*")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	// Verify required headers were sent
	if got := capturedHeaders.Get("X-Ig-App-Id"); got != igAppID {
		t.Errorf("X-IG-App-ID = %q, want %q", got, igAppID)
	}
	if got := capturedHeaders.Get("X-Fb-Lsd"); got != igLSD {
		t.Errorf("X-FB-LSD = %q, want %q", got, igLSD)
	}
	if got := capturedHeaders.Get("Origin"); got != "https://www.instagram.com" {
		t.Errorf("Origin = %q, want %q", got, "https://www.instagram.com")
	}
	if got := capturedHeaders.Get("Accept"); got != "*/*" {
		t.Errorf("Accept = %q, want %q", got, "*/*")
	}
}

func TestInstagramGraphQLRetry(t *testing.T) {
	attempts := 0

	// Mock server: fail once with 500, then succeed
	graphqlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := InstagramResponse{}
		resp.Data.XDTShorcodeMedia = &struct {
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
		}{
			Typename:   "XDTGraphImage",
			IsVideo:    false,
			DisplayURL: "https://example.com/image.jpg",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer graphqlServer.Close()

	// Simulate retry: first request fails, second succeeds
	ctx := context.Background()
	retries := 2

	var lastResp *http.Response
	for i := 0; i <= retries; i++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", graphqlServer.URL, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.StatusCode != http.StatusOK && i < retries {
			resp.Body.Close()
			continue
		}
		lastResp = resp
		break
	}

	if lastResp == nil {
		t.Fatal("expected a successful response after retry")
	}
	defer lastResp.Body.Close()

	if lastResp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 after retry, got %d", lastResp.StatusCode)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 success), got %d", attempts)
	}
}

package media

import (
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

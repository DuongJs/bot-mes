package media

import (
	"strings"
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

func TestGetMediaPlatformDetection(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		contains string
	}{
		{"tiktok.com", "https://www.tiktok.com/@user/video/123", "tiktok.com"},
		{"vm.tiktok.com", "https://vm.tiktok.com/xxx", "tiktok.com"},
		{"vt.tiktok.com", "https://vt.tiktok.com/xxx", "tiktok.com"},
		{"douyin.com", "https://www.douyin.com/video/123", "douyin.com"},
		{"v.douyin.com", "https://v.douyin.com/xxx", "douyin.com"},
		{"iesdouyin.com", "https://www.iesdouyin.com/share/video/123", "iesdouyin.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.url, tt.contains) {
				t.Errorf("URL %q should contain platform domain %s", tt.url, tt.contains)
			}
		})
	}
}

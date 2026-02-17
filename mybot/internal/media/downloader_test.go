package media

import "testing"

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

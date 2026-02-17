package facebook

import "testing"

func TestMaxUploadSize(t *testing.T) {
	// maxUploadSize must match Facebook's documented 25 MB limit
	if maxUploadSize != 25*1000*1000 {
		t.Fatalf("expected maxUploadSize = 25000000, got %d", maxUploadSize)
	}
}

func TestSendMediaRejectsTooLargeFile(t *testing.T) {
	c := &Client{} // nil messagix client – should never be reached
	data := make([]byte, maxUploadSize+1)
	err := c.SendMedia(nil, 0, data, "video.mp4", "video/mp4")
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	want := "file too large"
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("expected error starting with %q, got %q", want, got)
	}
}

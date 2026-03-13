package messagix

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// buildMercuryMediaPayload tests
// ---------------------------------------------------------------------------

// parseParts parses a raw multipart body back into a map of
// part-name → {header, body} for easy assertions.
func parseParts(t *testing.T, body []byte, contentType string) map[string]struct {
	Header map[string]string
	Body   []byte
} {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	boundary := params["boundary"]
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	result := map[string]struct {
		Header map[string]string
		Body   []byte
	}{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		cd := p.Header.Get("Content-Disposition")
		name := p.FormName()
		if name == "" {
			// Parse name from Content-Disposition for non-standard parts
			_, cdParams, _ := mime.ParseMediaType(cd)
			name = cdParams["name"]
		}
		body, _ := io.ReadAll(p)
		result[name] = struct {
			Header map[string]string
			Body   []byte
		}{
			Header: map[string]string{
				"Content-Disposition": cd,
				"Content-Type":        p.Header.Get("Content-Type"),
			},
			Body: body,
		}
	}
	return result
}

func TestBuildMercuryMediaPayload_MediaData(t *testing.T) {
	fileData := []byte("fake video bytes")
	media := &MercuryUploadMedia{
		Filename:  "media.mp4",
		MimeType:  "video/mp4",
		MediaData: fileData,
	}

	payload, ct, err := buildMercuryMediaPayload(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("wrong content-type: %s", ct)
	}

	parts := parseParts(t, payload, ct)

	farr, ok := parts["farr"]
	if !ok {
		t.Fatalf("expected multipart field 'farr' not found; got fields: %v", keys(parts))
	}
	if !bytes.Equal(farr.Body, fileData) {
		t.Errorf("field 'farr' body mismatch: got %q, want %q", farr.Body, fileData)
	}
	if farr.Header["Content-Type"] != "video/mp4" {
		t.Errorf("Content-Type: got %q, want %q", farr.Header["Content-Type"], "video/mp4")
	}
	if !strings.Contains(farr.Header["Content-Disposition"], `filename="media.mp4"`) {
		t.Errorf("filename not in Content-Disposition: %s", farr.Header["Content-Disposition"])
	}
	// voice_clip field must NOT be present for regular files
	if _, has := parts["voice_clip"]; has {
		t.Error("unexpected 'voice_clip' field for non-voice upload")
	}
}

func TestBuildMercuryMediaPayload_MediaReader(t *testing.T) {
	fileData := []byte("streamed audio data")
	media := &MercuryUploadMedia{
		Filename:    "voice.m4a",
		MimeType:    "audio/mp4",
		MediaReader: bytes.NewReader(fileData),
	}

	payload, ct, err := buildMercuryMediaPayload(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parts := parseParts(t, payload, ct)
	farr, ok := parts["farr"]
	if !ok {
		t.Fatalf("expected multipart field 'farr' not found")
	}
	if !bytes.Equal(farr.Body, fileData) {
		t.Errorf("streaming reader body mismatch: got %q, want %q", farr.Body, fileData)
	}
}

func TestBuildMercuryMediaPayload_VoiceClip(t *testing.T) {
	media := &MercuryUploadMedia{
		Filename:    "voice.m4a",
		MimeType:    "audio/mp4",
		MediaData:   []byte("audio"),
		IsVoiceClip: true,
	}

	payload, ct, err := buildMercuryMediaPayload(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parts := parseParts(t, payload, ct)
	vc, ok := parts["voice_clip"]
	if !ok {
		t.Fatal("expected 'voice_clip' field for voice upload")
	}
	if string(vc.Body) != "true" {
		t.Errorf("voice_clip value: got %q, want %q", vc.Body, "true")
	}
	// waveform field must NOT be present when WaveformData is nil
	if _, has := parts["voice_clip_waveform_data"]; has {
		t.Error("unexpected 'voice_clip_waveform_data' field when WaveformData is nil")
	}
}

func TestBuildMercuryMediaPayload_VoiceClipWithWaveform(t *testing.T) {
	waveform := &WaveformData{
		Amplitudes:        []float64{0.1, 0.5, 0.9, 0.3},
		SamplingFrequency: 100,
	}
	media := &MercuryUploadMedia{
		Filename:     "voice.m4a",
		MimeType:     "audio/mp4",
		MediaData:    []byte("audio"),
		IsVoiceClip:  true,
		WaveformData: waveform,
	}

	payload, ct, err := buildMercuryMediaPayload(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parts := parseParts(t, payload, ct)
	wfd, ok := parts["voice_clip_waveform_data"]
	if !ok {
		t.Fatal("expected 'voice_clip_waveform_data' field")
	}
	var decoded WaveformData
	if err := json.Unmarshal(wfd.Body, &decoded); err != nil {
		t.Fatalf("waveform JSON unmarshal: %v", err)
	}
	if decoded.SamplingFrequency != 100 {
		t.Errorf("SamplingFrequency: got %d, want 100", decoded.SamplingFrequency)
	}
	if len(decoded.Amplitudes) != 4 {
		t.Errorf("Amplitudes length: got %d, want 4", len(decoded.Amplitudes))
	}
}

// ---------------------------------------------------------------------------
// buildMercuryAnalyticsTags tests
// ---------------------------------------------------------------------------

func TestBuildMercuryAnalyticsTags_DefaultProductID(t *testing.T) {
	tag := buildMercuryAnalyticsTags(MercuryUploadProductID)

	if !json.Valid([]byte(tag)) {
		t.Fatalf("analytics tag is not valid JSON: %s", tag)
	}
	if !strings.Contains(tag, MercuryUploadProductID) {
		t.Errorf("product ID %q not found in tag: %s", MercuryUploadProductID, tag)
	}
	if !strings.Contains(tag, "graphservice") {
		t.Errorf("expected application_tags=graphservice in: %s", tag)
	}
	if !strings.Contains(tag, "graphql") {
		t.Errorf("expected request_category=graphql in: %s", tag)
	}
}

func TestBuildMercuryAnalyticsTags_CustomProductID(t *testing.T) {
	custom := "999000111222333"
	tag := buildMercuryAnalyticsTags(custom)

	if !json.Valid([]byte(tag)) {
		t.Fatalf("analytics tag with custom product is not valid JSON: %s", tag)
	}
	if !strings.Contains(tag, custom) {
		t.Errorf("custom product ID %q not found in tag: %s", custom, tag)
	}
}

// ---------------------------------------------------------------------------
// Package-level var override tests
// ---------------------------------------------------------------------------

func TestMercuryUploadAsbdID_Overrideable(t *testing.T) {
	original := MercuryUploadAsbdID
	defer func() { MercuryUploadAsbdID = original }()

	MercuryUploadAsbdID = "999999"
	if MercuryUploadAsbdID != "999999" {
		t.Error("MercuryUploadAsbdID override failed")
	}
}

func TestDefaultUploadTimeout_Overrideable(t *testing.T) {
	original := DefaultUploadTimeout
	defer func() { DefaultUploadTimeout = original }()

	import_time_used_above := "already imported"
	_ = import_time_used_above
	DefaultUploadTimeout = 30_000_000_000 // 30s in nanoseconds
	if DefaultUploadTimeout == original {
		t.Error("DefaultUploadTimeout override failed")
	}
}

func TestMercuryUploadMedia_TimeoutField(t *testing.T) {
	m := &MercuryUploadMedia{Timeout: 45_000_000_000}
	if m.Timeout <= 0 {
		t.Error("Timeout field not set")
	}
}

// keys returns the map keys for error messages.
func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

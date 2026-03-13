package messagix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/rs/zerolog"
	"go.mau.fi/util/random"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

// MercuryUploadAsbdID is the x-asbd-id header value sent with Mercury upload
// requests. It differs from the default web value (129477) and identifies the
// upload product surface on Facebook's infrastructure. Can be overridden at
// runtime if Facebook changes it without a code deploy.
var MercuryUploadAsbdID = "359341"

// MercuryUploadProductID is the product identifier embedded in the
// x-fb-request-analytics-tags header for Mercury uploads.
var MercuryUploadProductID = "256002347743983"

// DefaultUploadTimeout is the per-upload context deadline used when
// MercuryUploadMedia.Timeout is zero. Videos and large files frequently need
// more than the default 60 s global HTTP timeout.
var DefaultUploadTimeout = 120 * time.Second

type MercuryUploadMedia struct {
	Filename  string
	MimeType  string
	MediaData []byte // legacy: in-memory data

	// MediaReader, if set, is used instead of MediaData to stream file
	// content directly into the multipart body.  This avoids holding the
	// entire file in a separate []byte while building the upload payload.
	MediaReader io.Reader

	IsVoiceClip  bool
	WaveformData *WaveformData

	// Timeout overrides DefaultUploadTimeout for this specific upload.
	// Leave zero to use DefaultUploadTimeout.
	Timeout time.Duration
}

type WaveformData struct {
	Amplitudes        []float64 `json:"amplitudes"`
	SamplingFrequency int       `json:"sampling_frequency"`
}

func (c *Client) SendMercuryUploadRequest(ctx context.Context, threadID int64, media *MercuryUploadMedia) (*types.MercuryUploadResponse, error) {
	if c == nil {
		return nil, ErrClientIsNil
	}

	// Extend the context deadline for uploads (videos can be large).
	// Honour a per-upload override if provided, otherwise use the package default.
	timeout := media.Timeout
	if timeout <= 0 {
		timeout = DefaultUploadTimeout
	}
	uploadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	urlQueries := c.newHTTPQuery()
	queryValues, err := query.Values(urlQueries)
	if err != nil {
		return nil, fmt.Errorf("failed to convert HttpQuery into query.Values for mercury upload: %w", err)
	}

	// __ccg comes from the session's WebConnectionClassServerGuess (set in
	// newHTTPQuery via Ccg field).  Fall back to "EXCELLENT" only when the
	// session hasn't populated the value yet.
	if queryValues.Get("__ccg") == "" {
		queryValues.Set("__ccg", "EXCELLENT")
	}

	payloadQuery := queryValues.Encode()
	url := c.GetEndpoint("media_upload") + payloadQuery
	payload, contentType, err := buildMercuryMediaPayload(media)
	if err != nil {
		return nil, err
	}
	h := c.buildHeaders(true, false)
	h.Set("accept", "*/*")
	h.Set("content-type", contentType)
	h.Set("origin", c.GetEndpoint("base_url"))
	h.Set("referer", c.getEndpointForThreadID(threadID))
	h.Set("priority", "u=1, i")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-origin") // header is required

	// Headers learned from JS FCA (fca-unofficial) that improve success rate.
	// MercuryUploadAsbdID and MercuryUploadProductID are package-level vars
	// so they can be patched without a code deploy if Facebook changes them.
	if c.configs != nil && c.configs.LSDToken != "" {
		h.Set("x-fb-lsd", c.configs.LSDToken)
	}
	h.Set("x-fb-friendly-name", "MercuryUpload")
	h.Set("x-asbd-id", MercuryUploadAsbdID)
	h.Set("x-fb-request-analytics-tags", buildMercuryAnalyticsTags(MercuryUploadProductID))

	var attempts int
	for {
		attempts += 1
		_, respBody, err := c.MakeRequest(uploadCtx, url, "POST", h, payload, types.NONE)
		if err != nil {
			// MakeRequest retries itself, so bail immediately if that fails
			return nil, fmt.Errorf("failed to send MercuryUploadRequest: %w", err)
		}
		resp, err := c.parseMercuryResponse(uploadCtx, respBody)
		if err == nil {
			return resp, nil
		} else if attempts > MaxHTTPRetries {
			return nil, err
		}
		c.Logger.Err(err).
			Str("url", sanitizeURLForLog(url)).
			Msg("Mercury response parsing failed, retrying")
		time.Sleep(time.Duration(attempts) * 3 * time.Second)
	}
}

var antiJSPrefix = []byte("for (;;);")

func (c *Client) parseMercuryResponse(ctx context.Context, respBody []byte) (*types.MercuryUploadResponse, error) {
	jsonData := bytes.TrimPrefix(respBody, antiJSPrefix)

	if json.Valid(jsonData) {
		zerolog.Ctx(ctx).Trace().RawJSON("response_body", jsonData).Msg("Mercury upload response")
	} else {
		zerolog.Ctx(ctx).Debug().Bytes("response_body", respBody).Msg("Mercury upload response (invalid JSON)")
	}

	var mercuryResponse *types.MercuryUploadResponse
	if err := json.Unmarshal(jsonData, &mercuryResponse); err != nil {
		return nil, fmt.Errorf("failed to parse mercury response: %w", err)
	} else if mercuryResponse.ErrorCode != 0 {
		return nil, fmt.Errorf("error in mercury upload: %w", &mercuryResponse.ErrorResponse)
	}
	mercuryResponse.Raw = jsonData

	err := c.parseMetadata(mercuryResponse)
	if err != nil {
		zerolog.Ctx(ctx).Debug().RawJSON("response_body", jsonData).Msg("Mercury upload response with unrecognized data")
		return nil, err
	}

	return mercuryResponse, nil
}

func (c *Client) parseMetadata(response *types.MercuryUploadResponse) error {
	if len(response.Payload.Metadata) == 0 {
		return fmt.Errorf("no metadata in upload response")
	}

	switch response.Payload.Metadata[0] {
	case '[':
		var realMetadata []types.FileMetadata
		err := json.Unmarshal(response.Payload.Metadata, &realMetadata)
		if err != nil {
			return fmt.Errorf("failed to unmarshal image metadata in upload response: %w", err)
		}
		response.Payload.RealMetadata = &realMetadata[0]
	case '{':
		var realMetadata map[string]types.FileMetadata
		err := json.Unmarshal(response.Payload.Metadata, &realMetadata)
		if err != nil {
			return fmt.Errorf("failed to unmarshal video metadata in upload response: %w", err)
		}
		realMetaEntry := realMetadata["0"]
		response.Payload.RealMetadata = &realMetaEntry
	default:
		return fmt.Errorf("unexpected metadata in upload response")
	}

	return nil
}

// buildMercuryAnalyticsTags constructs the x-fb-request-analytics-tags header
// value for a Mercury upload request. productID is MercuryUploadProductID by
// default, but can be overridden by callers that supply a different product surface.
func buildMercuryAnalyticsTags(productID string) string {
	tags := RequestAnalytics{
		NetworkTags: NetworkTags{
			Product:         productID,
			Purpose:         "none",
			RequestCategory: "graphql",
			RetryAttempt:    "0",
		},
	}
	// application_tags cannot be expressed through the struct above; marshal
	// the struct and then inject the extra field manually to avoid a wrapper type.
	inner, _ := json.Marshal(tags)
	// Trim trailing } and append the application_tags field.
	return string(inner[:len(inner)-1]) + `,"application_tags":"graphservice"}`
}

// buildMercuryMediaPayload constructs the multipart/form-data body for a
// Mercury upload. It is a standalone function (no Client receiver) so it can
// be unit-tested without a live session.
//
// returns payloadBytes, multipart content-type header
func buildMercuryMediaPayload(media *MercuryUploadMedia) ([]byte, string, error) {
	var mercuryPayload bytes.Buffer
	writer := multipart.NewWriter(&mercuryPayload)

	err := writer.SetBoundary("----WebKitFormBoundary" + random.String(16))
	if err != nil {
		return nil, "", fmt.Errorf("messagix-mercury: Failed to set boundary (%w)", err)
	}

	if media.IsVoiceClip {
		err = writer.WriteField("voice_clip", "true")
		if err != nil {
			return nil, "", fmt.Errorf("messagix-mercury: Failed to write voice_clip field (%w)", err)
		}

		if media.WaveformData != nil {
			waveformBytes, err := json.Marshal(media.WaveformData)
			if err != nil {
				return nil, "", fmt.Errorf("messagix-mercury: Failed to marshal waveform (%w)", err)
			}

			err = writer.WriteField("voice_clip_waveform_data", string(waveformBytes))
			if err != nil {
				return nil, "", fmt.Errorf("messagix-mercury: Failed to write waveform field (%w)", err)
			}
		}
	}

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="farr"; filename="%s"`, media.Filename))
	partHeader.Set("Content-Type", media.MimeType)

	mediaPart, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, "", fmt.Errorf("messagix-mercury: Failed to create multipart writer (%w)", err)
	}

	if media.MediaReader != nil {
		_, err = io.Copy(mediaPart, media.MediaReader)
	} else {
		_, err = mediaPart.Write(media.MediaData)
	}
	if err != nil {
		return nil, "", fmt.Errorf("messagix-mercury: Failed to write data to multipart section (%w)", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, "", fmt.Errorf("messagix-mercury: Failed to close multipart writer (%w)", err)
	}

	return mercuryPayload.Bytes(), writer.FormDataContentType(), nil
}

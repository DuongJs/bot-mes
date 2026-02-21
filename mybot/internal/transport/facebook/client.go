package facebook

import (
	"context"
	"fmt"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"mybot/internal/core"
)

type Client struct {
	client *messagix.Client
	selfID int64
}

// Ensure Client implements MessageSender
var _ core.MessageSender = (*Client)(nil)

func NewClient(client *messagix.Client, selfID int64) *Client {
	return &Client{
		client: client,
		selfID: selfID,
	}
}

const maxRetries = 10

func (c *Client) SendMessage(ctx context.Context, threadID int64, text string) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		task := &socket.SendMessageTask{
			ThreadId:  threadID,
			Text:      text,
			Source:    table.MESSENGER_INBOX_IN_THREAD,
			SendType:  table.TEXT,
			SyncGroup: 1,
			Otid:      methods.GenerateEpochID(),
		}
		_, lastErr = c.client.ExecuteTask(ctx, task)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

const maxUploadSize = 25 * 1000 * 1000 // 25 MB – Facebook's Mercury upload limit

func (c *Client) SendMedia(ctx context.Context, threadID int64, data []byte, filename, mimeType string) error {
	if len(data) > maxUploadSize {
		return fmt.Errorf("file too large (%d bytes, max %d)", len(data), maxUploadSize)
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		uploadResp, err := c.client.SendMercuryUploadRequest(ctx, threadID, &messagix.MercuryUploadMedia{
			Filename:  filename,
			MimeType:  mimeType,
			MediaData: data,
		})
		if err != nil {
			lastErr = fmt.Errorf("upload failed: %w", err)
			continue
		}

		var realFBID int64
		if uploadResp.Payload.RealMetadata != nil {
			realFBID = uploadResp.Payload.RealMetadata.GetFbId()
		}
		if realFBID == 0 {
			lastErr = fmt.Errorf("failed to get media ID from upload response")
			continue
		}

		task := &socket.SendMessageTask{
			ThreadId:        threadID,
			AttachmentFBIds: []int64{realFBID},
			Source:          table.MESSENGER_INBOX_IN_THREAD,
			SendType:        table.MEDIA,
			SyncGroup:       1,
			Otid:            methods.GenerateEpochID(),
		}
		_, err = c.client.ExecuteTask(ctx, task)
		if err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (c *Client) GetSelfID() int64 {
	return c.selfID
}

func (c *Client) SendMultiMedia(ctx context.Context, threadID int64, items []core.MediaAttachment) error {
	if len(items) == 0 {
		return nil
	}
	// Single item: use existing SendMedia
	if len(items) == 1 {
		return c.SendMedia(ctx, threadID, items[0].Data, items[0].Filename, items[0].MimeType)
	}

	// Upload all items in parallel, collect FBIDs
	type uploadResult struct {
		index int
		fbID  int64
		err   error
	}

	results := make(chan uploadResult, len(items))
	for i, item := range items {
		go func(idx int, att core.MediaAttachment) {
			if len(att.Data) > maxUploadSize {
				results <- uploadResult{index: idx, err: fmt.Errorf("file #%d too large (%d bytes)", idx+1, len(att.Data))}
				return
			}

			var lastErr error
			for retry := 0; retry < maxRetries; retry++ {
				uploadResp, err := c.client.SendMercuryUploadRequest(ctx, threadID, &messagix.MercuryUploadMedia{
					Filename:  att.Filename,
					MimeType:  att.MimeType,
					MediaData: att.Data,
				})
				if err != nil {
					lastErr = err
					continue
				}

				var realFBID int64
				if uploadResp.Payload.RealMetadata != nil {
					realFBID = uploadResp.Payload.RealMetadata.GetFbId()
				}
				if realFBID == 0 {
					lastErr = fmt.Errorf("failed to get media ID")
					continue
				}

				results <- uploadResult{index: idx, fbID: realFBID}
				return
			}
			results <- uploadResult{index: idx, err: fmt.Errorf("upload #%d failed after retries: %w", idx+1, lastErr)}
		}(i, item)
	}

	// Collect results
	fbIDs := make([]int64, len(items))
	var uploadErr error
	for range items {
		r := <-results
		if r.err != nil {
			uploadErr = r.err
			continue
		}
		fbIDs[r.index] = r.fbID
	}

	// Filter out failed uploads (fbID == 0)
	var validIDs []int64
	for _, id := range fbIDs {
		if id != 0 {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return fmt.Errorf("all uploads failed: %w", uploadErr)
	}

	// Send one message with all attachment IDs
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		task := &socket.SendMessageTask{
			ThreadId:        threadID,
			AttachmentFBIds: validIDs,
			Source:          table.MESSENGER_INBOX_IN_THREAD,
			SendType:        table.MEDIA,
			SyncGroup:       1,
			Otid:            methods.GenerateEpochID(),
		}
		_, lastErr = c.client.ExecuteTask(ctx, task)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

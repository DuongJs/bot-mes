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

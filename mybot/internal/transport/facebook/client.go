package facebook

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"mybot/internal/core"
	"mybot/internal/messaging"
)

type EditTracker interface {
	WaitForEdit(ctx context.Context, messageID, text string) (*core.MessageRecord, error)
}

type Client struct {
	client      *messagix.Client
	selfID      int64
	editTracker EditTracker
}

var _ core.MessageSender = (*Client)(nil)
var _ messaging.Transport = (*Client)(nil)

func NewClient(client *messagix.Client, selfID int64, editTrackers ...EditTracker) *Client {
	fbClient := &Client{
		client: client,
		selfID: selfID,
	}
	if len(editTrackers) > 0 {
		fbClient.editTracker = editTrackers[0]
	}
	return fbClient
}

const (
	maxRetries    = 3
	maxUploadSize = 25 * 1000 * 1000
)

func (c *Client) SendMessage(ctx context.Context, threadID int64, text string) error {
	_, err := c.SendText(ctx, core.SendTextRequest{
		ThreadID: threadID,
		Text:     text,
	})
	return err
}

func (c *Client) SendMedia(ctx context.Context, threadID int64, data []byte, filename, mimeType string) error {
	_, err := c.SendMediaRich(ctx, core.SendMediaRequest{
		ThreadID: threadID,
		Items: []core.MediaAttachment{{
			Data:     data,
			Filename: filename,
			MimeType: mimeType,
		}},
	})
	return err
}

func (c *Client) SendMultiMedia(ctx context.Context, threadID int64, items []core.MediaAttachment) error {
	_, err := c.SendMediaRich(ctx, core.SendMediaRequest{
		ThreadID: threadID,
		Items:    items,
	})
	return err
}

func (c *Client) GetSelfID() int64 {
	return c.selfID
}

func (c *Client) SendText(ctx context.Context, req core.SendTextRequest) (*core.MessageRecord, error) {
	// Generate OTID once so retries reuse the same ID (Facebook deduplicates by OTID).
	otid := methods.GenerateEpochID()

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			select {
			case <-time.After(time.Duration(i) * 500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		task := &socket.SendMessageTask{
			ThreadId:  req.ThreadID,
			Text:      req.Text,
			Source:    table.MESSENGER_INBOX_IN_THREAD,
			SendType:  table.TEXT,
			SyncGroup: 1,
			Otid:      otid,
		}
		if req.ReplyTo != nil && req.ReplyTo.MessageID != "" {
			task.ReplyMetaData = &socket.ReplyMetaData{
				ReplyMessageId:  req.ReplyTo.MessageID,
				ReplySourceType: 1,
				ReplyType:       0,
			}
		}

		resp, err := c.client.ExecuteTask(ctx, task)
		if err != nil {
			lastErr = err
			continue
		}

		// ExecuteTask succeeded → message was sent. Extract metadata if available.
		rec := messageRecordFromSendResponse(resp, req.ThreadID, c.selfID)
		if rec == nil {
			// Message sent but response lacked metadata – return a minimal record.
			rec = &core.MessageRecord{
				ThreadID: req.ThreadID,
				SenderID: c.selfID,
			}
		}
		rec.Text = req.Text
		if req.ReplyTo != nil {
			rec.ReplyToMessageID = req.ReplyTo.MessageID
		}
		return rec, nil
	}
	return nil, lastErr
}

func (c *Client) SendMediaRich(ctx context.Context, req core.SendMediaRequest) (*core.MessageRecord, error) {
	if len(req.Items) == 0 {
		return nil, nil
	}

	attachmentIDs, err := c.uploadAttachmentIDs(ctx, req.ThreadID, req.Items)
	if err != nil {
		return nil, err
	}

	// Generate OTID once so retries reuse the same ID (Facebook deduplicates by OTID).
	otid := methods.GenerateEpochID()

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			select {
			case <-time.After(time.Duration(i) * 500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		task := &socket.SendMessageTask{
			ThreadId:        req.ThreadID,
			AttachmentFBIds: attachmentIDs,
			Source:          table.MESSENGER_INBOX_IN_THREAD,
			SendType:        table.MEDIA,
			SyncGroup:       1,
			Otid:            otid,
		}
		if req.ReplyTo != nil && req.ReplyTo.MessageID != "" {
			task.ReplyMetaData = &socket.ReplyMetaData{
				ReplyMessageId:  req.ReplyTo.MessageID,
				ReplySourceType: 1,
				ReplyType:       0,
			}
		}

		resp, err := c.client.ExecuteTask(ctx, task)
		if err != nil {
			lastErr = err
			continue
		}

		// ExecuteTask succeeded → message was sent. Extract metadata if available.
		rec := messageRecordFromSendResponse(resp, req.ThreadID, c.selfID)
		if rec == nil {
			rec = &core.MessageRecord{
				ThreadID: req.ThreadID,
				SenderID: c.selfID,
			}
		}
		rec.HasMedia = true
		rec.Attachments = attachmentMetaFromItems(req.Items, attachmentIDs)
		if req.ReplyTo != nil {
			rec.ReplyToMessageID = req.ReplyTo.MessageID
		}
		return rec, nil
	}
	return nil, lastErr
}

func (c *Client) SendMediaMessage(ctx context.Context, req core.SendMediaRequest) (*core.MessageRecord, error) {
	return c.SendMediaRich(ctx, req)
}

func (c *Client) EditText(ctx context.Context, messageID, newText string) (*core.MessageRecord, error) {
	resp, err := c.client.ExecuteTask(ctx, &socket.EditMessageTask{
		MessageID: messageID,
		Text:      newText,
	})
	if err != nil {
		return nil, err
	}

	if rec := recordFromEditResponse(resp, messageID, newText, c.selfID); rec != nil {
		return rec, nil
	}

	if c.editTracker == nil {
		return nil, messaging.ErrEditNotConfirmed
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rec, err := c.editTracker.WaitForEdit(waitCtx, messageID, newText)
	if err == nil && rec != nil {
		return rec, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, messaging.ErrEditNotConfirmed
}

func (c *Client) Recall(ctx context.Context, messageID string) error {
	_, err := c.client.ExecuteTask(ctx, &socket.DeleteMessageTask{MessageId: messageID})
	return err
}

func (c *Client) uploadAttachmentIDs(ctx context.Context, threadID int64, items []core.MediaAttachment) ([]int64, error) {
	ids := make([]int64, 0, len(items))

	for idx, item := range items {
		if len(item.Data) > maxUploadSize {
			if len(items) == 1 {
				return nil, fmt.Errorf("file too large (%d bytes, max %d)", len(item.Data), maxUploadSize)
			}
			return nil, fmt.Errorf("file #%d too large (%d bytes, max %d)", idx+1, len(item.Data), maxUploadSize)
		}

		var lastErr error
		for retry := 0; retry < maxRetries; retry++ {
			uploadResp, err := c.client.SendMercuryUploadRequest(ctx, threadID, &messagix.MercuryUploadMedia{
				Filename:  item.Filename,
				MimeType:  item.MimeType,
				MediaData: item.Data,
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

			ids = append(ids, realFBID)
			lastErr = nil
			break
		}
		if lastErr != nil {
			return nil, lastErr
		}
	}

	return ids, nil
}

func messageRecordFromSendResponse(resp *table.LSTable, threadID, senderID int64) *core.MessageRecord {
	if resp == nil {
		return nil
	}

	if len(resp.LSInsertMessage) > 0 {
		return messageRecordFromInsert(resp.LSInsertMessage[0], senderID)
	}
	if len(resp.LSUpsertMessage) > 0 {
		return messageRecordFromInsert(resp.LSUpsertMessage[0].ToInsert(), senderID)
	}
	return nil
}

func messageRecordFromInsert(msg *table.LSInsertMessage, senderID int64) *core.MessageRecord {
	if msg == nil || msg.MessageId == "" {
		return nil
	}
	return &core.MessageRecord{
		MessageID:          msg.MessageId,
		ThreadID:           msg.ThreadKey,
		SenderID:           firstNonZero(msg.SenderId, senderID),
		Text:               msg.Text,
		ReplyToMessageID:   msg.ReplySourceId,
		OfflineThreadingID: msg.OfflineThreadingId,
		TimestampMs:        msg.TimestampMs,
		EditCount:          msg.EditCount,
		IsEdited:           msg.EditCount > 0,
		IsRecalled:         msg.IsUnsent,
		HasMedia:           msg.StickerId != 0,
	}
}

func recordFromEditResponse(resp *table.LSTable, messageID, newText string, senderID int64) *core.MessageRecord {
	if resp == nil {
		return nil
	}
	for _, edit := range resp.LSEditMessage {
		if edit.MessageID != messageID {
			continue
		}
		if edit.Text != newText {
			return nil
		}
		return &core.MessageRecord{
			MessageID: messageID,
			SenderID:  senderID,
			Text:      edit.Text,
			EditCount: edit.EditCount,
			IsEdited:  true,
		}
	}
	return nil
}

func attachmentMetaFromItems(items []core.MediaAttachment, attachmentIDs []int64) []core.AttachmentMeta {
	out := make([]core.AttachmentMeta, 0, len(items))
	for idx, item := range items {
		meta := core.AttachmentMeta{
			Filename:  item.Filename,
			MimeType:  item.MimeType,
			SizeBytes: int64(len(item.Data)),
			Kind:      "upload",
		}
		if idx < len(attachmentIDs) {
			meta.AttachmentID = fmt.Sprintf("%d", attachmentIDs[idx])
		}
		out = append(out, meta)
	}
	return out
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

package facebook

import (
	"context"
	"fmt"
	"sync"
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
	// For a single file, upload directly (no goroutine overhead).
	if len(items) == 1 {
		return c.uploadSingleAttachment(ctx, threadID, &items[0])
	}

	// Parallel upload with concurrency limit (inspired by JS FCA pLimit).
	const maxConcurrency = 3

	type uploadResult struct {
		idx  int
		fbID int64
		err  error
	}

	results := make([]uploadResult, len(items))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for idx := range items {
		item := &items[idx]

		if item.DataSize() > int64(maxUploadSize) {
			// Fail fast — clean up items that haven't started yet.
			return nil, fmt.Errorf("file #%d too large (%d bytes, max %d)", idx+1, item.DataSize(), maxUploadSize)
		}

		wg.Add(1)
		go func(i int, it *core.MediaAttachment) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			ids, err := c.uploadSingleAttachment(ctx, threadID, it)
			if err != nil {
				results[i] = uploadResult{idx: i, err: err}
			} else if len(ids) > 0 {
				results[i] = uploadResult{idx: i, fbID: ids[0]}
			} else {
				results[i] = uploadResult{idx: i, err: fmt.Errorf("upload returned no ID")}
			}
		}(idx, item)
	}

	wg.Wait()

	// Collect results in order; fail on first error.
	ids := make([]int64, 0, len(items))
	for _, r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("upload #%d failed: %w", r.idx+1, r.err)
		}
		ids = append(ids, r.fbID)
	}
	return ids, nil
}

// uploadSingleAttachment uploads one media item with retries and returns its FB ID.
func (c *Client) uploadSingleAttachment(ctx context.Context, threadID int64, item *core.MediaAttachment) ([]int64, error) {
	if item.DataSize() > int64(maxUploadSize) {
		return nil, fmt.Errorf("file too large (%d bytes, max %d)", item.DataSize(), maxUploadSize)
	}

	var lastErr error
	for retry := 0; retry < maxRetries; retry++ {
		reader, err := item.OpenReader()
		if err != nil {
			return nil, fmt.Errorf("failed to open media: %w", err)
		}

		uploadResp, err := c.client.SendMercuryUploadRequest(ctx, threadID, &messagix.MercuryUploadMedia{
			Filename:    item.Filename,
			MimeType:    item.MimeType,
			MediaReader: reader,
		})
		reader.Close()

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

		item.Cleanup()
		return []int64{realFBID}, nil
	}

	item.Cleanup()
	return nil, lastErr
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
			SizeBytes: item.DataSize(),
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

// ---------------------------------------------------------------------------
// Additional transport actions (ported from JS FCA analysis)
// ---------------------------------------------------------------------------

// ForwardMessage forwards an existing message to another thread.
func (c *Client) ForwardMessage(ctx context.Context, threadID int64, forwardedMsgID string) error {
	otid := methods.GenerateEpochID()
	task := &socket.SendMessageTask{
		ThreadId:                 threadID,
		Otid:                     otid,
		Source:                   65544,
		SendType:                 5, // forward
		SyncGroup:                1,
		ForwardedMsgId:           forwardedMsgID,
		StripForwardedMsgCaption: 0,
		InitiatingSource:         1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// SendReaction sets a reaction emoji on a message.
func (c *Client) SendReaction(ctx context.Context, threadID int64, messageID string, reaction string) error {
	task := &socket.SendReactionTask{
		ThreadKey:       threadID,
		MessageID:       messageID,
		ActorID:         c.selfID,
		Reaction:        reaction,
		SyncGroup:       1,
		SendAttribution: table.MESSENGER_INBOX_IN_THREAD,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// ShareContact shares a contact card in a thread (label 359).
func (c *Client) ShareContact(ctx context.Context, threadID int64, contactID int64, text string) error {
	task := &socket.ShareContactTask{
		ContactID: contactID,
		SyncGroup: 1,
		Text:      text,
		ThreadID:  threadID,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// SetThreadImage uploads an image and sets it as the group thread image.
func (c *Client) SetThreadImage(ctx context.Context, threadID int64, imageData []byte, filename, mimeType string) error {
	// Upload the image first.
	uploadResp, err := c.client.SendMercuryUploadRequest(ctx, threadID, &messagix.MercuryUploadMedia{
		Filename:  filename,
		MimeType:  mimeType,
		MediaData: imageData,
	})
	if err != nil {
		return fmt.Errorf("upload thread image: %w", err)
	}
	var imageID int64
	if uploadResp.Payload.RealMetadata != nil {
		imageID = uploadResp.Payload.RealMetadata.GetFbId()
	}
	if imageID == 0 {
		return fmt.Errorf("upload thread image: no image ID returned")
	}

	task := &socket.SetThreadImageTask{
		ThreadKey: threadID,
		ImageID:   imageID,
		SyncGroup: 1,
	}
	_, err = c.client.ExecuteTask(ctx, task)
	return err
}

// CreatePoll creates a poll in a thread.
func (c *Client) CreatePoll(ctx context.Context, threadID int64, question string, options []string) error {
	task := &socket.CreatePollTask{
		QuestionText: question,
		ThreadKey:    threadID,
		Options:      options,
		SyncGroup:    1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// RenameThread renames a group thread.
func (c *Client) RenameThread(ctx context.Context, threadID int64, name string) error {
	task := &socket.RenameThreadTask{
		ThreadKey:  threadID,
		ThreadName: name,
		SyncGroup:  1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// MuteThread mutes a thread until the given expiry time (0 = unmute).
func (c *Client) MuteThread(ctx context.Context, threadID int64, muteExpireMs int64) error {
	task := &socket.MuteThreadTask{
		ThreadKey:        threadID,
		MuteExpireTimeMS: muteExpireMs,
		SyncGroup:        1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// AddParticipants adds users to a group thread.
func (c *Client) AddParticipants(ctx context.Context, threadID int64, contactIDs []int64) error {
	task := &socket.AddParticipantsTask{
		ThreadKey:  threadID,
		ContactIDs: contactIDs,
		SyncGroup:  1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// RemoveParticipant removes a user from a group thread.
func (c *Client) RemoveParticipant(ctx context.Context, threadID int64, contactID int64) error {
	task := &socket.RemoveParticipantTask{
		ThreadID:  threadID,
		ContactID: contactID,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// UpdateAdmin promotes or demotes a user to/from admin (isAdmin: 1=promote, 0=demote).
func (c *Client) UpdateAdmin(ctx context.Context, threadID int64, contactID int64, isAdmin int) error {
	task := &socket.UpdateAdminTask{
		ThreadKey: threadID,
		ContactID: contactID,
		IsAdmin:   isAdmin,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// MarkThreadRead marks a thread as read.
func (c *Client) MarkThreadRead(ctx context.Context, threadID int64) error {
	task := &socket.ThreadMarkReadTask{
		ThreadId:            threadID,
		LastReadWatermarkTs: time.Now().UnixMilli(),
		SyncGroup:           1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// DeleteThread deletes a thread.
func (c *Client) DeleteThread(ctx context.Context, threadID int64) error {
	task := &socket.DeleteThreadTask{
		ThreadKey:  threadID,
		RemoveType: 0,
		SyncGroup:  1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// HandleMessageRequest accepts or rejects a message request (Facebook only).
func (c *Client) HandleMessageRequest(ctx context.Context, threadID int64, accept bool) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("HandleMessageRequest: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.HandleMessageRequest(ctx, threadID, accept)
}

// MarkAllRead marks all inbox threads as read (Facebook only).
func (c *Client) MarkAllRead(ctx context.Context) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("MarkAllRead: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.MarkAllRead(ctx)
}

// ResolvePhotoURL resolves the full-size URL of a photo attachment (Facebook only).
func (c *Client) ResolvePhotoURL(ctx context.Context, photoID string) (string, error) {
	if c.client.Facebook == nil {
		return "", fmt.Errorf("ResolvePhotoURL: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.ResolvePhotoURL(ctx, photoID)
}

// RefreshTokens refreshes fb_dtsg, LSD, and jazoest tokens by re-fetching the page.
// Should be called periodically (e.g. every 24h) to keep uploads working.
func (c *Client) RefreshTokens(ctx context.Context) error {
	return c.client.RefreshConfigs(ctx)
}

// StartTokenRefreshLoop starts a background goroutine that refreshes
// tokens every interval (recommended: 24h). Cancel the context to stop.
func (c *Client) StartTokenRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.RefreshTokens(ctx); err != nil {
					// Log but don't stop – next tick will retry.
					_ = err // caller should set up logging externally
				}
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// New messaging + social APIs (ported from JS FCA)
// ---------------------------------------------------------------------------

// ChangeNickname changes a participant's nickname in a thread.
func (c *Client) ChangeNickname(ctx context.Context, threadID, contactID int64, nickname string) error {
	task := &socket.ChangeNicknameTask{
		ThreadKey: threadID,
		ContactID: contactID,
		Nickname:  nickname,
		SyncGroup: 1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// ChangeThreadColor changes a thread's color/theme by theme FBID.
func (c *Client) ChangeThreadColor(ctx context.Context, threadID int64, themeFBID string) error {
	task := &socket.ChangeThreadColorTask{
		ThreadKey: threadID,
		ThemeFBID: themeFBID,
		SyncGroup: 1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// ChangeThreadEmoji changes the default quick-reaction emoji for a thread.
func (c *Client) ChangeThreadEmoji(ctx context.Context, threadID int64, emoji string) error {
	task := &socket.ChangeThreadEmojiTask{
		ThreadKey:   threadID,
		CustomEmoji: emoji,
		SyncGroup:   1,
	}
	_, err := c.client.ExecuteTask(ctx, task)
	return err
}

// SendTypingIndicator sends or stops a typing indicator to a thread.
// isGroup should be true for group threads.
func (c *Client) SendTypingIndicator(ctx context.Context, threadID int64, isTyping, isGroup bool) error {
	isTypingInt := 0
	if isTyping {
		isTypingInt = 1
	}
	isGroupInt := 0
	threadType := 1
	if isGroup {
		isGroupInt = 1
		threadType = 2
	}
	task := &socket.TypingIndicatorTask{
		ThreadKey:     threadID,
		IsGroupThread: isGroupInt,
		IsTyping:      isTypingInt,
		Attribution:   0,
		SyncGroup:     1,
		ThreadType:    threadType,
	}
	return c.client.ExecuteStatelessTask(ctx, task)
}

// ChangeArchivedStatus archives or unarchives threads (Facebook only).
func (c *Client) ChangeArchivedStatus(ctx context.Context, threadIDs []int64, archive bool) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("ChangeArchivedStatus: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.ChangeArchivedStatus(ctx, threadIDs, archive)
}

// ChangeBlockedStatus blocks or unblocks a user's messages (Facebook only).
func (c *Client) ChangeBlockedStatus(ctx context.Context, userID int64, block bool) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("ChangeBlockedStatus: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.ChangeBlockedStatus(ctx, userID, block)
}

// MarkAsDelivered sends a delivery receipt for a message (Facebook only).
func (c *Client) MarkAsDelivered(ctx context.Context, threadID int64, messageID string) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("MarkAsDelivered: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.MarkAsDelivered(ctx, threadID, messageID)
}

// MarkAsSeen marks all messages as seen up to a given timestamp (Facebook only).
func (c *Client) MarkAsSeen(ctx context.Context, timestampMs int64) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("MarkAsSeen: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.MarkAsSeen(ctx, timestampMs)
}

// SearchForThread searches for threads by name (Facebook only).
func (c *Client) SearchForThread(ctx context.Context, queryStr string) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("SearchForThread: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.SearchForThread(ctx, queryStr)
}

// GetThreadPictures fetches shared photos from a thread (Facebook only).
func (c *Client) GetThreadPictures(ctx context.Context, threadID int64, offset, limit int) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("GetThreadPictures: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.GetThreadPictures(ctx, threadID, offset, limit)
}

// GetUserInfo fetches detailed user info via GraphQL (Facebook only).
func (c *Client) GetUserInfo(ctx context.Context, userIDs []int64) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("GetUserInfo: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.GetUserInfo(ctx, userIDs)
}

// GetUserInfoV2 fetches user info via CometHovercard GraphQL (Facebook only).
func (c *Client) GetUserInfoV2(ctx context.Context, userID int64) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("GetUserInfoV2: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.GetUserInfoV2(ctx, userID)
}

// CreateThemeAI generates an AI-designed chat theme from a prompt (Facebook only).
func (c *Client) CreateThemeAI(ctx context.Context, prompt string) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("CreateThemeAI: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.CreateThemeAI(ctx, prompt, c.selfID)
}

// GetThemePictures fetches theme assets by theme ID (Facebook only).
func (c *Client) GetThemePictures(ctx context.Context, themeID string) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("GetThemePictures: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.GetThemePictures(ctx, themeID)
}

// SetPostReaction sets a reaction on a Facebook post (Facebook only).
// Reaction types: 0=unlike, 1=like, 2=heart, 16=love, 4=haha, 3=wow, 7=sad, 8=angry
func (c *Client) SetPostReaction(ctx context.Context, postID string, reactionType int) ([]byte, error) {
	if c.client.Facebook == nil {
		return nil, fmt.Errorf("SetPostReaction: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.SetPostReaction(ctx, postID, reactionType, c.selfID)
}

// HandleFriendRequest accepts or rejects a friend request (Facebook only).
func (c *Client) HandleFriendRequest(ctx context.Context, userID int64, accept bool) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("HandleFriendRequest: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.HandleFriendRequest(ctx, userID, accept)
}

// Unfriend removes a user from the friends list (Facebook only).
func (c *Client) Unfriend(ctx context.Context, userID int64) error {
	if c.client.Facebook == nil {
		return fmt.Errorf("Unfriend: only supported on Facebook/Messenger")
	}
	return c.client.Facebook.Unfriend(ctx, userID)
}

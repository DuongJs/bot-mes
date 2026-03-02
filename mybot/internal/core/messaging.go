package core

import "context"

type ThreadRecord struct {
	ThreadID        int64  `json:"thread_id"`
	Name            string `json:"name"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
	LastActivityMs  int64  `json:"last_activity_ms"`
	Deleted         bool   `json:"deleted"`
}

type UserRecord struct {
	UserID          int64  `json:"user_id"`
	Name            string `json:"name"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
	Deleted         bool   `json:"deleted"`
}

type AttachmentMeta struct {
	AttachmentID string `json:"attachment_id"`
	Kind         string `json:"kind"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
}

type MessageRecord struct {
	MessageID          string           `json:"message_id"`
	ThreadID           int64            `json:"thread_id"`
	SenderID           int64            `json:"sender_id"`
	SenderNameSnapshot string           `json:"sender_name_snapshot"`
	Text               string           `json:"text"`
	ReplyToMessageID   string           `json:"reply_to_message_id"`
	OfflineThreadingID string           `json:"offline_threading_id"`
	IsFromBot          bool             `json:"is_from_bot"`
	HasMedia           bool             `json:"has_media"`
	Attachments        []AttachmentMeta `json:"attachments,omitempty"`
	TimestampMs        int64            `json:"timestamp_ms"`
	EditCount          int64            `json:"edit_count"`
	IsEdited           bool             `json:"is_edited"`
	IsRecalled         bool             `json:"is_recalled"`
	CreatedAtUnixMs    int64            `json:"created_at_unix_ms"`
	UpdatedAtUnixMs    int64            `json:"updated_at_unix_ms"`
	RecalledAtUnixMs   int64            `json:"recalled_at_unix_ms"`
}

type ReplyTarget struct {
	MessageID string
}

type SendTextRequest struct {
	ThreadID int64
	Text     string
	ReplyTo  *ReplyTarget
}

type SendMediaRequest struct {
	ThreadID int64
	Items    []MediaAttachment
	ReplyTo  *ReplyTarget
}

type MessageController interface {
	SendText(ctx context.Context, req SendTextRequest) (*MessageRecord, error)
	SendMedia(ctx context.Context, req SendMediaRequest) (*MessageRecord, error)
	ReplyText(ctx context.Context, threadID int64, replyToMessageID, text string) (*MessageRecord, error)
	EditText(ctx context.Context, messageID, newText string) (*MessageRecord, error)
	Recall(ctx context.Context, messageID string) error
	GetMessage(ctx context.Context, messageID string) (*MessageRecord, error)
	GetLastBotMessage(ctx context.Context, threadID int64) (*MessageRecord, error)
}

type ConversationReader interface {
	GetThread(ctx context.Context, threadID int64) (*ThreadRecord, error)
	GetUser(ctx context.Context, userID int64) (*UserRecord, error)
	ListThreadMessages(ctx context.Context, threadID int64, limit int, beforeMessageID string) ([]*MessageRecord, error)
}

package messaging

import (
	"context"

	"mybot/internal/core"
)

type Transport interface {
	SendText(ctx context.Context, req core.SendTextRequest) (*core.MessageRecord, error)
	SendMediaMessage(ctx context.Context, req core.SendMediaRequest) (*core.MessageRecord, error)
	EditText(ctx context.Context, messageID, newText string) (*core.MessageRecord, error)
	Recall(ctx context.Context, messageID string) error
	GetSelfID() int64
}

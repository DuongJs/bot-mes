package messaging

import (
	"context"

	"mybot/internal/core"
)

type Store interface {
	Close() error
	UpsertThread(ctx context.Context, rec *core.ThreadRecord) error
	GetThread(ctx context.Context, threadID int64) (*core.ThreadRecord, error)
	UpsertUser(ctx context.Context, rec *core.UserRecord) error
	GetUser(ctx context.Context, userID int64) (*core.UserRecord, error)
	UpsertMessage(ctx context.Context, rec *core.MessageRecord) error
	GetMessage(ctx context.Context, messageID string) (*core.MessageRecord, error)
	ListThreadMessages(ctx context.Context, threadID int64, limit int, beforeMessageID string) ([]*core.MessageRecord, error)
	SetLastBotMessage(ctx context.Context, threadID int64, messageID string) error
	GetLastBotMessage(ctx context.Context, threadID int64) (*core.MessageRecord, error)
	ClearLastBotMessage(ctx context.Context, threadID int64, messageID string) error
}

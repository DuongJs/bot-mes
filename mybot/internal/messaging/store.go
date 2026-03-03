package messaging

import (
	"context"

	"github.com/rs/zerolog"

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

// BatchedStore wraps a Store with a WriteBatcher that groups writes into
// single transactions for higher throughput.
type BatchedStore struct {
	Store                       // embedded for read operations
	batcher *WriteBatcher
}

// NewBatchedStore wraps store with batching. Caller must call Close() to
// flush pending writes and shut down the batcher goroutine.
func NewBatchedStore(store *SQLiteStore, log zerolog.Logger, queueSize, maxBatch, flushMs int) *BatchedStore {
	return &BatchedStore{
		Store:   store,
		batcher: NewWriteBatcher(store, log, queueSize, maxBatch, flushMs),
	}
}

func (b *BatchedStore) UpsertThread(_ context.Context, rec *core.ThreadRecord) error {
	return b.batcher.Submit(writeOp{kind: opUpsertThread, thread: rec})
}

func (b *BatchedStore) UpsertUser(_ context.Context, rec *core.UserRecord) error {
	return b.batcher.Submit(writeOp{kind: opUpsertUser, user: rec})
}

func (b *BatchedStore) UpsertMessage(_ context.Context, rec *core.MessageRecord) error {
	return b.batcher.Submit(writeOp{kind: opUpsertMessage, msg: rec})
}

func (b *BatchedStore) SetLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	return b.batcher.Submit(writeOp{kind: opSetLastBot, threadID: threadID, messageID: messageID})
}

func (b *BatchedStore) ClearLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	if messageID != "" {
		return b.batcher.Submit(writeOp{kind: opClearLastBot, threadID: threadID, messageID: messageID})
	}
	return b.batcher.Submit(writeOp{kind: opClearLastBotByThread, threadID: threadID})
}

func (b *BatchedStore) Close() error {
	b.batcher.Stop()
	return b.Store.Close()
}

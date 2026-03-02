package messaging

import (
	"context"
	"path/filepath"
	"testing"

	"mybot/internal/core"
)

func TestBoltStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "messages.db")

	store, err := OpenBoltStore(dbPath)
	if err != nil {
		t.Fatalf("OpenBoltStore() error = %v", err)
	}

	thread := &core.ThreadRecord{ThreadID: 123, Name: "General", UpdatedAtUnixMs: 100}
	user := &core.UserRecord{UserID: 456, Name: "Alice", UpdatedAtUnixMs: 100}
	message := &core.MessageRecord{
		MessageID:          "m1",
		ThreadID:           123,
		SenderID:           456,
		SenderNameSnapshot: "Alice",
		Text:               "hello",
		TimestampMs:        1000,
		CreatedAtUnixMs:    1000,
		UpdatedAtUnixMs:    1000,
	}

	if err := store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread() error = %v", err)
	}
	if err := store.UpsertUser(ctx, user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}
	if err := store.UpsertMessage(ctx, message); err != nil {
		t.Fatalf("UpsertMessage() error = %v", err)
	}
	if err := store.SetLastBotMessage(ctx, message.ThreadID, message.MessageID); err != nil {
		t.Fatalf("SetLastBotMessage() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenBoltStore(dbPath)
	if err != nil {
		t.Fatalf("OpenBoltStore(reopen) error = %v", err)
	}
	defer reopened.Close()

	gotThread, err := reopened.GetThread(ctx, thread.ThreadID)
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if gotThread == nil || gotThread.Name != thread.Name {
		t.Fatalf("GetThread() = %+v, want name %q", gotThread, thread.Name)
	}

	gotUser, err := reopened.GetUser(ctx, user.UserID)
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if gotUser == nil || gotUser.Name != user.Name {
		t.Fatalf("GetUser() = %+v, want name %q", gotUser, user.Name)
	}

	gotMessage, err := reopened.GetMessage(ctx, message.MessageID)
	if err != nil {
		t.Fatalf("GetMessage() error = %v", err)
	}
	if gotMessage == nil || gotMessage.Text != message.Text {
		t.Fatalf("GetMessage() = %+v, want text %q", gotMessage, message.Text)
	}

	lastBot, err := reopened.GetLastBotMessage(ctx, message.ThreadID)
	if err != nil {
		t.Fatalf("GetLastBotMessage() error = %v", err)
	}
	if lastBot == nil || lastBot.MessageID != message.MessageID {
		t.Fatalf("GetLastBotMessage() = %+v, want %q", lastBot, message.MessageID)
	}
}

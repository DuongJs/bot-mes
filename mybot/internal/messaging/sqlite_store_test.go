package messaging

import (
	"context"
	"path/filepath"
	"testing"

	"mybot/internal/core"
)

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "messages.sqlite")

	store, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
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

	reopened, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLiteStore(reopen) error = %v", err)
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

func TestSQLiteStoreListThreadMessages(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "messages.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	msgs := []*core.MessageRecord{
		{MessageID: "m1", ThreadID: 1, TimestampMs: 100, Text: "first", CreatedAtUnixMs: 100, UpdatedAtUnixMs: 100},
		{MessageID: "m2", ThreadID: 1, TimestampMs: 200, Text: "second", CreatedAtUnixMs: 200, UpdatedAtUnixMs: 200},
		{MessageID: "m3", ThreadID: 1, TimestampMs: 300, Text: "third", CreatedAtUnixMs: 300, UpdatedAtUnixMs: 300},
	}
	for _, m := range msgs {
		if err := store.UpsertMessage(ctx, m); err != nil {
			t.Fatalf("UpsertMessage() error = %v", err)
		}
	}

	// List all: newest first
	all, err := store.ListThreadMessages(ctx, 1, 10, "")
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListThreadMessages() got %d, want 3", len(all))
	}
	if all[0].MessageID != "m3" {
		t.Fatalf("ListThreadMessages()[0] = %q, want m3", all[0].MessageID)
	}

	// Before m3 → should get m2, m1
	before, err := store.ListThreadMessages(ctx, 1, 10, "m3")
	if err != nil {
		t.Fatalf("ListThreadMessages(before m3) error = %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("ListThreadMessages(before m3) got %d, want 2", len(before))
	}
	if before[0].MessageID != "m2" {
		t.Fatalf("ListThreadMessages(before m3)[0] = %q, want m2", before[0].MessageID)
	}
}

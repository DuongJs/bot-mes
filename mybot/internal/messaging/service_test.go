package messaging

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"mybot/internal/core"
)

type fakeTransport struct {
	selfID        int64
	nextTextResp  *core.MessageRecord
	nextMediaResp *core.MessageRecord
	nextEditResp  *core.MessageRecord
	lastTextReq   core.SendTextRequest
	lastMediaReq  core.SendMediaRequest
	lastRecallID  string
}

func (f *fakeTransport) SendText(_ context.Context, req core.SendTextRequest) (*core.MessageRecord, error) {
	f.lastTextReq = req
	return f.nextTextResp, nil
}

func (f *fakeTransport) SendMediaMessage(_ context.Context, req core.SendMediaRequest) (*core.MessageRecord, error) {
	f.lastMediaReq = req
	return f.nextMediaResp, nil
}

func (f *fakeTransport) EditText(_ context.Context, _, _ string) (*core.MessageRecord, error) {
	return f.nextEditResp, nil
}

func (f *fakeTransport) Recall(_ context.Context, messageID string) error {
	f.lastRecallID = messageID
	return nil
}

func (f *fakeTransport) GetSelfID() int64 {
	return f.selfID
}

func TestServicePersistsSentMessagesAndHistory(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "messages.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	transport := &fakeTransport{
		selfID: 42,
		nextTextResp: &core.MessageRecord{
			MessageID:   "m1",
			ThreadID:    1001,
			SenderID:    42,
			Text:        "first",
			TimestampMs: 1000,
		},
	}

	service := NewService(
		zerolog.Nop(),
		store,
		func() int64 { return 42 },
		func() Transport { return transport },
		nil,
	)

	first, err := service.SendText(ctx, core.SendTextRequest{ThreadID: 1001, Text: "first"})
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if first == nil || first.MessageID != "m1" {
		t.Fatalf("SendText() = %+v", first)
	}

	transport.nextTextResp = &core.MessageRecord{
		MessageID:   "m2",
		ThreadID:    1001,
		SenderID:    42,
		Text:        "reply",
		TimestampMs: 2000,
	}
	second, err := service.ReplyText(ctx, 1001, "m1", "reply")
	if err != nil {
		t.Fatalf("ReplyText() error = %v", err)
	}
	if second == nil || second.ReplyToMessageID != "m1" {
		t.Fatalf("ReplyText() = %+v", second)
	}
	if transport.lastTextReq.ReplyTo == nil || transport.lastTextReq.ReplyTo.MessageID != "m1" {
		t.Fatalf("transport last reply request = %+v", transport.lastTextReq)
	}

	lastBot, err := service.GetLastBotMessage(ctx, 1001)
	if err != nil {
		t.Fatalf("GetLastBotMessage() error = %v", err)
	}
	if lastBot == nil || lastBot.MessageID != "m2" {
		t.Fatalf("GetLastBotMessage() = %+v", lastBot)
	}

	history, err := service.ListThreadMessages(ctx, 1001, 10, "")
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
	}
	if len(history) != 2 || history[0].MessageID != "m2" || history[1].MessageID != "m1" {
		t.Fatalf("history = %+v", history)
	}

	transport.nextEditResp = &core.MessageRecord{
		MessageID:   "m2",
		ThreadID:    1001,
		SenderID:    42,
		Text:        "reply-edited",
		TimestampMs: 2000,
		EditCount:   1,
		IsEdited:    true,
	}
	edited, err := service.EditText(ctx, "m2", "reply-edited")
	if err != nil {
		t.Fatalf("EditText() error = %v", err)
	}
	if edited == nil || edited.Text != "reply-edited" || !edited.IsEdited {
		t.Fatalf("EditText() = %+v", edited)
	}

	if err := service.Recall(ctx, "m2"); err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if transport.lastRecallID != "m2" {
		t.Fatalf("Recall() transport messageID = %q, want %q", transport.lastRecallID, "m2")
	}

	lastBot, err = service.GetLastBotMessage(ctx, 1001)
	if err != nil {
		t.Fatalf("GetLastBotMessage(after recall) error = %v", err)
	}
	if lastBot != nil {
		t.Fatalf("expected last bot message to be cleared, got %+v", lastBot)
	}
}

func TestTrackerWaitForEdit(t *testing.T) {
	tracker := NewTracker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan *core.MessageRecord, 1)
	go func() {
		rec, _ := tracker.WaitForEdit(ctx, "m1", "edited")
		done <- rec
	}()
	time.Sleep(10 * time.Millisecond)

	tracker.NotifyEdit(&core.MessageRecord{
		MessageID: "m1",
		Text:      "edited",
	})

	select {
	case rec := <-done:
		if rec == nil || rec.Text != "edited" {
			t.Fatalf("WaitForEdit() = %+v", rec)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForEdit() timed out")
	}
}

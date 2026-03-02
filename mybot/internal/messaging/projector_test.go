package messaging

import (
	"context"
	"path/filepath"
	"testing"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func TestProjectorProjectsThreadsUsersMessagesAndMutations(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "messages.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	projector := NewProjector(store, func() int64 { return 42 })
	tbl := &table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
			ThreadKey:               1001,
			ThreadName:              "Team Chat",
			LastActivityTimestampMs: 12345,
		}},
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{
			ContactId: 42,
			Name:      "Bot",
		}},
		LSInsertMessage: []*table.LSInsertMessage{{
			ThreadKey:          1001,
			MessageId:          "m1",
			SenderId:           42,
			Text:               "hello",
			TimestampMs:        999,
			OfflineThreadingId: "ot1",
		}},
		LSInsertAttachment: []*table.LSInsertAttachment{{
			MessageId:          "m1",
			AttachmentFbid:     "att1",
			Filename:           "photo.jpg",
			AttachmentMimeType: "image/jpeg",
			Filesize:           12,
		}},
	}

	if _, err := projector.ProjectTable(ctx, tbl, FullEvents); err != nil {
		t.Fatalf("ProjectTable(full) error = %v", err)
	}

	threadRec, err := store.GetThread(ctx, 1001)
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if threadRec == nil || threadRec.Name != "Team Chat" {
		t.Fatalf("thread record = %+v", threadRec)
	}

	userRec, err := store.GetUser(ctx, 42)
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if userRec == nil || userRec.Name != "Bot" {
		t.Fatalf("user record = %+v", userRec)
	}

	msgRec, err := store.GetMessage(ctx, "m1")
	if err != nil {
		t.Fatalf("GetMessage() error = %v", err)
	}
	if msgRec == nil || msgRec.Text != "hello" || !msgRec.IsFromBot {
		t.Fatalf("message record = %+v", msgRec)
	}
	if len(msgRec.Attachments) != 1 || msgRec.Attachments[0].AttachmentID != "att1" {
		t.Fatalf("attachments = %+v", msgRec.Attachments)
	}

	lastBot, err := store.GetLastBotMessage(ctx, 1001)
	if err != nil {
		t.Fatalf("GetLastBotMessage() error = %v", err)
	}
	if lastBot == nil || lastBot.MessageID != "m1" {
		t.Fatalf("last bot message = %+v", lastBot)
	}

	updateTbl := &table.LSTable{
		LSEditMessage: []*table.LSEditMessage{{
			MessageID: "m1",
			Text:      "edited",
			EditCount: 1,
		}},
	}
	if _, err := projector.ProjectTable(ctx, updateTbl, FullEvents); err != nil {
		t.Fatalf("ProjectTable(edit) error = %v", err)
	}

	msgRec, err = store.GetMessage(ctx, "m1")
	if err != nil {
		t.Fatalf("GetMessage(after edit) error = %v", err)
	}
	if msgRec == nil || msgRec.Text != "edited" || !msgRec.IsEdited || msgRec.EditCount != 1 {
		t.Fatalf("edited message record = %+v", msgRec)
	}

	deleteTbl := &table.LSTable{
		LSDeleteMessage: []*table.LSDeleteMessage{{
			ThreadKey: 1001,
			MessageId: "m1",
		}},
	}
	if _, err := projector.ProjectTable(ctx, deleteTbl, FullEvents); err != nil {
		t.Fatalf("ProjectTable(delete) error = %v", err)
	}

	msgRec, err = store.GetMessage(ctx, "m1")
	if err != nil {
		t.Fatalf("GetMessage(after delete) error = %v", err)
	}
	if msgRec == nil || !msgRec.IsRecalled {
		t.Fatalf("recalled message record = %+v", msgRec)
	}

	lastBot, err = store.GetLastBotMessage(ctx, 1001)
	if err != nil {
		t.Fatalf("GetLastBotMessage(after delete) error = %v", err)
	}
	if lastBot != nil {
		t.Fatalf("expected last bot message to be cleared, got %+v", lastBot)
	}
}

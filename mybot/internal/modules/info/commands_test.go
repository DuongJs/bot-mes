package info

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"mybot/internal/core"
)

type mockSender struct {
	lastMessage string
}

func (m *mockSender) SendMessage(_ context.Context, _ int64, text string) error {
	m.lastMessage = text
	return nil
}
func (m *mockSender) SendMedia(_ context.Context, _ int64, _ []byte, _, _ string) error {
	return nil
}
func (m *mockSender) GetSelfID() int64 { return 0 }

func TestStatusCommandOutput(t *testing.T) {
	sender := &mockSender{}
	ctx := &core.CommandContext{
		Ctx:       context.Background(),
		Sender:    sender,
		ThreadID:  1,
		SenderID:  2,
		StartTime: time.Now().Add(-2 * time.Hour),
	}

	cmd := &StatusCommand{}
	if err := cmd.Execute(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := sender.lastMessage
	checks := []string{
		"📊 Bot Status",
		"⏱ Uptime:",
		"💾 RAM:",
		"📦 Alloc:",
		"🔄 GC Cycles:",
		"🧵 Goroutines:",
		"💻 OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH,
		"🔧 Go: " + runtime.Version(),
	}
	for _, check := range checks {
		if !strings.Contains(msg, check) {
			t.Errorf("expected message to contain %q, got:\n%s", check, msg)
		}
	}
}

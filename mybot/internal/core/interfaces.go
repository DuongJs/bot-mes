package core

import (
	"context"
	"time"
)

// MessageSender abstracts the underlying transport (e.g., Facebook/Messagix).
type MessageSender interface {
	SendMessage(ctx context.Context, threadID int64, text string) error
	SendMedia(ctx context.Context, threadID int64, data []byte, filename, mimeType string) error
	GetSelfID() int64
}

// CommandContext provides context for command execution.
type CommandContext struct {
	Ctx       context.Context
	Sender    MessageSender
	ThreadID  int64
	SenderID  int64
	Args      []string
	RawText   string
	StartTime time.Time
}

// CommandHandler handles the execution of a command.
type CommandHandler interface {
	Execute(ctx *CommandContext) error
	Description() string
	Name() string
}

// Service is a marker interface for business logic services.
type Service interface {
	Name() string
}

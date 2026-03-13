package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// MessageSender abstracts the underlying transport (e.g., Facebook/Messagix).
type MessageSender interface {
	SendMessage(ctx context.Context, threadID int64, text string) error
	SendMedia(ctx context.Context, threadID int64, data []byte, filename, mimeType string, caption ...string) error
	SendMultiMedia(ctx context.Context, threadID int64, items []MediaAttachment, caption ...string) error
	GetSelfID() int64
}

// MediaAttachment represents a single media file to be uploaded and sent.
// It supports two modes:
//   - In-memory: Data is set directly (legacy).
//   - File-backed: FilePath points to a temp file on disk (memory-efficient).
//
// Use OpenReader() for streaming or GetData() for in-memory access.
// Call Cleanup() to release resources.
type MediaAttachment struct {
	Data     []byte
	FilePath string // if set, data is read from this file instead of Data
	FileSize int64  // size in bytes (set when using FilePath)
	Filename string
	MimeType string
}

// GetData returns the media payload bytes.
// If FilePath is set, the data is read from disk.
// Prefer OpenReader() for large files to avoid loading everything into RAM.
func (m *MediaAttachment) GetData() ([]byte, error) {
	if m.Data != nil {
		return m.Data, nil
	}
	if m.FilePath != "" {
		return os.ReadFile(m.FilePath)
	}
	return nil, fmt.Errorf("media attachment has no data or file path")
}

// OpenReader returns a streaming reader for the media payload.
// For file-backed attachments this avoids loading the entire file into RAM.
// The caller MUST close the returned ReadCloser.
func (m *MediaAttachment) OpenReader() (io.ReadCloser, error) {
	if m.FilePath != "" {
		return os.Open(m.FilePath)
	}
	if m.Data != nil {
		return io.NopCloser(bytes.NewReader(m.Data)), nil
	}
	return nil, fmt.Errorf("media attachment has no data or file path")
}

// DataSize returns the size of the media payload without loading it into memory.
func (m *MediaAttachment) DataSize() int64 {
	if m.Data != nil {
		return int64(len(m.Data))
	}
	return m.FileSize
}

// Cleanup releases resources: nils in-memory data and removes temp file.
func (m *MediaAttachment) Cleanup() {
	m.Data = nil
	if m.FilePath != "" {
		if err := os.Remove(m.FilePath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: failed to remove temp file %s: %v\n", m.FilePath, err)
		}
		m.FilePath = ""
	}
}

// CommandContext provides context for command execution.
type CommandContext struct {
	Ctx               context.Context
	Sender            MessageSender
	Messages          MessageController
	Conversation      ConversationReader
	ThreadID          int64
	SenderID          int64
	IncomingMessageID string
	Args              []string
	RawText           string
	StartTime         time.Time
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

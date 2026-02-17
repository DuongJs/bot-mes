package dashboard

import (
	"sync"
	"time"
)

// LogEntry represents a single log entry.
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// LogBuffer is a thread-safe ring buffer that stores recent log entries.
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
}

// NewLogBuffer creates a new LogBuffer with the given capacity.
func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, 0, size),
		maxSize: size,
	}
}

// Add appends a log entry to the buffer, evicting the oldest if full.
func (lb *LogBuffer) Add(level, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	entry := LogEntry{
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Message: message,
	}
	if len(lb.entries) >= lb.maxSize {
		lb.entries = lb.entries[1:]
	}
	lb.entries = append(lb.entries, entry)
}

// Entries returns a copy of all buffered log entries.
func (lb *LogBuffer) Entries() []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	out := make([]LogEntry, len(lb.entries))
	copy(out, lb.entries)
	return out
}

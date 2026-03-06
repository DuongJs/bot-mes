package messaging

import (
	"context"
	"fmt"
	"sync"
	"time"

	"mybot/internal/core"
)

// ScheduledMessage represents a message queued for future delivery.
type ScheduledMessage struct {
	ID        string
	ThreadID  int64
	Text      string
	SendAt    time.Time
	CreatedAt time.Time
	Cancelled bool
}

// Scheduler manages future-delivery messages.
// Ported from JS FCA scheduler.js.
type Scheduler struct {
	mu       sync.Mutex
	items    map[string]*schedulerEntry
	nextID   int
	sender   core.MessageSender
	cancelFn context.CancelFunc
}

type schedulerEntry struct {
	msg    ScheduledMessage
	timer  *time.Timer
	cancel context.CancelFunc
}

// NewScheduler creates a Scheduler backed by the given sender.
func NewScheduler(sender core.MessageSender) *Scheduler {
	return &Scheduler{
		items:  make(map[string]*schedulerEntry),
		sender: sender,
	}
}

// Schedule queues a message to be sent at sendAt.  Returns a unique ID that
// can be used to cancel.
func (s *Scheduler) Schedule(ctx context.Context, threadID int64, text string, sendAt time.Time) (string, error) {
	delay := time.Until(sendAt)
	if delay <= 0 {
		return "", fmt.Errorf("thời gian gửi phải ở tương lai")
	}

	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("sched_%d_%d", s.nextID, time.Now().UnixMilli())
	entry := &schedulerEntry{
		msg: ScheduledMessage{
			ID:        id,
			ThreadID:  threadID,
			Text:      text,
			SendAt:    sendAt,
			CreatedAt: time.Now(),
		},
	}

	sendCtx, cancel := context.WithCancel(ctx)
	entry.cancel = cancel
	entry.timer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		e, ok := s.items[id]
		if ok {
			delete(s.items, id)
		}
		s.mu.Unlock()
		if !ok || e.msg.Cancelled {
			return
		}
		_ = s.sender.SendMessage(sendCtx, threadID, text)
	})

	s.items[id] = entry
	s.mu.Unlock()

	return id, nil
}

// Cancel cancels a scheduled message by ID.
func (s *Scheduler) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.items[id]
	if !ok {
		return false
	}
	entry.msg.Cancelled = true
	entry.timer.Stop()
	if entry.cancel != nil {
		entry.cancel()
	}
	delete(s.items, id)
	return true
}

// List returns all pending scheduled messages.
func (s *Scheduler) List() []ScheduledMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ScheduledMessage, 0, len(s.items))
	for _, e := range s.items {
		if !e.msg.Cancelled {
			out = append(out, e.msg)
		}
	}
	return out
}

// CancelAll stops all scheduled messages.
func (s *Scheduler) CancelAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.items)
	for _, e := range s.items {
		e.timer.Stop()
		if e.cancel != nil {
			e.cancel()
		}
	}
	s.items = make(map[string]*schedulerEntry)
	return n
}

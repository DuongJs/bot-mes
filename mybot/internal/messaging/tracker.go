package messaging

import (
	"context"
	"sync"

	"mybot/internal/core"
)

type Tracker struct {
	mu      sync.Mutex
	waiters map[string][]chan *core.MessageRecord
}

func NewTracker() *Tracker {
	return &Tracker{
		waiters: make(map[string][]chan *core.MessageRecord),
	}
}

func (t *Tracker) WaitForEdit(ctx context.Context, messageID, text string) (*core.MessageRecord, error) {
	ch := make(chan *core.MessageRecord, 1)

	t.mu.Lock()
	t.waiters[messageID] = append(t.waiters[messageID], ch)
	t.mu.Unlock()

	defer t.removeWaiter(messageID, ch)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case rec := <-ch:
			if rec == nil {
				continue
			}
			if text == "" || rec.Text == text {
				return rec, nil
			}
		}
	}
}

func (t *Tracker) NotifyEdit(rec *core.MessageRecord) {
	if rec == nil || rec.MessageID == "" {
		return
	}

	t.mu.Lock()
	waiters := append([]chan *core.MessageRecord(nil), t.waiters[rec.MessageID]...)
	t.mu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- rec:
		default:
		}
	}
}

func (t *Tracker) removeWaiter(messageID string, target chan *core.MessageRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()

	waiters := t.waiters[messageID]
	if len(waiters) == 0 {
		return
	}

	filtered := waiters[:0]
	for _, ch := range waiters {
		if ch != target {
			filtered = append(filtered, ch)
		}
	}
	if len(filtered) == 0 {
		delete(t.waiters, messageID)
		return
	}
	t.waiters[messageID] = filtered
}

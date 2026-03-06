package messaging

import (
	"context"
	"fmt"
	"time"

	"mybot/internal/core"
)

// BroadcastResult holds the outcome of sending to a single thread.
type BroadcastResult struct {
	ThreadID int64
	OK       bool
	Err      error
}

// Broadcast sends the same text message to multiple threads with a delay
// between each send to avoid rate-limiting.  It returns one result per thread.
//
// Ported from JS FCA broadcast.js.
func Broadcast(ctx context.Context, sender core.MessageSender, threadIDs []int64, text string, delayBetween time.Duration) []BroadcastResult {
	if delayBetween <= 0 {
		delayBetween = time.Second
	}

	results := make([]BroadcastResult, 0, len(threadIDs))
	for i, tid := range threadIDs {
		if ctx.Err() != nil {
			results = append(results, BroadcastResult{ThreadID: tid, Err: ctx.Err()})
			continue
		}

		err := sender.SendMessage(ctx, tid, text)
		results = append(results, BroadcastResult{
			ThreadID: tid,
			OK:       err == nil,
			Err:      err,
		})

		// Rate-limit delay between sends (skip after last).
		if i < len(threadIDs)-1 && delayBetween > 0 {
			select {
			case <-time.After(delayBetween):
			case <-ctx.Done():
			}
		}
	}
	return results
}

// BroadcastSummary returns a human-readable summary of broadcast results.
func BroadcastSummary(results []BroadcastResult) string {
	ok, fail := 0, 0
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
		}
	}
	return fmt.Sprintf("Broadcast: %d/%d thành công, %d thất bại", ok, len(results), fail)
}

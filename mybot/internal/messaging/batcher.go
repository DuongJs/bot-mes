package messaging

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"mybot/internal/core"
	"mybot/internal/metrics"
)

// ── Write operation types ───────────────────────────────────────────────────

type writeOpKind int

const (
	opUpsertThread writeOpKind = iota
	opUpsertUser
	opUpsertMessage
	opSetLastBot
	opClearLastBot
	opClearLastBotByThread
)

type writeOp struct {
	kind   writeOpKind
	thread *core.ThreadRecord
	user   *core.UserRecord
	msg    *core.MessageRecord
	// for SetLastBotMessage / ClearLastBotMessage
	threadID  int64
	messageID string
	err       chan error
}

// ── WriteBatcher ────────────────────────────────────────────────────────────

// WriteBatcher groups individual write operations into batched SQLite
// transactions for dramatically higher throughput.
type WriteBatcher struct {
	store    *SQLiteStore
	log      zerolog.Logger
	queue    chan writeOp
	maxBatch int
	flushDur time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewWriteBatcher creates a batcher that reads from an internal queue and
// flushes to the underlying store in batched transactions.
func NewWriteBatcher(store *SQLiteStore, log zerolog.Logger, queueSize, maxBatch int, flushMs int) *WriteBatcher {
	if queueSize <= 0 {
		queueSize = 500
	}
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if flushMs <= 0 {
		flushMs = 50
	}
	b := &WriteBatcher{
		store:    store,
		log:      log.With().Str("component", "write_batcher").Logger(),
		queue:    make(chan writeOp, queueSize),
		maxBatch: maxBatch,
		flushDur: time.Duration(flushMs) * time.Millisecond,
		stopCh:   make(chan struct{}),
	}
	b.wg.Add(1)
	go b.loop()
	return b
}

// Submit enqueues a write operation and blocks until it is committed.
func (b *WriteBatcher) Submit(op writeOp) error {
	op.err = make(chan error, 1)
	select {
	case b.queue <- op:
	case <-b.stopCh:
		return context.Canceled
	}
	return <-op.err
}

// Stop signals the batcher to drain remaining work and exit.
func (b *WriteBatcher) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	b.wg.Wait()
}

func (b *WriteBatcher) loop() {
	defer b.wg.Done()

	batch := make([]writeOp, 0, b.maxBatch)
	timer := time.NewTimer(b.flushDur)
	defer timer.Stop()

	for {
		// Wait for the first item or stop signal.
		select {
		case op := <-b.queue:
			batch = append(batch, op)
		case <-b.stopCh:
			// Drain remaining queue items.
			b.drainAndFlush()
			return
		}

		// Collect more items up to maxBatch or flushDur.
		timer.Reset(b.flushDur)
	collect:
		for len(batch) < b.maxBatch {
			select {
			case op := <-b.queue:
				batch = append(batch, op)
			case <-timer.C:
				break collect
			case <-b.stopCh:
				// Stop requested during collection; flush what we have then drain.
				b.flush(batch)
				batch = batch[:0]
				b.drainAndFlush()
				return
			}
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		b.flush(batch)
		batch = batch[:0]
	}
}

func (b *WriteBatcher) drainAndFlush() {
	batch := make([]writeOp, 0, b.maxBatch)
	for {
		select {
		case op := <-b.queue:
			batch = append(batch, op)
			if len(batch) >= b.maxBatch {
				b.flush(batch)
				batch = batch[:0]
			}
		default:
			if len(batch) > 0 {
				b.flush(batch)
			}
			return
		}
	}
}

func (b *WriteBatcher) flush(ops []writeOp) {
	if len(ops) == 0 {
		return
	}

	start := time.Now()
	err := b.store.ExecBatch(func(tx txExecer) error {
		for i := range ops {
			if opErr := b.execOp(tx, &ops[i]); opErr != nil {
				return opErr
			}
		}
		return nil
	})
	dur := time.Since(start)

	metrics.Global.RecordDBWrite(len(ops), dur)

	// Notify all waiters.
	for i := range ops {
		ops[i].err <- err
	}

	if err != nil {
		b.log.Error().Err(err).Int("batch_size", len(ops)).Dur("dur", dur).Msg("Batch write failed")
	} else if dur > 100*time.Millisecond {
		b.log.Warn().Int("batch_size", len(ops)).Dur("dur", dur).Msg("Slow batch write")
	}
}

func (b *WriteBatcher) execOp(tx txExecer, op *writeOp) error {
	switch op.kind {
	case opUpsertThread:
		return b.store.upsertThreadTx(tx, op.thread)
	case opUpsertUser:
		return b.store.upsertUserTx(tx, op.user)
	case opUpsertMessage:
		return b.store.upsertMessageTx(tx, op.msg)
	case opSetLastBot:
		return b.store.setLastBotMessageTx(tx, op.threadID, op.messageID)
	case opClearLastBot:
		return b.store.clearLastBotMessageTx(tx, op.threadID, op.messageID)
	case opClearLastBotByThread:
		return b.store.clearLastBotMessageByThreadTx(tx, op.threadID)
	default:
		return nil
	}
}

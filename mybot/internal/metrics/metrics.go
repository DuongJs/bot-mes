package metrics

import (
	"runtime"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Perf holds application-wide performance counters.
// All fields are safe for concurrent use.
type Perf struct {
	MessagesReceived  atomic.Int64
	MessagesProcessed atomic.Int64
	MessagesDropped   atomic.Int64
	CommandsExecuted  atomic.Int64
	SendRateLimited   atomic.Int64

	DBWriteOps       atomic.Int64
	DBWriteBatches   atomic.Int64
	DBWriteDurationNs atomic.Int64 // total nanoseconds spent writing

	WorkerQueueDepth atomic.Int64
}

// Global is the singleton metrics instance.
var Global = &Perf{}

// RecordDBWrite records one batch write operation.
func (p *Perf) RecordDBWrite(ops int, dur time.Duration) {
	p.DBWriteOps.Add(int64(ops))
	p.DBWriteBatches.Add(1)
	p.DBWriteDurationNs.Add(dur.Nanoseconds())
}

// Snapshot returns a point-in-time copy of all counters and resets
// accumulating counters (write duration) so the next interval is clean.
type Snapshot struct {
	MessagesReceived  int64
	MessagesProcessed int64
	MessagesDropped   int64
	CommandsExecuted  int64
	SendRateLimited   int64
	DBWriteOps        int64
	DBWriteBatches    int64
	DBWriteDurationMs int64
	WorkerQueueDepth  int64
}

// Snap takes a snapshot of all counters.
func (p *Perf) Snap() Snapshot {
	return Snapshot{
		MessagesReceived:  p.MessagesReceived.Load(),
		MessagesProcessed: p.MessagesProcessed.Load(),
		MessagesDropped:   p.MessagesDropped.Load(),
		CommandsExecuted:  p.CommandsExecuted.Load(),
		SendRateLimited:   p.SendRateLimited.Load(),
		DBWriteOps:        p.DBWriteOps.Load(),
		DBWriteBatches:    p.DBWriteBatches.Load(),
		DBWriteDurationMs: p.DBWriteDurationNs.Load() / int64(time.Millisecond),
		WorkerQueueDepth:  p.WorkerQueueDepth.Load(),
	}
}

// StartPeriodicLog logs a snapshot every interval until ctx is done.
func StartPeriodicLog(log zerolog.Logger, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s := Global.Snap()

			// Collect runtime memory and goroutine stats.
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			numGoroutines := runtime.NumGoroutine()

			log.Info().
				Int64("msg_received", s.MessagesReceived).
				Int64("msg_processed", s.MessagesProcessed).
				Int64("msg_dropped", s.MessagesDropped).
				Int64("cmds_executed", s.CommandsExecuted).
				Int64("send_rate_limited", s.SendRateLimited).
				Int64("db_write_ops", s.DBWriteOps).
				Int64("db_batches", s.DBWriteBatches).
				Int64("db_write_ms_total", s.DBWriteDurationMs).
				Int64("worker_queue_depth", s.WorkerQueueDepth).
				Int("goroutines", numGoroutines).
				Uint64("heap_alloc_mb", memStats.HeapAlloc/1024/1024).
				Uint64("heap_inuse_mb", memStats.HeapInuse/1024/1024).
				Uint64("sys_mb", memStats.Sys/1024/1024).
				Uint32("gc_cycles", memStats.NumGC).
				Msg("Performance metrics")
		case <-stop:
			return
		}
	}
}

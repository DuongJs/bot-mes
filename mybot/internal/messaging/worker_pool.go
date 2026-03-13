package messaging

import (
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"

	"mybot/internal/metrics"
)

// Job is a unit of work submitted to the WorkerPool.
type Job struct {
	Fn func()
}

// WorkerPool maintains a fixed number of goroutines that pull work from a
// shared job queue.  This replaces the unbounded goroutine-per-message
// semaphore pattern with predictable resource usage.
type WorkerPool struct {
	log     zerolog.Logger
	queue   chan Job
	wg      sync.WaitGroup
	workers int
	stopped atomic.Bool
}

// NewWorkerPool creates a pool with the given number of workers and queue capacity.
func NewWorkerPool(log zerolog.Logger, workers, queueSize int) *WorkerPool {
	if workers <= 0 {
		workers = 20
	}
	if queueSize <= 0 {
		queueSize = 500
	}
	p := &WorkerPool{
		log:     log.With().Str("component", "worker_pool").Logger(),
		queue:   make(chan Job, queueSize),
		workers: workers,
	}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go p.worker(i)
	}
	p.log.Info().Int("workers", workers).Int("queue_size", queueSize).Msg("Worker pool started")
	return p
}

// Submit enqueues a job.  Returns false if the pool is stopped or the queue
// is full (the job is dropped in both cases).
func (p *WorkerPool) Submit(fn func()) bool {
	if p.stopped.Load() {
		metrics.Global.MessagesDropped.Add(1)
		return false
	}
	select {
	case p.queue <- Job{Fn: fn}:
		metrics.Global.WorkerQueueDepth.Store(int64(len(p.queue)))
		return true
	default:
		metrics.Global.MessagesDropped.Add(1)
		p.log.Warn().Int("queue_len", len(p.queue)).Msg("Worker queue full, dropping job")
		return false
	}
}

// QueueLen returns the current number of pending jobs.
func (p *WorkerPool) QueueLen() int {
	return len(p.queue)
}

// Stop signals all workers to finish and waits for them to drain.
func (p *WorkerPool) Stop() {
	if p.stopped.Swap(true) {
		return // already stopped
	}
	close(p.queue)
	p.wg.Wait()
	p.log.Info().Msg("Worker pool stopped")
}

func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	for job := range p.queue {
		metrics.Global.WorkerQueueDepth.Store(int64(len(p.queue)))
		func() {
			defer func() {
				if r := recover(); r != nil {
					p.log.Error().Int("worker_id", id).Interface("panic", r).Msg("Worker recovered from panic")
				}
			}()
			job.Fn()
		}()
	}
}

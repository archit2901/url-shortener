package analytics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/archit2901/url-shortener/backend/internal/repository"
)

// ClickRepository is what the worker needs from the repository layer.
// Defined as an interface so we can mock it in tests.
type ClickRepository interface {
	InsertBatch(ctx context.Context, clicks []repository.Click) (int, error)
}

// Config controls worker pool behavior.
type Config struct {
	NumWorkers    int           // how many goroutines drain the channel
	ChannelSize   int           // buffer capacity (events held in memory)
	BatchSize     int           // flush when this many events accumulate
	FlushInterval time.Duration // flush at least this often, even if batch isn't full
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		NumWorkers:    3,
		ChannelSize:   1000,
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
	}
}

// Worker is the analytics worker pool. It accepts click events through
// Record() and asynchronously batch-writes them via the repository.
type Worker struct {
	cfg     Config
	repo    ClickRepository
	log     *slog.Logger
	events  chan repository.Click
	wg      sync.WaitGroup
	dropped uint64
	mu      sync.Mutex // protects dropped counter
}

// New constructs a Worker. Call Start() to spin up the goroutines.
func New(cfg Config, repo ClickRepository, log *slog.Logger) *Worker {
	return &Worker{
		cfg:    cfg,
		repo:   repo,
		log:    log,
		events: make(chan repository.Click, cfg.ChannelSize),
	}
}

// Record submits a click event for async processing.
// If the channel is full, the event is dropped (non-blocking).
// Returns true if the event was accepted, false if dropped.
func (w *Worker) Record(click repository.Click) bool {
	select {
	case w.events <- click:
		return true
	default:
		// Channel is full — drop the event rather than block the caller.
		w.mu.Lock()
		w.dropped++
		dropped := w.dropped
		w.mu.Unlock()
		// Log every 100th drop so we don't spam logs under sustained overload.
		if dropped%100 == 1 {
			w.log.Warn("click event dropped, channel full", "total_dropped", dropped)
		}
		return false
	}
}

// Start launches the worker goroutines. Call Stop() to drain and exit.
func (w *Worker) Start() {
	for i := 0; i < w.cfg.NumWorkers; i++ {
		w.wg.Add(1)
		go w.run(i)
	}
	w.log.Info("analytics workers started", "count", w.cfg.NumWorkers)
}

// Stop closes the event channel and waits for all workers to drain and exit.
// Safe to call multiple times.
func (w *Worker) Stop(ctx context.Context) error {
	close(w.events)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		w.log.Info("analytics workers stopped cleanly")
		return nil
	case <-ctx.Done():
		w.log.Warn("analytics workers stop timed out, some events may be lost")
		return ctx.Err()
	}
}

// run is the per-worker loop. Each worker accumulates events into a batch
// and flushes when either the batch is full or the flush interval elapses.
func (w *Worker) run(id int) {
	defer w.wg.Done()

	batch := make([]repository.Click, 0, w.cfg.BatchSize)
	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Each flush gets its own short-deadline context so a slow DB
		// doesn't block shutdown indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, err := w.repo.InsertBatch(ctx, batch)
		if err != nil {
			w.log.Error("flush failed", "worker", id, "size", len(batch), "error", err)
		} else {
			w.log.Debug("flush ok", "worker", id, "size", n)
		}
		// Reuse the underlying array but reset length to zero.
		batch = batch[:0]
	}

	for {
		select {
		case click, ok := <-w.events:
			if !ok {
				// Channel closed — flush remaining and exit.
				flush()
				return
			}
			batch = append(batch, click)
			if len(batch) >= w.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

package ingest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrNoJob indicates an idle queue, not a worker failure.
var ErrNoJob = errors.New("no job available")

// ErrPaused asks the worker to retry after a fixed pause.
var ErrPaused = errors.New("job processing is temporarily paused")

// ErrBackpressure is a retryable pause caused by a configured queue limit.
var ErrBackpressure = fmt.Errorf("%w: pipeline backpressure", ErrPaused)

// Handler performs one idempotent durable job kind.
type Handler interface {
	Kind() JobKind
	Handle(ctx context.Context, job Job) error
}

// Worker claims one job at a time, renews its lease, and records bounded retries.
type Worker struct {
	Queue        Queue
	Handlers     map[JobKind]Handler
	Owner        string
	Lease        time.Duration
	PollInterval time.Duration
	MaxAttempts  int
	OnTransition func(context.Context, Job, JobStatus, error)
}

// Run processes jobs until cancellation or a queue-level failure. Cancellation
// releases the current lease for immediate recovery.
func (w *Worker) Run(ctx context.Context) error {
	if w.Queue == nil {
		return errors.New("ingest worker: nil queue")
	}
	if w.Lease <= 0 {
		w.Lease = time.Minute
	}
	if w.PollInterval <= 0 {
		w.PollInterval = 500 * time.Millisecond
	}
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 3
	}

	for {
		job, err := w.Queue.Claim(ctx, w.Owner, w.Lease)
		if err != nil {
			if errors.Is(err, ErrNoJob) {
				if err := wait(ctx, w.PollInterval); err != nil {
					return nil
				}
				continue
			}
			return fmt.Errorf("claim job: %w", err)
		}

		handler := w.Handlers[job.Kind]
		if handler == nil {
			if err := w.Queue.Fail(ctx, job.ID, fmt.Errorf("no handler for %q", job.Kind)); err != nil {
				return err
			}
			continue
		}

		handleErr := w.handleWithLease(ctx, handler, *job)
		if ctx.Err() != nil || errors.Is(handleErr, context.Canceled) {
			cause := handleErr
			if cause == nil {
				cause = ctx.Err()
			}
			releaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			releaseErr := w.Queue.Retry(releaseContext, job.ID, time.Now(), cause)
			cancel()
			if releaseErr != nil {
				return fmt.Errorf("release job lease: %w", releaseErr)
			}
			w.notify(context.Background(), *job, JobRetry, cause)
			return nil
		}
		if handleErr != nil {
			err := handleErr
			if errors.Is(err, ErrBackpressure) {
				if retryErr := w.Queue.Retry(ctx, job.ID, time.Now().Add(30*time.Second), err); retryErr != nil {
					return retryErr
				}
				w.notify(ctx, *job, JobRetry, err)
				continue
			}
			if job.Attempts >= w.MaxAttempts {
				if failErr := w.Queue.Fail(ctx, job.ID, err); failErr != nil {
					return failErr
				}
				w.notify(ctx, *job, JobFailed, err)
				continue
			}
			delay := time.Duration(math.Pow(2, float64(job.Attempts-1))) * time.Second
			if errors.Is(err, ErrPaused) {
				delay = 30 * time.Second
			}
			if delay > time.Minute {
				delay = time.Minute
			}
			if retryErr := w.Queue.Retry(ctx, job.ID, time.Now().Add(delay), err); retryErr != nil {
				return retryErr
			}
			w.notify(ctx, *job, JobRetry, err)
			continue
		}

		if err := w.Queue.Complete(ctx, job.ID); err != nil {
			return fmt.Errorf("complete job: %w", err)
		}
		w.notify(ctx, *job, JobDone, nil)
	}
}

func (w *Worker) handleWithLease(ctx context.Context, handler Handler, job Job) error {
	jobContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- handler.Handle(jobContext, job)
	}()
	renewEvery := w.Lease / 3
	if renewEvery < 100*time.Millisecond {
		renewEvery = 100 * time.Millisecond
	}
	ticker := time.NewTicker(renewEvery)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			cancel()
			return <-result
		case <-ticker.C:
			if err := w.Queue.Renew(ctx, job.ID, w.Owner, w.Lease); err != nil {
				cancel()
				<-result
				return fmt.Errorf("renew job lease: %w", err)
			}
		}
	}
}

func (w *Worker) notify(ctx context.Context, job Job, status JobStatus, cause error) {
	if w.OnTransition != nil {
		w.OnTransition(ctx, job, status, cause)
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWorkerReleasesLeaseWhenCancelledDuringHandler(t *testing.T) {
	queue := &cancellationQueue{job: &Job{ID: "job-1", Kind: JobExtractAnalysisBatch, Attempts: 1}}
	started := make(chan struct{})
	handler := blockingHandler{started: started}
	worker := &Worker{
		Queue: queue, Handlers: map[JobKind]Handler{handler.Kind(): handler}, Owner: "worker-1",
		Lease: time.Minute, PollInterval: time.Millisecond, MaxAttempts: 3,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if !queue.retried {
		t.Fatal("running job lease was not released to retry")
	}
	if queue.retryContextError != nil {
		t.Fatalf("lease release used cancelled context: %v", queue.retryContextError)
	}
}

func TestWorkerFailsUnavailableJobAtAttemptLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &failureQueue{job: &Job{ID: "job-1", Kind: JobExtractAnalysisBatch, Attempts: 3}, cancel: cancel}
	handler := errorHandler{err: ErrPaused}
	worker := &Worker{Queue: queue, Handlers: map[JobKind]Handler{handler.Kind(): handler}, Owner: "worker-1", PollInterval: time.Millisecond, MaxAttempts: 3}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !queue.failed || queue.retried {
		t.Fatalf("failed=%v retried=%v", queue.failed, queue.retried)
	}
}

func TestWorkerKeepsBackpressureJobQueued(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &failureQueue{job: &Job{ID: "job-1", Kind: JobExtractAnalysisBatch, Attempts: 3}, cancel: cancel}
	handler := errorHandler{err: ErrBackpressure}
	worker := &Worker{Queue: queue, Handlers: map[JobKind]Handler{handler.Kind(): handler}, Owner: "worker-1", PollInterval: time.Millisecond, MaxAttempts: 3}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if queue.failed || !queue.retried {
		t.Fatalf("failed=%v retried=%v", queue.failed, queue.retried)
	}
}

type errorHandler struct{ err error }

func (h errorHandler) Kind() JobKind                     { return JobExtractAnalysisBatch }
func (h errorHandler) Handle(context.Context, Job) error { return h.err }

type failureQueue struct {
	job             *Job
	cancel          context.CancelFunc
	failed, retried bool
}

func (q *failureQueue) Claim(context.Context, string, time.Duration) (*Job, error) {
	if q.job == nil {
		return nil, ErrNoJob
	}
	job := q.job
	q.job = nil
	return job, nil
}
func (q *failureQueue) Complete(context.Context, string) error                     { return nil }
func (q *failureQueue) Renew(context.Context, string, string, time.Duration) error { return nil }
func (q *failureQueue) Retry(context.Context, string, time.Time, error) error {
	q.retried = true
	q.cancel()
	return nil
}
func (q *failureQueue) Fail(context.Context, string, error) error {
	q.failed = true
	q.cancel()
	return nil
}
func (q *failureQueue) Stats(context.Context) (QueueStats, error) { return QueueStats{}, nil }

type blockingHandler struct {
	started chan struct{}
}

func (h blockingHandler) Kind() JobKind { return JobExtractAnalysisBatch }
func (h blockingHandler) Handle(ctx context.Context, _ Job) error {
	close(h.started)
	<-ctx.Done()
	return ctx.Err()
}

type cancellationQueue struct {
	mu                sync.Mutex
	job               *Job
	retried           bool
	retryContextError error
}

func (q *cancellationQueue) Claim(context.Context, string, time.Duration) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.job == nil {
		return nil, ErrNoJob
	}
	job := *q.job
	q.job = nil
	return &job, nil
}

func (q *cancellationQueue) Complete(context.Context, string) error { return nil }
func (q *cancellationQueue) Renew(context.Context, string, string, time.Duration) error {
	return nil
}
func (q *cancellationQueue) Retry(ctx context.Context, _ string, _ time.Time, _ error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retried = true
	q.retryContextError = ctx.Err()
	return nil
}
func (q *cancellationQueue) Fail(context.Context, string, error) error {
	return errors.New("unexpected Fail")
}
func (q *cancellationQueue) Stats(context.Context) (QueueStats, error) { return QueueStats{}, nil }

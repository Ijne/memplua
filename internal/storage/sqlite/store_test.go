package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"crawler/internal/app"
	"crawler/internal/ingest"
	"crawler/internal/observability"
)

func TestFailedJobCanBeRetriedManually(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.Enqueue(ctx, ingest.JobExport, ingest.ExportPayload{Exporter: "test"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(ctx, job.ID, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if err := store.RetryFailedJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := claimJobByID(ctx, store, job.ID, "manual", 10)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Attempts != 1 || retried.LastError != "" {
		t.Fatalf("manual retry did not reset job: %+v", retried)
	}
}

func TestEventCursorDoesNotLoseEventsFromSameMillisecond(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, eventID := range []string{"z-first", "a-second"} {
		if err := store.SaveEvent(ctx, observability.Event{
			ID: eventID, Time: now, Severity: observability.SeverityInfo, Type: "test", Message: eventID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListEvents(ctx, "z-first", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "a-second" {
		t.Fatalf("events after cursor = %+v", events)
	}
}

func TestStartingRunRecoversInterruptedRuntimeState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	firstRun := app.AppRun{ID: "run-1", Version: "test", State: app.StateRunning, StartedAt: time.Now().UTC()}
	if err := store.StartAppRun(ctx, firstRun); err != nil {
		t.Fatal(err)
	}
	session := app.SourceSession{ID: "interrupted-source", SourceID: "fake", Kind: "test", State: app.SourceRunning, StartedAt: time.Now().UTC()}
	if err := store.SaveSourceSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, ingest.JobExport, ingest.ExportPayload{Exporter: "test"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx, "old-worker", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	secondRun := app.AppRun{ID: "run-2", Version: "test", State: app.StateStarting, StartedAt: time.Now().UTC()}
	if err := store.StartAppRun(ctx, secondRun); err != nil {
		t.Fatal(err)
	}
	var runState app.State
	var runError string
	if err := store.db.QueryRow(`SELECT state, last_error FROM app_runs WHERE id = ?`, firstRun.ID).Scan(&runState, &runError); err != nil {
		t.Fatal(err)
	}
	if runState != app.StateStopped || runError == "" {
		t.Fatalf("stale app run was not recovered: state=%s error=%q", runState, runError)
	}
	recoveredSession, err := store.GetSourceSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredSession.State != app.SourceFailed || recoveredSession.LastError == "" {
		t.Fatalf("stale source session was not recovered: %+v", recoveredSession)
	}
	recoveredJob, err := claimJobByID(ctx, store, job.ID, "new-worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredJob.LeaseOwner != "new-worker" || recoveredJob.Status != ingest.JobRunning {
		t.Fatalf("stale job was not recovered: %+v", recoveredJob)
	}
}

func TestStartingRunRetriesOnlyTransientFailedExtractions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.Enqueue(ctx, ingest.JobExtractAnalysisBatch, ingest.BatchPayload{AnalysisBatchID: "transient"}); err != nil {
		t.Fatal(err)
	}
	transient, err := store.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Fail(ctx, transient.ID, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(ctx, ingest.JobExtractAnalysisBatch, ingest.BatchPayload{AnalysisBatchID: "structural"}); err != nil {
		t.Fatal(err)
	}
	structural, err := store.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Fail(ctx, structural.ID, context.Canceled); err != nil {
		t.Fatal(err)
	}

	run := app.AppRun{ID: "recovery-run", Version: "test", State: app.StateStarting, StartedAt: time.Now().UTC()}
	if err = store.StartAppRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Claim(ctx, "new-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != transient.ID || recovered.Attempts != 1 || recovered.LastError != "" {
		t.Fatalf("transient extraction was not reset: %+v", recovered)
	}
	jobs, err := store.ListJobs(ctx, ingest.JobFailed, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != structural.ID {
		t.Fatalf("structural failures must remain failed: %+v", jobs)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func claimJobByID(ctx context.Context, store *Store, jobID, owner string, attempts int) (*ingest.Job, error) {
	for index := 0; index < attempts; index++ {
		job, err := store.Claim(ctx, owner, time.Minute)
		if err != nil {
			return nil, err
		}
		if job.ID == jobID {
			return job, nil
		}
		if err := store.Complete(ctx, job.ID); err != nil {
			return nil, err
		}
	}
	return nil, ingest.ErrNoJob
}

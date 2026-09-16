package processing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"crawler/internal/inference/prompts"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"crawler/internal/observability"
	"crawler/internal/review"
)

// Coordinator periodically closes analysis batches whose duration or idle
// deadline has elapsed.
type Coordinator struct {
	Repository interface {
		SweepBatches(context.Context, time.Time) error
	}
	Interval time.Duration
}

// Run performs one initial sweep and then sweeps until cancellation.
func (c Coordinator) Run(ctx context.Context) error {
	interval := c.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if err := c.Repository.SweepBatches(ctx, time.Now().UTC()); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := c.Repository.SweepBatches(ctx, now.UTC()); err != nil {
				return err
			}
		}
	}
}

// ExtractHandler runs one LLM extraction and persists success or diagnostic
// failure while respecting review backpressure.
type ExtractHandler struct {
	Repository ingest.Repository
	Extractor  ingest.Extractor
	Review     interface {
		PendingCount(context.Context) (int, error)
	}
	ReviewLimit int
	Model       string
	Events      *observability.Bus
}

// Kind returns the durable job kind handled by ExtractHandler.
func (h ExtractHandler) Kind() ingest.JobKind { return ingest.JobExtractAnalysisBatch }

// Handle loads one batch, enforces review pressure, performs extraction, and
// persists either a committed result or retry diagnostics.
func (h ExtractHandler) Handle(ctx context.Context, job ingest.Job) error {
	var payload ingest.BatchPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	batch, err := h.Repository.GetAnalysisBatch(ctx, payload.AnalysisBatchID)
	if err != nil {
		return err
	}
	if batch == nil {
		return errors.New("analysis batch does not exist")
	}
	if batch.Status == ingest.BatchExtracted {
		return nil
	}
	if h.Review != nil && h.ReviewLimit > 0 {
		count, err := h.Review.PendingCount(ctx)
		if err != nil {
			return err
		}
		if count >= h.ReviewLimit {
			return ingest.ErrBackpressure
		}
	}
	if err = h.Repository.BeginExtraction(ctx, batch.ID); err != nil {
		return err
	}
	extraction, raw, err := h.Extractor.Extract(ctx, batch.Input)
	if err == nil {
		err = h.Repository.SaveBatchExtraction(ctx, batch.ID, extraction, raw, h.Model, prompts.Version)
	}
	if err != nil {
		// Cancellation is retried by the durable worker; preserve a returned response even during shutdown.
		saveCtx := ctx
		var cancel context.CancelFunc
		if ctx.Err() != nil {
			saveCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}
		if saveErr := h.Repository.SaveBatchFailure(saveCtx, batch.ID, raw, h.Model, prompts.Version, err); saveErr != nil {
			return errors.Join(err, saveErr)
		}
	}
	return err
}

// PrepareReviewHandler builds deterministic review state for one conspect.
type PrepareReviewHandler struct{ Preparer review.Preparer }

// Kind returns the durable job kind handled by PrepareReviewHandler.
func (h PrepareReviewHandler) Kind() ingest.JobKind { return ingest.JobPrepareConspectReview }

// Handle decodes the conspect identifier and runs deterministic preparation.
func (h PrepareReviewHandler) Handle(ctx context.Context, job ingest.Job) error {
	var payload ingest.ConspectPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	return h.Preparer.Prepare(ctx, payload.ConspectID)
}

// SnapshotRepository supplies a consistent canonical graph to exporters.
type SnapshotRepository interface {
	Snapshot(ctx context.Context) (knowledge.Snapshot, error)
}

// ExportHandler runs a selected exporter or all registered exporters.
type ExportHandler struct {
	Repository SnapshotRepository
	Exporters  map[string]knowledge.Exporter
	Auto       bool
	Events     *observability.Bus
}

// Kind returns the durable job kind handled by ExportHandler.
func (h ExportHandler) Kind() ingest.JobKind { return ingest.JobExport }

// Handle reads one graph snapshot and runs the selected or all exporters.
func (h ExportHandler) Handle(ctx context.Context, job ingest.Job) error {
	var payload ingest.ExportPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	if payload.Automatic && !h.Auto {
		return nil
	}
	snapshot, err := h.Repository.Snapshot(ctx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(h.Exporters))
	if payload.Exporter != "" {
		if h.Exporters[payload.Exporter] == nil {
			return fmt.Errorf("unknown exporter %q", payload.Exporter)
		}
		names = append(names, payload.Exporter)
	} else {
		for name := range h.Exporters {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		if err := h.Exporters[name].Export(ctx, snapshot); err != nil {
			return fmt.Errorf("export %s: %w", name, err)
		}
		h.publish(ctx, observability.SeverityInfo, "export.completed", "Knowledge graph export completed", name)
	}
	return nil
}

func (h ExportHandler) publish(ctx context.Context, severity observability.Severity, eventType, message, resourceID string) {
	if h.Events != nil {
		h.Events.Publish(ctx, observability.Event{Severity: severity, Type: eventType, Message: message, ResourceKind: "exporter", ResourceID: resourceID, Time: time.Now().UTC()})
	}
}

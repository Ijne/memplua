package ingest

import (
	"context"
	"encoding/json"
	"time"
)

// ChunkStatus records whether a durable text chunk is processable.
type ChunkStatus string

const (
	ChunkReady  ChunkStatus = "ready"
	ChunkFailed ChunkStatus = "failed"
)

// Chunk is the smallest durable text unit emitted by a source session.
type Chunk struct {
	ID              string      `json:"id"`
	SourceSessionID string      `json:"source_session_id"`
	Sequence        int64       `json:"sequence"`
	CapturedAt      time.Time   `json:"captured_at"`
	Text            string      `json:"text"`
	Status          ChunkStatus `json:"status"`
}

// Provenance links an extracted candidate to focus chunks. AnchorChunkID is the
// strongest single location; EvidenceChunkIDs contains all supporting chunks.
type Provenance struct {
	EvidenceChunkIDs []string `json:"evidence_chunk_ids"`
	AnchorChunkID    string   `json:"anchor_chunk_id"`
}

// TermCandidate is an untrusted term proposed by extraction.
type TermCandidate struct {
	ID          string   `json:"id,omitempty"`
	Ref         string   `json:"ref"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Provenance
}

// ThoughtCandidate is an untrusted thought proposed by extraction.
type ThoughtCandidate struct {
	ID              string   `json:"id,omitempty"`
	Thesis          string   `json:"thesis"`
	Body            string   `json:"body"`
	RelatedTermRefs []string `json:"related_term_refs"`
	Tags            []string `json:"tags,omitempty"`
	Provenance
}

// TopicExtraction groups candidates that form one draft conspect.
type TopicExtraction struct {
	Topic    string             `json:"topic"`
	Terms    []TermCandidate    `json:"terms"`
	Thoughts []ThoughtCandidate `json:"thoughts"`
}

// Extraction is the strict normalized result of one analysis-batch LLM call.
type Extraction struct {
	Conspects []TopicExtraction `json:"conspects"`
}

// BatchStatus describes collection and extraction progress.
type BatchStatus string

const (
	BatchCollecting BatchStatus = "collecting"
	BatchReady      BatchStatus = "ready"
	BatchExtracting BatchStatus = "extracting"
	BatchExtracted  BatchStatus = "extracted"
	BatchFailed     BatchStatus = "failed"
)

// BatchInput separates continuity-only Context from uniquely owned Focus chunks.
type BatchInput struct {
	Context []Chunk `json:"context"`
	Focus   []Chunk `json:"focus"`
}

// AnalysisBatch is a non-overlapping focus window plus extraction diagnostics.
type AnalysisBatch struct {
	ID                 string      `json:"id"`
	SourceSessionID    string      `json:"source_session_id"`
	Status             BatchStatus `json:"status"`
	StartedAt          time.Time   `json:"started_at"`
	LastTextAt         time.Time   `json:"last_text_at"`
	ClosedAt           *time.Time  `json:"closed_at,omitempty"`
	CloseReason        string      `json:"close_reason,omitempty"`
	Input              BatchInput  `json:"input"`
	RawModelResponse   string      `json:"raw_model_response,omitempty"`
	NormalizedResponse string      `json:"normalized_response,omitempty"`
	Model              string      `json:"model,omitempty"`
	PromptVersion      string      `json:"prompt_version,omitempty"`
	LastError          string      `json:"last_error,omitempty"`
}

// BatchPolicy controls duration, idle close, and bounded context-tail behavior.
type BatchPolicy struct {
	MaxDuration      time.Duration
	IdleTimeout      time.Duration
	ContextTailChars int
}

// DefaultBatchPolicy returns production batching defaults.
func DefaultBatchPolicy() BatchPolicy { return BatchPolicy{5 * time.Minute, 90 * time.Second, 1500} }

// ConspectStatus describes progress through review and application.
type ConspectStatus string

const (
	ConspectPreparingReview ConspectStatus = "preparing_review"
	ConspectReviewPending   ConspectStatus = "review_pending"
	ConspectReadyToApply    ConspectStatus = "ready_to_apply"
	ConspectApplied         ConspectStatus = "applied"
	ConspectRejected        ConspectStatus = "rejected"
	ConspectFailed          ConspectStatus = "failed"
)

// Conspect is one topic-level draft produced from an analysis batch.
type Conspect struct {
	ID              string         `json:"id"`
	AnalysisBatchID string         `json:"analysis_batch_id"`
	SourceSessionID string         `json:"source_session_id"`
	CreatedAt       time.Time      `json:"created_at"`
	Status          ConspectStatus `json:"status"`
	TopicExtraction
}

// JobKind identifies a durable background operation.
type JobKind string

const (
	JobExtractAnalysisBatch  JobKind = "extract_analysis_batch"
	JobPrepareConspectReview JobKind = "prepare_conspect_review"
	JobExport                JobKind = "export"
)

// JobStatus records queue, lease, retry, completion, or terminal failure state.
type JobStatus string

const (
	JobQueued  JobStatus = "queued"
	JobRunning JobStatus = "running"
	JobRetry   JobStatus = "retry"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job is one leased durable task. Payload contains identifiers, not copied data.
type Job struct {
	ID          string          `json:"id"`
	Kind        JobKind         `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	Status      JobStatus       `json:"status"`
	Attempts    int             `json:"attempts"`
	AvailableAt time.Time       `json:"available_at"`
	LeaseOwner  string          `json:"lease_owner,omitempty"`
	LeaseUntil  time.Time       `json:"lease_until,omitempty"`
	LastError   string          `json:"last_error,omitempty"`
}

// BatchPayload addresses one analysis-batch extraction.
type BatchPayload struct {
	AnalysisBatchID string `json:"analysis_batch_id"`
}

// ConspectPayload addresses one conspect review-preparation job.
type ConspectPayload struct {
	ConspectID string `json:"conspect_id"`
}

// ExportPayload selects one/all exporters and records whether scheduling was automatic.
type ExportPayload struct {
	Exporter  string `json:"exporter,omitempty"`
	Automatic bool   `json:"automatic,omitempty"`
}

// QueueStats summarizes durable job states for health and backpressure.
type QueueStats struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Retry   int `json:"retry"`
	Failed  int `json:"failed"`
}

// TextSubmission identifies a manually submitted text chunk. Manual input is
// persisted through the same durable pipeline as captured audio transcripts.
type TextSubmission struct {
	SourceSessionID string    `json:"source_session_id"`
	ChunkID         string    `json:"chunk_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// TextSubmitter persists manually supplied text through the normal chunk pipeline.
type TextSubmitter interface {
	SubmitText(ctx context.Context, text string) (TextSubmission, error)
}

// Queue is the leased durable-job contract consumed by Worker.
type Queue interface {
	Claim(ctx context.Context, owner string, lease time.Duration) (*Job, error)
	Renew(ctx context.Context, id, owner string, lease time.Duration) error
	Complete(ctx context.Context, id string) error
	Retry(ctx context.Context, id string, availableAt time.Time, cause error) error
	Fail(ctx context.Context, id string, cause error) error
	Stats(ctx context.Context) (QueueStats, error)
}

// Repository contains persistence operations consumed by extraction handlers.
type Repository interface {
	SaveChunk(context.Context, Chunk) error
	GetAnalysisBatch(context.Context, string) (*AnalysisBatch, error)
	BeginExtraction(context.Context, string) error
	SaveBatchExtraction(context.Context, string, Extraction, string, string, string) error
	SaveBatchFailure(context.Context, string, string, string, string, error) error
	GetConspect(context.Context, string) (*Conspect, error)
}

// Extractor converts one batch input into validated candidate conspects.
type Extractor interface {
	Extract(context.Context, BatchInput) (Extraction, string, error)
}

// Source is a blocking, cancellation-aware producer of transcript chunks.
// Implementations own their resources and must make Close idempotent.
type Source interface {
	ID() string
	Kind() string
	Run(ctx context.Context, emit func(context.Context, Chunk) error) error
	Close() error
}

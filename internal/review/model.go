package review

import (
	"context"
	"crawler/internal/ingest"
	"crawler/internal/knowledge"
	"errors"
)

// ObjectKind identifies the canonical entity type represented by a review item.
type ObjectKind string

const (
	ObjectTerm    ObjectKind = "term"
	ObjectThought ObjectKind = "thought"
)

// Resolution is the user's intended canonical action for incoming candidates.
type Resolution string

const (
	Create       Resolution = "create"
	Update       Resolution = "update"
	KeepExisting Resolution = "keep_existing"
	Reject       Resolution = "reject"
)

// ItemStatus records whether an item still needs a decision or was applied.
type ItemStatus string

const (
	Pending  ItemStatus = "pending"
	Resolved ItemStatus = "resolved"
	Applied  ItemStatus = "applied"
	Failed   ItemStatus = "failed"
)

var (
	// ErrConflict means a reviewed target changed and matching was refreshed.
	ErrConflict = errors.New("canonical entity changed; review refreshed matches before applying")
	// ErrInvalid means a decision or final value violates the review contract.
	ErrInvalid = errors.New("invalid review decision")
	// ErrNotReady means at least one required item or taxonomy value is unresolved.
	ErrNotReady = errors.New("conspect has unresolved review items or taxonomy")
	// ErrExactMatch prevents creating a duplicate of an exact canonical match.
	ErrExactMatch = errors.New("an exact canonical match must be kept or overwritten")
	// ErrDependency means a surviving thought points to a rejected term item.
	ErrDependency = errors.New("thought references a rejected term")
)

// Value is the editable union of term and thought fields.
type Value struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Thesis      string   `json:"thesis,omitempty"`
	Body        string   `json:"body,omitempty"`
	Tags        []string `json:"tags"`
}

// Title returns the name or thesis used as the entity's display title.
func (v Value) Title(kind ObjectKind) string {
	if kind == ObjectTerm {
		return v.Name
	}
	return v.Thesis
}

// Text returns all searchable textual fields for deterministic similarity.
func (v Value) Text(kind ObjectKind) string {
	if kind == ObjectTerm {
		return v.Name + " " + v.Description
	}
	return v.Thesis + " " + v.Body
}

// Variant preserves one incoming candidate inside a grouped review item.
type Variant struct {
	CandidateID     string   `json:"candidate_id"`
	Value           Value    `json:"value"`
	RelatedTermRefs []string `json:"related_term_refs,omitempty"`
	ingest.Provenance
}

// Match is an immutable snapshot of a possible canonical or pending target.
type Match struct {
	ID         string     `json:"id"`
	Kind       ObjectKind `json:"kind"`
	Source     string     `json:"source"`
	ConspectID string     `json:"conspect_id,omitempty"`
	Score      float64    `json:"score"`
	Exact      bool       `json:"exact"`
	Version    int64      `json:"version"`
	Value      Value      `json:"value"`
}

// Item contains one review decision, its variants, candidate targets, final
// value, dependencies, and optimistic target version.
type Item struct {
	ID               string     `json:"id"`
	ConspectID       string     `json:"conspect_id"`
	Kind             ObjectKind `json:"kind"`
	IncomingVariants []Variant  `json:"incoming_variants"`
	CanonicalMatches []Match    `json:"canonical_matches"`
	PendingMatches   []Match    `json:"pending_matches"`
	Resolution       Resolution `json:"resolution"`
	TargetID         string     `json:"target_id,omitempty"`
	TargetVersion    int64      `json:"target_version"`
	FinalValue       Value      `json:"final_value"`
	RelatedItemIDs   []string   `json:"related_item_ids"`
	Status           ItemStatus `json:"status"`
}

// TaxonomyResolution tracks an unknown tag awaiting map/create/remove review.
type TaxonomyResolution struct {
	ID         string                 `json:"id"`
	ConspectID string                 `json:"conspect_id"`
	Kind       knowledge.TaxonomyKind `json:"kind"`
	Value      string                 `json:"value"`
	Resolution string                 `json:"resolution"`
	TargetID   string                 `json:"target_id,omitempty"`
	Required   bool                   `json:"required"`
}

// Conspect combines the extracted topic with its prepared review state.
type Conspect struct {
	ingest.Conspect
	Items    []Item               `json:"items"`
	Taxonomy []TaxonomyResolution `json:"taxonomy"`
}

// Decision is the validated command used to resolve one review item.
type Decision struct {
	Resolution     Resolution `json:"resolution"`
	TargetID       string     `json:"target_id,omitempty"`
	TargetVersion  int64      `json:"target_version,omitempty"`
	FinalValue     *Value     `json:"final_value,omitempty"`
	RelatedItemIDs *[]string  `json:"related_item_ids,omitempty"`
}

// TaxonomyDecision resolves an unknown tag by mapping, creating, or removing it.
type TaxonomyDecision struct {
	Value      *string `json:"value,omitempty"`
	Resolution string  `json:"resolution"`
	TargetID   string  `json:"target_id,omitempty"`
}

// Repository is the persistence contract consumed by Service and Preparer.
type Repository interface {
	GetConspect(context.Context, string) (*ingest.Conspect, error)
	ListReviewConspects(context.Context, int) ([]Conspect, error)
	GetReviewConspect(context.Context, string) (*Conspect, error)
	PendingReviewItems(context.Context) ([]Item, error)
	SavePreparedReview(context.Context, string, []Item) error
	SaveReviewDecision(context.Context, string, Decision) error
	SaveTaxonomyDecision(context.Context, string, TaxonomyDecision) error
	SplitReviewItem(context.Context, string, []string) error
	ApplyConspect(context.Context, string) error
	PendingCount(context.Context) (int, error)
}

// Service exposes review use cases without storage or transport details.
type Service struct{ repository Repository }

// NewService creates a review service backed by repository.
func NewService(repository Repository) *Service { return &Service{repository: repository} }

// ListPending returns bounded pending conspects, defaulting to 50.
func (s *Service) ListPending(ctx context.Context, limit int) ([]Conspect, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repository.ListReviewConspects(ctx, limit)
}

// Get loads one conspect with all current review state.
func (s *Service) Get(ctx context.Context, id string) (*Conspect, error) {
	return s.repository.GetReviewConspect(ctx, id)
}

// Decide persists one item decision without changing canonical knowledge.
func (s *Service) Decide(ctx context.Context, id string, decision Decision) error {
	return s.repository.SaveReviewDecision(ctx, id, decision)
}

// Taxonomy persists one unknown-tag decision without changing the graph.
func (s *Service) Taxonomy(ctx context.Context, id string, decision TaxonomyDecision) error {
	return s.repository.SaveTaxonomyDecision(ctx, id, decision)
}

// Split moves selected candidate variants into a separate review item.
func (s *Service) Split(ctx context.Context, id string, variants []string) error {
	return s.repository.SplitReviewItem(ctx, id, variants)
}

// Apply asks the repository to atomically apply a fully resolved conspect.
func (s *Service) Apply(ctx context.Context, id string) error {
	return s.repository.ApplyConspect(ctx, id)
}

package knowledge

import (
	"context"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"strings"
	"time"
)

// Term is a versioned canonical concept with a stable ID.
type Term struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Thought is a versioned canonical assertion or idea with a stable ID.
type Thought struct {
	ID        string    `json:"id"`
	Thesis    string    `json:"thesis"`
	Body      string    `json:"body"`
	Tags      []string  `json:"tags"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ThoughtTerm links one canonical thought to one canonical term.
type ThoughtTerm struct {
	ID        string    `json:"id"`
	ThoughtID string    `json:"thought_id"`
	TermID    string    `json:"term_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TaxonomyKind identifies a controlled taxonomy namespace.
type TaxonomyKind string

const (
	TaxonomyTag TaxonomyKind = "tag"
)

// TaxonomyEntry is an enabled or disabled canonical tag with aliases.
type TaxonomyEntry struct {
	ID        string       `json:"id"`
	Kind      TaxonomyKind `json:"kind"`
	Name      string       `json:"name"`
	Aliases   []string     `json:"aliases"`
	System    bool         `json:"system"`
	Enabled   bool         `json:"enabled"`
	CreatedAt time.Time    `json:"created_at"`
}

// Snapshot is a consistent read-only canonical graph for queries and exporters.
type Snapshot struct {
	Revision int64         `json:"revision"`
	Terms    []Term        `json:"terms"`
	Thoughts []Thought     `json:"thoughts"`
	Links    []ThoughtTerm `json:"links"`
}

// Repository exposes canonical lookup and controlled-taxonomy operations to
// inference and review without exposing persistence details.
type Repository interface {
	Snapshot(ctx context.Context) (Snapshot, error)
	ListTerms(ctx context.Context) ([]Term, error)
	ListThoughts(ctx context.Context) ([]Thought, error)
	FindTerm(ctx context.Context, name string) (*Term, error)
	FindThought(ctx context.Context, thesis string) (*Thought, error)
	ResolveTaxonomy(ctx context.Context, kind TaxonomyKind, value string) (*TaxonomyEntry, error)
	ListTaxonomy(ctx context.Context, kind TaxonomyKind) ([]TaxonomyEntry, error)
}

// Exporter renders an independent projection of a canonical snapshot.
type Exporter interface {
	Name() string
	Export(ctx context.Context, snapshot Snapshot) error
}

// Normalize produces a case-folded, whitespace-clean comparison value.
func Normalize(value string) string {
	return cases.Fold().String(Clean(value))
}

// Clean trims and collapses Unicode whitespace for display/storage.
func Clean(value string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(value)), " ")
}

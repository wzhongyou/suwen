// Package query handles query understanding: intent classification, rewriting, and expansion.
// Phase 1: simple pass-through of raw queries.
// Phase 2: LLM-powered intent classification and query expansion.
package query

import "context"

// Intent classifies the type of query.
type Intent string

const (
	IntentFactual      Intent = "factual"
	IntentHowTo        Intent = "howto"
	IntentNavigational Intent = "navigational"
	IntentExploratory  Intent = "exploratory"
)

// ParsedQuery is the structured output of query understanding.
type ParsedQuery struct {
	Raw           string   // Original raw query from user
	Rewrites      []string // Expanded/rewritten queries (includes original)
	Intent        Intent   // Intent classification
	Domain        string   // Domain label (e.g. "database", "programming")
	VectorWeight  float64  // Weight for vector search results
	KeywordWeight float64  // Weight for keyword (BM25) search results
}

// Parser is the interface for query understanding.
type Parser interface {
	Parse(ctx context.Context, raw string) (*ParsedQuery, error)
}

// SimpleParser is the Phase 1 pass-through implementation.
// It returns the raw query as-is without rewriting or classification.
type SimpleParser struct{}

// NewSimpleParser creates a SimpleParser.
func NewSimpleParser() *SimpleParser {
	return &SimpleParser{}
}

// Parse implements Parser by passing through the raw query unchanged.
func (p *SimpleParser) Parse(_ context.Context, raw string) (*ParsedQuery, error) {
	return &ParsedQuery{
		Raw:           raw,
		Rewrites:      []string{raw},
		Intent:        IntentFactual,
		Domain:        "",
		VectorWeight:  0.5,
		KeywordWeight: 0.5,
	}, nil
}

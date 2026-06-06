// Package ranking provides multi-stage result ranking.
// Phase 1: pass-through (RRF results used directly).
// Phase 2: Cross-Encoder reranking on top candidates.
package ranking

import (
	"github.com/wzhongyou/suwen/pkg/config"
	"github.com/wzhongyou/suwen/pkg/retrieval"
)

// RankedResult wraps a SearchResult with its rerank score and position.
type RankedResult struct {
	*retrieval.SearchResult
	RerankScore float64
	Rank        int
}

// Ranker reranks a candidate list into a final ordered result set.
type Ranker interface {
	Rerank(query string, candidates []*retrieval.SearchResult) []*RankedResult
}

// PassthroughRanker (Phase 1) returns candidates as-is, preserving their FinalScore order.
type PassthroughRanker struct{}

// NewPassthroughRanker creates a PassthroughRanker.
func NewPassthroughRanker() *PassthroughRanker {
	return &PassthroughRanker{}
}

// Rerank preserves the existing order from retrieval (RRF score).
func (r *PassthroughRanker) Rerank(_ string, candidates []*retrieval.SearchResult) []*RankedResult {
	results := make([]*RankedResult, 0, len(candidates))
	for i, c := range candidates {
		results = append(results, &RankedResult{
			SearchResult: c,
			RerankScore:  c.FinalScore,
			Rank:         i + 1,
		})
	}
	return results
}

// NewRanker creates the appropriate ranker based on config.
func NewRanker(cfg *config.Config) Ranker {
	if cfg.Ranking.CrossEncoderEnabled {
		// Phase 2: return CrossEncoderRanker here
		return NewPassthroughRanker()
	}
	return NewPassthroughRanker()
}

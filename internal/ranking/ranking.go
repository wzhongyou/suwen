// Package ranking provides multi-stage result ranking.
// Phase 1: pass-through (RRF results used directly).
// Phase 2: Cross-Encoder reranking on top candidates.
package ranking

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/wzhongyou/suwen/internal/config"
	"github.com/wzhongyou/suwen/internal/retrieval"
)

// RankedResult wraps a SearchResult with its rerank score and position.
type RankedResult struct {
	*retrieval.SearchResult
	RerankScore float64 `json:"rerank_score"`
	Rank        int     `json:"rank"`
}

// Ranker reranks a candidate list into a final ordered result set.
type Ranker interface {
	Rerank(ctx context.Context, query string, candidates []*retrieval.SearchResult) []*RankedResult
}

// PassthroughRanker (Phase 1) returns candidates as-is, preserving their FinalScore order.
type PassthroughRanker struct{}

// NewPassthroughRanker creates a PassthroughRanker.
func NewPassthroughRanker() *PassthroughRanker {
	return &PassthroughRanker{}
}

// Rerank preserves the existing order from retrieval (RRF score).
func (r *PassthroughRanker) Rerank(_ context.Context, _ string, candidates []*retrieval.SearchResult) []*RankedResult {
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

// ---- Phase 2: Cross-Encoder reranker ----

// CrossEncoderRanker reranks candidates using a Cross-Encoder model via HTTP API.
// It takes the top-N candidates from RRF fusion and sends them to a dedicated
// reranker service (e.g. bge-reranker-v2-m3 behind a FastAPI wrapper).
type CrossEncoderRanker struct {
	addr       string
	model      string
	httpClient *http.Client
	topK       int // number of candidates to send to Cross-Encoder
	limit      int // number to return after reranking
}

// NewCrossEncoderRanker creates a CrossEncoderRanker.
func NewCrossEncoderRanker(addr, model string) *CrossEncoderRanker {
	return &CrossEncoderRanker{
		addr:  addr,
		model: model,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		topK:  30,
		limit: 10,
	}
}

// rerankRequest is the JSON body sent to the Cross-Encoder service.
type rerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	Model     string   `json:"model,omitempty"`
}

// rerankResponse is the JSON response from the Cross-Encoder service.
type rerankResponse struct {
	Scores []float64 `json:"scores"`
	Error  string    `json:"error,omitempty"`
}

// Rerank calls the Cross-Encoder service to rescore candidates.
// Falls back to passthrough if the service is unavailable.
func (r *CrossEncoderRanker) Rerank(ctx context.Context, query string, candidates []*retrieval.SearchResult) []*RankedResult {
	if len(candidates) == 0 {
		return nil
	}

	// Trim to topK candidates for reranking.
	input := candidates
	if len(input) > r.topK {
		input = input[:r.topK]
	}

	// Build document texts for the reranker.
	docs := make([]string, len(input))
	for i, c := range input {
		docs[i] = buildDocText(c)
	}

	scores, err := r.callReranker(ctx, query, docs)
	if err != nil {
		log.Printf("[ranking] Cross-Encoder call failed, falling back to RRF order: %v", err)
		// Fall back to passthrough.
		p := &PassthroughRanker{}
		all := p.Rerank(ctx, query, candidates)
		if len(all) > r.limit {
			all = all[:r.limit]
		}
		return all
	}

	// Pair results with new scores.
	type pair struct {
		result *retrieval.SearchResult
		score  float64
	}
	pairs := make([]pair, len(input))
	for i := range input {
		s := 0.0
		if i < len(scores) {
			s = scores[i]
		}
		pairs[i] = pair{result: input[i], score: s}
	}

	// Sort by Cross-Encoder score descending.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	// Keep top limit results.
	if len(pairs) > r.limit {
		pairs = pairs[:r.limit]
	}

	results := make([]*RankedResult, len(pairs))
	for i, p := range pairs {
		results[i] = &RankedResult{
			SearchResult: p.result,
			RerankScore:  p.score,
			Rank:         i + 1,
		}
	}

	return results
}

// buildDocText constructs a single text block from a search result for the reranker.
func buildDocText(r *retrieval.SearchResult) string {
	text := r.Title
	if text == "" {
		text = r.Snippet
	}
	if r.Snippet != "" && r.Snippet != r.Title {
		text += "\n" + r.Snippet
	}
	return text
}

// callReranker sends the query and documents to the Cross-Encoder service.
func (r *CrossEncoderRanker) callReranker(ctx context.Context, query string, documents []string) ([]float64, error) {
	body := rerankRequest{
		Query:     query,
		Documents: documents,
		Model:     r.model,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.addr+"/rerank",
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reranker unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("reranker returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var rr rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rr.Error != "" {
		return nil, fmt.Errorf("reranker error: %s", rr.Error)
	}

	return rr.Scores, nil
}

// NewRanker creates the appropriate ranker based on config.
func NewRanker(cfg *config.Config) Ranker {
	if cfg.Ranking.CrossEncoderEnabled {
		log.Printf("[ranking] Cross-Encoder reranking enabled: model=%s addr=%s",
			cfg.Ranking.CrossEncoderModel, cfg.Ranking.CrossEncoderAddr)
		return NewCrossEncoderRanker(cfg.Ranking.CrossEncoderAddr, cfg.Ranking.CrossEncoderModel)
	}
	return NewPassthroughRanker()
}

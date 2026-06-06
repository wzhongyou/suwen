// Package gateway provides the HTTP API layer for suwen.
package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/wzhongyou/suwen/internal/cache"
	"github.com/wzhongyou/suwen/internal/generation"
	"github.com/wzhongyou/suwen/internal/middleware"
	"github.com/wzhongyou/suwen/internal/query"
	"github.com/wzhongyou/suwen/internal/ranking"
	"github.com/wzhongyou/suwen/internal/retrieval"
)

// SearchRequest is the JSON body for a search request.
type SearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	Stream     bool   `json:"stream,omitempty"`
}

// SearchResponse is the JSON body for a search response.
type SearchResponse struct {
	Answer  string                 `json:"answer"`
	Sources []generation.Citation  `json:"sources"`
	Results []*ranking.RankedResult `json:"results"`
	TimeMS  int64                  `json:"time_ms"`
	Cached  bool                   `json:"cached,omitempty"`
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// Handler holds all the search pipeline dependencies.
type Handler struct {
	parser    query.Parser
	searcher  retrieval.Searcher
	ranker    ranking.Ranker
	generator *generation.Generator
	cache     *cache.Cache
	metrics   *middleware.Metrics
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(
	parser query.Parser,
	searcher retrieval.Searcher,
	ranker ranking.Ranker,
	generator *generation.Generator,
	queryCache *cache.Cache,
	metrics *middleware.Metrics,
) *Handler {
	return &Handler{
		parser:    parser,
		searcher:  searcher,
		ranker:    ranker,
		generator: generator,
		cache:     queryCache,
		metrics:   metrics,
	}
}

// HandleSearchDebug handles POST /api/v1/search/debug — retrieval only, no LLM.
func (h *Handler) HandleSearchDebug(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	ctx := r.Context()

	pq, err := h.parser.Parse(ctx, req.Query)
	if err != nil {
		pq = &query.ParsedQuery{Raw: req.Query, Rewrites: []string{req.Query}}
	}

	searchResults, err := h.searcher.Search(ctx, pq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	ranked := h.ranker.Rerank(ctx, req.Query, searchResults)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": ranked,
		"total":   len(searchResults),
		"time_ms": time.Since(start).Milliseconds(),
	})
}

// HandleSearch handles POST /api/v1/search
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	ctx := r.Context()

	// Check cache first (Phase 3).
	cacheKey := normalizeQuery(req.Query)
	if h.cache != nil {
		if cached, ok := h.cache.Get(cacheKey); ok {
			if resp, ok := cached.(*SearchResponse); ok {
				resp.Cached = true
				resp.TimeMS = time.Since(start).Milliseconds()
				writeJSON(w, http.StatusOK, resp)
				log.Printf("[search] cache hit: %q (%dms)", req.Query, resp.TimeMS)
				return
			}
		}
	}

	// 1. Query understanding
	pq, err := h.parser.Parse(ctx, req.Query)
	if err != nil {
		log.Printf("[search] query parse error: %v", err)
		pq = &query.ParsedQuery{Raw: req.Query, Rewrites: []string{req.Query}}
	}

	// 2. Hybrid retrieval
	searchResults, err := h.searcher.Search(ctx, pq)
	if err != nil {
		log.Printf("[search] retrieval error: %v", err)
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	// 3. Ranking
	ranked := h.ranker.Rerank(ctx, req.Query, searchResults)

	// 4. Generation
	answer, sources, err := h.generator.Generate(ctx, req.Query, ranked)
	if err != nil {
		log.Printf("[search] generation error: %v", err)
		writeError(w, http.StatusInternalServerError, "generation failed: "+err.Error())
		return
	}

	// Track LLM call cost (approximate).
	if h.metrics != nil {
		// Rough cost estimate: input + output tokens for answer generation.
		h.metrics.RecordLLMCall(estimateLLMCost(answer))
	}

	// 5. Response
	topResults := ranked
	if len(topResults) > 10 {
		topResults = topResults[:10]
	}
	for i := range topResults {
		topResults[i].Rank = i + 1
	}

	resp := &SearchResponse{
		Answer:  answer,
		Sources: sources,
		Results: topResults,
		TimeMS:  time.Since(start).Milliseconds(),
	}

	// Store in cache (Phase 3).
	if h.cache != nil {
		h.cache.Set(cacheKey, resp)
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleSearchStream handles GET /api/v1/search/stream (SSE)
func (h *Handler) HandleSearchStream(w http.ResponseWriter, r *http.Request) {
	queryStr := r.URL.Query().Get("q")
	if queryStr == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	// Parse and retrieve synchronously, then stream generation.
	pq, err := h.parser.Parse(ctx, queryStr)
	if err != nil {
		pq = &query.ParsedQuery{Raw: queryStr, Rewrites: []string{queryStr}}
	}

	emitSSE(w, flusher, "status", map[string]string{"stage": "retrieving", "message": "正在检索..."})

	searchResults, err := h.searcher.Search(ctx, pq)
	if err != nil {
		emitSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}

	ranked := h.ranker.Rerank(ctx, queryStr, searchResults)

	emitSSE(w, flusher, "status", map[string]string{"stage": "generating", "message": "正在生成答案..."})

	tokens, err := h.generator.GenerateStream(ctx, queryStr, ranked)
	if err != nil {
		emitSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}

	for token := range tokens {
		if len(token.Citations) > 0 {
			emitSSE(w, flusher, "citations", token.Citations)
		}
		if token.Text != "" {
			emitSSE(w, flusher, "token", map[string]string{"text": token.Text})
		}
		if token.Done {
			emitSSE(w, flusher, "done", map[string]bool{"done": true})
		}
	}
}

// emitSSE writes a single SSE event.
func emitSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
	flusher.Flush()
}

// normalizeQuery normalizes a query string for cache lookup.
func normalizeQuery(q string) string {
	return strings.TrimSpace(strings.ToLower(q))
}

// estimateLLMCost returns a rough USD cost estimate for an answer.
// This is a placeholder; real cost depends on the model and token counts.
func estimateLLMCost(answer string) float64 {
	// Rough estimate: ~1 token per 4 chars, ~$0.5/1M tokens (cheap models).
	tokens := float64(len(answer)) / 4.0
	return tokens * 0.5 / 1_000_000
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

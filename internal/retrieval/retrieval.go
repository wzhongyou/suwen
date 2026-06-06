// Package retrieval handles the hybrid search pipeline:
// concurrent querying of Vortex (keyword) and Proximia (vector), then RRF fusion.
package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"sync"

	"github.com/wzhongyou/suwen/internal/config"
	"github.com/wzhongyou/suwen/internal/query"
)

// SearchResult represents a single search result from any source.
type SearchResult struct {
	DocID       string  `json:"doc_id"`
	Title       string  `json:"title"`
	Snippet     string  `json:"snippet"`
	URL         string  `json:"url"`
	Site        string  `json:"site"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	BM25Score   float64 `json:"bm25_score"`
	VectorScore float64 `json:"vector_score"`
	FinalScore  float64 `json:"final_score"`
}

// Searcher executes hybrid search across multiple engines.
type Searcher interface {
	Search(ctx context.Context, pq *query.ParsedQuery) ([]*SearchResult, error)
}

// HybridSearcher implements Searcher using concurrent Vortex + Proximia calls.
type HybridSearcher struct {
	vortexURL   string
	proximiaURL string
	rrfK        int
	httpClient  *http.Client
}

// NewHybridSearcher creates a HybridSearcher from config.
func NewHybridSearcher(cfg *config.Config) *HybridSearcher {
	return &HybridSearcher{
		vortexURL:   cfg.Vortex.Addr,
		proximiaURL: cfg.Proximia.Addr,
		rrfK:        cfg.Retrieval.RRFK,
		httpClient: &http.Client{
			Timeout: config.TimeoutDuration(cfg.Retrieval.Timeout),
		},
	}
}

// Search runs concurrent keyword and vector searches, then fuses results with RRF.
func (s *HybridSearcher) Search(ctx context.Context, pq *query.ParsedQuery) ([]*SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.httpClient.Timeout)
	defer cancel()

	var (
		keywordResults []*SearchResult
		vectorResults  []*SearchResult
		wg             sync.WaitGroup
		kwErr, vecErr  error
	)

	wg.Add(2)

	// Concurrent call 1: Vortex keyword search
	go func() {
		defer wg.Done()
		keywordResults, kwErr = s.searchVortex(ctx, pq.Raw)
	}()

	// Concurrent call 2: Proximia vector search
	go func() {
		defer wg.Done()
		// For Phase 1, we skip embedding generation and use Proximia's text_query
		// via the hybrid endpoint, or fall back to just vortex if proximia is unavailable.
		// Phase 2 will add proper embedding generation.
		vectorResults, vecErr = s.searchProximia(ctx, pq.Raw)
	}()

	wg.Wait()

	// If both fail, use mock results so the pipeline can still be demoed.
	if kwErr != nil && vecErr != nil {
		log.Printf("[retrieval] both sources unavailable (vortex: %v, proximia: %v), using mock results", kwErr, vecErr)
		return mockResults(pq.Raw), nil
	}

	// If one fails, we still use the other.
	merged := s.rrfFuse(keywordResults, vectorResults, pq.KeywordWeight, pq.VectorWeight, 50)

	// If merged results are empty but services responded, also fall back to mock for demo purposes.
	if len(merged) == 0 {
		log.Printf("[retrieval] services available but returned empty results, using mock results")
		return mockResults(pq.Raw), nil
	}

	return merged, nil
}

// --- Vortex search ---

type vortexSearchResponse struct {
	Results []vortexResult `json:"results"`
}

type vortexResult struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Site        string  `json:"site"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Score       float64 `json:"score"`
}

func (s *HybridSearcher) searchVortex(ctx context.Context, q string) ([]*SearchResult, error) {
	reqURL := fmt.Sprintf("%s/api/search?%s", s.vortexURL, url.Values{
		"q":         {q},
		"page":      {"1"},
		"page_size": {"50"},
	}.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vortex request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vortex returned %d: %s", resp.StatusCode, string(body))
	}

	var vr vortexSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decode vortex response: %w", err)
	}

	results := make([]*SearchResult, 0, len(vr.Results))
	for _, r := range vr.Results {
		results = append(results, &SearchResult{
			DocID:       r.ID,
			Title:       r.Title,
			URL:         r.URL,
			Site:        r.Site,
			Description: r.Description,
			Category:    r.Category,
			BM25Score:   r.Score,
			Snippet:     r.Description,
		})
	}
	return results, nil
}

// --- Proximia search ---

type proximiaSearchRequest struct {
	Query     []float64 `json:"query"`
	TextQuery string    `json:"text_query,omitempty"`
	Limit     int       `json:"limit"`
}

type proximiaSearchResponse struct {
	Results []proximiaResult `json:"results"`
}

type proximiaResult struct {
	ID     string                 `json:"id"`
	Score  float64                `json:"score"`
	Fields map[string]interface{} `json:"fields"`
}

func (s *HybridSearcher) searchProximia(ctx context.Context, q string) ([]*SearchResult, error) {
	// Phase 1: use text_query field without actual embedding.
	// The Proximia server must have hybrid search enabled for this collection.
	// If Proximia is not running or the endpoint fails, return nil (graceful degradation).
	body := proximiaSearchRequest{
		TextQuery: q,
		Limit:     50,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.proximiaURL+"/collections/default/search",
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proximia request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Phase 1: graceful degradation — Proximia may not be running yet.
		return nil, nil
	}

	var pr proximiaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decode proximia response: %w", err)
	}

	results := make([]*SearchResult, 0, len(pr.Results))
	for _, r := range pr.Results {
		title, _ := r.Fields["title"].(string)
		urlStr := ""
		if u, ok := r.Fields["url"].(string); ok {
			urlStr = u
		}
		site, _ := r.Fields["site"].(string)
		desc, _ := r.Fields["description"].(string)

		results = append(results, &SearchResult{
			DocID:       r.ID,
			Title:       title,
			URL:         urlStr,
			Site:        site,
			Description: desc,
			Snippet:     desc,
			VectorScore: r.Score,
		})
	}
	return results, nil
}

// --- RRF Fusion ---

// rrfFuse merges two ranked lists using Reciprocal Rank Fusion.
func (s *HybridSearcher) rrfFuse(
	keywordResults, vectorResults []*SearchResult,
	kwWeight, vecWeight float64,
	limit int,
) []*SearchResult {
	k := float64(s.rrfK)

	// Aggregate RRF scores keyed by URL (best unique identifier).
	type entry struct {
		result     *SearchResult
		rrfScore   float64
	}
	seen := make(map[string]*entry)

	for i, r := range keywordResults {
		key := r.URL
		if key == "" {
			key = r.DocID
		}
		rrf := kwWeight / (k + float64(i+1))
		if ex, ok := seen[key]; ok {
			ex.rrfScore += rrf
			if r.BM25Score > ex.result.BM25Score {
				ex.result.BM25Score = r.BM25Score
			}
		} else {
			seen[key] = &entry{result: r, rrfScore: rrf}
		}
	}

	for i, r := range vectorResults {
		key := r.URL
		if key == "" {
			key = r.DocID
		}
		rrf := vecWeight / (k + float64(i+1))
		if ex, ok := seen[key]; ok {
			ex.rrfScore += rrf
			ex.result.VectorScore = math.Max(ex.result.VectorScore, r.VectorScore)
		} else {
			seen[key] = &entry{result: r, rrfScore: rrf}
		}
	}

	// Collect and sort by RRF score descending.
	merged := make([]*entry, 0, len(seen))
	for _, e := range seen {
		e.result.FinalScore = e.rrfScore
		merged = append(merged, e)
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].rrfScore > merged[j].rrfScore
	})

	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}

	results := make([]*SearchResult, len(merged))
	for i, e := range merged {
		results[i] = e.result
	}
	return results
}

// mockResults returns demo results when upstream services are unavailable.
func mockResults(query string) []*SearchResult {
	return []*SearchResult{
		{
			DocID: "mock-1", Title: "Go 语言并发编程指南",
			URL: "https://go.dev/tour/concurrency", Site: "go.dev",
			Description: "Goroutine 是 Go 语言中轻量级的执行单元，channel 用于 goroutine 之间的通信。" +
				"通过 go 关键字可以启动一个新的 goroutine，channel 则保证了并发安全的数据传递。",
			Snippet:    "Goroutine 是 Go 语言中轻量级的执行单元，channel 用于 goroutine 之间的通信。",
			BM25Score:  12.5, FinalScore: 0.85,
		},
		{
			DocID: "mock-2", Title: "数据库索引优化最佳实践",
			URL: "https://use-the-index-luke.com", Site: "use-the-index-luke.com",
			Description: "索引是数据库性能优化的核心手段。B-tree 索引适合等值和范围查询，" +
				"而 Hash 索引仅适用于等值查询。复合索引遵循最左前缀原则。",
			Snippet:    "索引是数据库性能优化的核心手段。B-tree 索引适合等值和范围查询。",
			BM25Score:  10.2, FinalScore: 0.78,
		},
		{
			DocID: "mock-3", Title: "AI 搜索引擎技术架构",
			URL: "https://github.com/wzhongyou/suwen", Site: "github.com",
			Description: "Suwen 是一个开源的 AI 搜索引擎，采用混合召回架构，结合 BM25 关键词检索和向量语义检索，" +
				"通过 RRF 融合后经 Cross-Encoder 精排，最终由 LLM 生成带引用的答案。",
			Snippet:    "Suwen 是一个开源的 AI 搜索引擎，采用混合召回架构。",
			BM25Score:  9.8, FinalScore: 0.72,
		},
		{
			DocID: "mock-4", Title: "React 19 服务端组件深度解析",
			URL: "https://react.dev/blog/2024/12/05/react-19", Site: "react.dev",
			Description: "React 19 引入了 Server Components 作为默认架构，允许组件在服务端渲染，" +
				"减少客户端 JavaScript 体积，提升首屏加载速度。配合 Server Actions 处理表单提交。",
			Snippet:    "React 19 引入了 Server Components 作为默认架构。",
			BM25Score:  8.6, FinalScore: 0.65,
		},
		{
			DocID: "mock-5", Title: "Kubernetes 自动伸缩原理与实践",
			URL: "https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/", Site: "kubernetes.io",
			Description: "HPA（Horizontal Pod Autoscaler）根据 CPU 利用率或自定义指标自动调整 Pod 副本数。" +
				"VPA 则垂直调整 Pod 的资源请求。两者结合可以实现全方位的自动伸缩。",
			Snippet:    "HPA 根据 CPU 利用率或自定义指标自动调整 Pod 副本数。",
			BM25Score:  7.9, FinalScore: 0.58,
		},
	}
}

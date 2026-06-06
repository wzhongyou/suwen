package retrieval

import (
	"testing"
)

func TestRRFFuse_Basic(t *testing.T) {
	s := &HybridSearcher{rrfK: 60}

	kw := []*SearchResult{
		{DocID: "1", Title: "A", URL: "a.com", BM25Score: 5.0},
		{DocID: "2", Title: "B", URL: "b.com", BM25Score: 3.0},
	}
	vec := []*SearchResult{
		{DocID: "1", Title: "A", URL: "a.com", VectorScore: 0.9},
		{DocID: "3", Title: "C", URL: "c.com", VectorScore: 0.8},
	}

	merged := s.rrfFuse(kw, vec, 1.0, 1.0, 10)

	if len(merged) != 3 {
		t.Fatalf("expected 3 results, got %d", len(merged))
	}
	// Doc "1" appears in both lists → should be ranked first
	if merged[0].DocID != "1" {
		t.Errorf("expected doc 1 first, got %s", merged[0].DocID)
	}
}

func TestRRFFuse_EmptyKeyword(t *testing.T) {
	s := &HybridSearcher{rrfK: 60}

	vec := []*SearchResult{
		{DocID: "1", Title: "A", URL: "a.com", VectorScore: 0.9},
	}

	merged := s.rrfFuse(nil, vec, 1.0, 1.0, 10)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
}

func TestRRFFuse_Weighted(t *testing.T) {
	s := &HybridSearcher{rrfK: 60}

	// Keyword-only result should be ranked lower when vector weight is higher
	kw := []*SearchResult{
		{DocID: "kw", Title: "Keyword Only", URL: "kw.com", BM25Score: 10.0},
	}
	vec := []*SearchResult{
		{DocID: "vec", Title: "Vector Only", URL: "vec.com", VectorScore: 0.95},
	}

	// Give vector much higher weight
	merged := s.rrfFuse(kw, vec, 0.2, 2.0, 10)

	if len(merged) != 2 {
		t.Fatalf("expected 2 results, got %d", len(merged))
	}
	if merged[0].DocID != "vec" {
		t.Errorf("with high vector weight, vector result should be first, got %s", merged[0].DocID)
	}
}

func TestRRFFuse_Limit(t *testing.T) {
	s := &HybridSearcher{rrfK: 60}

	kw := []*SearchResult{
		{DocID: "1", Title: "A", URL: "a.com", BM25Score: 1.0},
		{DocID: "2", Title: "B", URL: "b.com", BM25Score: 1.0},
		{DocID: "3", Title: "C", URL: "c.com", BM25Score: 1.0},
	}

	merged := s.rrfFuse(kw, nil, 1.0, 0, 2)
	if len(merged) != 2 {
		t.Fatalf("expected 2 results with limit=2, got %d", len(merged))
	}
}

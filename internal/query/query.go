// Package query handles query understanding: intent classification, rewriting, and expansion.
// Phase 1: simple pass-through of raw queries.
// Phase 2: LLM-powered intent classification and query expansion.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/wzhongyou/llmgate/core"
	"github.com/wzhongyou/llmgate/sdk"
)

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

// ---- Phase 2: LLM-powered query parser ----

// queryParseOutput is the structured JSON we expect the LLM to return.
type queryParseOutput struct {
	Intent        string   `json:"intent"`
	Domain        string   `json:"domain"`
	Rewrites      []string `json:"rewrites"`
	VectorWeight  float64  `json:"vector_weight"`
	KeywordWeight float64  `json:"keyword_weight"`
}

const queryParsePrompt = `You are a query understanding engine for a search system. Analyze the user's query and output a JSON object.

Intent must be one of:
- "factual": seeking a specific fact or piece of data (e.g. "What is the capital of France?")
- "howto": asking how to do something, instructions, or tutorials (e.g. "How do I optimize SQL queries?")
- "navigational": looking for a specific website, page, or resource (e.g. "Go standard library docs")
- "exploratory": broad, open-ended exploration of a topic (e.g. "state of AI in 2026")

Weights control the balance between keyword (BM25) and semantic (vector) search:
- For factual/navigational queries, keyword_weight should be higher (0.6-0.7), vector_weight lower (0.3-0.4).
- For howto/exploratory queries, vector_weight should be higher (0.6-0.7), keyword_weight lower (0.3-0.4).
- Weights must sum to 1.0.

Domain: a short label for the topic area (e.g. "programming", "database", "math", "history", "health", "general"). Use "general" if unsure.

Rewrites: 1-3 alternative phrasings of the query that capture different angles or search terms. Always include the original query as the first element.

Output only valid JSON, no markdown, no explanation:
{"intent":"<intent>","domain":"<domain>","rewrites":["<original>","<rewrite1>","<rewrite2>"],"vector_weight":<float>,"keyword_weight":<float>}`

// LLMParser uses an LLM via llmgate to parse and rewrite queries.
type LLMParser struct {
	gateway *sdk.Gateway
	model   string
	timeout time.Duration
}

// NewLLMParser creates an LLM-powered query parser.
func NewLLMParser(gateway *sdk.Gateway, model string, timeout time.Duration) *LLMParser {
	return &LLMParser{
		gateway: gateway,
		model:   model,
		timeout: timeout,
	}
}

// Parse runs the query through an LLM for intent classification and expansion.
// Falls back to simple pass-through on error or timeout.
func (p *LLMParser) Parse(ctx context.Context, raw string) (*ParsedQuery, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	messages := []core.Message{
		{Role: "system", Content: queryParsePrompt},
		{Role: "user", Content: raw},
	}

	maxTokens := 512
	req := &core.ChatRequest{
		Messages:  messages,
		Model:     p.model,
		MaxTokens: &maxTokens,
	}

	resp, err := p.gateway.Chat(ctx, req)
	if err != nil {
		log.Printf("[query] LLM parse failed, falling back to pass-through: %v", err)
		return fallbackParse(raw), nil
	}

	output, err := parseQueryOutput(resp.Content)
	if err != nil {
		log.Printf("[query] failed to parse LLM output, falling back to pass-through: %v", err)
		return fallbackParse(raw), nil
	}

	// Validate intent.
	intent := Intent(output.Intent)
	switch intent {
	case IntentFactual, IntentHowTo, IntentNavigational, IntentExploratory:
	default:
		intent = IntentFactual
	}

	// Validate weights.
	vw, kw := output.VectorWeight, output.KeywordWeight
	if vw <= 0 || kw <= 0 || vw > 1 || kw > 1 {
		vw, kw = 0.5, 0.5
	}

	// Ensure rewrites includes the original.
	rewrites := output.Rewrites
	if len(rewrites) == 0 {
		rewrites = []string{raw}
	}

	domain := output.Domain
	if domain == "" {
		domain = "general"
	}

	log.Printf("[query] parsed: intent=%s domain=%s rewrites=%d vw=%.2f kw=%.2f",
		intent, domain, len(rewrites), vw, kw)

	return &ParsedQuery{
		Raw:           raw,
		Rewrites:      rewrites,
		Intent:        intent,
		Domain:        domain,
		VectorWeight:  vw,
		KeywordWeight: kw,
	}, nil
}

// parseQueryOutput extracts JSON from the LLM response.
func parseQueryOutput(content string) (*queryParseOutput, error) {
	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		// Remove ```json ... ``` or ``` ... ```
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var out queryParseOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}
	return &out, nil
}

// fallbackParse returns a pass-through ParsedQuery when LLM parsing fails.
func fallbackParse(raw string) *ParsedQuery {
	return &ParsedQuery{
		Raw:           raw,
		Rewrites:      []string{raw},
		Intent:        IntentFactual,
		Domain:        "",
		VectorWeight:  0.5,
		KeywordWeight: 0.5,
	}
}

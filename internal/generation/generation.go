// Package generation handles answer synthesis:
// extracting relevant snippets, constructing prompts, calling LLM, and tracking citations.
package generation

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/wzhongyou/llmgate/core"
	"github.com/wzhongyou/llmgate/sdk"

	"github.com/wzhongyou/suwen/internal/config"
	"github.com/wzhongyou/suwen/internal/ranking"
)

const systemPrompt = `你是素问（Suwen），一个开源的AI搜索引擎。你的任务是基于提供的【参考资料】回答用户的问题。

你必须严格遵守以下规则：
1. 只能基于【参考资料】中的信息回答。如果资料不包含答案，直接说"未找到相关信息"。
2. 对于每个事实性主张，必须在对应位置标注来源编号，例如 [1] [2]。
3. 不要编造参考资料中不存在的数据、日期或数字。
4. 回答应简洁、准确、有条理。
5. 可以适当总结和归纳，但不能超出参考资料的范围。`

// Citation represents a source reference in the generated answer.
type Citation struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Token is a unit of streaming output.
type Token struct {
	Text      string     `json:"text,omitempty"`
	Citations []Citation `json:"citations,omitempty"`
	Done      bool       `json:"done"`
}

// Generator synthesizes answers from ranked search results.
type Generator struct {
	gateway *sdk.Gateway
	model   string
}

// New creates a Generator backed by llmgate.
// If configPath is non-empty, loads provider config from that file.
// Otherwise, auto-loads from environment variables (LLMGATE_*).
func New(cfg *config.Config) *Generator {
	var gw *sdk.Gateway
	if path := cfg.LLM.ConfigPath; path != "" {
		var err error
		gw, err = sdk.NewFromFile(path)
		if err != nil {
			log.Printf("[generation] failed to load llmgate config from %s, falling back to env: %v", path, err)
			gw = sdk.New()
		}
	} else {
		gw = sdk.New()
	}
	return &Generator{
		gateway: gw,
		model:   cfg.LLM.Model,
	}
}

// Generate creates a synchronous answer from the given documents.
func (g *Generator) Generate(ctx context.Context, query string, results []*ranking.RankedResult) (string, []Citation, error) {
	messages, citations := g.buildMessages(query, results)
	if len(messages) == 0 {
		return "未找到相关信息。", nil, nil
	}

	req := &core.ChatRequest{
		Messages: messages,
		Model:    g.model,
	}

	resp, err := g.gateway.Chat(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("llm chat: %w", err)
	}

	return resp.Content, citations, nil
}

// GenerateStream creates a streaming answer, sending tokens to the channel.
func (g *Generator) GenerateStream(ctx context.Context, query string, results []*ranking.RankedResult) (<-chan Token, error) {
	messages, citations := g.buildMessages(query, results)
	if len(messages) == 0 {
		ch := make(chan Token, 1)
		ch <- Token{Text: "未找到相关信息。", Done: true}
		close(ch)
		return ch, nil
	}

	req := &core.ChatRequest{
		Messages: messages,
		Model:    g.model,
	}

	streamCh, err := g.gateway.ChatStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm chat stream: %w", err)
	}

	out := make(chan Token, 64)
	go func() {
		defer close(out)
		// Send citations first
		out <- Token{Citations: citations}
		for chunk := range streamCh {
			if chunk.Error != nil {
				return
			}
			out <- Token{Text: chunk.Content}
			if chunk.FinishReason == "stop" {
				out <- Token{Done: true}
				return
			}
		}
		out <- Token{Done: true}
	}()

	return out, nil
}

// buildMessages constructs the LLM prompt from search results.
func (g *Generator) buildMessages(query string, results []*ranking.RankedResult) ([]core.Message, []Citation) {
	if len(results) == 0 {
		return nil, nil
	}

	// Take top 10 results for context.
	topK := results
	if len(topK) > 10 {
		topK = topK[:10]
	}

	citations := make([]Citation, 0, len(topK))
	var refs strings.Builder
	for i, r := range topK {
		idx := i + 1
		refs.WriteString(fmt.Sprintf("[%d] %s (%s)\n%s\n\n", idx, r.Title, r.URL, r.Snippet))
		citations = append(citations, Citation{
			Index: idx,
			URL:   r.URL,
			Title: r.Title,
		})
	}

	userMsg := fmt.Sprintf("问题：%s\n\n参考资料：\n%s请基于以上参考资料回答。", query, refs.String())

	return []core.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	}, citations
}

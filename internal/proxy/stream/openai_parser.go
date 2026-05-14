package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/pkg/sse"
)

// OpenAIParser parses OpenAI-compatible streaming responses (`data: {...}\n\n` + `data: [DONE]`).
type OpenAIParser struct {
	// All fields are mutated only from the goroutine that calls Feed.
	chunks          []ChunkRecord
	textBuilder     strings.Builder
	totalIn         int
	totalOut        int
	totalAll        int
	reasoningToks   int
	cachedInToks    int
	finishReason    string
	model           string
	tokensFromUsage bool
}

func NewOpenAIParser() *OpenAIParser { return &OpenAIParser{} }

// Feed consumes the response stream bytes (already tee'd from the wire).
// keepRaw caps how many raw SSE bytes to retain per chunk in stream records.
func (p *OpenAIParser) Feed(r io.Reader, tl *Timeline, onFirstToken func(time.Time)) {
	sc := sse.NewScanner(r)
	for {
		ev, err := sc.Next()
		if ev != nil && len(ev.Data) > 0 {
			now := time.Now()
			tl.AppendChunk(now)
			payload := bytes.TrimSpace(ev.Data)
			if bytes.Equal(payload, []byte("[DONE]")) {
				// no more
			} else {
				p.handleChunk(payload, ev.Raw, len(p.chunks), now, onFirstToken, tl)
			}
		}
		if err != nil {
			return
		}
	}
}

type openaiChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			// We don't reassemble tool_calls server-side (the UI does that
			// from the raw SSE), but we DO need to know whether a tool-call
			// delta arrived so the stream timeline picks up FirstToken /
			// LastToken — otherwise tool-call-only responses look aborted.
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openaiUsage `json:"usage"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// DeepSeek-style cache reporting: cache hit/miss live at usage root
	// instead of under prompt_tokens_details. We accept either spelling.
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// cachedTokens returns the cached-input portion regardless of which dialect
// the upstream uses. OpenAI puts it under prompt_tokens_details.cached_tokens;
// DeepSeek puts it at usage.prompt_cache_hit_tokens.
func (u *openaiUsage) cachedTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return u.PromptCacheHitTokens
}

func (u *openaiUsage) reasoningTokens() int {
	if u.CompletionTokensDetails != nil {
		return u.CompletionTokensDetails.ReasoningTokens
	}
	return 0
}

func (p *OpenAIParser) handleChunk(data, raw []byte, seq int, now time.Time, onFirstToken func(time.Time), tl *Timeline) {
	var c openaiChunk
	if err := json.Unmarshal(data, &c); err != nil {
		// keep going; record raw only
		p.chunks = append(p.chunks, ChunkRecord{Seq: seq, Ts: now.UnixMilli(), Raw: capRaw(raw)})
		return
	}
	if c.Model != "" && p.model == "" {
		p.model = c.Model
	}
	delta := ""
	sawToolDelta := false
	for _, ch := range c.Choices {
		if ch.Delta.Content != "" {
			delta += ch.Delta.Content
		}
		if len(ch.Delta.ToolCalls) > 0 {
			sawToolDelta = true
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			p.finishReason = *ch.FinishReason
		}
	}
	if delta != "" {
		p.textBuilder.WriteString(delta)
	}
	// Update timeline on any meaningful generated content — text OR tool
	// calls. Otherwise tool-call-only streams have a zero LastToken and the
	// proxy mislabels them as "aborted".
	if delta != "" || sawToolDelta {
		if tl.FirstToken.IsZero() {
			if onFirstToken != nil {
				onFirstToken(now)
			}
			tl.FirstToken = now
		}
		tl.LastToken = now
	}
	if c.Usage != nil {
		p.totalIn = c.Usage.PromptTokens
		p.totalOut = c.Usage.CompletionTokens
		p.totalAll = c.Usage.TotalTokens
		p.cachedInToks = c.Usage.cachedTokens()
		p.reasoningToks = c.Usage.reasoningTokens()
		p.tokensFromUsage = true
	}
	p.chunks = append(p.chunks, ChunkRecord{
		Seq:       seq,
		Ts:        now.UnixMilli(),
		DeltaText: delta,
		Raw:       capRaw(raw),
	})
}

func capRaw(raw []byte) string {
	const cap = 2 << 10 // 2 KB per chunk
	if len(raw) > cap {
		return string(raw[:cap]) + "...(truncated)"
	}
	return string(raw)
}

// Finalize collects the parsing result into the given Event.
//
// Token normalization: OpenAI reports prompt_tokens as the FULL prompt (cached
// tokens included as a subset). We subtract the cached portion so the
// downstream Event field carries the same "uncached input only" semantics that
// pricing.Cost (and tier resolution) expect — matching Anthropic's native
// shape. This lets billing run a single non-branching formula regardless of
// upstream.
func (p *OpenAIParser) Finalize(ev *Event) {
	ev.ResponseText = p.textBuilder.String()
	ev.StreamChunks = p.chunks
	ev.ChunkCount = len(p.chunks)
	ev.FinishReason = p.finishReason
	if p.tokensFromUsage {
		ev.InputTokens = max(p.totalIn-p.cachedInToks, 0)
		ev.OutputTokens = p.totalOut
		ev.TotalTokens = p.totalAll
		ev.ReasoningTokens = p.reasoningToks
		ev.CachedInputTokens = p.cachedInToks
		ev.TokensEstimated = false
	}
	if p.model != "" && ev.Model == "" {
		ev.Model = p.model
	}
}

// ParseNonStream parses a single JSON body (non-stream) for OpenAI chat.completions / embeddings.
func (p *OpenAIParser) ParseNonStream(body []byte, ev *Event) {
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *openaiUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	if resp.Model != "" && ev.Model == "" {
		ev.Model = resp.Model
	}
	for _, c := range resp.Choices {
		if c.Message.Content != "" {
			ev.ResponseText = c.Message.Content
		}
		if c.FinishReason != "" {
			ev.FinishReason = c.FinishReason
		}
	}
	if resp.Usage != nil {
		cached := resp.Usage.cachedTokens()
		ev.InputTokens = max(resp.Usage.PromptTokens-cached, 0)
		ev.OutputTokens = resp.Usage.CompletionTokens
		ev.TotalTokens = resp.Usage.TotalTokens
		ev.CachedInputTokens = cached
		ev.ReasoningTokens = resp.Usage.reasoningTokens()
		ev.TokensEstimated = false
	}
}

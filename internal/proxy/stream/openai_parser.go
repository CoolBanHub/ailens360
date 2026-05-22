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
	chunkCount      int
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
			if !bytes.Equal(payload, []byte("[DONE]")) {
				p.handleChunk(payload, now, onFirstToken, tl)
			}
			p.chunkCount++
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

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
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

func (u *responsesUsage) cachedTokens() int {
	if u != nil && u.InputTokensDetails != nil {
		return u.InputTokensDetails.CachedTokens
	}
	return 0
}

func (u *responsesUsage) reasoningTokens() int {
	if u != nil && u.OutputTokensDetails != nil {
		return u.OutputTokensDetails.ReasoningTokens
	}
	return 0
}

func (p *OpenAIParser) handleChunk(data []byte, now time.Time, onFirstToken func(time.Time), tl *Timeline) {
	if p.handleResponsesChunk(data, now, onFirstToken, tl) {
		return
	}

	var c openaiChunk
	if err := json.Unmarshal(data, &c); err != nil {
		// keep going; the chunk count was already bumped by the caller.
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
		p.setOpenAIUsage(c.Usage)
	}
}

func (p *OpenAIParser) handleResponsesChunk(data []byte, now time.Time, onFirstToken func(time.Time), tl *Timeline) bool {
	var ev struct {
		Type         string          `json:"type"`
		Delta        string          `json:"delta"`
		Text         string          `json:"text"`
		Sequence     int             `json:"sequence_number"`
		Response     json.RawMessage `json:"response"`
		Item         json.RawMessage `json:"item"`
		Usage        *responsesUsage `json:"usage"`
		OutputIndex  int             `json:"output_index"`
		ContentIndex int             `json:"content_index"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return false
	}
	if !strings.HasPrefix(ev.Type, "response.") {
		return false
	}

	switch ev.Type {
	case "response.output_text.delta", "response.refusal.delta":
		p.appendResponsesDelta(ev.Delta, now, onFirstToken, tl)
	case "response.output_item.added":
		if p.captureResponsesItem(ev.Item, now, onFirstToken, tl) {
			return true
		}
	case "response.completed", "response.incomplete":
		p.captureResponsesObject(ev.Response)
	default:
		if ev.Usage != nil {
			p.setResponsesUsage(ev.Usage)
		}
		if ev.Text != "" && strings.HasSuffix(ev.Type, ".done") {
			p.appendResponsesDelta(ev.Text, now, onFirstToken, tl)
		}
	}
	return true
}

func (p *OpenAIParser) appendResponsesDelta(delta string, now time.Time, onFirstToken func(time.Time), tl *Timeline) {
	if delta == "" {
		return
	}
	p.textBuilder.WriteString(delta)
	if tl.FirstToken.IsZero() {
		if onFirstToken != nil {
			onFirstToken(now)
		}
		tl.FirstToken = now
	}
	tl.LastToken = now
}

func (p *OpenAIParser) captureResponsesItem(raw json.RawMessage, now time.Time, onFirstToken func(time.Time), tl *Timeline) bool {
	if len(raw) == 0 {
		return false
	}
	var item struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return false
	}
	if item.Type != "function_call" && item.Type != "web_search_call" && item.Type != "file_search_call" &&
		item.Type != "computer_call" && item.Type != "mcp_call" && item.Type != "code_interpreter_call" {
		return false
	}
	if tl.FirstToken.IsZero() {
		if onFirstToken != nil {
			onFirstToken(now)
		}
		tl.FirstToken = now
	}
	tl.LastToken = now
	return true
}

func (p *OpenAIParser) captureResponsesObject(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var resp struct {
		Model             string          `json:"model"`
		OutputText        string          `json:"output_text"`
		Status            string          `json:"status"`
		Usage             *responsesUsage `json:"usage"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}
	if resp.Model != "" && p.model == "" {
		p.model = resp.Model
	}
	if resp.OutputText != "" && p.textBuilder.Len() == 0 {
		p.textBuilder.WriteString(resp.OutputText)
	}
	if resp.Status != "" {
		p.finishReason = resp.Status
	}
	if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
		p.finishReason = resp.IncompleteDetails.Reason
	}
	if resp.Usage != nil {
		p.setResponsesUsage(resp.Usage)
	}
}

func (p *OpenAIParser) setOpenAIUsage(u *openaiUsage) {
	p.totalIn = u.PromptTokens
	p.totalOut = u.CompletionTokens
	p.totalAll = u.TotalTokens
	p.cachedInToks = u.cachedTokens()
	p.reasoningToks = u.reasoningTokens()
	p.tokensFromUsage = true
}

func (p *OpenAIParser) setResponsesUsage(u *responsesUsage) {
	p.totalIn = u.InputTokens
	p.totalOut = u.OutputTokens
	p.totalAll = u.TotalTokens
	p.cachedInToks = u.cachedTokens()
	p.reasoningToks = u.reasoningTokens()
	p.tokensFromUsage = true
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
	ev.ChunkCount = p.chunkCount
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
	if p.parseResponsesNonStream(body, ev) {
		return
	}

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

func (p *OpenAIParser) parseResponsesNonStream(body []byte, ev *Event) bool {
	var resp struct {
		Object     string          `json:"object"`
		ID         string          `json:"id"`
		Model      string          `json:"model"`
		OutputText string          `json:"output_text"`
		Status     string          `json:"status"`
		Usage      *responsesUsage `json:"usage"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	if resp.Object != "response" && !strings.HasPrefix(resp.ID, "resp_") && resp.OutputText == "" && len(resp.Output) == 0 {
		return false
	}
	if resp.Model != "" && ev.Model == "" {
		ev.Model = resp.Model
	}
	if resp.OutputText != "" {
		ev.ResponseText = resp.OutputText
	} else {
		var sb strings.Builder
		for _, out := range resp.Output {
			if out.Type != "message" {
				continue
			}
			for _, c := range out.Content {
				if c.Type == "output_text" || c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
		}
		ev.ResponseText = sb.String()
	}
	if resp.Status != "" {
		ev.FinishReason = resp.Status
	}
	if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
		ev.FinishReason = resp.IncompleteDetails.Reason
	}
	if resp.Usage != nil {
		cached := resp.Usage.cachedTokens()
		ev.InputTokens = max(resp.Usage.InputTokens-cached, 0)
		ev.OutputTokens = resp.Usage.OutputTokens
		ev.TotalTokens = resp.Usage.TotalTokens
		ev.CachedInputTokens = cached
		ev.ReasoningTokens = resp.Usage.reasoningTokens()
		ev.TokensEstimated = false
	}
	return true
}

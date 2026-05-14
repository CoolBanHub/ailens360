package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/pkg/sse"
)

// GeminiParser parses Google Gemini `streamGenerateContent` SSE.
// Frames are JSON objects in `data:` SSE lines; the last frame typically carries
// `usageMetadata`.
type GeminiParser struct {
	chunks        []ChunkRecord
	textBuilder   strings.Builder
	inputTokens   int
	outputTokens  int
	totalTokens   int
	reasoningToks int
	cachedInToks  int
	finishReason  string
	model         string
	usageSeen     bool
}

func NewGeminiParser() *GeminiParser { return &GeminiParser{} }

type geminiChunk struct {
	ModelVersion string `json:"modelVersion"`
	Candidates   []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
}

func (p *GeminiParser) Feed(r io.Reader, tl *Timeline, onFirstToken func(time.Time)) {
	sc := sse.NewScanner(r)
	for {
		ev, err := sc.Next()
		if ev != nil && len(ev.Data) > 0 {
			now := time.Now()
			tl.AppendChunk(now)
			p.handleChunk(bytes.TrimSpace(ev.Data), ev.Raw, len(p.chunks), now, onFirstToken, tl)
		}
		if err != nil {
			return
		}
	}
}

func (p *GeminiParser) handleChunk(data, raw []byte, seq int, now time.Time, onFirstToken func(time.Time), tl *Timeline) {
	rec := ChunkRecord{Seq: seq, Ts: now.UnixMilli(), Raw: capRaw(raw)}
	var c geminiChunk
	if err := json.Unmarshal(data, &c); err != nil {
		p.chunks = append(p.chunks, rec)
		return
	}
	if c.ModelVersion != "" && p.model == "" {
		p.model = c.ModelVersion
	}
	var delta string
	for _, cand := range c.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				delta += part.Text
			}
		}
		if cand.FinishReason != "" {
			p.finishReason = cand.FinishReason
		}
	}
	if delta != "" {
		if p.textBuilder.Len() == 0 && onFirstToken != nil {
			onFirstToken(now)
			tl.FirstToken = now
		}
		p.textBuilder.WriteString(delta)
		tl.LastToken = now
		rec.DeltaText = delta
	}
	if c.UsageMetadata != nil {
		p.inputTokens = c.UsageMetadata.PromptTokenCount
		p.outputTokens = c.UsageMetadata.CandidatesTokenCount
		p.totalTokens = c.UsageMetadata.TotalTokenCount
		p.cachedInToks = c.UsageMetadata.CachedContentTokenCount
		p.reasoningToks = c.UsageMetadata.ThoughtsTokenCount
		p.usageSeen = true
	}
	p.chunks = append(p.chunks, rec)
}

// Token normalization: Gemini's promptTokenCount is the FULL prompt
// (cachedContentTokenCount is a subset). We subtract cached so the Event
// carries "uncached input only" semantics that billing assumes — see the
// equivalent OpenAI parser comment.
func (p *GeminiParser) Finalize(ev *Event) {
	ev.ResponseText = p.textBuilder.String()
	ev.StreamChunks = p.chunks
	ev.ChunkCount = len(p.chunks)
	ev.FinishReason = p.finishReason
	if p.usageSeen {
		ev.InputTokens = max(p.inputTokens-p.cachedInToks, 0)
		ev.OutputTokens = p.outputTokens
		ev.TotalTokens = p.totalTokens
		ev.ReasoningTokens = p.reasoningToks
		ev.CachedInputTokens = p.cachedInToks
		if ev.TotalTokens == 0 {
			ev.TotalTokens = ev.InputTokens + ev.CachedInputTokens + ev.OutputTokens
		}
		ev.TokensEstimated = false
	}
	if p.model != "" && ev.Model == "" {
		ev.Model = p.model
	}
}

func (p *GeminiParser) ParseNonStream(body []byte, ev *Event) {
	var c geminiChunk
	if err := json.Unmarshal(body, &c); err != nil {
		return
	}
	if c.ModelVersion != "" && ev.Model == "" {
		ev.Model = c.ModelVersion
	}
	var sb strings.Builder
	for _, cand := range c.Candidates {
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
		}
		if cand.FinishReason != "" {
			ev.FinishReason = cand.FinishReason
		}
	}
	ev.ResponseText = sb.String()
	if c.UsageMetadata != nil {
		cached := c.UsageMetadata.CachedContentTokenCount
		ev.InputTokens = max(c.UsageMetadata.PromptTokenCount-cached, 0)
		ev.OutputTokens = c.UsageMetadata.CandidatesTokenCount
		ev.TotalTokens = c.UsageMetadata.TotalTokenCount
		ev.ReasoningTokens = c.UsageMetadata.ThoughtsTokenCount
		ev.CachedInputTokens = cached
		ev.TokensEstimated = false
	}
}

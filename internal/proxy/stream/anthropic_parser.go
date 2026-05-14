package stream

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/pkg/sse"
)

// AnthropicParser handles Anthropic's typed SSE stream:
//
//	message_start → content_block_start → content_block_delta (*) →
//	content_block_stop → message_delta → message_stop
type AnthropicParser struct {
	chunks          []ChunkRecord
	textBuilder     strings.Builder
	inputTokens     int
	outputTokens    int
	cacheReadToks   int
	cacheCreateToks int
	finishReason    string
	model           string
	usageSeen       bool
}

func NewAnthropicParser() *AnthropicParser { return &AnthropicParser{} }

type anthUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type anthMessageStart struct {
	Message struct {
		Model string    `json:"model"`
		Usage anthUsage `json:"usage"`
	} `json:"message"`
}

type anthContentDelta struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type anthMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthUsage `json:"usage"`
}

func (p *AnthropicParser) Feed(r io.Reader, tl *Timeline, onFirstToken func(time.Time)) {
	sc := sse.NewScanner(r)
	for {
		ev, err := sc.Next()
		if ev != nil {
			now := time.Now()
			tl.AppendChunk(now)
			p.handleEvent(ev, now, onFirstToken, tl)
		}
		if err != nil {
			return
		}
	}
}

func (p *AnthropicParser) handleEvent(ev *sse.Event, now time.Time, onFirstToken func(time.Time), tl *Timeline) {
	seq := len(p.chunks)
	rec := ChunkRecord{Seq: seq, Ts: now.UnixMilli(), Raw: capRaw(ev.Raw)}
	switch ev.Event {
	case "message_start":
		var m anthMessageStart
		if err := json.Unmarshal(ev.Data, &m); err == nil {
			if m.Message.Model != "" {
				p.model = m.Message.Model
			}
			u := m.Message.Usage
			if u.InputTokens > 0 {
				p.inputTokens = u.InputTokens
				p.usageSeen = true
			}
			if u.CacheReadInputTokens > 0 {
				p.cacheReadToks = u.CacheReadInputTokens
				p.usageSeen = true
			}
			if u.CacheCreationInputTokens > 0 {
				p.cacheCreateToks = u.CacheCreationInputTokens
				p.usageSeen = true
			}
		}
	case "content_block_delta":
		var d anthContentDelta
		if err := json.Unmarshal(ev.Data, &d); err == nil && d.Delta.Text != "" {
			if p.textBuilder.Len() == 0 && onFirstToken != nil {
				onFirstToken(now)
				tl.FirstToken = now
			}
			p.textBuilder.WriteString(d.Delta.Text)
			tl.LastToken = now
			rec.DeltaText = d.Delta.Text
		}
	case "message_delta":
		var m anthMessageDelta
		if err := json.Unmarshal(ev.Data, &m); err == nil {
			if m.Delta.StopReason != "" {
				p.finishReason = m.Delta.StopReason
			}
			if m.Usage.OutputTokens > 0 {
				p.outputTokens = m.Usage.OutputTokens
				p.usageSeen = true
			}
			if m.Usage.CacheReadInputTokens > 0 {
				p.cacheReadToks = m.Usage.CacheReadInputTokens
			}
			if m.Usage.CacheCreationInputTokens > 0 {
				p.cacheCreateToks = m.Usage.CacheCreationInputTokens
			}
		}
	}
	p.chunks = append(p.chunks, rec)
}

func (p *AnthropicParser) Finalize(ev *Event) {
	ev.ResponseText = p.textBuilder.String()
	ev.StreamChunks = p.chunks
	ev.ChunkCount = len(p.chunks)
	ev.FinishReason = p.finishReason
	if p.usageSeen {
		ev.InputTokens = p.inputTokens
		ev.OutputTokens = p.outputTokens
		ev.CachedInputTokens = p.cacheReadToks
		ev.CacheCreationInputTokens = p.cacheCreateToks
		// Anthropic reports input_tokens as UNCACHED only — cache_read and
		// cache_creation are tracked separately and bill at their own rates.
		// Total billable units = uncached + cached + cache_creation + output.
		ev.TotalTokens = p.inputTokens + p.cacheReadToks + p.cacheCreateToks + p.outputTokens
		ev.TokensEstimated = false
	}
	if p.model != "" && ev.Model == "" {
		ev.Model = p.model
	}
}

func (p *AnthropicParser) ParseNonStream(body []byte, ev *Event) {
	var resp struct {
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage anthUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	if resp.Model != "" && ev.Model == "" {
		ev.Model = resp.Model
	}
	var sb strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	ev.ResponseText = sb.String()
	if resp.StopReason != "" {
		ev.FinishReason = resp.StopReason
	}
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 ||
		resp.Usage.CacheReadInputTokens > 0 || resp.Usage.CacheCreationInputTokens > 0 {
		ev.InputTokens = resp.Usage.InputTokens
		ev.OutputTokens = resp.Usage.OutputTokens
		ev.CachedInputTokens = resp.Usage.CacheReadInputTokens
		ev.CacheCreationInputTokens = resp.Usage.CacheCreationInputTokens
		ev.TotalTokens = ev.InputTokens + ev.CachedInputTokens + ev.CacheCreationInputTokens + ev.OutputTokens
		ev.TokensEstimated = false
	}
}

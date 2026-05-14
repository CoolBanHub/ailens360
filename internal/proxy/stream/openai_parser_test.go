package stream

import (
	"strings"
	"testing"
	"time"
)

func TestOpenAIParserStreamCollectsTextAndUsage(t *testing.T) {
	body := "" +
		`data: {"model":"gpt-4o-mini","choices":[{"delta":{"content":"Hel"}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":"lo"}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}` + "\n\n" +
		"data: [DONE]\n\n"

	p := NewOpenAIParser()
	tl := &Timeline{RequestIn: time.Now()}
	var firstTokenAt time.Time
	p.Feed(strings.NewReader(body), tl, func(ts time.Time) { firstTokenAt = ts })

	ev := &Event{}
	p.Finalize(ev)

	if ev.ResponseText != "Hello" {
		t.Fatalf("want %q got %q", "Hello", ev.ResponseText)
	}
	if ev.Model != "gpt-4o-mini" {
		t.Fatalf("model: %q", ev.Model)
	}
	if ev.InputTokens != 5 || ev.OutputTokens != 2 || ev.TotalTokens != 7 {
		t.Fatalf("usage: in=%d out=%d total=%d", ev.InputTokens, ev.OutputTokens, ev.TotalTokens)
	}
	if ev.TokensEstimated {
		t.Fatal("usage came from upstream, should not be flagged as estimated")
	}
	if ev.FinishReason != "stop" {
		t.Fatalf("finish: %q", ev.FinishReason)
	}
	if firstTokenAt.IsZero() {
		t.Fatal("onFirstToken never invoked")
	}
	if len(tl.ChunkTimes) < 3 {
		t.Fatalf("want >=3 chunk timestamps, got %d", len(tl.ChunkTimes))
	}
}

func TestOpenAIParserNonStream(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","choices":[{"message":{"content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	p := NewOpenAIParser()
	ev := &Event{}
	p.ParseNonStream(body, ev)
	if ev.ResponseText != "Hi" {
		t.Fatalf("text: %q", ev.ResponseText)
	}
	if ev.Model != "gpt-4o" || ev.OutputTokens != 1 || ev.FinishReason != "stop" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestAnthropicParserStream(t *testing.T) {
	body := "" +
		"event: message_start\ndata: {\"message\":{\"model\":\"claude-3-5-sonnet\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {}\n\n" +
		"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi \"}}\n\n" +
		"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"there\"}}\n\n" +
		"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {}\n\n"

	p := NewAnthropicParser()
	tl := &Timeline{RequestIn: time.Now()}
	p.Feed(strings.NewReader(body), tl, func(ts time.Time) {})
	ev := &Event{}
	p.Finalize(ev)

	if ev.ResponseText != "Hi there" {
		t.Fatalf("text: %q", ev.ResponseText)
	}
	if ev.Model != "claude-3-5-sonnet" {
		t.Fatalf("model: %q", ev.Model)
	}
	if ev.InputTokens != 10 || ev.OutputTokens != 3 {
		t.Fatalf("usage: in=%d out=%d", ev.InputTokens, ev.OutputTokens)
	}
	if ev.FinishReason != "end_turn" {
		t.Fatalf("finish: %q", ev.FinishReason)
	}
}

func TestOpenAIParserCapturesReasoningAndCacheTokens(t *testing.T) {
	body := []byte(`{"model":"o1-mini","choices":[{"message":{"content":"x"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140,
			"prompt_tokens_details":{"cached_tokens":60},
			"completion_tokens_details":{"reasoning_tokens":25}}}`)
	p := NewOpenAIParser()
	ev := &Event{}
	p.ParseNonStream(body, ev)
	if ev.ReasoningTokens != 25 {
		t.Fatalf("reasoning: got %d want 25", ev.ReasoningTokens)
	}
	if ev.CachedInputTokens != 60 {
		t.Fatalf("cached: got %d want 60", ev.CachedInputTokens)
	}
}

func TestOpenAIParserDeepSeekStyleCacheTokens(t *testing.T) {
	// DeepSeek reports cache hit/miss at the usage root rather than under
	// prompt_tokens_details — verify the parser picks it up.
	body := []byte(`{"model":"deepseek-chat","choices":[{"message":{"content":"x"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":120,"completion_tokens":10,"total_tokens":130,
			"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":40,
			"completion_tokens_details":{"reasoning_tokens":3}}}`)
	p := NewOpenAIParser()
	ev := &Event{}
	p.ParseNonStream(body, ev)
	if ev.CachedInputTokens != 80 {
		t.Fatalf("cached: got %d want 80", ev.CachedInputTokens)
	}
	if ev.ReasoningTokens != 3 {
		t.Fatalf("reasoning: got %d want 3", ev.ReasoningTokens)
	}
}

func TestAnthropicParserCapturesCacheTokens(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-sonnet","stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"input_tokens":80,"output_tokens":10,
			"cache_read_input_tokens":1200,"cache_creation_input_tokens":50}}`)
	p := NewAnthropicParser()
	ev := &Event{}
	p.ParseNonStream(body, ev)
	if ev.CachedInputTokens != 1200 {
		t.Fatalf("cache_read: got %d want 1200", ev.CachedInputTokens)
	}
	if ev.CacheCreationInputTokens != 50 {
		t.Fatalf("cache_creation: got %d want 50", ev.CacheCreationInputTokens)
	}
	// Anthropic bills cache creation separately, so it must be added to the total.
	if ev.TotalTokens != 80+10+50 {
		t.Fatalf("total: got %d want %d", ev.TotalTokens, 80+10+50)
	}
}

func TestGeminiParserCapturesThoughtsAndCacheTokens(t *testing.T) {
	body := "" +
		`data: {"candidates":[{"content":{"parts":[{"text":"a"}]},"finishReason":"STOP"}],` +
		`"modelVersion":"gemini-2.0-flash-thinking",` +
		`"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":20,"totalTokenCount":120,` +
		`"thoughtsTokenCount":50,"cachedContentTokenCount":30}}` + "\n\n"

	p := NewGeminiParser()
	tl := &Timeline{RequestIn: time.Now()}
	p.Feed(strings.NewReader(body), tl, func(ts time.Time) {})
	ev := &Event{}
	p.Finalize(ev)
	if ev.ReasoningTokens != 50 {
		t.Fatalf("thoughts: got %d want 50", ev.ReasoningTokens)
	}
	if ev.CachedInputTokens != 30 {
		t.Fatalf("cached: got %d want 30", ev.CachedInputTokens)
	}
}

func TestGeminiParserStream(t *testing.T) {
	body := "" +
		`data: {"candidates":[{"content":{"parts":[{"text":"foo "}]}}],"modelVersion":"gemini-1.5-flash"}` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":"bar"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}` + "\n\n"

	p := NewGeminiParser()
	tl := &Timeline{RequestIn: time.Now()}
	p.Feed(strings.NewReader(body), tl, func(ts time.Time) {})
	ev := &Event{}
	p.Finalize(ev)

	if ev.ResponseText != "foo bar" {
		t.Fatalf("text: %q", ev.ResponseText)
	}
	if ev.Model != "gemini-1.5-flash" {
		t.Fatalf("model: %q", ev.Model)
	}
	if ev.InputTokens != 4 || ev.OutputTokens != 2 || ev.TotalTokens != 6 {
		t.Fatalf("usage: in=%d out=%d total=%d", ev.InputTokens, ev.OutputTokens, ev.TotalTokens)
	}
	if ev.FinishReason != "STOP" {
		t.Fatalf("finish: %q", ev.FinishReason)
	}
}

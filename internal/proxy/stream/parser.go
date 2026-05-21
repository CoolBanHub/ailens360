package stream

import (
	"io"
	"strings"
	"time"
)

// Parser consumes the upstream response body and emits derived data into an Event.
// Implementations are stateful and not safe for concurrent use.
type Parser interface {
	// Feed reads streaming response bytes (already tee'd from the wire), tagging
	// timestamps on the timeline and invoking onFirstToken the first time a real
	// token delta is observed.
	Feed(r io.Reader, tl *Timeline, onFirstToken func(time.Time))

	// Finalize copies the parser's collected stream output into the event.
	Finalize(ev *Event)

	// ParseNonStream populates the event from a complete (non-streaming) response body.
	ParseNonStream(body []byte, ev *Event)
}

// NewParserForHost returns the right Parser for an upstream host. Only the SSE
// wire format matters here — DeepSeek, Grok, Together, Moonshot and local vLLM
// all speak OpenAI's Chat Completions SSE, so the default branch covers them.
func NewParserForHost(host string) Parser {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "anthropic"):
		return NewAnthropicParser()
	case strings.Contains(h, "googleapis"), strings.Contains(h, "generativelanguage"):
		return NewGeminiParser()
	default:
		return NewOpenAIParser()
	}
}

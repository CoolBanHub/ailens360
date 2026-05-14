package stream

import (
	"io"
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

// NewParser returns the right Parser for the given provider. Unknown providers
// fall back to the OpenAI parser, which is the broadest format in the wild.
func NewParser(provider string) Parser {
	switch provider {
	case "anthropic":
		return NewAnthropicParser()
	case "gemini":
		return NewGeminiParser()
	default:
		return NewOpenAIParser()
	}
}

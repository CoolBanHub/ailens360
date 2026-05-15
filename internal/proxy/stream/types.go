package stream

import (
	"time"
)

type Timeline struct {
	RequestIn          time.Time
	UpstreamDial       time.Time
	UpstreamRequestOut time.Time
	UpstreamFirstByte  time.Time
	FirstToken         time.Time
	LastToken          time.Time
	UpstreamDone       time.Time
	ResponseOut        time.Time
	ChunkTimes         []time.Time
}

// Event is the normalized trace event produced by the proxy and consumed by
// the collector. Bodies live in the body store; the event only carries the
// object key + size so Redis Stream payloads stay small.
type Event struct {
	TraceID  string
	IsStream bool
	Model    string

	StatusCode   int
	Status       string // success | error | aborted
	ErrorMsg     string
	FinishReason string
	StreamStatus string // completed | aborted | errored | stalled

	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	TokensEstimated bool

	// Token breakdown. Populated only when the upstream usage payload reports
	// them — zero means "not provided", not "zero usage".
	//   ReasoningTokens         — thinking/reasoning tokens (OpenAI o-series,
	//                              Gemini thoughtsTokenCount). Where the
	//                              provider includes thinking inside
	//                              OutputTokens (Anthropic), the reasoning
	//                              tokens are a subset of OutputTokens.
	//   CachedInputTokens       — input tokens served from cache at the
	//                              discounted rate (OpenAI cached_tokens,
	//                              Anthropic cache_read_input_tokens, Gemini
	//                              cachedContentTokenCount). A subset of
	//                              InputTokens.
	//   CacheCreationInputTokens — Anthropic-only: tokens billed to write
	//                              the cache entry. NOT a subset of
	//                              InputTokens.
	ReasoningTokens          int
	CachedInputTokens        int
	CacheCreationInputTokens int

	// ResponseText carries the joined delta text (streams) or extracted
	// content (non-stream) for fallback token estimation by the collector when
	// upstream usage is missing. Bound by the proxy's RawBodyLimit.
	ResponseText  string
	BytesStreamed int64
	ChunkCount    int

	// Header maps are JSON-marshaled into the Redis Stream payload by the
	// proxy sink. Sensitive headers are redacted before this struct is built.
	RequestHeaders  map[string][]string
	ResponseHeaders map[string][]string

	RequestPath string // absolute upstream URL including scheme

	// Body store references. Empty key signals "body unavailable" (upload
	// failed or skipped); size is the uncompressed byte count of what was
	// streamed past the wire on the proxy.
	RequestBodyKey   string
	ResponseBodyKey  string
	RequestBodySize  int64
	ResponseBodySize int64

	Timeline Timeline

	ProjectID    string
	UserID       string // from X-AILens-User
	SessionID    string // from X-AILens-Session
	Tags         string // from X-AILens-Tag (comma-separated)
	LogicTraceID string // from X-AILens-Trace-Id (groups spans of the same logical run)
	TraceName    string // from X-AILens-Trace-Name (human label for the logical trace)
}

func (t *Timeline) AppendChunk(ts time.Time) {
	t.ChunkTimes = append(t.ChunkTimes, ts)
}

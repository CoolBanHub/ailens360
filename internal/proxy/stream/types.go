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

// Event is the normalized stream event consumed by the collector.
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

	ResponseText  string // joined delta text for streams, body for non-stream
	BytesStreamed int64
	ChunkCount    int

	// Raw payloads (truncated to RawBodyLimit by caller).
	RequestHeaders  map[string][]string
	RequestBody     []byte
	RequestPath     string // path after /p/{code}/, with leading slash
	ResponseHeaders map[string][]string
	ResponseBody    []byte
	StreamChunks    []ChunkRecord

	Timeline Timeline

	ProjectID    string
	UserID       string // from X-AILens-User
	SessionID    string // from X-AILens-Session
	Tags         string // from X-AILens-Tag (comma-separated)
	LogicTraceID string // from X-AILens-Trace-Id (groups spans of the same logical run)
	TraceName    string // from X-AILens-Trace-Name (human label for the logical trace)
}

type ChunkRecord struct {
	Seq       int    `json:"seq"`
	Ts        int64  `json:"ts"` // unix ms
	DeltaText string `json:"delta_text,omitempty"`
	DeltaToks int    `json:"delta_tokens,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

func (t *Timeline) AppendChunk(ts time.Time) {
	t.ChunkTimes = append(t.ChunkTimes, ts)
}

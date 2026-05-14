package repo

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("repo: not found")

// Project is the only first-class tenant. Each project owns a unique ProjectKey
// that callers send in the X-AILens-Project-Key header to route traffic to this
// project. The proxy URL path itself only carries the upstream URL: /p/{upstream}.
type Project struct {
	ID         string
	ProjectKey string
	Name       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Trace is one proxied HTTP call's full record — semantically a SPAN of a
// logical trace. Multiple Trace rows that share the same TraceID belong to
// one logical run (e.g. all model calls inside a single agent.Generate).
type Trace struct {
	ID                       string // span id (one HTTP round-trip)
	TraceID                  string // groups spans of the same logical run (X-AILens-Trace-Id)
	TraceName                string // human label of the logical trace (X-AILens-Trace-Name)
	ProjectID                string
	UserID                   string // X-AILens-User on the inbound request
	SessionID                string // X-AILens-Session
	Tags                     string // X-AILens-Tag, comma-separated
	Provider                 string
	Model                    string
	IsStream                 bool
	Status                   string // success | error | aborted
	StatusCode               int
	ErrorMessage             string
	RequestHeaders           string // JSON
	RequestBody              string // truncated JSON
	RequestPath              string // path after /p/{code}/, with leading slash
	ResponseHeaders          string // JSON
	ResponseBody             string // truncated; for streams: joined deltas or final JSON
	StreamChunks             string // JSON array of chunks (capped)
	Timeline                 string // JSON array of {event,ts}
	InputTokens              int
	OutputTokens             int
	TotalTokens              int
	ReasoningTokens          int // thinking/reasoning (subset of OutputTokens for Anthropic; separate for OpenAI/Gemini)
	CachedInputTokens        int // input tokens served from cache (subset of InputTokens)
	CacheCreationInputTokens int // Anthropic-only: tokens billed to write the cache
	TokensEstimated          bool
	CostUSD                  float64
	LatencyMs                int64
	TTFTMs                   *int64
	TTFBMs                   *int64
	GenDurationMs            *int64
	TPS                      float64
	ChunkCount               int
	BytesStreamed            int64
	FinishReason             string
	StreamStatus             string
	CreatedAt                time.Time
}

type ListTraceFilter struct {
	ProjectID   string
	TraceID     string
	UserID      string
	SessionID   string
	Provider    string
	Model       string
	Status      string
	StartUnixMs int64
	EndUnixMs   int64
	Limit       int
	Offset      int
}

type UsageStat struct {
	Key                      string
	Calls                    int64
	InputTokens              int64
	OutputTokens             int64
	TotalTokens              int64
	ReasoningTokens          int64
	CachedInputTokens        int64
	CacheCreationInputTokens int64
	CostUSD                  float64
	AvgLatencyMs             float64
	ErrorRate                float64
}

type ProjectRepo interface {
	Create(ctx context.Context, p *Project) error
	GetByID(ctx context.Context, id string) (*Project, error)
	GetByProjectKey(ctx context.Context, key string) (*Project, error)
	List(ctx context.Context) ([]*Project, error)
	Update(ctx context.Context, p *Project) error
	UpdateProjectKey(ctx context.Context, id, projectKey string) error
	Delete(ctx context.Context, id string) error
}

// TraceGroup is the Langfuse-style "logical trace": one row per trace_id,
// aggregating all spans that share that id. Used by the trace-list UI.
type TraceGroup struct {
	TraceID                  string
	TraceName                string
	ProjectID                string
	UserID                   string
	SessionID                string
	SpanCount                int
	InputTokens              int64
	OutputTokens             int64
	TotalTokens              int64
	ReasoningTokens          int64
	CachedInputTokens        int64
	CacheCreationInputTokens int64
	CostUSD                  float64
	LatencyMs                int64  // last_ts - first_ts within the group
	Status                   string // worst-of: error > aborted > success
	StartedAt                time.Time
}

type ListTraceGroupFilter struct {
	ProjectID   string
	UserID      string
	SessionID   string
	TraceName   string // exact match on traces.trace_name
	Status      string // success | error | aborted — uses "worst-of-span" via subquery
	Provider    string // matches if ANY span in the trace uses this provider
	Model       string // matches if ANY span in the trace uses this model
	StartUnixMs int64
	EndUnixMs   int64
	Limit       int
	Offset      int
}

type TraceRepo interface {
	Create(ctx context.Context, t *Trace) error
	BatchCreate(ctx context.Context, ts []*Trace) error
	GetByID(ctx context.Context, id string) (*Trace, error)
	List(ctx context.Context, f ListTraceFilter) ([]*Trace, int64, error)
	ListGroups(ctx context.Context, f ListTraceGroupFilter) ([]*TraceGroup, int64, error)
	UsageByDimension(ctx context.Context, dim string, startMs, endMs int64, projectID string) ([]UsageStat, error)
	// Facets returns the distinct providers and models seen for a project,
	// in descending count order. Used to populate dynamic filter dropdowns.
	Facets(ctx context.Context, projectID string) (providers, models []string, err error)
}

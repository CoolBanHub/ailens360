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
	ID              string // span id (one HTTP round-trip)
	TraceID         string // groups spans of the same logical run (X-AILens-Trace-Id)
	TraceName       string // human label of the logical trace (X-AILens-Trace-Name)
	ProjectID       string
	UserID          string // X-AILens-User on the inbound request
	SessionID       string // X-AILens-Session
	Tags            string // X-AILens-Tag, comma-separated
	Model           string
	IsStream        bool
	Status          string // success | error | aborted
	StatusCode      int
	ErrorMessage    string
	RequestHeaders  string // JSON
	RequestPath     string // upstream URL (absolute, including scheme)
	ResponseHeaders string // JSON
	// Body keys point to objects in the configured body store. Empty means the
	// upload failed or was skipped; UI shows a "body unavailable" placeholder.
	// Size is the uncompressed byte count of what was uploaded.
	RequestBodyKey           string
	ResponseBodyKey          string
	RequestBodySize          int64
	ResponseBodySize         int64
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
	// Facets returns dynamic filter inputs for the trace-list UI. `models` is
	// the distinct non-empty model names seen in the project (desc by count,
	// drives the dropdown). `hasAny` reports whether ANY trace exists for the
	// project — empty-model traces still count — so the UI can pick between
	// the onboarding "project has no data" and the "no matches" empty states.
	Facets(ctx context.Context, projectID string) (models []string, hasAny bool, err error)
	// DeleteByProject removes every trace row owned by projectID. Used by the
	// project hard-delete flow; blob bodies are wiped separately via the
	// bodystore. Returns the number of rows removed.
	DeleteByProject(ctx context.Context, projectID string) (int64, error)
}

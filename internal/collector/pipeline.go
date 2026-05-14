package collector

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

type Config struct {
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
}

// Pipeline ingests proxy events, computes derived metrics, and persists them in batches.
type Pipeline struct {
	cfg      Config
	logger   *slog.Logger
	traces   repo.TraceRepo
	pricing  *pricing.Catalog
	tok      tokenizer.Estimator
	realtime *metrics.Realtime

	ch     chan *stream.Event
	wg     sync.WaitGroup
	cancel context.CancelFunc
	once   sync.Once
}

func New(cfg Config, logger *slog.Logger, traces repo.TraceRepo, p *pricing.Catalog, tok tokenizer.Estimator, rt *metrics.Realtime) *Pipeline {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = time.Second
	}
	return &Pipeline{
		cfg:      cfg,
		logger:   logger,
		traces:   traces,
		pricing:  p,
		tok:      tok,
		realtime: rt,
		ch:       make(chan *stream.Event, cfg.BufferSize),
	}
}

func (p *Pipeline) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.wg.Add(1)
	go p.run(ctx)
}

func (p *Pipeline) Stop() {
	p.once.Do(func() {
		close(p.ch)
	})
	p.wg.Wait()
	if p.cancel != nil {
		p.cancel()
	}
}

// Submit implements proxy.EventSink. Drops events if the buffer is full to avoid blocking the proxy hot path.
func (p *Pipeline) Submit(ev *stream.Event) {
	select {
	case p.ch <- ev:
	default:
		p.logger.Warn("collector buffer full, dropping event", "trace_id", ev.TraceID)
	}
}

func (p *Pipeline) run(ctx context.Context) {
	defer p.wg.Done()
	tick := time.NewTicker(p.cfg.FlushInterval)
	defer tick.Stop()
	batch := make([]*repo.Trace, 0, p.cfg.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		flushCtx := ctx
		cancel := func() {}
		if ctx.Err() != nil {
			flushCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		}
		err := p.traces.BatchCreate(flushCtx, batch)
		cancel()
		if err != nil {
			p.logger.Error("trace batch insert failed", "err", err, "n", len(batch))
			batch = batch[:0]
			return
		}
		if p.realtime != nil {
			samples := make([]metrics.Sample, 0, len(batch))
			for _, t := range batch {
				samples = append(samples, metrics.Sample{
					ProjectID:  t.ProjectID,
					Tokens:     t.TotalTokens,
					CostMicros: int64(t.CostUSD * 1_000_000),
				})
			}
			metricsCtx := flushCtx
			if metricsCtx.Err() != nil {
				metricsCtx = context.Background()
			}
			p.realtime.RecordBatch(metricsCtx, samples)
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case ev, ok := <-p.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, p.transform(ev))
			if len(batch) >= p.cfg.BatchSize {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func (p *Pipeline) transform(ev *stream.Event) *repo.Trace {
	tl := ev.Timeline
	latencyMs := int64(0)
	if !tl.ResponseOut.IsZero() && !tl.RequestIn.IsZero() {
		latencyMs = tl.ResponseOut.Sub(tl.RequestIn).Milliseconds()
	}
	var ttftMs, ttfbMs, genMs *int64
	if !tl.FirstToken.IsZero() && !tl.RequestIn.IsZero() {
		v := tl.FirstToken.Sub(tl.RequestIn).Milliseconds()
		ttftMs = &v
	}
	if !tl.UpstreamFirstByte.IsZero() && !tl.UpstreamRequestOut.IsZero() {
		v := tl.UpstreamFirstByte.Sub(tl.UpstreamRequestOut).Milliseconds()
		ttfbMs = &v
	}
	if !tl.LastToken.IsZero() && !tl.FirstToken.IsZero() {
		v := tl.LastToken.Sub(tl.FirstToken).Milliseconds()
		genMs = &v
	}

	if ev.OutputTokens == 0 && ev.ResponseText != "" {
		ev.OutputTokens = p.tok.Count(ev.Model, ev.ResponseText)
		ev.TokensEstimated = true
	}
	if ev.InputTokens == 0 && len(ev.RequestBody) > 0 {
		ev.InputTokens = p.tok.Count(ev.Model, string(ev.RequestBody))
		ev.TokensEstimated = true
	}
	if ev.TotalTokens == 0 {
		ev.TotalTokens = ev.InputTokens + ev.OutputTokens
	}
	cost := p.pricing.Cost(ev.Model, pricing.TokenUsage{
		Input:         ev.InputTokens,
		Output:        ev.OutputTokens,
		CachedInput:   ev.CachedInputTokens,
		CacheCreation: ev.CacheCreationInputTokens,
		ContextTokens: contextSize(ev),
	})

	var tps float64
	if genMs != nil && *genMs > 0 && ev.OutputTokens > 0 {
		tps = float64(ev.OutputTokens) / (float64(*genMs) / 1000.0)
	}

	timelineJSON, _ := json.Marshal(buildTimelineEntries(tl))
	reqHdr, _ := json.Marshal(ev.RequestHeaders)
	respHdr, _ := json.Marshal(ev.ResponseHeaders)

	respBody := ev.ResponseText
	if respBody == "" {
		respBody = string(ev.ResponseBody)
	}

	logicTraceID := ev.LogicTraceID
	if logicTraceID == "" {
		logicTraceID = ev.TraceID
	}

	t := &repo.Trace{
		ID:              ev.TraceID,
		TraceID:         logicTraceID,
		TraceName:       ev.TraceName,
		ProjectID:       ev.ProjectID,
		UserID:          ev.UserID,
		SessionID:       ev.SessionID,
		Tags:            ev.Tags,
		Provider:        ev.Provider,
		Model:           ev.Model,
		IsStream:        ev.IsStream,
		Status:          ev.Status,
		StatusCode:      ev.StatusCode,
		ErrorMessage:    ev.ErrorMsg,
		RequestHeaders:  string(reqHdr),
		RequestBody:     string(ev.RequestBody),
		RequestPath:     ev.RequestPath,
		ResponseHeaders: string(respHdr),
		ResponseBody:    respBody,
		// Per-chunk SSE records used to be persisted here for a "stream
		// chunks" debug view that nobody used; ChunkCount above is enough.
		StreamChunks:             "",
		Timeline:                 string(timelineJSON),
		InputTokens:              ev.InputTokens,
		OutputTokens:             ev.OutputTokens,
		TotalTokens:              ev.TotalTokens,
		ReasoningTokens:          ev.ReasoningTokens,
		CachedInputTokens:        ev.CachedInputTokens,
		CacheCreationInputTokens: ev.CacheCreationInputTokens,
		TokensEstimated:          ev.TokensEstimated,
		CostUSD:                  cost,
		LatencyMs:                latencyMs,
		TTFTMs:                   ttftMs,
		TTFBMs:                   ttfbMs,
		GenDurationMs:            genMs,
		TPS:                      tps,
		ChunkCount:               ev.ChunkCount,
		BytesStreamed:            ev.BytesStreamed,
		FinishReason:             ev.FinishReason,
		StreamStatus:             ev.StreamStatus,
		CreatedAt:                tl.RequestIn,
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	return t
}

// contextSize computes the prompt-side context for tier-pricing lookup. The
// upstreams report token usage with different semantics:
//
//   - OpenAI / Gemini: ev.InputTokens already includes the cached portion, so
//     it IS the context.
//   - Anthropic: ev.InputTokens is uncached only — cached + cache_creation are
//     reported separately and must be added back.
//   - Unknown providers: fall back to Anthropic-style addition. Overshooting
//     tier on a non-tiered model is harmless (no tier matches); undershooting
//     on a tiered model would silently undercharge.
func contextSize(ev *stream.Event) int {
	switch strings.ToLower(ev.Provider) {
	case "openai", "gemini", "google":
		return ev.InputTokens
	default:
		return ev.InputTokens + ev.CachedInputTokens + ev.CacheCreationInputTokens
	}
}

type timelineEntry struct {
	Event string `json:"event"`
	Ts    int64  `json:"ts"`
}

func buildTimelineEntries(tl stream.Timeline) []timelineEntry {
	out := make([]timelineEntry, 0, 8)
	add := func(name string, t time.Time) {
		if !t.IsZero() {
			out = append(out, timelineEntry{Event: name, Ts: t.UnixMilli()})
		}
	}
	add("request_received", tl.RequestIn)
	add("upstream_request_sent", tl.UpstreamRequestOut)
	add("upstream_first_byte", tl.UpstreamFirstByte)
	add("first_token", tl.FirstToken)
	add("last_token", tl.LastToken)
	add("upstream_done", tl.UpstreamDone)
	add("response_out", tl.ResponseOut)
	return out
}

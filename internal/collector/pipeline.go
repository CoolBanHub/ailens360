// Package collector consumes proxy events off a Redis Stream, decorates them
// (token counts, pricing, derived latencies) and batches them into Postgres.
// It also feeds the realtime Redis metrics dashboard.
//
// The hot path is fully decoupled from the proxy: even if Postgres is slow or
// Redis is degraded, the proxy keeps serving — events queue up in the stream.
package collector

import (
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

// Transformer converts a proxy stream.Event into a repo.Trace by computing
// derived fields (token estimates for outputs only, cost, latencies, TPS).
//
// Note: input-token estimation was dropped in the body-store refactor because
// request bytes no longer travel through the IPC stream. Modern upstream
// providers report usage for both prompt and completion tokens in their final
// SSE event, so the fallback path was rarely exercised; the value of keeping
// stream messages small outweighed it.
type Transformer struct {
	logger  *slog.Logger
	pricing *pricing.Catalog
	tok     tokenizer.Estimator
}

func NewTransformer(logger *slog.Logger, p *pricing.Catalog, tok tokenizer.Estimator) *Transformer {
	return &Transformer{logger: logger, pricing: p, tok: tok}
}

func (t *Transformer) Transform(ev *stream.Event) *repo.Trace {
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
		ev.OutputTokens = t.tok.Count(ev.Model, ev.ResponseText)
		ev.TokensEstimated = true
	}
	if ev.TotalTokens == 0 {
		ev.TotalTokens = ev.InputTokens + ev.OutputTokens
	}
	cost := t.pricing.Cost(ev.Model, pricing.TokenUsage{
		Input:         ev.InputTokens,
		Output:        ev.OutputTokens,
		CachedInput:   ev.CachedInputTokens,
		CacheCreation: ev.CacheCreationInputTokens,
	})

	var tps float64
	if genMs != nil && *genMs > 0 && ev.OutputTokens > 0 {
		tps = float64(ev.OutputTokens) / (float64(*genMs) / 1000.0)
	}

	timelineJSON, _ := json.Marshal(buildTimelineEntries(tl))
	reqHdr, _ := json.Marshal(ev.RequestHeaders)
	respHdr, _ := json.Marshal(ev.ResponseHeaders)

	logicTraceID := ev.LogicTraceID
	if logicTraceID == "" {
		logicTraceID = ev.TraceID
	}

	tr := &repo.Trace{
		ID:                       ev.TraceID,
		TraceID:                  logicTraceID,
		TraceName:                ev.TraceName,
		ProjectID:                ev.ProjectID,
		UserID:                   ev.UserID,
		SessionID:                ev.SessionID,
		Tags:                     ev.Tags,
		Model:                    ev.Model,
		IsStream:                 ev.IsStream,
		Status:                   ev.Status,
		StatusCode:               ev.StatusCode,
		ErrorMessage:             ev.ErrorMsg,
		RequestHeaders:           string(reqHdr),
		RequestPath:              ev.RequestPath,
		ResponseHeaders:          string(respHdr),
		RequestBodyKey:           ev.RequestBodyKey,
		ResponseBodyKey:          ev.ResponseBodyKey,
		RequestBodySize:          ev.RequestBodySize,
		ResponseBodySize:         ev.ResponseBodySize,
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
	if tr.CreatedAt.IsZero() {
		tr.CreatedAt = time.Now()
	}
	return tr
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

// realtimeSamples builds the rolling metrics samples written alongside every
// PG flush. Realtime metrics live in Redis with 1s buckets — see internal/metrics.
func realtimeSamples(batch []*repo.Trace) []metrics.Sample {
	out := make([]metrics.Sample, 0, len(batch))
	for _, t := range batch {
		out = append(out, metrics.Sample{
			ProjectID:  t.ProjectID,
			Tokens:     t.TotalTokens,
			CostMicros: int64(t.CostUSD * 1_000_000),
		})
	}
	return out
}

// hostnameOnce returns a stable hostname suffix for consumer names; falls back
// to "unknown" only if os.Hostname errors, which should never happen.
func hostnameOnce() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

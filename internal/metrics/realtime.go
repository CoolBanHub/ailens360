// Package metrics maintains near-real-time counters in Redis so the
// dashboard can show "last 60s QPS" / "last 60s cost" without scanning the
// traces table. It is approximate by design — the source of truth for
// billing remains Postgres aggregates.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Realtime writes per-second buckets keyed by (projectID, metric). Reads sum
// the last N buckets via a single MGET.
type Realtime struct {
	rdb           *redis.Client
	windowSecs    int
	retentionSecs int
	logger        *slog.Logger
}

func New(rdb *redis.Client, windowSecs, retentionSecs int, logger *slog.Logger) *Realtime {
	if windowSecs <= 0 {
		windowSecs = 60
	}
	if retentionSecs <= 0 || retentionSecs < windowSecs {
		retentionSecs = 7200
	}
	return &Realtime{rdb: rdb, windowSecs: windowSecs, retentionSecs: retentionSecs, logger: logger}
}

// Sample is one trace's contribution to the rolling counters.
type Sample struct {
	ProjectID  string
	Tokens     int
	CostMicros int64 // cost stored as micro-USD to keep buckets integer-safe under INCRBY
}

// RecordBatch bumps the per-project second-buckets for every sample using a
// single Redis pipeline (1 RTT). Intended to be called fire-and-forget from
// the collector goroutine right after a successful DB batch insert. Errors
// are logged but not surfaced — Redis blips must never poison ingest.
func (r *Realtime) RecordBatch(ctx context.Context, samples []Sample) {
	if r == nil || len(samples) == 0 {
		return
	}
	bucket := strconv.FormatInt(time.Now().Unix(), 10)
	retention := time.Duration(r.retentionSecs) * time.Second
	pipe := r.rdb.Pipeline()
	for _, s := range samples {
		if s.ProjectID == "" {
			continue
		}
		keyQPS := projectKey("qps", s.ProjectID, bucket)
		pipe.IncrBy(ctx, keyQPS, 1)
		pipe.Expire(ctx, keyQPS, retention)
		if s.Tokens > 0 {
			keyTok := projectKey("tok", s.ProjectID, bucket)
			pipe.IncrBy(ctx, keyTok, int64(s.Tokens))
			pipe.Expire(ctx, keyTok, retention)
		}
		if s.CostMicros > 0 {
			keyCost := projectKey("cost", s.ProjectID, bucket)
			pipe.IncrBy(ctx, keyCost, s.CostMicros)
			pipe.Expire(ctx, keyCost, retention)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		if r.logger != nil {
			r.logger.Warn("realtime record failed", "n", len(samples), "err", err)
		}
	}
}

// ProjectLive returns (qps, tokens/sec, cost USD) summed over the last
// windowSecs buckets (excluding the current second to avoid in-flight skew).
func (r *Realtime) ProjectLive(ctx context.Context, projectID string) (qps float64, tokensPerSec float64, costUSD float64, err error) {
	if projectID == "" {
		return 0, 0, 0, nil
	}
	now := time.Now().Unix()
	// Range: [now-windowSecs, now-1]. Skipping `now` keeps the result stable
	// even when a sample is being written into that bucket concurrently.
	start := now - int64(r.windowSecs)
	end := now - 1
	n := r.windowSecs
	qpsKeys := make([]string, n)
	tokKeys := make([]string, n)
	costKeys := make([]string, n)
	for i := 0; i < n; i++ {
		b := strconv.FormatInt(start+int64(i), 10)
		qpsKeys[i] = projectKey("qps", projectID, b)
		tokKeys[i] = projectKey("tok", projectID, b)
		costKeys[i] = projectKey("cost", projectID, b)
	}
	_ = end // start..start+n-1 inclusive
	qpsSum, err := sumKeys(ctx, r.rdb, qpsKeys)
	if err != nil {
		return 0, 0, 0, err
	}
	tokSum, err := sumKeys(ctx, r.rdb, tokKeys)
	if err != nil {
		return 0, 0, 0, err
	}
	costSum, err := sumKeys(ctx, r.rdb, costKeys)
	if err != nil {
		return 0, 0, 0, err
	}
	w := float64(r.windowSecs)
	return float64(qpsSum) / w, float64(tokSum) / w, float64(costSum) / 1_000_000 / w, nil
}

func sumKeys(ctx context.Context, rdb *redis.Client, keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	vals, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, v := range vals {
		if v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			continue
		}
		total += n
	}
	return total, nil
}

func projectKey(metric, projectID, bucket string) string {
	return fmt.Sprintf("m:%s:%s:%s", metric, projectID, bucket)
}

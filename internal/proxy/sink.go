package proxy

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
)

// StreamSink ships finalized proxy events to a Redis Stream for the collector
// to consume. Bodies are NOT inlined here — only the body store object keys
// the proxy already uploaded.
//
// The sink is best-effort: XADD failures are logged but never propagated to
// the proxy hot path. The client has already received its response by the
// time Submit is called; losing one trace is preferable to slowing the next
// request.
type StreamSink struct {
	rdb        *redis.Client
	key        string
	logger     *slog.Logger
	addTimeout time.Duration
}

func NewStreamSink(rdb *redis.Client, key string, logger *slog.Logger) *StreamSink {
	return &StreamSink{
		rdb:        rdb,
		key:        key,
		logger:     logger,
		addTimeout: 2 * time.Second,
	}
}

// Submit serializes the event to JSON and XADDs it to the stream.
func (s *StreamSink) Submit(ev *stream.Event) {
	payload, err := json.Marshal(ev)
	if err != nil {
		s.logger.Error("stream sink: marshal event", "err", err, "trace_id", ev.TraceID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.addTimeout)
	defer cancel()
	if err := s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: s.key,
		Values: map[string]any{"data": payload},
	}).Err(); err != nil {
		s.logger.Error("stream sink: XADD failed", "err", err, "trace_id", ev.TraceID)
	}
}

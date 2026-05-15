package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// Config tunes the Redis Stream consumer loop.
type Config struct {
	StreamKey      string
	ConsumerGroup  string
	ConsumerName   string // optional — defaults to "${hostname}-${pid}"
	BlockTimeout   time.Duration
	BatchSize      int
	PendingIdleMax time.Duration
	ClaimInterval  time.Duration
}

// Consumer reads events from a Redis Stream, transforms them, and batch-inserts
// them into Postgres. Failed-to-ACK messages stuck in the Pending Entries List
// past PendingIdleMax are XCLAIM'd by the periodic reclaimer.
type Consumer struct {
	cfg    Config
	rdb    *redis.Client
	logger *slog.Logger

	transformer *Transformer
	traces      repo.TraceRepo
	realtime    *metrics.Realtime

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewConsumer(
	cfg Config,
	rdb *redis.Client,
	logger *slog.Logger,
	transformer *Transformer,
	traces repo.TraceRepo,
	realtime *metrics.Realtime,
) *Consumer {
	if cfg.StreamKey == "" {
		cfg.StreamKey = "ailens360:traces"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "collector"
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = fmt.Sprintf("%s-%d", hostnameOnce(), os.Getpid())
	}
	if cfg.BlockTimeout <= 0 {
		cfg.BlockTimeout = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.PendingIdleMax <= 0 {
		cfg.PendingIdleMax = time.Minute
	}
	if cfg.ClaimInterval <= 0 {
		cfg.ClaimInterval = 30 * time.Second
	}
	return &Consumer{
		cfg:         cfg,
		rdb:         rdb,
		logger:      logger,
		transformer: transformer,
		traces:      traces,
		realtime:    realtime,
	}
}

// Start ensures the consumer group exists, then runs the read + claim loops.
// Returns after the group is created — actual consumption is in background
// goroutines.
func (c *Consumer) Start(parent context.Context) error {
	if err := c.ensureGroup(parent); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.wg.Add(2)
	go c.readLoop(ctx)
	go c.claimLoop(ctx)
	return nil
}

func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	// MKSTREAM creates the stream too if it doesn't exist yet — saves a separate
	// XADD bootstrap on a fresh cluster.
	err := c.rdb.XGroupCreateMkStream(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("collector: XGROUP CREATE: %w", err)
	}
	return nil
}

func (c *Consumer) readLoop(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.cfg.ConsumerGroup,
			Consumer: c.cfg.ConsumerName,
			Streams:  []string{c.cfg.StreamKey, ">"},
			Count:    int64(c.cfg.BatchSize),
			Block:    c.cfg.BlockTimeout,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			c.logger.Warn("XREADGROUP failed", "err", err)
			// Avoid a hot loop when Redis is down.
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, s := range streams {
			c.processBatch(ctx, s.Messages)
		}
	}
}

// claimLoop scans the consumer group's PEL and reclaims messages whose owning
// consumer has been silent for too long. Lets a dead collector replica's work
// migrate to a healthy one.
func (c *Consumer) claimLoop(ctx context.Context) {
	defer c.wg.Done()
	t := time.NewTicker(c.cfg.ClaimInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.reclaim(ctx)
		}
	}
}

func (c *Consumer) reclaim(ctx context.Context) {
	// XAUTOCLAIM transparently claims + returns the messages — far simpler
	// than XPENDING + XCLAIM in two steps.
	res, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.cfg.StreamKey,
		Group:    c.cfg.ConsumerGroup,
		Consumer: c.cfg.ConsumerName,
		MinIdle:  c.cfg.PendingIdleMax,
		Start:    "0-0",
		Count:    int64(c.cfg.BatchSize),
	}).Result()
	if err != nil {
		// On a freshly created stream Redis sometimes returns NOGROUP momentarily;
		// just retry next tick.
		if !errors.Is(err, redis.Nil) {
			c.logger.Warn("XAUTOCLAIM failed", "err", err)
		}
		return
	}
	if len(res) > 0 {
		c.logger.Info("reclaimed pending messages", "n", len(res))
		c.processBatch(ctx, res)
	}
}

func (c *Consumer) processBatch(ctx context.Context, msgs []redis.XMessage) {
	if len(msgs) == 0 {
		return
	}
	batch := make([]*repo.Trace, 0, len(msgs))
	ackIDs := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ev, err := decodeEvent(m)
		if err != nil {
			c.logger.Error("decode stream message", "id", m.ID, "err", err)
			// ACK-and-drop: a malformed message would loop forever otherwise.
			// Future improvement: route to a dead-letter stream instead.
			ackIDs = append(ackIDs, m.ID)
			continue
		}
		batch = append(batch, c.transformer.Transform(ev))
		ackIDs = append(ackIDs, m.ID)
	}
	opCtx := ctx
	if opCtx.Err() != nil {
		// Don't lose work on shutdown — the write/realtime/XACK all need a
		// live context. Wrap a fresh background with a generous timeout.
		var cancel context.CancelFunc
		opCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	if len(batch) > 0 {
		if err := c.traces.BatchCreate(opCtx, batch); err != nil {
			c.logger.Error("trace batch insert failed", "err", err, "n", len(batch))
			// Without ACK, the messages stay in PEL — the claim loop will give
			// another collector a shot. Skip both ACK and realtime update on
			// failure so we don't double-count later.
			return
		}
		if c.realtime != nil {
			c.realtime.RecordBatch(opCtx, realtimeSamples(batch))
		}
	}
	if len(ackIDs) > 0 {
		if err := c.rdb.XAck(opCtx, c.cfg.StreamKey, c.cfg.ConsumerGroup, ackIDs...).Err(); err != nil {
			c.logger.Warn("XACK failed", "err", err, "n", len(ackIDs))
		}
	}
}

func decodeEvent(m redis.XMessage) (*stream.Event, error) {
	raw, ok := m.Values["data"]
	if !ok {
		return nil, errors.New("missing 'data' field")
	}
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected 'data' type %T", raw)
	}
	var ev stream.Event
	if err := json.Unmarshal([]byte(s), &ev); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}
	return &ev, nil
}

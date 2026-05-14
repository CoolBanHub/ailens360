package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/redis/go-redis/v9"
)

type entry[V any] struct {
	val       V
	expiresAt time.Time
}

// Tiered combines a per-process LRU (L1) with Redis (L2). Pub/Sub broadcasts
// Delete() so peer replicas drop their L1 entry within network latency.
// Missed messages are caught by L1 TTL.
type Tiered[V any] struct {
	namespace  string
	redis      *redis.Client
	l1         *lru.Cache[string, entry[V]]
	l1TTL      time.Duration
	l2TTL      time.Duration
	logger     *slog.Logger
	instanceID string

	pubsubCancel context.CancelFunc
	pubsubWG     sync.WaitGroup
}

// NewTiered constructs a Tiered cache. The pubsub subscriber is started
// immediately; Close() to stop it.
func NewTiered[V any](
	namespace string,
	rdb *redis.Client,
	l1Cap int,
	l1TTL, l2TTL time.Duration,
	logger *slog.Logger,
) (*Tiered[V], error) {
	if namespace == "" {
		return nil, errors.New("cache: namespace required")
	}
	if rdb == nil {
		return nil, errors.New("cache: redis client required")
	}
	if l1Cap <= 0 {
		l1Cap = 10000
	}
	if l1TTL <= 0 {
		l1TTL = 30 * time.Second
	}
	if l2TTL <= 0 {
		l2TTL = 10 * time.Minute
	}
	l1, err := lru.New[string, entry[V]](l1Cap)
	if err != nil {
		return nil, err
	}
	t := &Tiered[V]{
		namespace:  namespace,
		redis:      rdb,
		l1:         l1,
		l1TTL:      l1TTL,
		l2TTL:      l2TTL,
		logger:     logger,
		instanceID: newInstanceID(),
	}
	pubsubCtx, cancel := context.WithCancel(context.Background())
	t.pubsubCancel = cancel
	t.pubsubWG.Add(1)
	go t.subscribe(pubsubCtx)
	return t, nil
}

func (t *Tiered[V]) l2Key(key string) string { return "cache:" + t.namespace + ":" + key }
func (t *Tiered[V]) channel() string         { return "cache-invalidate:" + t.namespace }

func (t *Tiered[V]) Get(ctx context.Context, key string) (V, bool, error) {
	var zero V
	if e, ok := t.l1.Get(key); ok {
		if e.expiresAt.IsZero() || time.Now().Before(e.expiresAt) {
			return e.val, true, nil
		}
		t.l1.Remove(key)
	}
	raw, err := t.redis.Get(ctx, t.l2Key(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return zero, false, nil
		}
		return zero, false, err
	}
	var v V
	if err := json.Unmarshal(raw, &v); err != nil {
		return zero, false, fmt.Errorf("cache decode: %w", err)
	}
	t.l1.Add(key, entry[V]{val: v, expiresAt: time.Now().Add(t.l1TTL)})
	return v, true, nil
}

func (t *Tiered[V]) Set(ctx context.Context, key string, val V) error {
	t.l1.Add(key, entry[V]{val: val, expiresAt: time.Now().Add(t.l1TTL)})
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return t.redis.Set(ctx, t.l2Key(key), raw, t.l2TTL).Err()
}

func (t *Tiered[V]) Delete(ctx context.Context, key string) error {
	t.l1.Remove(key)
	if err := t.redis.Del(ctx, t.l2Key(key)).Err(); err != nil {
		return err
	}
	payload := t.instanceID + "|" + key
	if err := t.redis.Publish(ctx, t.channel(), payload).Err(); err != nil {
		// Broadcast failure isn't fatal: peers will see the stale entry until
		// their L1 TTL expires (cfg.Cache.L1TTL — default 30s). We log it so
		// ops can spot persistent pubsub issues.
		if t.logger != nil {
			t.logger.Warn("cache invalidate broadcast failed", "ns", t.namespace, "err", err)
		}
	}
	return nil
}

// subscribe holds the pubsub connection open and applies remote invalidations.
// It self-heals on disconnect with exponential backoff capped at 30s.
func (t *Tiered[V]) subscribe(ctx context.Context) {
	defer t.pubsubWG.Done()
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		ps := t.redis.Subscribe(ctx, t.channel())
		if _, err := ps.Receive(ctx); err != nil {
			_ = ps.Close()
			if ctx.Err() != nil {
				return
			}
			if t.logger != nil {
				t.logger.Warn("cache pubsub subscribe failed", "ns", t.namespace, "err", err)
			}
			select {
			case <-time.After(backoff):
				if backoff < 30*time.Second {
					backoff *= 2
				}
			case <-ctx.Done():
				return
			}
			continue
		}
		backoff = time.Second
		ch := ps.Channel()
	inner:
		for {
			select {
			case <-ctx.Done():
				_ = ps.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					_ = ps.Close()
					break inner
				}
				sep := strings.IndexByte(msg.Payload, '|')
				if sep < 0 {
					continue
				}
				instance, key := msg.Payload[:sep], msg.Payload[sep+1:]
				// Skip our own broadcasts — Delete() already cleared L1 locally.
				if instance == t.instanceID {
					continue
				}
				t.l1.Remove(key)
			}
		}
	}
}

func (t *Tiered[V]) Close() error {
	if t.pubsubCancel != nil {
		t.pubsubCancel()
	}
	t.pubsubWG.Wait()
	return nil
}

func newInstanceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

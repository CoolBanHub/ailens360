package pricing

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// DefaultRefreshInterval is how often the refresher re-fetches from the
// upstream Source. The Redis warm copy is set with a TTL slightly longer than
// this so a process restart between refreshes still loads non-stale data.
const DefaultRefreshInterval = 12 * time.Hour

// redisKey is the single key holding the serialized price table. Versioning
// the key (`:v1`) lets us evolve PricePerMTok without poisoning peers running
// older code.
const redisKey = "pricing:models.dev:v1"

// Refresher keeps a Catalog in sync with a remote Source. Lifecycle:
//
//  1. Start(ctx) — block on one initial load attempt (Redis warm → Source).
//     If both fail, the Catalog keeps its seed prices and Start returns nil.
//  2. A background ticker re-fetches every Interval and atomically replaces
//     the catalog table on success. Fetch errors are logged, not fatal.
//  3. Stop() — cancels the ticker; safe to call multiple times.
type Refresher struct {
	Catalog  *Catalog
	Source   Source
	Redis    *redis.Client // optional warm-cache; nil disables L2
	Interval time.Duration
	Logger   *slog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Start performs the initial load synchronously and then spawns the refresh
// loop. Returns nil even when the upstream is unreachable — the seed table is
// always good enough to keep the request path serving.
func (r *Refresher) Start(parent context.Context) error {
	if r.Catalog == nil {
		r.Catalog = NewCatalog()
	}
	if r.Interval <= 0 {
		r.Interval = DefaultRefreshInterval
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}

	// Initial load. We give the network step a bounded budget so app startup
	// doesn't hang on a slow models.dev.
	initCtx, cancelInit := context.WithTimeout(parent, 30*time.Second)
	defer cancelInit()
	r.loadOnce(initCtx, true)

	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.wg.Add(1)
	go r.loop(ctx)
	return nil
}

// Stop signals the loop to exit and waits for it.
func (r *Refresher) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Refresher) loop(ctx context.Context) {
	defer r.wg.Done()
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			r.loadOnce(tickCtx, false)
			cancel()
		}
	}
}

// loadOnce tries Redis first (cheap), falls back to Source on miss. On a
// successful Source fetch the result is written back to Redis. initial=true
// only affects logging verbosity.
func (r *Refresher) loadOnce(ctx context.Context, initial bool) {
	// 1. Try Redis warm cache. Only fall back to the source when missing or
	//    decode fails — a stale-but-decodable Redis copy is preferable to a
	//    long blocking HTTP fetch on every replica startup.
	if r.Redis != nil {
		raw, err := r.Redis.Get(ctx, redisKey).Bytes()
		if err == nil && len(raw) > 0 {
			if table, perr := ParseModelsDev(raw); perr == nil && len(table) > 0 {
				r.Catalog.Replace(table)
				if initial {
					r.Logger.Info("pricing: loaded from redis warm cache", "models", len(table))
				}
				// Even on a warm-cache hit we still want to refresh from upstream
				// asynchronously so the warm copy doesn't drift indefinitely.
				// We do that on the regular ticker — not inline here — to keep
				// startup snappy.
				return
			}
		}
	}

	// 2. Fetch from upstream.
	if r.Source == nil {
		r.Logger.Warn("pricing: no source configured; using seed table")
		return
	}
	rawJSON, table, err := fetchAndKeep(ctx, r.Source)
	if err != nil {
		if initial {
			r.Logger.Warn("pricing: initial fetch failed, keeping seed table", "err", err)
		} else {
			r.Logger.Warn("pricing: refresh fetch failed", "err", err)
		}
		return
	}
	if len(table) == 0 {
		r.Logger.Warn("pricing: source returned empty table; ignoring")
		return
	}
	r.Catalog.Replace(table)
	r.Logger.Info("pricing: refreshed from upstream", "models", len(table))

	// 3. Write back to Redis with a TTL longer than the refresh interval so a
	//    cold replica that boots between refreshes can still warm from it.
	if r.Redis != nil && rawJSON != nil {
		ttl := max(r.Interval*2, 24*time.Hour)
		if err := r.Redis.Set(ctx, redisKey, rawJSON, ttl).Err(); err != nil {
			r.Logger.Warn("pricing: redis warm cache write failed", "err", err)
		}
	}
}

// fetchAndKeep calls Source.Fetch but also produces a JSON blob we can persist
// to Redis. We re-marshal the flattened table rather than caching the raw
// upstream body — Redis stays small (a few hundred KB vs multi-MB), and the
// shape is stable across upstream schema tweaks.
func fetchAndKeep(ctx context.Context, src Source) ([]byte, map[string]PricePerMTok, error) {
	table, err := src.Fetch(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Marshal as the flat shape ModelsDevSource emits — wrapped so ParseModelsDev
	// can read it back. We use a single "_cache" provider key for the wrapper.
	wrapped := map[string]modelsDevProvider{
		"_cache": {Models: make(map[string]modelsDevModel, len(table))},
	}
	for k, v := range table {
		wrapped["_cache"].Models[k] = modelsDevModel{Cost: &v}
	}
	raw, err := json.Marshal(wrapped)
	if err != nil {
		return nil, table, err
	}
	return raw, table, nil
}

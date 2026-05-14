// Package cache implements a two-tier (LRU + Redis) cache with Pub/Sub
// invalidation. It is the only cache abstraction used by the application —
// callers depend on the Cache[V] interface, not the concrete tiered impl,
// so tests can substitute an in-memory fake.
package cache

import "context"

// Cache is one logical namespaced cache. Implementations must be safe for
// concurrent use.
type Cache[V any] interface {
	Get(ctx context.Context, key string) (V, bool, error)
	Set(ctx context.Context, key string, val V) error
	// Delete removes the entry locally and from L2 (if any), and broadcasts
	// to other replicas so they drop their L1 copy. Returning nil means at
	// least the local + L2 delete succeeded; broadcast failure is logged but
	// not surfaced because L1 TTL is the safety net.
	Delete(ctx context.Context, key string) error
	Close() error
}

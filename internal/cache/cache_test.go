package cache

import (
	"context"
	"sync"
	"testing"
)

// TestCacheInterfaceShape ensures the documented Cache[V] contract is
// satisfied by a minimal in-memory impl. The Tiered (LRU + Redis + Pub/Sub)
// impl is exercised by the project-resolver tests and the E2E docker-compose
// flow — testing it here would require a live Redis or miniredis dep we'd
// rather not pull in for unit tests.
type memCache[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func (c *memCache[V]) Get(_ context.Context, key string) (V, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok, nil
}

func (c *memCache[V]) Set(_ context.Context, key string, val V) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = val
	return nil
}

func (c *memCache[V]) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}

func (c *memCache[V]) Close() error { return nil }

func TestCacheContract(t *testing.T) {
	var c Cache[string] = &memCache[string]{m: map[string]string{}}
	ctx := context.Background()

	if _, ok, err := c.Get(ctx, "k"); err != nil || ok {
		t.Fatalf("miss should be (zero, false, nil); got (_, %v, %v)", ok, err)
	}
	if err := c.Set(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || v != "v" {
		t.Fatalf("after Set: got (%q, %v, %v)", v, ok, err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Delete should remove the entry")
	}
}

package project

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

type fakeProjectRepo struct {
	p     *repo.Project
	calls int32
}

func (f *fakeProjectRepo) Create(context.Context, *repo.Project) error            { return nil }
func (f *fakeProjectRepo) GetByID(context.Context, string) (*repo.Project, error) { return f.p, nil }
func (f *fakeProjectRepo) List(context.Context) ([]*repo.Project, error)          { return nil, nil }
func (f *fakeProjectRepo) Update(context.Context, *repo.Project) error            { return nil }
func (f *fakeProjectRepo) UpdateProjectKey(context.Context, string, string) error { return nil }
func (f *fakeProjectRepo) Delete(context.Context, string) error                   { return nil }
func (f *fakeProjectRepo) GetByProjectKey(_ context.Context, key string) (*repo.Project, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.p == nil || f.p.ProjectKey != key {
		return nil, repo.ErrNotFound
	}
	return f.p, nil
}

// memCache is a minimal in-memory cache used by tests. It deliberately does
// not implement TTL or broadcast — the Tiered impl owns those concerns.
type memCache[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func newMemCache[V any]() *memCache[V] { return &memCache[V]{m: map[string]V{}} }

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

func TestResolverHitsCacheOnSecondCall(t *testing.T) {
	pr := &fakeProjectRepo{p: &repo.Project{ID: "prj_1", ProjectKey: "abc", Name: "demo"}}
	r := NewResolver(pr, newMemCache[*repo.Project]())
	for i := 0; i < 5; i++ {
		got, err := r.Resolve(context.Background(), "abc")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got.ID != "prj_1" {
			t.Fatalf("wrong project: %+v", got)
		}
	}
	if c := atomic.LoadInt32(&pr.calls); c != 1 {
		t.Fatalf("repo should be hit exactly once, got %d", c)
	}
}

func TestResolverNotFoundIsTyped(t *testing.T) {
	pr := &fakeProjectRepo{}
	r := NewResolver(pr, newMemCache[*repo.Project]())
	_, err := r.Resolve(context.Background(), "missing")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
}

func TestResolverInvalidateForcesRefetch(t *testing.T) {
	pr := &fakeProjectRepo{p: &repo.Project{ID: "prj_1", ProjectKey: "abc", Name: "old"}}
	r := NewResolver(pr, newMemCache[*repo.Project]())
	if _, err := r.Resolve(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	pr.p = &repo.Project{ID: "prj_1", ProjectKey: "abc", Name: "new"}
	if err := r.Invalidate(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Resolve(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new" {
		t.Fatalf("expected refreshed project, got %+v", got)
	}
}

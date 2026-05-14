package project

import (
	"context"
	"errors"

	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

var ErrProjectNotFound = errors.New("project not found")

// Resolver looks up project_key → *Project through a two-tier cache. The cache
// handles L1/L2/TTL/Pub-Sub broadcast; this type stays purely about the
// resolve-then-cache logic.
type Resolver struct {
	projects repo.ProjectRepo
	cache    cache.Cache[*repo.Project]
}

func NewResolver(projects repo.ProjectRepo, c cache.Cache[*repo.Project]) *Resolver {
	return &Resolver{projects: projects, cache: c}
}

func (r *Resolver) Resolve(ctx context.Context, projectKey string) (*repo.Project, error) {
	if v, ok, err := r.cache.Get(ctx, projectKey); err == nil && ok && v != nil {
		return v, nil
	}
	// Cache miss (or transient cache error) — fall through to DB.
	p, err := r.projects.GetByProjectKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	_ = r.cache.Set(ctx, projectKey, p)
	return p, nil
}

// Invalidate drops the cached entry for projectKey on this replica and
// broadcasts to peers. Callers: project update / delete handlers.
func (r *Resolver) Invalidate(ctx context.Context, projectKey string) error {
	return r.cache.Delete(ctx, projectKey)
}

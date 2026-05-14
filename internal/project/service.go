package project

import (
	"context"
	"errors"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
	"github.com/CoolBanHub/ailens360/pkg/shortid"
)

type Service struct {
	repo repo.ProjectRepo
}

func NewService(r repo.ProjectRepo) *Service {
	return &Service{repo: r}
}

type CreateInput struct {
	Name string
}

// projectKeyLen is the length of the per-project secret callers send in the
// X-AILens-Project-Key header. 64 chars of base62 ≈ 381 bits of entropy.
const projectKeyLen = 64

func (s *Service) Create(ctx context.Context, in CreateInput) (*repo.Project, error) {
	if in.Name == "" {
		return nil, errors.New("name required")
	}
	id, err := shortid.New(16)
	if err != nil {
		return nil, err
	}
	key, err := s.allocProjectKey(ctx)
	if err != nil {
		return nil, err
	}
	p := &repo.Project{
		ID:         "prj_" + id,
		ProjectKey: key,
		Name:       in.Name,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) allocProjectKey(ctx context.Context) (string, error) {
	for i := 0; i < 10; i++ {
		key, err := shortid.New(projectKeyLen)
		if err != nil {
			return "", err
		}
		_, err = s.repo.GetByProjectKey(ctx, key)
		if errors.Is(err, repo.ErrNotFound) {
			return key, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("failed to allocate unique project key")
}

// ResetProjectKey rotates the project's project_key to a freshly generated value.
// Returns the project with both the old and new keys so callers can invalidate
// caches keyed on the old key before serving traffic against the new one.
func (s *Service) ResetProjectKey(ctx context.Context, id string) (p *repo.Project, oldKey string, err error) {
	p, err = s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	oldKey = p.ProjectKey
	key, err := s.allocProjectKey(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.UpdateProjectKey(ctx, p.ID, key); err != nil {
		return nil, "", err
	}
	p.ProjectKey = key
	return p, oldKey, nil
}

func (s *Service) Get(ctx context.Context, id string) (*repo.Project, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*repo.Project, error) {
	return s.repo.List(ctx)
}

type UpdateInput struct {
	ID   string
	Name string
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*repo.Project, error) {
	p, err := s.repo.GetByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		p.Name = in.Name
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

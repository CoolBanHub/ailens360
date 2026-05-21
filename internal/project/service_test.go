package project

import (
	"context"
	"strings"
	"testing"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

func TestServiceCreatesProjectKeyWithSKPrefix(t *testing.T) {
	store := &serviceProjectRepo{}
	svc := NewService(store)

	p, err := svc.Create(context.Background(), CreateInput{Name: "demo"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if !strings.HasPrefix(p.ProjectKey, projectKeyPrefix) {
		t.Fatalf("project key = %q, want prefix %q", p.ProjectKey, projectKeyPrefix)
	}
	if len(p.ProjectKey) != len(projectKeyPrefix)+projectKeyLen {
		t.Fatalf("project key length = %d, want %d", len(p.ProjectKey), len(projectKeyPrefix)+projectKeyLen)
	}
}

func TestServiceResetProjectKeyUsesSKPrefix(t *testing.T) {
	store := &serviceProjectRepo{
		byID: map[string]*repo.Project{
			"prj_1": {ID: "prj_1", ProjectKey: "sk-old", Name: "demo"},
		},
	}
	svc := NewService(store)

	p, oldKey, err := svc.ResetProjectKey(context.Background(), "prj_1")
	if err != nil {
		t.Fatalf("ResetProjectKey returned error: %v", err)
	}
	if oldKey != "sk-old" {
		t.Fatalf("oldKey = %q, want %q", oldKey, "sk-old")
	}
	if !strings.HasPrefix(p.ProjectKey, projectKeyPrefix) {
		t.Fatalf("project key = %q, want prefix %q", p.ProjectKey, projectKeyPrefix)
	}
	if p.ProjectKey == oldKey {
		t.Fatal("project key did not rotate")
	}
}

type serviceProjectRepo struct {
	byID map[string]*repo.Project
}

func (r *serviceProjectRepo) Create(_ context.Context, p *repo.Project) error {
	if r.byID == nil {
		r.byID = make(map[string]*repo.Project)
	}
	r.byID[p.ID] = p
	return nil
}

func (r *serviceProjectRepo) GetByID(_ context.Context, id string) (*repo.Project, error) {
	if p, ok := r.byID[id]; ok {
		return p, nil
	}
	return nil, repo.ErrNotFound
}

func (r *serviceProjectRepo) List(context.Context) ([]*repo.Project, error) {
	return nil, nil
}

func (r *serviceProjectRepo) Update(context.Context, *repo.Project) error {
	return nil
}

func (r *serviceProjectRepo) UpdateProjectKey(_ context.Context, id, projectKey string) error {
	p, ok := r.byID[id]
	if !ok {
		return repo.ErrNotFound
	}
	p.ProjectKey = projectKey
	return nil
}

func (r *serviceProjectRepo) Delete(context.Context, string) error {
	return nil
}

func (r *serviceProjectRepo) GetByProjectKey(_ context.Context, key string) (*repo.Project, error) {
	for _, p := range r.byID {
		if p.ProjectKey == key {
			return p, nil
		}
	}
	return nil, repo.ErrNotFound
}

package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

type ProjectRepo struct{ pool *pgxpool.Pool }

func NewProjectRepo(pool *pgxpool.Pool) *ProjectRepo { return &ProjectRepo{pool: pool} }

func (r *ProjectRepo) Create(ctx context.Context, p *repo.Project) error {
	now := time.Now().Unix()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Unix(now, 0)
	}
	p.UpdatedAt = time.Unix(now, 0)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO projects(id, project_key, name, created_at, updated_at) VALUES($1,$2,$3,$4,$5)`,
		p.ID, p.ProjectKey, p.Name, p.CreatedAt.Unix(), p.UpdatedAt.Unix())
	return err
}

func (r *ProjectRepo) GetByID(ctx context.Context, id string) (*repo.Project, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, project_key, name, created_at, updated_at FROM projects WHERE id=$1`, id)
	return scanProject(row)
}

func (r *ProjectRepo) GetByProjectKey(ctx context.Context, key string) (*repo.Project, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, project_key, name, created_at, updated_at FROM projects WHERE project_key=$1`, key)
	return scanProject(row)
}

func (r *ProjectRepo) List(ctx context.Context) ([]*repo.Project, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, project_key, name, created_at, updated_at FROM projects ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*repo.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *ProjectRepo) Update(ctx context.Context, p *repo.Project) error {
	p.UpdatedAt = time.Now()
	_, err := r.pool.Exec(ctx,
		`UPDATE projects SET name=$1, updated_at=$2 WHERE id=$3`,
		p.Name, p.UpdatedAt.Unix(), p.ID)
	return err
}

func (r *ProjectRepo) UpdateProjectKey(ctx context.Context, id, projectKey string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE projects SET project_key=$1, updated_at=$2 WHERE id=$3`,
		projectKey, time.Now().Unix(), id)
	return err
}

func (r *ProjectRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(s rowScanner) (*repo.Project, error) {
	var p repo.Project
	var created, updated int64
	if err := s.Scan(&p.ID, &p.ProjectKey, &p.Name, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	p.CreatedAt = time.Unix(created, 0)
	p.UpdatedAt = time.Unix(updated, 0)
	return &p, nil
}

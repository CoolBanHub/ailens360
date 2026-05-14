// Package postgres is the production storage backend. It is the only DB
// driver the application supports — SQLite was retired when the project
// moved to a distributed-ready baseline (multi-replica, shared Postgres,
// shared Redis).
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open returns a configured pgxpool. The caller owns Close().
func Open(ctx context.Context, dsn string, maxConns, maxIdle int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	if maxIdle > 0 {
		cfg.MinConns = int32(maxIdle)
	}
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// Migrate applies embedded *.up.sql files in lexical order, tracking applied
// versions in `schema_migrations`. Idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	var ups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)
	for _, name := range ups {
		version := strings.TrimSuffix(name, ".up.sql")
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(1) FROM schema_migrations WHERE version=$1`, version,
		).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		raw, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(raw)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES($1, EXTRACT(EPOCH FROM NOW())::BIGINT)`, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

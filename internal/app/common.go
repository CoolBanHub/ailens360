// Package app composes the three deployable processes (proxy, collector, api)
// out of the shared internal packages. Each `Build*` function returns a small
// struct exposing Run/Shutdown — cmd/ailens360 wires signal handling around it.
//
// Shared dependency constructors live here so the per-role builders read like
// recipes, not plumbing.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/storage/postgres"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// openPGPool establishes the pgxpool. Migrations are NOT applied here — the
// collector process owns migrations.
func openPGPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	return postgres.Open(ctx, cfg.DB.DSN, cfg.DB.MaxConns, cfg.DB.MaxIdleConns)
}

func openRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	return cache.NewRedisClient(ctx, cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
}

func newProjectCache(rdb *redis.Client, cfg *config.Config, log *slog.Logger) (cache.Cache[*repo.Project], error) {
	return cache.NewTiered[*repo.Project](
		"project", rdb, cfg.Cache.L1Cap, cfg.Cache.L1TTL, cfg.Cache.L2TTL, log,
	)
}

func newBodyStore(cfg *config.Config) (bodystore.Store, error) {
	return bodystore.New(bodystore.Config{
		Endpoint:        cfg.BodyStore.Endpoint,
		Bucket:          cfg.BodyStore.Bucket,
		Region:          cfg.BodyStore.Region,
		AccessKeyID:     cfg.BodyStore.AccessKeyID,
		SecretAccessKey: cfg.BodyStore.SecretAccessKey,
		UseSSL:          cfg.BodyStore.UseSSL,
		PathStyle:       cfg.BodyStore.PathStyle,
		PartSize:        cfg.BodyStore.PartSize,
		GzipBodies:      cfg.BodyStore.GzipBodies,
		UploadTimeout:   cfg.BodyStore.UploadTimeout,
		PresignTTL:      cfg.BodyStore.PresignTTL,
		PublicEndpoint:  cfg.BodyStore.PublicEndpoint,
	})
}

// runHTTPServer wraps the boilerplate of ListenAndServe + ErrServerClosed
// recognition. Callers get a single error channel to select on.
func runHTTPServer(srv *http.Server, logger *slog.Logger, name string) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "name", name, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("%s server: %w", name, err)
			return
		}
		errCh <- nil
	}()
	return errCh
}

// healthzHandler is the canonical /healthz endpoint. We deliberately do NOT
// touch downstream dependencies — a passing /healthz means "this process is
// alive and listening." Liveness vs readiness is a separate concern that can
// be added per-role when we have a need for it.
func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

// shutdownHTTP attempts a graceful shutdown bounded by a small timeout. Any
// error is logged at the caller.
func shutdownHTTP(srv *http.Server, timeout time.Duration) error {
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return srv.Shutdown(ctx)
}

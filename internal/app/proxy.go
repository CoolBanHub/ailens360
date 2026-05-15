package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/api/middleware"
	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/logger"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/proxy"
	"github.com/CoolBanHub/ailens360/internal/storage/postgres"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// ProxyApp is the LLM-facing reverse proxy process. It speaks HTTP to clients
// and to upstreams, uploads bodies to the body store, and XADDs minimal trace
// events to a Redis Stream for the collector to consume. It does not touch
// the traces table at all.
type ProxyApp struct {
	Cfg          *config.Config
	Logger       *slog.Logger
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	ProjectCache cache.Cache[*repo.Project]
	BodyStore    bodystore.Store
	HTTPSrv      *http.Server
}

func BuildProxy(ctx context.Context, cfg *config.Config) (*ProxyApp, error) {
	log := logger.New(cfg.Log)

	pool, err := openPGPool(ctx, cfg)
	if err != nil {
		return nil, err
	}
	rdb, err := openRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	projectCache, err := newProjectCache(rdb, cfg, log)
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}
	store, err := newBodyStore(cfg)
	if err != nil {
		_ = projectCache.Close()
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}
	if err := store.EnsureBucket(ctx); err != nil {
		log.Warn("body store bucket ensure failed; continuing", "err", err)
	}

	projectRepo := postgres.NewProjectRepo(pool)
	resolver := project.NewResolver(projectRepo, projectCache)
	sink := proxy.NewStreamSink(rdb, cfg.Stream.Key, log)

	handler := proxy.NewHandler(proxy.Deps{
		Logger:    log,
		Resolver:  resolver,
		Sink:      sink,
		BodyStore: store,
		RawLimit:  cfg.Proxy.RawBodyLimit,
		MaxBody:   cfg.Proxy.MaxRequestBody,
		Timeout:   cfg.Proxy.UpstreamTimeout,
	})

	r := chi.NewRouter()
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logging(log))
	r.Get("/healthz", healthzHandler)
	handler.Mount(r)

	srv := &http.Server{
		Addr:         cfg.Proxy.Addr,
		Handler:      r,
		ReadTimeout:  cfg.Proxy.ReadTimeout,
		WriteTimeout: cfg.Proxy.WriteTimeout,
		IdleTimeout:  cfg.Proxy.IdleTimeout,
	}

	return &ProxyApp{
		Cfg:          cfg,
		Logger:       log,
		Pool:         pool,
		Redis:        rdb,
		ProjectCache: projectCache,
		BodyStore:    store,
		HTTPSrv:      srv,
	}, nil
}

func (a *ProxyApp) Run() <-chan error {
	return runHTTPServer(a.HTTPSrv, a.Logger, "proxy")
}

// Shutdown drains in-flight requests, then closes shared resources.
func (a *ProxyApp) Shutdown(_ context.Context) {
	a.Logger.Info("proxy shutting down")
	if err := shutdownHTTP(a.HTTPSrv, 10*time.Second); err != nil {
		a.Logger.Error("http shutdown", "err", err)
	}
	if a.ProjectCache != nil {
		_ = a.ProjectCache.Close()
	}
	if a.Redis != nil {
		if err := a.Redis.Close(); err != nil {
			a.Logger.Error("redis close", "err", err)
		}
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}

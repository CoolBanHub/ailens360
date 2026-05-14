package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/api"
	"github.com/CoolBanHub/ailens360/internal/api/handler"
	"github.com/CoolBanHub/ailens360/internal/api/middleware"
	"github.com/CoolBanHub/ailens360/internal/auth"
	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/collector"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/logger"
	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/proxy"
	"github.com/CoolBanHub/ailens360/internal/storage/postgres"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

type App struct {
	Cfg              *config.Config
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	Redis            *redis.Client
	ProjectCache     cache.Cache[*repo.Project]
	Pipeline         *collector.Pipeline
	PricingRefresher *pricing.Refresher
	HTTPSrv          *http.Server
}

func Build(ctx context.Context, cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Log)

	pool, err := postgres.Open(ctx, cfg.DB.DSN, cfg.DB.MaxConns, cfg.DB.MaxIdleConns)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	rdb, err := cache.NewRedisClient(ctx, cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		pool.Close()
		return nil, err
	}

	projectCache, err := cache.NewTiered[*repo.Project](
		"project", rdb, cfg.Cache.L1Cap, cfg.Cache.L1TTL, cfg.Cache.L2TTL, log,
	)
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}

	projectRepo := postgres.NewProjectRepo(pool)
	traceRepo := postgres.NewTraceRepo(pool)

	projectSvc := project.NewService(projectRepo)
	resolver := project.NewResolver(projectRepo, projectCache)
	priceCatalog := pricing.NewCatalog()
	var priceRefresher *pricing.Refresher
	if !cfg.Pricing.Disable {
		priceRefresher = &pricing.Refresher{
			Catalog:  priceCatalog,
			Source:   pricing.NewModelsDevSource(cfg.Pricing.SourceURL),
			Redis:    rdb,
			Interval: cfg.Pricing.RefreshInterval,
			Logger:   log,
		}
		if err := priceRefresher.Start(ctx); err != nil {
			log.Warn("pricing refresher start failed", "err", err)
		}
	}
	tokenEst := tokenizer.New()
	authSvc, err := auth.New(cfg.Auth.Username, cfg.Auth.Password, cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	if err != nil {
		_ = projectCache.Close()
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}
	realtime := metrics.New(rdb, cfg.Metrics.WindowSecs, cfg.Metrics.RetentionSecs, log)

	pipeline := collector.New(collector.Config{
		BufferSize:    cfg.Collector.BufferSize,
		BatchSize:     cfg.Collector.BatchSize,
		FlushInterval: cfg.Collector.FlushInterval,
	}, log, traceRepo, priceCatalog, tokenEst, realtime)
	pipeline.Start(ctx)

	proxyHandler := proxy.NewHandler(proxy.Deps{
		Logger:   log,
		Resolver: resolver,
		Sink:     pipeline,
		RawLimit: cfg.Collector.RawBodyLimit,
		MaxBody:  cfg.Proxy.MaxRequestBody,
		Timeout:  cfg.Proxy.UpstreamTimeout,
	})

	handlers := &handler.Handlers{
		Projects:  projectSvc,
		Resolver:  resolver,
		Traces:    traceRepo,
		Auth:      authSvc,
		Realtime:  realtime,
		PublicURL: cfg.Proxy.PublicURL,
	}

	r := chi.NewRouter()
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logging(log))
	proxyHandler.Mount(r)
	api.Mount(r, api.RouterDeps{
		Handlers:    handlers,
		Auth:        authSvc,
		CORSOrigins: cfg.API.CORSOrigins,
	})
	if uiDir := resolveUIDir(cfg.UI.Dir); uiDir != "" {
		log.Info("web ui enabled", "dir", uiDir)
		ui := newSPAHandler(uiDir)
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			ui.ServeHTTP(w, r)
		})
		r.Head("/*", func(w http.ResponseWriter, r *http.Request) {
			ui.ServeHTTP(w, r)
		})
	} else {
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("AILens360 server. UI build not found. Run frontend build or set AILENS360_UI_DIR."))
		})
	}

	srv := &http.Server{
		Addr:         cfg.Proxy.Addr,
		Handler:      r,
		ReadTimeout:  cfg.Proxy.ReadTimeout,
		WriteTimeout: cfg.Proxy.WriteTimeout,
		IdleTimeout:  cfg.Proxy.IdleTimeout,
	}

	return &App{
		Cfg:              cfg,
		Logger:           log,
		Pool:             pool,
		Redis:            rdb,
		ProjectCache:     projectCache,
		Pipeline:         pipeline,
		PricingRefresher: priceRefresher,
		HTTPSrv:          srv,
	}, nil
}

func (a *App) Run() error {
	a.Logger.Info("http server starting", "addr", a.HTTPSrv.Addr)
	if err := a.HTTPSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown drains in-flight requests, then the collector batch, then closes
// pubsub/Redis/DB in that order — so a SIGTERM does not lose buffered events
// or leave dangling Redis subscriptions.
func (a *App) Shutdown(ctx context.Context) {
	a.Logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := a.HTTPSrv.Shutdown(shutdownCtx); err != nil {
		a.Logger.Error("http shutdown", "err", err)
	}
	a.Pipeline.Stop()
	if a.PricingRefresher != nil {
		a.PricingRefresher.Stop()
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

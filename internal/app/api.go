package app

import (
	"context"
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
	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/logger"
	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/storage/postgres"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// APIApp serves the REST control plane and the built UI. It is the only
// process that runs the upstream pricing refresher (models.dev → Redis); the
// collector consumes that warm copy. The API process is read-only against
// traces and read/write against projects.
type APIApp struct {
	Cfg              *config.Config
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	Redis            *redis.Client
	ProjectCache     cache.Cache[*repo.Project]
	PricingRefresher *pricing.Refresher
	BodyStore        bodystore.Store
	HTTPSrv          *http.Server
}

func BuildAPI(ctx context.Context, cfg *config.Config) (*APIApp, error) {
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

	projectRepo := postgres.NewProjectRepo(pool)
	traceRepo := postgres.NewTraceRepo(pool)
	projectSvc := project.NewService(projectRepo)
	resolver := project.NewResolver(projectRepo, projectCache)
	realtime := metrics.New(rdb, cfg.Metrics.WindowSecs, cfg.Metrics.RetentionSecs, log)

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

	authSvc, err := auth.New(cfg.Auth.Username, cfg.Auth.Password, cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	if err != nil {
		if priceRefresher != nil {
			priceRefresher.Stop()
		}
		_ = projectCache.Close()
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}

	handlers := &handler.Handlers{
		Projects:        projectSvc,
		Resolver:        resolver,
		Traces:          traceRepo,
		Auth:            authSvc,
		Realtime:        realtime,
		BodyStore:       store,
		PublicURL:       cfg.ProxyPublicURL(),
		ProxyAddr:       cfg.Proxy.Addr,
		PresignRedirect: cfg.BodyStore.PresignRedirect,
	}

	r := chi.NewRouter()
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logging(log))

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
			_, _ = w.Write([]byte("AILens360 api. UI build not found. Run frontend build or set AILENS360_UI_DIR."))
		})
	}

	srv := &http.Server{
		Addr:         cfg.API.Addr,
		Handler:      r,
		ReadTimeout:  cfg.Proxy.ReadTimeout,
		WriteTimeout: cfg.Proxy.WriteTimeout,
		IdleTimeout:  cfg.Proxy.IdleTimeout,
	}

	return &APIApp{
		Cfg:              cfg,
		Logger:           log,
		Pool:             pool,
		Redis:            rdb,
		ProjectCache:     projectCache,
		PricingRefresher: priceRefresher,
		BodyStore:        store,
		HTTPSrv:          srv,
	}, nil
}

func (a *APIApp) Run() <-chan error {
	return runHTTPServer(a.HTTPSrv, a.Logger, "api")
}

func (a *APIApp) Shutdown(_ context.Context) {
	a.Logger.Info("api shutting down")
	if err := shutdownHTTP(a.HTTPSrv, 10*time.Second); err != nil {
		a.Logger.Error("http shutdown", "err", err)
	}
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

package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/CoolBanHub/ailens360/internal/collector"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/logger"
	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/partition"
	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/storage/postgres"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

// CollectorApp drains the Redis Stream into Postgres, updates realtime metrics,
// and runs the partition maintainer. It is the only process that writes to
// the `traces` table — and is therefore also the canonical owner of schema
// migrations.
type CollectorApp struct {
	Cfg              *config.Config
	Logger           *slog.Logger
	Pool             *pgxpool.Pool
	Redis            *redis.Client
	Consumer         *collector.Consumer
	PartitionMaint   *partition.Maintainer
	PricingRefresher *pricing.Refresher
	HealthSrv        *http.Server
}

func BuildCollector(ctx context.Context, cfg *config.Config) (*CollectorApp, error) {
	log := logger.New(cfg.Log)

	pool, err := openPGPool(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	rdb, err := openRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}

	traceRepo := postgres.NewTraceRepo(pool)
	projectRepo := postgres.NewProjectRepo(pool)
	realtime := metrics.New(rdb, cfg.Metrics.WindowSecs, cfg.Metrics.RetentionSecs, log)

	// BodyStore is wired into the partition maintainer so it can purge MinIO
	// objects in lockstep with dropping old `traces_YYYYMM` partitions.
	bodyStore, err := newBodyStore(cfg)
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, fmt.Errorf("body store: %w", err)
	}

	priceCatalog := pricing.NewCatalog()
	// Collector reads pricing from the Redis warm cache populated by the api
	// process — Source=nil means "never hit models.dev from here".
	priceRefresher := &pricing.Refresher{
		Catalog:  priceCatalog,
		Source:   nil,
		Redis:    rdb,
		Interval: cfg.Pricing.RefreshInterval,
		Logger:   log,
	}
	if err := priceRefresher.Start(ctx); err != nil {
		log.Warn("pricing refresher start failed", "err", err)
	}

	transformer := collector.NewTransformer(log, priceCatalog, tokenizer.New())

	partMaint := partition.New(pool, partition.Config{
		PreCreate:       cfg.Partition.PreCreate,
		RetentionMonths: cfg.Partition.RetentionMonths,
		CheckInterval:   cfg.Partition.CheckInterval,
	}, log, projectRepo, bodyStore)
	if err := partMaint.Start(ctx); err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, fmt.Errorf("partition maintainer: %w", err)
	}

	consumer := collector.NewConsumer(collector.Config{
		StreamKey:      cfg.Stream.Key,
		ConsumerGroup:  cfg.Stream.ConsumerGroup,
		BlockTimeout:   cfg.Stream.BlockTimeout,
		BatchSize:      cfg.Stream.BatchSize,
		PendingIdleMax: cfg.Stream.PendingIdleMax,
		ClaimInterval:  cfg.Stream.ClaimInterval,
	}, rdb, log, transformer, traceRepo, realtime)

	if err := consumer.Start(ctx); err != nil {
		partMaint.Stop()
		_ = rdb.Close()
		pool.Close()
		return nil, fmt.Errorf("consumer: %w", err)
	}

	r := chi.NewRouter()
	r.Get("/healthz", healthzHandler)
	healthSrv := &http.Server{Addr: cfg.Collector.HealthAddr, Handler: r}

	return &CollectorApp{
		Cfg:              cfg,
		Logger:           log,
		Pool:             pool,
		Redis:            rdb,
		Consumer:         consumer,
		PartitionMaint:   partMaint,
		PricingRefresher: priceRefresher,
		HealthSrv:        healthSrv,
	}, nil
}

func (a *CollectorApp) Run() <-chan error {
	return runHTTPServer(a.HealthSrv, a.Logger, "collector-health")
}

// Shutdown stops the consumer (drains in-flight batches), then dependencies.
func (a *CollectorApp) Shutdown(_ context.Context) {
	a.Logger.Info("collector shutting down")
	if err := shutdownHTTP(a.HealthSrv, 5*time.Second); err != nil {
		a.Logger.Error("collector health shutdown", "err", err)
	}
	if a.Consumer != nil {
		a.Consumer.Stop()
	}
	if a.PartitionMaint != nil {
		a.PartitionMaint.Stop()
	}
	if a.PricingRefresher != nil {
		a.PricingRefresher.Stop()
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

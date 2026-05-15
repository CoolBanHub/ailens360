package config

import (
	"errors"
	"time"
)

type Config struct {
	// PublicURL is the shared default origin used when role-specific overrides
	// are empty. Typical use: behind a single reverse-proxy domain. See
	// ProxyPublicURL() for the resolution chain.
	PublicURL string          `yaml:"public_url"`
	Proxy     ProxyConfig     `yaml:"proxy"`
	API       APIConfig       `yaml:"api"`
	UI        UIConfig        `yaml:"ui"`
	DB        DBConfig        `yaml:"db"`
	Cache     CacheConfig     `yaml:"cache"`
	Redis     RedisConfig     `yaml:"redis"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Log       LogConfig       `yaml:"log"`
	Collector CollectorConfig `yaml:"collector"`
	Auth      AuthConfig      `yaml:"auth"`
	Pricing   PricingConfig   `yaml:"pricing"`
	BodyStore BodyStoreConfig `yaml:"body_store"`
	Stream    StreamConfig    `yaml:"stream"`
	Partition PartitionConfig `yaml:"partition"`
}

// ProxyPublicURL returns the externally reachable origin of the proxy listener.
// Resolution order: Proxy.PublicURL → PublicURL → "". An empty result tells the
// api process to derive from the inbound request — fine for dev, never for prod.
func (c *Config) ProxyPublicURL() string {
	if c.Proxy.PublicURL != "" {
		return c.Proxy.PublicURL
	}
	return c.PublicURL
}

// PricingConfig controls the models.dev price-catalog refresher. The seed
// table in code is used when SourceURL is empty or unreachable; otherwise
// prices are refetched every RefreshInterval and warm-cached in Redis.
type PricingConfig struct {
	SourceURL       string        `yaml:"source_url"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	// Disable disables remote refresh entirely (tests / air-gapped).
	Disable bool `yaml:"disable"`
}

type AuthConfig struct {
	Username  string        `yaml:"username"`
	Password  string        `yaml:"password"`
	JWTSecret string        `yaml:"jwt_secret"`
	TokenTTL  time.Duration `yaml:"token_ttl"`
}

type ProxyConfig struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	MaxRequestBody  int64         `yaml:"max_request_body"`
	UpstreamTimeout time.Duration `yaml:"upstream_timeout"`
	// RawBodyLimit caps the in-memory snapshot of request bodies and the parser
	// preview of response bodies. Streamed responses still get fully forwarded to
	// the client and uploaded to the body store — this only bounds RAM usage on
	// the proxy hot path.
	RawBodyLimit int `yaml:"raw_body_limit"`
	// PublicURL is the externally reachable origin used to build the
	// proxy_prefix returned to clients (e.g. "https://proxy.example.com").
	// When empty, the prefix is derived from the inbound request host.
	// The proxy URL form is `<public_url>/<scheme>://<upstream>` — no `/p`
	// prefix.
	PublicURL string `yaml:"public_url"`
}

type APIConfig struct {
	Addr        string   `yaml:"addr"`
	CORSOrigins []string `yaml:"cors_origins"`
}

type UIConfig struct {
	// Dir points to a built frontend/dist directory to be served by the Go
	// process. When empty, the app falls back to well-known locations such as
	// /app/ui (Docker image) and frontend/dist (repo root).
	Dir string `yaml:"dir"`
}

// DBConfig holds the Postgres connection parameters. Only Postgres is supported;
// the single-machine SQLite path was removed when the project moved to a
// distributed-ready baseline.
type DBConfig struct {
	DSN          string `yaml:"dsn"`
	MaxConns     int    `yaml:"max_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// RedisConfig is the shared Redis client used by Cache (L2) + Metrics + Pub/Sub
// + Stream IPC between proxy and collector.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// CacheConfig controls the two-tier cache. L1 is per-process LRU; L2 is Redis.
type CacheConfig struct {
	// L1 capacity (entries). 0 → default 10k.
	L1Cap int `yaml:"l1_cap"`
	// L1 TTL — also the fallback if Pub/Sub invalidation messages are missed.
	L1TTL time.Duration `yaml:"l1_ttl"`
	// L2 TTL — Redis SET EX expiry.
	L2TTL time.Duration `yaml:"l2_ttl"`
}

type MetricsConfig struct {
	// Realtime window in seconds (number of 1s buckets summed for "live" QPS/cost).
	WindowSecs int `yaml:"window_secs"`
	// Bucket retention — keys auto-expire after this many seconds.
	RetentionSecs int `yaml:"retention_secs"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// CollectorConfig only carries collector-process-specific settings now. Buffer
// / batch / flush settings moved to StreamConfig because the consumer pulls
// from Redis Stream, not an in-memory channel.
type CollectorConfig struct {
	// HealthAddr is the listen address for /healthz on the collector process.
	HealthAddr string `yaml:"health_addr"`
}

// BodyStoreConfig configures the S3-compatible object store (MinIO or AWS S3)
// used to persist request / response bodies.
type BodyStoreConfig struct {
	Endpoint        string `yaml:"endpoint"`
	Bucket          string `yaml:"bucket"`
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	UseSSL          bool   `yaml:"use_ssl"`
	// PathStyle forces path-style addressing (bucket.example.com/key vs
	// example.com/bucket/key). MinIO requires path-style.
	PathStyle bool `yaml:"path_style"`
	// PartSize is the multipart upload part size in bytes. Minimum 5 MiB per S3 spec.
	PartSize int64 `yaml:"part_size"`
	// GzipBodies enables gzip compression before upload (with Content-Encoding: gzip).
	GzipBodies bool `yaml:"gzip_bodies"`
	// UploadTimeout caps a single upload (PUT or multipart) end-to-end.
	UploadTimeout time.Duration `yaml:"upload_timeout"`

	// PresignRedirect chooses how the api hands trace bodies to the browser.
	//   false (default): API streams bytes from MinIO through itself. MinIO can
	//                    stay private; deployment is single-host. Burns api
	//                    bandwidth.
	//   true:            API responds with 302 to a presigned MinIO URL. Browser
	//                    fetches direct. MinIO must be browser-reachable and
	//                    CORS-configured; the api is fully bypassed.
	PresignRedirect bool `yaml:"presign_redirect"`
	// PresignTTL is the TTL of presigned GET URLs — only relevant when
	// PresignRedirect is true.
	PresignTTL time.Duration `yaml:"presign_ttl"`
	// PublicEndpoint, when set, overrides Endpoint when generating presigned
	// URLs — useful when MinIO is reachable from the browser via a different
	// hostname than from the server (e.g. inside docker compose). Only used in
	// PresignRedirect mode.
	PublicEndpoint string `yaml:"public_endpoint"`
}

// StreamConfig configures the Redis Stream that decouples proxy from collector.
// Proxy XADDs each finalized trace; one or more collector instances consume via
// XREADGROUP. The group must be created before consumption — collector handles
// this with XGROUP CREATE MKSTREAM on startup.
type StreamConfig struct {
	Key            string        `yaml:"key"`
	ConsumerGroup  string        `yaml:"consumer_group"`
	BlockTimeout   time.Duration `yaml:"block_timeout"`
	BatchSize      int           `yaml:"batch_size"`
	PendingIdleMax time.Duration `yaml:"pending_idle_max"`
	// ClaimInterval is how often the reclaimer scans XPENDING for stuck messages.
	ClaimInterval time.Duration `yaml:"claim_interval"`
}

// PartitionConfig governs the Go-side maintainer that automates declarative
// monthly partitions on the `traces` table. Interval is fixed to "month" for
// now; only PreCreate and RetentionMonths are tunable.
type PartitionConfig struct {
	// PreCreate is the number of future months to ensure exist (in addition to
	// the current month). 1 means "create next month".
	PreCreate int `yaml:"pre_create"`
	// RetentionMonths > 0 detaches partitions older than N months. 0 keeps all.
	// Detach (not drop) — operators can archive the detached table separately.
	RetentionMonths int `yaml:"retention_months"`
	// CheckInterval is how often the maintainer wakes up to ensure partitions.
	CheckInterval time.Duration `yaml:"check_interval"`
}

func (c *Config) Defaults() {
	if c.Proxy.Addr == "" {
		c.Proxy.Addr = "0.0.0.0:8080"
	}
	if c.Proxy.ReadTimeout == 0 {
		c.Proxy.ReadTimeout = 60 * time.Second
	}
	if c.Proxy.WriteTimeout == 0 {
		c.Proxy.WriteTimeout = 0
	}
	if c.Proxy.IdleTimeout == 0 {
		c.Proxy.IdleTimeout = 120 * time.Second
	}
	if c.Proxy.MaxRequestBody == 0 {
		c.Proxy.MaxRequestBody = 32 << 20
	}
	if c.Proxy.UpstreamTimeout == 0 {
		c.Proxy.UpstreamTimeout = 5 * time.Minute
	}
	if c.Proxy.RawBodyLimit == 0 {
		c.Proxy.RawBodyLimit = 8 << 20
	}
	if c.API.Addr == "" {
		c.API.Addr = "0.0.0.0:8081"
	}
	if c.Collector.HealthAddr == "" {
		c.Collector.HealthAddr = "0.0.0.0:8082"
	}
	if c.DB.MaxConns == 0 {
		c.DB.MaxConns = 20
	}
	if c.DB.MaxIdleConns == 0 {
		c.DB.MaxIdleConns = 5
	}
	if c.Cache.L1Cap == 0 {
		c.Cache.L1Cap = 10000
	}
	if c.Cache.L1TTL == 0 {
		c.Cache.L1TTL = 30 * time.Second
	}
	if c.Cache.L2TTL == 0 {
		c.Cache.L2TTL = 10 * time.Minute
	}
	if c.Metrics.WindowSecs == 0 {
		c.Metrics.WindowSecs = 60
	}
	if c.Metrics.RetentionSecs == 0 {
		c.Metrics.RetentionSecs = 7200
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	if c.Auth.Username == "" {
		c.Auth.Username = "admin"
	}
	if c.Auth.Password == "" {
		c.Auth.Password = "admin"
	}
	if c.Auth.TokenTTL == 0 {
		c.Auth.TokenTTL = 7 * 24 * time.Hour
	}
	if c.Pricing.SourceURL == "" {
		c.Pricing.SourceURL = "https://models.dev/api.json"
	}
	if c.Pricing.RefreshInterval == 0 {
		c.Pricing.RefreshInterval = 12 * time.Hour
	}
	if c.BodyStore.Bucket == "" {
		c.BodyStore.Bucket = "ailens360-traces"
	}
	if c.BodyStore.Region == "" {
		c.BodyStore.Region = "us-east-1"
	}
	if c.BodyStore.PartSize == 0 {
		c.BodyStore.PartSize = 5 << 20
	}
	if c.BodyStore.UploadTimeout == 0 {
		c.BodyStore.UploadTimeout = 30 * time.Second
	}
	if c.BodyStore.PresignTTL == 0 {
		c.BodyStore.PresignTTL = 15 * time.Minute
	}
	if c.Stream.Key == "" {
		c.Stream.Key = "ailens360:traces"
	}
	if c.Stream.ConsumerGroup == "" {
		c.Stream.ConsumerGroup = "collector"
	}
	if c.Stream.BlockTimeout == 0 {
		c.Stream.BlockTimeout = 5 * time.Second
	}
	if c.Stream.BatchSize == 0 {
		c.Stream.BatchSize = 200
	}
	if c.Stream.PendingIdleMax == 0 {
		c.Stream.PendingIdleMax = 60 * time.Second
	}
	if c.Stream.ClaimInterval == 0 {
		c.Stream.ClaimInterval = 30 * time.Second
	}
	if c.Partition.PreCreate == 0 {
		c.Partition.PreCreate = 1
	}
	if c.Partition.CheckInterval == 0 {
		c.Partition.CheckInterval = 24 * time.Hour
	}
}

// Role names a process role for role-aware validation. Each binary subcommand
// validates only the config sections it actually uses.
type Role string

const (
	RoleProxy     Role = "proxy"
	RoleCollector Role = "collector"
	RoleAPI       Role = "api"
)

// Validate enforces the constraints required for the given process role. Shared
// dependencies (DB, Redis, body store) are checked everywhere; role-specific
// fields are checked where they matter.
func (c *Config) Validate(role Role) error {
	if c.DB.DSN == "" {
		return errors.New("db.dsn is required (Postgres connection string)")
	}
	if c.Redis.Addr == "" {
		return errors.New("redis.addr is required")
	}
	if c.BodyStore.Endpoint == "" {
		return errors.New("body_store.endpoint is required (e.g. minio:9000)")
	}
	if c.BodyStore.Bucket == "" {
		return errors.New("body_store.bucket is required")
	}
	if c.BodyStore.AccessKeyID == "" || c.BodyStore.SecretAccessKey == "" {
		return errors.New("body_store.access_key_id and secret_access_key are required")
	}
	if c.BodyStore.PartSize < 5<<20 {
		return errors.New("body_store.part_size must be at least 5 MiB (S3 multipart minimum)")
	}
	if role == RoleAPI {
		if c.Auth.JWTSecret == "" {
			return errors.New("auth.jwt_secret is required for the api process (set AILENS360_JWT_SECRET); empty would break sessions across replicas")
		}
	}
	return nil
}

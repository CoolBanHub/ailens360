package config

import (
	"errors"
	"time"
)

type Config struct {
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
	// PublicURL is the externally reachable origin used to build the
	// proxy_prefix returned to clients (e.g. "https://proxy.example.com").
	// When empty, the prefix is derived from the inbound request host.
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

// RedisConfig is the shared Redis client used by Cache (L2) + Metrics + Pub/Sub.
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

type CollectorConfig struct {
	BufferSize    int           `yaml:"buffer_size"`
	BatchSize     int           `yaml:"batch_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	RawBodyLimit  int           `yaml:"raw_body_limit"`
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
	if c.API.Addr == "" {
		c.API.Addr = c.Proxy.Addr
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
	if c.Collector.BufferSize == 0 {
		c.Collector.BufferSize = 10000
	}
	if c.Collector.BatchSize == 0 {
		c.Collector.BatchSize = 200
	}
	if c.Collector.FlushInterval == 0 {
		c.Collector.FlushInterval = time.Second
	}
	if c.Collector.RawBodyLimit == 0 {
		c.Collector.RawBodyLimit = 8 << 20
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
}

// Validate enforces the constraints that make the process safe to run as one
// replica among many: shared Postgres, shared Redis, shared JWT secret.
func (c *Config) Validate() error {
	if c.DB.DSN == "" {
		return errors.New("db.dsn is required (Postgres connection string)")
	}
	if c.Redis.Addr == "" {
		return errors.New("redis.addr is required")
	}
	if c.Auth.JWTSecret == "" {
		return errors.New("auth.jwt_secret is required (set AILENS360_JWT_SECRET); empty would break sessions across replicas")
	}
	return nil
}

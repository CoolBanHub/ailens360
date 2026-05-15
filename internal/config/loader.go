package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Load reads configuration purely from environment variables.
// If envFile is non-empty and exists, it is loaded into the process env first
// (existing OS env vars take precedence — godotenv.Load semantics).
func Load(envFile string) (*Config, error) {
	if envFile != "" {
		if _, err := os.Stat(envFile); err == nil {
			if err := godotenv.Load(envFile); err != nil {
				return nil, fmt.Errorf("load env file %s: %w", envFile, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat env file %s: %w", envFile, err)
		}
	}

	var cfg Config
	if err := applyEnv(&cfg); err != nil {
		return nil, err
	}
	cfg.Defaults()
	return &cfg, nil
}

func applyEnv(c *Config) error {
	// Shared
	setString(&c.PublicURL, "AILENS360_PUBLIC_URL")
	c.PublicURL = strings.TrimRight(strings.TrimSpace(c.PublicURL), "/")
	if c.PublicURL != "" &&
		!strings.HasPrefix(c.PublicURL, "http://") &&
		!strings.HasPrefix(c.PublicURL, "https://") {
		return fmt.Errorf("AILENS360_PUBLIC_URL: must start with http:// or https://")
	}

	// Proxy
	setString(&c.Proxy.Addr, "AILENS360_PROXY_ADDR")
	if err := setDuration(&c.Proxy.ReadTimeout, "AILENS360_PROXY_READ_TIMEOUT"); err != nil {
		return err
	}
	if err := setDuration(&c.Proxy.WriteTimeout, "AILENS360_PROXY_WRITE_TIMEOUT"); err != nil {
		return err
	}
	if err := setDuration(&c.Proxy.IdleTimeout, "AILENS360_PROXY_IDLE_TIMEOUT"); err != nil {
		return err
	}
	if err := setInt64(&c.Proxy.MaxRequestBody, "AILENS360_PROXY_MAX_REQUEST_BODY"); err != nil {
		return err
	}
	if err := setDuration(&c.Proxy.UpstreamTimeout, "AILENS360_PROXY_UPSTREAM_TIMEOUT"); err != nil {
		return err
	}
	if err := setInt(&c.Proxy.RawBodyLimit, "AILENS360_PROXY_RAW_BODY_LIMIT"); err != nil {
		return err
	}
	setString(&c.Proxy.PublicURL, "AILENS360_PROXY_PUBLIC_URL")
	c.Proxy.PublicURL = strings.TrimRight(strings.TrimSpace(c.Proxy.PublicURL), "/")
	if c.Proxy.PublicURL != "" &&
		!strings.HasPrefix(c.Proxy.PublicURL, "http://") &&
		!strings.HasPrefix(c.Proxy.PublicURL, "https://") {
		return fmt.Errorf("AILENS360_PROXY_PUBLIC_URL: must start with http:// or https://")
	}

	// API
	setString(&c.API.Addr, "AILENS360_API_ADDR")
	if v := os.Getenv("AILENS360_API_CORS_ORIGINS"); v != "" {
		parts := strings.Split(v, ",")
		out := parts[:0]
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		c.API.CORSOrigins = out
	}
	setString(&c.UI.Dir, "AILENS360_UI_DIR")

	// DB
	setString(&c.DB.DSN, "AILENS360_DB_DSN")
	if err := setInt(&c.DB.MaxConns, "AILENS360_DB_MAX_CONNS"); err != nil {
		return err
	}
	if err := setInt(&c.DB.MaxIdleConns, "AILENS360_DB_MAX_IDLE_CONNS"); err != nil {
		return err
	}

	// Redis
	setString(&c.Redis.Addr, "AILENS360_REDIS_ADDR")
	setString(&c.Redis.Password, "AILENS360_REDIS_PASSWORD")
	if err := setInt(&c.Redis.DB, "AILENS360_REDIS_DB"); err != nil {
		return err
	}

	// Cache
	if err := setInt(&c.Cache.L1Cap, "AILENS360_CACHE_L1_CAP"); err != nil {
		return err
	}
	if err := setDuration(&c.Cache.L1TTL, "AILENS360_CACHE_L1_TTL"); err != nil {
		return err
	}
	if err := setDuration(&c.Cache.L2TTL, "AILENS360_CACHE_L2_TTL"); err != nil {
		return err
	}

	// Metrics
	if err := setInt(&c.Metrics.WindowSecs, "AILENS360_METRICS_WINDOW_SECS"); err != nil {
		return err
	}
	if err := setInt(&c.Metrics.RetentionSecs, "AILENS360_METRICS_RETENTION_SECS"); err != nil {
		return err
	}

	// Log
	setString(&c.Log.Level, "AILENS360_LOG_LEVEL")
	setString(&c.Log.Format, "AILENS360_LOG_FORMAT")

	// Collector
	setString(&c.Collector.HealthAddr, "AILENS360_COLLECTOR_HEALTH_ADDR")

	// Auth
	setString(&c.Auth.Username, "AILENS360_AUTH_USERNAME")
	setString(&c.Auth.Password, "AILENS360_AUTH_PASSWORD")
	setString(&c.Auth.JWTSecret, "AILENS360_JWT_SECRET")
	if err := setDuration(&c.Auth.TokenTTL, "AILENS360_AUTH_TOKEN_TTL"); err != nil {
		return err
	}

	// Pricing
	setString(&c.Pricing.SourceURL, "AILENS360_PRICING_SOURCE_URL")
	if err := setDuration(&c.Pricing.RefreshInterval, "AILENS360_PRICING_REFRESH_INTERVAL"); err != nil {
		return err
	}
	if err := setBool(&c.Pricing.Disable, "AILENS360_PRICING_DISABLE"); err != nil {
		return err
	}

	// BodyStore
	setString(&c.BodyStore.Endpoint, "AILENS360_BODY_STORE_ENDPOINT")
	setString(&c.BodyStore.Bucket, "AILENS360_BODY_STORE_BUCKET")
	setString(&c.BodyStore.Region, "AILENS360_BODY_STORE_REGION")
	setString(&c.BodyStore.AccessKeyID, "AILENS360_BODY_STORE_ACCESS_KEY_ID")
	setString(&c.BodyStore.SecretAccessKey, "AILENS360_BODY_STORE_SECRET_ACCESS_KEY")
	if err := setBool(&c.BodyStore.UseSSL, "AILENS360_BODY_STORE_USE_SSL"); err != nil {
		return err
	}
	if err := setBool(&c.BodyStore.PathStyle, "AILENS360_BODY_STORE_PATH_STYLE"); err != nil {
		return err
	}
	if err := setInt64(&c.BodyStore.PartSize, "AILENS360_BODY_STORE_PART_SIZE"); err != nil {
		return err
	}
	if err := setBool(&c.BodyStore.GzipBodies, "AILENS360_BODY_STORE_GZIP_BODIES"); err != nil {
		return err
	}
	if err := setDuration(&c.BodyStore.UploadTimeout, "AILENS360_BODY_STORE_UPLOAD_TIMEOUT"); err != nil {
		return err
	}
	if err := setBool(&c.BodyStore.PresignRedirect, "AILENS360_BODY_STORE_PRESIGN_REDIRECT"); err != nil {
		return err
	}
	if err := setDuration(&c.BodyStore.PresignTTL, "AILENS360_BODY_STORE_PRESIGN_TTL"); err != nil {
		return err
	}
	setString(&c.BodyStore.PublicEndpoint, "AILENS360_BODY_STORE_PUBLIC_ENDPOINT")

	// Stream
	setString(&c.Stream.Key, "AILENS360_STREAM_KEY")
	setString(&c.Stream.ConsumerGroup, "AILENS360_STREAM_CONSUMER_GROUP")
	if err := setDuration(&c.Stream.BlockTimeout, "AILENS360_STREAM_BLOCK_TIMEOUT"); err != nil {
		return err
	}
	if err := setInt(&c.Stream.BatchSize, "AILENS360_STREAM_BATCH_SIZE"); err != nil {
		return err
	}
	if err := setDuration(&c.Stream.PendingIdleMax, "AILENS360_STREAM_PENDING_IDLE_MAX"); err != nil {
		return err
	}
	if err := setDuration(&c.Stream.ClaimInterval, "AILENS360_STREAM_CLAIM_INTERVAL"); err != nil {
		return err
	}

	// Partition
	if err := setInt(&c.Partition.PreCreate, "AILENS360_PARTITION_PRE_CREATE"); err != nil {
		return err
	}
	if err := setInt(&c.Partition.RetentionMonths, "AILENS360_PARTITION_RETENTION_MONTHS"); err != nil {
		return err
	}
	if err := setDuration(&c.Partition.CheckInterval, "AILENS360_PARTITION_CHECK_INTERVAL"); err != nil {
		return err
	}

	return nil
}

func setString(dst *string, keys ...string) {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			*dst = v
			return
		}
	}
}

func setInt(dst *int, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%s: invalid int %q: %w", key, v, err)
	}
	*dst = n
	return nil
}

func setInt64(dst *int64, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fmt.Errorf("%s: invalid int64 %q: %w", key, v, err)
	}
	*dst = n
	return nil
}

func setDuration(dst *time.Duration, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("%s: invalid duration %q: %w", key, v, err)
	}
	*dst = d
	return nil
}

func setBool(dst *bool, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fmt.Errorf("%s: invalid bool %q: %w", key, v, err)
	}
	*dst = b
	return nil
}

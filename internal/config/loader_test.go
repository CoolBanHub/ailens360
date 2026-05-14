package config

import (
	"testing"
	"time"
)

func TestLoadReadsPricingEnv(t *testing.T) {
	t.Setenv("AILENS360_PRICING_SOURCE_URL", "https://example.test/prices.json")
	t.Setenv("AILENS360_PRICING_REFRESH_INTERVAL", "30m")
	t.Setenv("AILENS360_PRICING_DISABLE", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Pricing.SourceURL != "https://example.test/prices.json" {
		t.Fatalf("source url = %q", cfg.Pricing.SourceURL)
	}
	if cfg.Pricing.RefreshInterval != 30*time.Minute {
		t.Fatalf("refresh interval = %s", cfg.Pricing.RefreshInterval)
	}
	if !cfg.Pricing.Disable {
		t.Fatal("pricing disable = false, want true")
	}
}

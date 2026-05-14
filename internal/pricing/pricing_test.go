package pricing

import (
	"context"
	"math"
	"testing"
)

func eq(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: got %v want %v", label, got, want)
	}
}

func TestCostBreakdownAppliesCacheRates(t *testing.T) {
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"claude-test": {Input: 3.0, Output: 15.0, CacheRead: 0.3, CacheWrite: 3.75},
	})
	// Disjoint inputs: 200k uncached + 800k cache_read + 50k cache_creation, 1M output.
	got := c.Cost("claude-test", TokenUsage{
		Input: 200_000, CachedInput: 800_000, CacheCreation: 50_000, Output: 1_000_000,
	})
	want := 200_000.0/1e6*3.0 + 800_000.0/1e6*0.3 + 50_000.0/1e6*3.75 + 1_000_000.0/1e6*15.0
	eq(t, "claude-test", got, want)
}

func TestCostFallsBackToInputWhenCacheRateMissing(t *testing.T) {
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"openai-test": {Input: 2.5, Output: 10.0}, // no cache_read/cache_write
	})
	// 600 uncached + 400 cache_read, 100 output. With no cache_read rate
	// configured, the cached portion bills at the full input rate — so the
	// total input charge equals the full 1000-token prompt at input rate.
	got := c.Cost("openai-test", TokenUsage{Input: 600, CachedInput: 400, Output: 100})
	want := 1000.0/1e6*2.5 + 100.0/1e6*10.0
	eq(t, "openai-test", got, want)
}

func TestCostUnknownModelReturnsZero(t *testing.T) {
	c := NewCatalog()
	if c.Cost("does-not-exist-xyz", TokenUsage{Input: 100, Output: 100}) != 0 {
		t.Fatal("unknown model should cost 0")
	}
}

func TestCatalogLookupLongestPrefix(t *testing.T) {
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"gpt-4o":         {Input: 2.5, Output: 10},
		"gpt-4o-mini":    {Input: 0.15, Output: 0.6},
		"gpt-4o-2024-08": {Input: 2.5, Output: 10},
	})
	// "gpt-4o-mini-2024-07-18" should prefer "gpt-4o-mini" over "gpt-4o".
	p, ok := c.lookup("gpt-4o-mini-2024-07-18")
	if !ok || p.Input != 0.15 {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestTieredPricingAppliesAboveThreshold(t *testing.T) {
	// claude-opus-style: base 5/25, above 200k tier 10/37.5 with separate
	// cache rates. Verify both bands resolve correctly.
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"opus-test": {
			Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25,
			Tiers: []TierRate{{
				Input: 10, Output: 37.5, CacheRead: 1, CacheWrite: 12.5,
				Threshold: TierThreshold{Type: "context", Size: 200000},
			}},
		},
	})

	// Below threshold: base rates.
	got := c.Cost("opus-test", TokenUsage{Input: 150_000, Output: 1000, ContextTokens: 150_000})
	want := 150_000.0/1e6*5 + 1000.0/1e6*25
	eq(t, "below tier", got, want)

	// Above threshold: tier rates.
	got = c.Cost("opus-test", TokenUsage{Input: 250_000, Output: 1000, ContextTokens: 250_000})
	want = 250_000.0/1e6*10 + 1000.0/1e6*37.5
	eq(t, "above tier", got, want)

	// At exactly threshold (==, not >): still base rates.
	got = c.Cost("opus-test", TokenUsage{Input: 200_000, Output: 1000, ContextTokens: 200_000})
	want = 200_000.0/1e6*5 + 1000.0/1e6*25
	eq(t, "at tier boundary", got, want)
}

func TestTieredPricingPicksHighestMatchingThreshold(t *testing.T) {
	// Multiple tier bands; verify the largest matching threshold wins.
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"multi": {
			Input: 1, Output: 2,
			Tiers: []TierRate{
				{Input: 2, Output: 4, Threshold: TierThreshold{Type: "context", Size: 100_000}},
				{Input: 4, Output: 8, Threshold: TierThreshold{Type: "context", Size: 500_000}},
			},
		},
	})
	got := c.Cost("multi", TokenUsage{Input: 600_000, Output: 1000, ContextTokens: 600_000})
	want := 600_000.0/1e6*4 + 1000.0/1e6*8 // 500k tier
	eq(t, "highest matching tier wins", got, want)
}

func TestTieredPricingTierMissingCacheKeepsBaseRate(t *testing.T) {
	// Common shape from models.dev: the tier re-states input/output but
	// omits cache_read. We must KEEP the base cache_read instead of treating
	// the tier's 0 as "free".
	c := NewCatalog()
	c.Replace(map[string]PricePerMTok{
		"gpt5-test": {
			Input: 2.5, Output: 15, CacheRead: 0.25,
			Tiers: []TierRate{{
				Input: 5, Output: 22.5, // no cache_read on tier
				Threshold: TierThreshold{Type: "context", Size: 272_000},
			}},
		},
	})
	// 200k uncached + 100k cache_read, total 300k context crosses the tier
	// threshold (272k). Uncached bills at the tier input rate; cached bills
	// at the base cache_read rate (the tier omitted it); output at tier.
	got := c.Cost("gpt5-test", TokenUsage{
		Input: 200_000, CachedInput: 100_000, Output: 1000, ContextTokens: 300_000,
	})
	want := 200_000.0/1e6*5 + 100_000.0/1e6*0.25 + 1000.0/1e6*22.5
	eq(t, "tier preserves base cache_read", got, want)
}

func TestParseModelsDevPicksUpTiers(t *testing.T) {
	raw := []byte(`{"x":{"models":{"opus":{"cost":{
		"input":5,"output":25,"cache_read":0.5,"cache_write":6.25,
		"tiers":[{"input":10,"output":37.5,"cache_read":1,"cache_write":12.5,
			"tier":{"type":"context","size":200000}}],
		"context_over_200k":{"input":10,"output":37.5}
	}}}}}`)
	table, err := ParseModelsDev(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := table["opus"]
	if len(p.Tiers) != 1 {
		t.Fatalf("tiers: %+v", p.Tiers)
	}
	if p.Tiers[0].Threshold.Size != 200_000 || p.Tiers[0].Input != 10 {
		t.Fatalf("tier: %+v", p.Tiers[0])
	}
}

func TestParseModelsDevFlattensProviders(t *testing.T) {
	raw := []byte(`{
		"anthropic": {"models": {
			"claude-test": {"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75}}
		}},
		"openai": {"models": {
			"gpt-test": {"cost": {"input": 2.5, "output": 10, "cache_read": 1.25}},
			"free-model": {"cost": {"input": 0, "output": 0}}
		}}
	}`)
	table, err := ParseModelsDev(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(table) != 2 {
		t.Fatalf("free-model should be skipped: %+v", table)
	}
	if table["claude-test"].CacheWrite != 3.75 {
		t.Fatalf("claude-test: %+v", table["claude-test"])
	}
	if table["gpt-test"].CacheRead != 1.25 {
		t.Fatalf("gpt-test: %+v", table["gpt-test"])
	}
}

// stubSource feeds canned data; verifies Refresher.Start performs the initial
// load synchronously and swaps the table.
type stubSource struct {
	table map[string]PricePerMTok
	err   error
}

func (s *stubSource) Fetch(_ context.Context) (map[string]PricePerMTok, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.table, nil
}

func TestRefresherStartLoadsInitialTable(t *testing.T) {
	cat := NewCatalog()
	beforeSeed, _ := cat.lookup("gpt-4o")
	if beforeSeed.Input == 0 {
		t.Fatal("seed table missing gpt-4o")
	}
	r := &Refresher{
		Catalog: cat,
		Source:  &stubSource{table: map[string]PricePerMTok{"only-model": {Input: 1, Output: 2}}},
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	p, ok := cat.lookup("only-model")
	if !ok || p.Input != 1 {
		t.Fatalf("expected swap; got %+v ok=%v size=%d", p, ok, cat.Size())
	}
	// seed prices are gone — Replace swaps the whole map.
	if _, ok := cat.lookup("gpt-4o"); ok {
		t.Fatal("expected gpt-4o to be gone after upstream load (atomic swap, not merge)")
	}
}

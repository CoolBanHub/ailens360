// Package pricing keeps the per-model USD price catalog. Prices are normally
// loaded from models.dev (see source.go + refresher.go); the seed table here
// is the fallback used before the first refresh succeeds.
package pricing

import (
	"strings"
	"sync/atomic"
)

// PricePerMTok holds USD price per 1M tokens. Zero values mean "not priced
// separately" — callers should fall back to Input for missing CacheRead and
// to Input for missing CacheWrite (Anthropic-style providers fill both;
// OpenAI/Gemini usually only have CacheRead).
//
// Tiers carries context-size–based overrides (long-context surcharges, e.g.
// Anthropic Opus and GPT-5 above 200k tokens). When the request's context
// exceeds a tier's Threshold, that tier's prices replace the base prices for
// every billed field that the tier sets non-zero. Use Effective() to resolve.
type PricePerMTok struct {
	Input      float64    `json:"input"`
	Output     float64    `json:"output"`
	CacheRead  float64    `json:"cache_read,omitempty"`
	CacheWrite float64    `json:"cache_write,omitempty"`
	Tiers      []TierRate `json:"tiers,omitempty"`
}

// TierRate is a price override that kicks in when the request's context size
// exceeds Threshold.Size. We only handle "context" tiers — that is the only
// type in the upstream catalog today (models.dev), but we ignore unknown types
// gracefully so future tier kinds don't crash billing.
type TierRate struct {
	Input      float64       `json:"input"`
	Output     float64       `json:"output"`
	CacheRead  float64       `json:"cache_read,omitempty"`
	CacheWrite float64       `json:"cache_write,omitempty"`
	Threshold  TierThreshold `json:"tier"`
}

type TierThreshold struct {
	Type string `json:"type"` // "context"
	Size int    `json:"size"` // # of tokens above which this tier applies
}

// Effective returns the price that applies for the given context size. When
// no tier matches, the base prices are returned unchanged. When multiple
// tiers match (request size > multiple thresholds), the one with the largest
// Threshold.Size wins — that mirrors the upstream semantics where each tier
// represents a band ceiling and only the highest band the request crosses
// dictates the rate.
//
// Each field of the chosen tier overrides the corresponding base field only
// when the tier sets it non-zero — this matches what models.dev emits: tiers
// often re-state input/output but omit cache_read/cache_write, in which case
// we want to keep the base discount rates rather than treat them as 0.
func (p PricePerMTok) Effective(contextSize int) PricePerMTok {
	base := p
	base.Tiers = nil
	var picked *TierRate
	for i := range p.Tiers {
		t := &p.Tiers[i]
		if t.Threshold.Type != "" && t.Threshold.Type != "context" {
			continue
		}
		if contextSize > t.Threshold.Size {
			if picked == nil || t.Threshold.Size > picked.Threshold.Size {
				picked = t
			}
		}
	}
	if picked == nil {
		return base
	}
	if picked.Input > 0 {
		base.Input = picked.Input
	}
	if picked.Output > 0 {
		base.Output = picked.Output
	}
	if picked.CacheRead > 0 {
		base.CacheRead = picked.CacheRead
	}
	if picked.CacheWrite > 0 {
		base.CacheWrite = picked.CacheWrite
	}
	return base
}

// TokenUsage is the breakdown the pipeline hands to Cost(). All fields are raw
// counts (not deltas). Cached and CacheCreation are NOT subtracted from Input
// by the caller — Cost() handles the math.
//
// Provider semantics for Input/CachedInput differ:
//
//   - OpenAI / Gemini: Input is the full prompt token count and CachedInput
//     is a subset of it.
//   - Anthropic: Input is uncached only; CachedInput (cache_read) and
//     CacheCreation are separate from it.
//
// ContextTokens disambiguates this for tier resolution. The pipeline is
// responsible for computing it correctly per provider; Cost() falls back to
// Input + CachedInput*(0 if OpenAI/Gemini-style) approximations only when
// ContextTokens is 0 (legacy callers).
type TokenUsage struct {
	Input         int // see provider semantics above
	Output        int
	CachedInput   int // discounted cache-read portion
	CacheCreation int // Anthropic-only; billed separately from Input
	// ContextTokens is the prompt-side context size used for tier matching.
	// 0 means "compute heuristically" (see resolveContextSize).
	ContextTokens int
}

// Catalog is the read side of the price table. Reads are lock-free via an
// atomic pointer swap; the refresher replaces the table whole when it loads
// new data. Unknown models return cost 0 — they do NOT crash the request.
type Catalog struct {
	// table is *map[string]PricePerMTok. Always non-nil after construction.
	table atomic.Pointer[map[string]PricePerMTok]
}

// NewCatalog returns a catalog pre-loaded with the minimal seed prices. It is
// safe to use directly (in tests, or before the first refresher fetch).
func NewCatalog() *Catalog {
	c := &Catalog{}
	c.Replace(seedPrices())
	return c
}

// Replace swaps the entire price table atomically. The refresher calls this
// after fetching from models.dev. Callers must not mutate the map afterwards.
func (c *Catalog) Replace(m map[string]PricePerMTok) {
	if m == nil {
		m = map[string]PricePerMTok{}
	}
	c.table.Store(&m)
}

// Size returns the number of priced models — useful for logging post-refresh.
func (c *Catalog) Size() int {
	p := c.table.Load()
	if p == nil {
		return 0
	}
	return len(*p)
}

// Cost returns the USD cost of a request, factoring cached-input discount and
// cache-creation surcharge. Unknown models return 0.
//
//	billed_input = (Input - CachedInput) * input + CachedInput * cache_read
//	billed_cache_write = CacheCreation * cache_write
//	billed_output      = Output * output
//
// When a price table entry omits CacheRead, the discount falls back to the
// full input price (effectively no discount). Same for CacheWrite.
func (c *Catalog) Cost(model string, u TokenUsage) float64 {
	if model == "" {
		return 0
	}
	raw, ok := c.lookup(model)
	if !ok {
		return 0
	}
	p := raw.Effective(resolveContextSize(u))
	uncached := max(u.Input-u.CachedInput, 0)
	cacheReadRate := p.CacheRead
	if cacheReadRate == 0 {
		cacheReadRate = p.Input
	}
	cacheWriteRate := p.CacheWrite
	if cacheWriteRate == 0 {
		cacheWriteRate = p.Input
	}
	return float64(uncached)/1e6*p.Input +
		float64(u.CachedInput)/1e6*cacheReadRate +
		float64(u.CacheCreation)/1e6*cacheWriteRate +
		float64(u.Output)/1e6*p.Output
}

// resolveContextSize falls back to a best-effort estimate when the caller
// didn't set ContextTokens. We assume Anthropic-style separation (cached and
// cache_creation NOT included in Input) because it overestimates context for
// OpenAI/Gemini at worst, which is the safer direction: tier prices are
// always higher, so overshooting tier means overcharging — visible — instead
// of undercharging — silent. Pipelines that know the provider should always
// set ContextTokens explicitly.
func resolveContextSize(u TokenUsage) int {
	if u.ContextTokens > 0 {
		return u.ContextTokens
	}
	// Conservative fallback: treat all three as additive.
	return u.Input + u.CachedInput + u.CacheCreation
}

// LegacyCost preserves the pre-breakdown signature. New callers should use
// Cost; this stays for code paths that haven't yet been threaded with the
// full breakdown.
func (c *Catalog) LegacyCost(model string, input, output int) float64 {
	return c.Cost(model, TokenUsage{Input: input, Output: output})
}

func (c *Catalog) lookup(model string) (PricePerMTok, bool) {
	p := c.table.Load()
	if p == nil {
		return PricePerMTok{}, false
	}
	table := *p
	if v, ok := table[model]; ok {
		return v, true
	}
	// fuzzy: longest-prefix. Catalogs from models.dev contain dated suffixes
	// (claude-3-5-sonnet-20241022); requests rarely include the date.
	var (
		best    PricePerMTok
		bestKey string
	)
	for k, v := range table {
		if strings.HasPrefix(model, k) && len(k) > len(bestKey) {
			best, bestKey = v, k
		}
	}
	if bestKey != "" {
		return best, true
	}
	// Also try the reverse: request is a prefix of a catalog key. This covers
	// the case where the upstream returns the short alias (gpt-4o) but the
	// catalog only has the dated variants.
	for k, v := range table {
		if strings.HasPrefix(k, model) && len(model) > 0 {
			if len(k) > len(bestKey) {
				best, bestKey = v, k
			}
		}
	}
	if bestKey == "" {
		return PricePerMTok{}, false
	}
	return best, true
}

// seedPrices is the fallback table used before the refresher succeeds. Keep
// it small — it only needs to cover the most common models so dev/test
// environments without network access still get plausible costs.
func seedPrices() map[string]PricePerMTok {
	return map[string]PricePerMTok{
		"gpt-4o":            {Input: 2.50, Output: 10.00},
		"gpt-4o-mini":       {Input: 0.15, Output: 0.60},
		"gpt-4-turbo":       {Input: 10.00, Output: 30.00},
		"gpt-3.5-turbo":     {Input: 0.50, Output: 1.50},
		"claude-3-5-sonnet": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
		"claude-3-5-haiku":  {Input: 0.80, Output: 4.00, CacheRead: 0.08, CacheWrite: 1.00},
		"claude-3-opus":     {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
		"gemini-1.5-pro":    {Input: 1.25, Output: 5.00},
		"gemini-1.5-flash":  {Input: 0.075, Output: 0.30},
	}
}

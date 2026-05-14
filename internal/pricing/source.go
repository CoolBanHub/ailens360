package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultModelsDevURL is the canonical models.dev catalog endpoint.
const DefaultModelsDevURL = "https://models.dev/api.json"

// Source is anything that can produce a flattened model_id → price table.
// Implementations should be safe to call concurrently from a single refresher
// loop (one call at a time is fine).
type Source interface {
	Fetch(ctx context.Context) (map[string]PricePerMTok, error)
}

// ModelsDevSource fetches https://models.dev/api.json and flattens it.
// The upstream schema is:
//
//	{
//	  "<provider_id>": {
//	    "models": {
//	      "<model_id>": { "cost": { "input": ..., "output": ..., "cache_read": ..., "cache_write": ... } }
//	    }
//	  }
//	}
//
// We key the flattened table by model_id only. If two providers ship the same
// model id with different prices, the last one wins — deterministic because
// Go's map iteration order is fine for "tie-break by load order", and the
// likely cases (Anthropic/OpenAI proper) already use canonical ids.
type ModelsDevSource struct {
	URL    string
	Client *http.Client
}

// NewModelsDevSource returns a source pointing at the public endpoint with a
// 30s HTTP timeout. Pass a custom URL for staging/mirrors.
func NewModelsDevSource(url string) *ModelsDevSource {
	if url == "" {
		url = DefaultModelsDevURL
	}
	return &ModelsDevSource{
		URL:    url,
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

type modelsDevModel struct {
	// Cost decodes directly into PricePerMTok; its `tiers` JSON tag picks up
	// the long-context override array. We intentionally do NOT decode the
	// sibling "context_over_200k" key — it duplicates the first tier entry
	// and the array form is canonical.
	Cost *PricePerMTok `json:"cost"`
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

func (s *ModelsDevSource) Fetch(ctx context.Context) (map[string]PricePerMTok, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", s.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("fetch %s: status %d", s.URL, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB hard cap
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.URL, err)
	}
	return ParseModelsDev(raw)
}

// ParseModelsDev exposes the flattening logic for tests / Redis-warm path.
func ParseModelsDev(raw []byte) (map[string]PricePerMTok, error) {
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("decode models.dev: %w", err)
	}
	out := make(map[string]PricePerMTok, 1024)
	for _, p := range providers {
		for mid, m := range p.Models {
			if m.Cost == nil {
				continue
			}
			// Skip entries with no real pricing (free / unspecified). Lets the
			// lookup fall through to seed/fuzzy match instead of returning 0.
			if m.Cost.Input == 0 && m.Cost.Output == 0 {
				continue
			}
			out[mid] = *m.Cost
		}
	}
	return out, nil
}

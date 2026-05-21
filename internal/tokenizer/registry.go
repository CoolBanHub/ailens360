package tokenizer

import (
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Registry routes Count(model, text) to the best tokenizer for the model family,
// caching tiktoken encoders on first use to avoid the multi-MB BPE init cost.
//
// Unknown models fall through to the CJK-aware heuristic.
type Registry struct {
	heuristic Heuristic
	mu        sync.Mutex
	cache     map[string]*tiktoken.Tiktoken
}

func NewRegistry() *Registry {
	return &Registry{cache: make(map[string]*tiktoken.Tiktoken)}
}

func (r *Registry) Count(model, text string) int {
	if text == "" {
		return 0
	}
	if enc := r.encoderFor(model); enc != nil {
		return len(enc.Encode(text, nil, nil))
	}
	return r.heuristic.Count(model, text)
}

func (r *Registry) encoderFor(model string) *tiktoken.Tiktoken {
	encName := encodingForModel(model)
	if encName == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if enc, ok := r.cache[encName]; ok {
		return enc
	}
	enc, err := tiktoken.GetEncoding(encName)
	if err != nil {
		// Cache the negative result to avoid retrying on every call.
		r.cache[encName] = nil
		return nil
	}
	r.cache[encName] = enc
	return enc
}

// encodingForModel maps a model identifier to a tiktoken encoding name.
// Only covers the OpenAI families that share a tokenizer; everything else
// returns "" and falls back to the heuristic.
func encodingForModel(model string) string {
	if model == "" {
		return ""
	}
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-5"),
		strings.HasPrefix(m, "gpt-4o"),
		strings.HasPrefix(m, "gpt-4.1"),
		strings.HasPrefix(m, "gpt-4.5"),
		strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"):
		return "o200k_base"
	case strings.HasPrefix(m, "gpt-4"),
		strings.HasPrefix(m, "text-embedding-3"),
		strings.HasPrefix(m, "text-embedding-ada-002"):
		return "cl100k_base"
	}
	return ""
}

package tokenizer

// Estimator returns an approximate token count for a string.
// Implementations are safe for concurrent use; choose with NewRegistry().
type Estimator interface {
	Count(model, text string) int
}

// New returns the default registry — tiktoken for OpenAI families, heuristic fallback for everyone else.
func New() Estimator { return NewRegistry() }

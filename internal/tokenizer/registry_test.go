package tokenizer

import "testing"

func TestRegistryUsesTiktokenForKnownModels(t *testing.T) {
	r := NewRegistry()
	got := r.Count("gpt-4o-mini", "Hello, world!")
	if got <= 0 {
		t.Fatalf("expected positive token count for OpenAI model, got %d", got)
	}
}

func TestRegistryFallsBackForUnknownModel(t *testing.T) {
	r := NewRegistry()
	got := r.Count("some-mystery-model", "你好世界")
	// CJK heuristic: 4 CJK runes → 4 tokens
	if got != 4 {
		t.Fatalf("heuristic expected 4 for 4 CJK runes, got %d", got)
	}
}

func TestRegistryEmptyText(t *testing.T) {
	r := NewRegistry()
	if got := r.Count("gpt-4o", ""); got != 0 {
		t.Fatalf("empty text should be 0 tokens, got %d", got)
	}
}

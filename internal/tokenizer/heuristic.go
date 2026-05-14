package tokenizer

import (
	"strings"
	"unicode"
)

// Heuristic is a cheap CJK-aware fallback: 1 token per CJK rune + 1 token per ~4 Latin chars.
type Heuristic struct{}

func (Heuristic) Count(_ string, text string) int {
	if text == "" {
		return 0
	}
	var cjk, other int
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else if !unicode.IsSpace(r) {
			other++
		}
	}
	est := cjk + other/4
	if est == 0 && strings.TrimSpace(text) != "" {
		est = 1
	}
	return est
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0x3040 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	}
	return false
}

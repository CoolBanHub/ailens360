// Package provider identifies the upstream LLM provider from an absolute
// upstream URL. AILens360 forwards client requests verbatim (Authorization
// passes through), so the only thing the proxy needs to know per request is
// the provider's name — used by the stream parser to decode SSE formats and
// by traces for filtering.
package provider

import "strings"

const (
	NameOpenAI    = "openai"
	NameAnthropic = "anthropic"
	NameGemini    = "gemini"
)

type Provider struct {
	Name string
}

// DetectFromHost picks a provider name from an upstream host (e.g.
// "api.openai.com", "api.deepseek.com", "api.anthropic.com",
// "generativelanguage.googleapis.com"). Unknown hosts default to OpenAI,
// since the OpenAI Chat Completions schema is the de-facto standard for
// third-party providers (Groq, DeepSeek, Together, Moonshot, local vLLM, ...).
func DetectFromHost(host string) Provider {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "anthropic"):
		return Provider{Name: NameAnthropic}
	case strings.Contains(h, "googleapis") || strings.Contains(h, "generativelanguage"):
		return Provider{Name: NameGemini}
	default:
		return Provider{Name: NameOpenAI}
	}
}

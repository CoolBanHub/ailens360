package provider

import "testing"

func TestDetectFromHost(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"api.openai.com", NameOpenAI},
		{"api.deepseek.com", NameOpenAI},
		{"api.groq.com", NameOpenAI},
		{"api.together.xyz", NameOpenAI},
		{"localhost:8000", NameOpenAI},
		{"api.anthropic.com", NameAnthropic},
		{"api.anthropic.cn", NameAnthropic},
		{"generativelanguage.googleapis.com", NameGemini},
		{"aiplatform.googleapis.com", NameGemini},
		{"", NameOpenAI},
	}
	for _, c := range cases {
		got := DetectFromHost(c.host)
		if got.Name != c.want {
			t.Errorf("DetectFromHost(%q) = %q, want %q", c.host, got.Name, c.want)
		}
	}
}

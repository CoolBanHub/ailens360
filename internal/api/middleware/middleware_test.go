package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newDummyRequest(origin string) *http.Request {
	r := httptest.NewRequest("GET", "/anything", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestCORSEmptyConfigAllowsAll(t *testing.T) {
	w := httptest.NewRecorder()
	CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})).ServeHTTP(w, newDummyRequest("https://app.example.com"))
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("want * with no origins configured, got %q", got)
	}
}

func TestCORSEchoesMatchingOrigin(t *testing.T) {
	mw := CORS([]string{"https://a.test", "https://b.test"})
	cases := map[string]string{
		"https://a.test":   "https://a.test",
		"https://b.test":   "https://b.test",
		"https://evil.com": "", // not in allow list → no header
	}
	for origin, want := range cases {
		w := httptest.NewRecorder()
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(204)
		})).ServeHTTP(w, newDummyRequest(origin))
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %q: want %q got %q", origin, want, got)
		}
		// Vary: Origin should be set whenever we echo a single origin.
		if want != "" {
			if v := w.Header().Get("Vary"); v == "" {
				t.Errorf("origin %q: missing Vary: Origin", origin)
			}
		}
	}
}

func TestCORSNeverEmitsCommaJoinedOrigins(t *testing.T) {
	w := httptest.NewRecorder()
	CORS([]string{"https://a.test", "https://b.test"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})).ServeHTTP(w, newDummyRequest("https://a.test"))
	got := w.Header().Get("Access-Control-Allow-Origin")
	if got == "https://a.test,https://b.test" || got == "https://a.test, https://b.test" {
		t.Fatalf("CORS leaked a comma-joined value: %q", got)
	}
}

package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/internal/api/response"
)

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			logger.Info("api_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"dur_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic", "value", rec, "stack", string(debug.Stack()))
					response.Error(w, http.StatusInternalServerError, 50000, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS implements a per-request Origin echo so multi-origin deployments work
// correctly. Browsers reject comma-joined Access-Control-Allow-Origin values, so
// we emit at most one origin per response and add Vary: Origin so caches don't
// mix responses from different callers.
//
//   - empty origins slice: open CORS, returns "*"
//   - one or more entries: returns the matching origin (or no header at all if
//     the request's Origin isn't in the list, which is also the spec-correct
//     way to deny without breaking same-origin callers)
func CORS(origins []string) func(http.Handler) http.Handler {
	allowAll := len(origins) == 0
	allow := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allow[strings.TrimSpace(o)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case allowAll:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "":
				if _, ok := allow[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// JWTVerifier is the minimal surface AdminJWT needs from internal/auth.Service.
// Defined as an interface here so the middleware package doesn't import auth
// (avoids a small import cycle risk).
type JWTVerifier interface {
	Verify(raw string) (string, error)
}

// AdminJWT requires a Bearer JWT signed by the configured auth service.
// OPTIONS preflights pass without auth so CORS works.
func AdminJWT(v JWTVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			h := r.Header.Get("Authorization")
			h = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if h == "" {
				response.Error(w, http.StatusUnauthorized, 40101, "missing token")
				return
			}
			if _, err := v.Verify(h); err != nil {
				response.Error(w, http.StatusUnauthorized, 40102, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

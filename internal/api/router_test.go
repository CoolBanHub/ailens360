package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/CoolBanHub/ailens360/internal/api/handler"
	"github.com/CoolBanHub/ailens360/internal/auth"
)

// Span ingest is called by application runtimes with a project key, not the
// console JWT. If it accidentally sits behind AdminJWT, the error is
// "missing token" instead of "missing project key".
func TestIngestRouteBypassesAdminJWT(t *testing.T) {
	authSvc, err := auth.New("admin", "admin", "test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	Mount(r, RouterDeps{
		Handlers: &handler.Handlers{},
		Auth:     authSvc,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ingest/spans", strings.NewReader(`{"spans":[]}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing project key") {
		t.Fatalf("request hit the JWT guard: body=%s", rec.Body.String())
	}
}

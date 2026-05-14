package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/CoolBanHub/ailens360/internal/api/handler"
	"github.com/CoolBanHub/ailens360/internal/api/middleware"
	"github.com/CoolBanHub/ailens360/internal/auth"
	"github.com/CoolBanHub/ailens360/internal/version"
)

type RouterDeps struct {
	Handlers    *handler.Handlers
	Auth        *auth.Service
	CORSOrigins []string
}

// Mount attaches the /api routes onto r. Useful when proxy & api share the same listener.
func Mount(r chi.Router, d RouterDeps) {
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(version.Version))
	})

	h := d.Handlers

	// /auth lives at the same depth as the protected routes. /login is public;
	// /me sits behind the JWT guard via a chi.Group inside the same subrouter.
	r.Route("/api/auth", func(r chi.Router) {
		r.Use(middleware.CORS(d.CORSOrigins))
		r.Post("/login", h.Login)
		r.Group(func(r chi.Router) {
			r.Use(middleware.AdminJWT(d.Auth))
			r.Get("/me", h.Me)
		})
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.CORS(d.CORSOrigins))
		r.Use(middleware.AdminJWT(d.Auth))

		r.Route("/projects", func(r chi.Router) {
			r.Get("/", h.ListProjects)
			r.Post("/", h.CreateProject)
			r.Get("/{id}", h.GetProject)
			r.Put("/{id}", h.UpdateProject)
			r.Post("/{id}/reset_project_key", h.ResetProjectKey)
			r.Delete("/{id}", h.DeleteProject)
		})

		// trace_groups: Langfuse-style logical traces (one row per trace_id).
		// /traces continues to return individual spans, filterable by trace_id.
		r.Get("/trace_groups", h.ListTraceGroups)
		r.Route("/traces", func(r chi.Router) {
			r.Get("/", h.ListTraces)
			r.Get("/{id}", h.GetTrace)
		})

		r.Get("/metrics/usage", h.Usage)
		// Live realtime counters from Redis (last N seconds, near real-time).
		r.Get("/metrics/live", h.Live)
		// Distinct models per project — powers the model filter dropdown.
		r.Get("/trace_facets", h.Facets)
	})
}

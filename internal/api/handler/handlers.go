package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/CoolBanHub/ailens360/internal/api/response"
	"github.com/CoolBanHub/ailens360/internal/auth"
	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

type Handlers struct {
	Projects *project.Service
	Resolver *project.Resolver
	Traces   repo.TraceRepo
	Auth     *auth.Service
	Realtime *metrics.Realtime
	// PublicURL, if set, overrides the request-derived origin used to build
	// proxy_prefix values returned by the project endpoints.
	PublicURL string
}

// ---- Projects ----

type projectIn struct {
	Name string `json:"name"`
}

func (h *Handlers) CreateProject(w http.ResponseWriter, r *http.Request) {
	var in projectIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		response.Error(w, http.StatusBadRequest, 40000, "name required")
		return
	}
	p, err := h.Projects.Create(r.Context(), project.CreateInput{Name: in.Name})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.Created(w, h.projectView(p, r))
}

func (h *Handlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	out, err := h.Projects.List(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(out))
	for _, p := range out {
		items = append(items, h.projectView(p, r))
	}
	response.OK(w, map[string]any{"items": items})
}

func (h *Handlers) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.Projects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.Error(w, http.StatusNotFound, 40400, "project not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, h.projectView(p, r))
}

func (h *Handlers) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in projectIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Error(w, http.StatusBadRequest, 40000, "invalid body")
		return
	}
	p, err := h.Projects.Update(r.Context(), project.UpdateInput{ID: id, Name: in.Name})
	if err != nil {
		response.Error(w, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if err := h.Resolver.Invalidate(r.Context(), p.ProjectKey); err != nil {
		response.Error(w, http.StatusInternalServerError, 50002, "invalidate cache: "+err.Error())
		return
	}
	response.OK(w, h.projectView(p, r))
}

func (h *Handlers) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.Projects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.OK(w, nil)
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	if err := h.Projects.Delete(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	if err := h.Resolver.Invalidate(r.Context(), p.ProjectKey); err != nil {
		response.Error(w, http.StatusInternalServerError, 50002, "invalidate cache: "+err.Error())
		return
	}
	response.OK(w, nil)
}

// ResetProjectKey rotates a project's project_key. The old key stops resolving
// as soon as the cache is invalidated; clients must update the
// X-AILens-Project-Key header to the new value returned here.
func (h *Handlers) ResetProjectKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, oldKey, err := h.Projects.ResetProjectKey(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.Error(w, http.StatusNotFound, 40400, "project not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	if err := h.Resolver.Invalidate(r.Context(), oldKey); err != nil {
		response.Error(w, http.StatusInternalServerError, 50002, "invalidate cache: "+err.Error())
		return
	}
	response.OK(w, h.projectView(p, r))
}

// projectView shapes a project for the API. proxy_prefix is the deployment-wide
// prefix clients prepend to their upstream URL:
//
//	client.base_url = "{proxy_prefix}/{your_real_upstream_base_url}"
//	e.g. "http://localhost:8080/p/https://api.openai.com/v1"
//
// The project itself is identified by the X-AILens-Project-Key header carrying
// the value of `project_key`.
func (h *Handlers) projectView(p *repo.Project, r *http.Request) map[string]any {
	prefix := h.buildProxyPrefix(r)
	return map[string]any{
		"id":           p.ID,
		"project_key":  p.ProjectKey,
		"name":         p.Name,
		"proxy_prefix": prefix,
		"example": map[string]string{
			"openai":    prefix + "/https://api.openai.com/v1",
			"anthropic": prefix + "/https://api.anthropic.com",
			"gemini":    prefix + "/https://generativelanguage.googleapis.com/v1beta",
		},
		"created_at": p.CreatedAt.Unix(),
		"updated_at": p.UpdatedAt.Unix(),
	}
}

func (h *Handlers) buildProxyPrefix(r *http.Request) string {
	if h.PublicURL != "" {
		return h.PublicURL + "/p"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return scheme + "://" + host + "/p"
}

// ---- Traces ----

func (h *Handlers) ListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repo.ListTraceFilter{
		ProjectID:   q.Get("project_id"),
		TraceID:     q.Get("trace_id"),
		UserID:      q.Get("user_id"),
		SessionID:   q.Get("session_id"),
		Model:       q.Get("model"),
		Status:      q.Get("status"),
		StartUnixMs: atoi64(q.Get("start_time")),
		EndUnixMs:   atoi64(q.Get("end_time")),
		Limit:       atoi(q.Get("limit")),
		Offset:      atoi(q.Get("offset")),
	}
	items, total, err := h.Traces.List(r.Context(), f)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"total": total,
		"items": items, // include full trace; clients can trim
	})
}

func (h *Handlers) GetTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.Traces.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.Error(w, http.StatusNotFound, 40400, "trace not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, t)
}

// ListTraceGroups returns the Langfuse-style logical trace list (one row per
// trace_id). Each item carries aggregated metrics + the span count.
func (h *Handlers) ListTraceGroups(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repo.ListTraceGroupFilter{
		ProjectID:   q.Get("project_id"),
		UserID:      q.Get("user_id"),
		SessionID:   q.Get("session_id"),
		TraceName:   q.Get("trace_name"),
		Status:      q.Get("status"),
		Model:       q.Get("model"),
		StartUnixMs: atoi64(q.Get("start_time")),
		EndUnixMs:   atoi64(q.Get("end_time")),
		Limit:       atoi(q.Get("limit")),
		Offset:      atoi(q.Get("offset")),
	}
	items, total, err := h.Traces.ListGroups(r.Context(), f)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"total": total,
		"items": items,
	})
}

// Facets returns the distinct model values seen for a project, used to
// populate the trace-filter model dropdown. Ordered by frequency (desc) and
// capped at 200 entries.
func (h *Handlers) Facets(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		response.Error(w, http.StatusBadRequest, 40000, "project_id required")
		return
	}
	models, err := h.Traces.Facets(r.Context(), projectID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"models": models,
	})
}

// Live returns near-real-time QPS / token rate / cost rate for a project,
// averaged over the configured window (default 60s). Sourced from Redis
// counters maintained by the collector — approximate, not for billing.
func (h *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	if h.Realtime == nil {
		response.Error(w, http.StatusServiceUnavailable, 50301, "realtime metrics not configured")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		response.Error(w, http.StatusBadRequest, 40000, "project_id required")
		return
	}
	qps, tps, costUSD, err := h.Realtime.ProjectLive(r.Context(), projectID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"project_id":     projectID,
		"qps":            qps,
		"tokens_per_sec": tps,
		"cost_usd_per_s": costUSD,
	})
}

func (h *Handlers) Usage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dim := q.Get("dimension")
	if dim == "" {
		dim = "model"
	}
	stats, err := h.Traces.UsageByDimension(
		r.Context(), dim,
		atoi64(q.Get("start_time")),
		atoi64(q.Get("end_time")),
		q.Get("project_id"),
	)
	if err != nil {
		response.Error(w, http.StatusBadRequest, 40001, err.Error())
		return
	}
	response.OK(w, map[string]any{"dimension": dim, "items": stats})
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func atoi64(s string) int64 {
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

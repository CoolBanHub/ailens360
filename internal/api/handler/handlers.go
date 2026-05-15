package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/CoolBanHub/ailens360/internal/api/response"
	"github.com/CoolBanHub/ailens360/internal/auth"
	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/metrics"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

type Handlers struct {
	Projects  *project.Service
	Resolver  *project.Resolver
	Traces    repo.TraceRepo
	Auth      *auth.Service
	Realtime  *metrics.Realtime
	BodyStore bodystore.Store
	// PublicURL, if set, overrides the request-derived origin used to build
	// proxy_prefix values returned by the project endpoints. Required in
	// production (behind a reverse proxy / different hostnames per role).
	PublicURL string
	// ProxyAddr is the proxy process's listen address (e.g. "0.0.0.0:8080").
	// Used to derive the proxy port when PublicURL is empty — otherwise the
	// derivation would use the request's own port, which is the api listener
	// (e.g. :8081), not the proxy.
	ProxyAddr string
	// PresignRedirect: when true, /api/traces/:id/body 302s to a presigned
	// MinIO URL (browser fetches direct, api is bypassed). When false, the
	// api streams bytes through itself (default; keeps MinIO private).
	PresignRedirect bool
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

// DeleteProject hard-deletes a project and every trace / body object owned by
// it. Step order matters: traces and bodies are wiped first (idempotent on
// retry), then the project row goes, then the resolver cache is invalidated so
// the project_key starts returning 401. If trace or body cleanup fails, the
// project row stays put — the caller can retry the same request.
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
	if _, err := h.Traces.DeleteByProject(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, "delete traces: "+err.Error())
		return
	}
	if h.BodyStore != nil {
		if err := h.BodyStore.DeletePrefix(r.Context(), id+"/"); err != nil {
			response.Error(w, http.StatusInternalServerError, 50001, "delete bodies: "+err.Error())
			return
		}
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
// origin of the proxy listener; clients construct their upstream URL by joining
// the prefix and the full upstream URL with a single `/`:
//
//	client.base_url = "{proxy_prefix}/{your_real_upstream_base_url}"
//	e.g. "http://localhost:8080/https://api.openai.com/v1"
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
		return h.PublicURL
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
	// The request hit the api process, so r.Host carries the api port. Swap
	// in the proxy port so the generated examples point to the right listener.
	if h.ProxyAddr != "" {
		host = swapPort(host, listenPort(h.ProxyAddr))
	}
	return scheme + "://" + host
}

// listenPort extracts the port from an address like "0.0.0.0:8080" / "[::]:8080" / ":8080".
// Returns "" if it can't, leaving the original host untouched in swapPort.
func listenPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return ""
}

func swapPort(hostPort, newPort string) string {
	if newPort == "" {
		return hostPort
	}
	// Strip any existing port. Use LastIndex so IPv6 brackets are preserved.
	i := strings.LastIndex(hostPort, ":")
	bracketed := strings.HasPrefix(hostPort, "[")
	if i > 0 && (!bracketed || strings.Contains(hostPort[i:], "]:")) {
		hostPort = hostPort[:i]
	}
	return hostPort + ":" + newPort
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

// GetTraceBody hands the browser the raw request or response body. Behaviour
// depends on PresignRedirect:
//   - true  → 302 redirect to a presigned MinIO URL; browser fetches direct.
//   - false → bytes are streamed from MinIO through this handler. The
//     ContentEncoding header (typically "gzip") is forwarded as-is so the
//     browser's decoder handles decompression.
func (h *Handlers) GetTraceBody(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	part := r.URL.Query().Get("part")
	if part != "request" && part != "response" {
		response.Error(w, http.StatusBadRequest, 40000, "part must be 'request' or 'response'")
		return
	}
	if h.BodyStore == nil {
		response.Error(w, http.StatusServiceUnavailable, 50300, "body store unavailable")
		return
	}
	t, err := h.Traces.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			response.Error(w, http.StatusNotFound, 40400, "trace not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	var key string
	if part == "request" {
		key = t.RequestBodyKey
	} else {
		key = t.ResponseBodyKey
	}
	if key == "" {
		response.Error(w, http.StatusNotFound, 40401, "body not stored for this trace (upload may have failed)")
		return
	}

	if h.PresignRedirect {
		u, err := h.BodyStore.PresignGet(r.Context(), key)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, 50002, "presign failed: "+err.Error())
			return
		}
		http.Redirect(w, r, u, http.StatusFound)
		return
	}

	rc, meta, err := h.BodyStore.Get(r.Context(), key)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50002, "fetch failed: "+err.Error())
		return
	}
	defer rc.Close()
	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	}
	if meta.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", meta.ContentEncoding)
	}
	if meta.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	}
	if _, err := io.Copy(w, rc); err != nil {
		// Body already partially sent; just log via the chi recovery layer.
		// We can't write an error response at this point — headers are flushed.
		return
	}
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

// Facets returns the dynamic filter inputs and an existence flag for the
// trace-list UI:
//   - `models`: distinct non-empty model names seen in the project, ordered
//     by frequency (desc), capped at 200 entries — populates the dropdown.
//   - `has_data`: whether the project has any trace at all (including ones
//     with empty model) — lets the UI tell "no traces yet" apart from
//     "filters exclude everything".
func (h *Handlers) Facets(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		response.Error(w, http.StatusBadRequest, 40000, "project_id required")
		return
	}
	models, hasAny, err := h.Traces.Facets(r.Context(), projectID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"models":   models,
		"has_data": hasAny,
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

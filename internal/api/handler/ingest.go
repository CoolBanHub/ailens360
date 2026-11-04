package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/internal/api/response"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// maxIngestBody bounds the JSON payload; a single ingest request is expected
// to carry a handful of agent/tool spans, not a bulk export.
const maxIngestBody = 4 << 20

// ingestSpan is the wire format for POST /api/ingest/spans. LLM spans are
// normally captured by the proxy, so this endpoint mainly carries agent and
// tool spans (which never traverse the proxy). model/token fields are accepted
// so custom LLM wrappers can also report spans directly.
type ingestSpan struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	AgentRole    string `json:"agent_role"`
	AgentPath    string `json:"agent_path"`

	StartTime int64 `json:"start_time"` // Unix milliseconds
	EndTime   int64 `json:"end_time"`   // Unix milliseconds; 0 = still running

	Status       string `json:"status"` // success | error | aborted
	StatusCode   int    `json:"status_code"`
	ErrorMessage string `json:"error_message"`

	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Tags      string `json:"tags"`
	Model     string `json:"model"`

	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	ReasoningTokens          int     `json:"reasoning_tokens"`
	CachedInputTokens        int     `json:"cached_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`

	Attributes json.RawMessage `json:"attributes"`
}

type ingestSpansRequest struct {
	TraceName string       `json:"trace_name"`
	Spans     []ingestSpan `json:"spans"`
}

// IngestSpans accepts agent/tool spans from an instrumented runtime. It is
// authenticated by the project key (not the admin JWT) because it is called
// by application code, exactly like the proxy.
func (h *Handlers) IngestSpans(w http.ResponseWriter, r *http.Request) {
	projectKey := strings.TrimSpace(r.Header.Get("X-AILens-Project-Key"))
	if projectKey == "" {
		projectKey = strings.TrimSpace(r.URL.Query().Get("sk"))
	}
	if projectKey == "" {
		response.Error(w, http.StatusUnauthorized, 40101, "missing project key")
		return
	}
	proj, err := h.Resolver.Resolve(r.Context(), projectKey)
	if err != nil {
		if errors.Is(err, project.ErrProjectNotFound) {
			response.Error(w, http.StatusNotFound, 40400, "project not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50001, err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxIngestBody+1))
	if err != nil {
		response.Error(w, http.StatusBadRequest, 40000, "read body: "+err.Error())
		return
	}
	if len(body) > maxIngestBody {
		response.Error(w, http.StatusRequestEntityTooLarge, 41300, "body too large")
		return
	}

	var req ingestSpansRequest
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		// Convenience: a bare array of spans.
		if err := json.Unmarshal(body, &req.Spans); err != nil {
			response.Error(w, http.StatusBadRequest, 40000, "invalid json: "+err.Error())
			return
		}
	} else {
		if err := json.Unmarshal(body, &req); err != nil {
			response.Error(w, http.StatusBadRequest, 40000, "invalid json: "+err.Error())
			return
		}
	}
	if len(req.Spans) == 0 {
		response.Error(w, http.StatusBadRequest, 40000, "spans is required")
		return
	}
	if len(req.Spans) > 1000 {
		response.Error(w, http.StatusBadRequest, 40000, "too many spans (max 1000)")
		return
	}

	now := time.Now()
	accepted := 0
	for _, in := range req.Spans {
		tr, err := buildIngestedTrace(proj.ID, req.TraceName, in, now)
		if err != nil {
			response.Error(w, http.StatusBadRequest, 40001, err.Error())
			return
		}
		if err := h.Traces.Upsert(r.Context(), tr); err != nil {
			response.Error(w, http.StatusInternalServerError, 50002, err.Error())
			return
		}
		accepted++
	}
	response.OK(w, map[string]any{"accepted": accepted})
}

func buildIngestedTrace(projectID, traceName string, in ingestSpan, now time.Time) (*repo.Trace, error) {
	in.SpanID = clipIngest(in.SpanID, 128)
	if in.SpanID == "" {
		return nil, errors.New("span_id is required")
	}
	in.TraceID = clipIngest(in.TraceID, 256)
	if in.TraceID == "" {
		in.TraceID = in.SpanID
	}
	in.ParentSpanID = clipIngest(in.ParentSpanID, 128)
	traceName = clipIngest(traceName, 256)
	in.Name = clipIngest(in.Name, 256)
	in.AgentID = clipIngest(in.AgentID, 256)
	in.AgentName = clipIngest(in.AgentName, 256)
	in.AgentPath = clipIngest(in.AgentPath, 512)
	in.UserID = clipIngest(in.UserID, 256)
	in.SessionID = clipIngest(in.SessionID, 256)
	in.Tags = clipIngest(in.Tags, 512)
	in.Model = clipIngest(in.Model, 256)
	in.ErrorMessage = clipIngest(in.ErrorMessage, 2048)
	if in.StartTime <= 0 {
		in.StartTime = now.UnixMilli()
	}
	if in.EndTime < in.StartTime {
		return nil, errors.New("end_time must be >= start_time")
	}
	latency := int64(0)
	if in.EndTime > 0 {
		latency = in.EndTime - in.StartTime
	}

	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		if in.AgentName != "" || in.AgentID != "" {
			kind = repo.SpanKindAgent
		} else {
			kind = repo.SpanKindTool
		}
	}
	switch kind {
	case repo.SpanKindAgent, repo.SpanKindTool, repo.SpanKindLLM:
	default:
		return nil, errors.New("kind must be agent, llm or tool")
	}

	role := strings.ToLower(strings.TrimSpace(in.AgentRole))
	switch role {
	case "", repo.AgentRoleMain, repo.AgentRoleSub, repo.AgentRoleSystem:
	default:
		return nil, errors.New("agent_role must be main, sub or system")
	}

	status := strings.ToLower(strings.TrimSpace(in.Status))
	switch status {
	case "":
		status = "success"
	case "success", "error", "aborted":
	default:
		return nil, errors.New("status must be success, error or aborted")
	}

	attrs := "{}"
	if len(in.Attributes) > 0 {
		if !json.Valid(in.Attributes) {
			return nil, errors.New("attributes must be valid json")
		}
		if len(in.Attributes) > 64<<10 {
			return nil, errors.New("attributes too large (max 64KiB)")
		}
		attrs = string(in.Attributes)
	}

	agentPath := strings.TrimSpace(in.AgentPath)
	if agentPath == "" && in.AgentName != "" {
		agentPath = "/" + in.AgentName
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = kind
	}

	total := in.InputTokens + in.OutputTokens
	return &repo.Trace{
		ID:                       in.SpanID,
		TraceID:                  in.TraceID,
		TraceName:                traceName,
		ProjectID:                projectID,
		UserID:                   in.UserID,
		SessionID:                in.SessionID,
		Tags:                     in.Tags,
		Model:                    in.Model,
		SpanKind:                 kind,
		SpanName:                 name,
		ParentSpanID:             in.ParentSpanID,
		AgentID:                  in.AgentID,
		AgentName:                in.AgentName,
		AgentRole:                role,
		AgentPath:                agentPath,
		Attributes:               attrs,
		Status:                   status,
		StatusCode:               in.StatusCode,
		ErrorMessage:             in.ErrorMessage,
		InputTokens:              in.InputTokens,
		OutputTokens:             in.OutputTokens,
		TotalTokens:              total,
		ReasoningTokens:          in.ReasoningTokens,
		CachedInputTokens:        in.CachedInputTokens,
		CacheCreationInputTokens: in.CacheCreationInputTokens,
		CostUSD:                  in.CostUSD,
		LatencyMs:                latency,
		CreatedAt:                time.UnixMilli(in.StartTime),
	}, nil
}

// clipIngest bounds client-supplied strings to a sane column size, by runes
// so multi-byte names stay valid UTF-8.
func clipIngest(v string, n int) string {
	v = strings.TrimSpace(v)
	if len(v) <= n {
		return v
	}
	r := []rune(v)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

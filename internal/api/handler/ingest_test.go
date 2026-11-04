package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

func TestBuildIngestedTraceAgentSpan(t *testing.T) {
	now := time.Now()
	tr, err := buildIngestedTrace("prj_1", "task_123", ingestSpan{
		TraceID:      "task_123",
		SpanID:       "sp_worker_1",
		ParentSpanID: "sp_root",
		AgentID:      "mtunion.worker",
		AgentName:    "worker",
		AgentRole:    "main",
		AgentPath:    "/main/worker",
		StartTime:    now.UnixMilli(),
		EndTime:      now.Add(1500 * time.Millisecond).UnixMilli(),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if tr.SpanKind != "agent" || tr.SpanName != "agent" {
		t.Fatalf("kind/name = %q/%q", tr.SpanKind, tr.SpanName)
	}
	if tr.LatencyMs != 1500 {
		t.Fatalf("latency = %d", tr.LatencyMs)
	}
	if tr.ParentSpanID != "sp_root" || tr.AgentName != "worker" || tr.AgentRole != "main" {
		t.Fatalf("tree fields = %+v", tr)
	}
	if tr.Attributes != "{}" || tr.Status != "success" {
		t.Fatalf("defaults = %q/%q", tr.Attributes, tr.Status)
	}
}

func TestBuildIngestedTraceDefaultsKindToTool(t *testing.T) {
	now := time.Now()
	tr, err := buildIngestedTrace("prj_1", "", ingestSpan{
		SpanID:    "sp_tool_1",
		Name:      "mtag_execute",
		StartTime: now.UnixMilli(),
		EndTime:   now.UnixMilli(),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if tr.SpanKind != "tool" || tr.SpanName != "mtag_execute" {
		t.Fatalf("kind/name = %q/%q", tr.SpanKind, tr.SpanName)
	}
	if tr.TraceID != "sp_tool_1" {
		t.Fatalf("trace id = %q, want span id fallback", tr.TraceID)
	}
}

func TestBuildIngestedTraceAttributesAndTokens(t *testing.T) {
	now := time.Now()
	tr, err := buildIngestedTrace("prj_1", "", ingestSpan{
		SpanID:       "sp_llm_1",
		Kind:         "llm",
		StartTime:    now.UnixMilli(),
		EndTime:      now.Add(time.Second).UnixMilli(),
		InputTokens:  10,
		OutputTokens: 5,
		Attributes:   json.RawMessage(`{"purpose":"worker"}`),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if tr.TotalTokens != 15 {
		t.Fatalf("total tokens = %d", tr.TotalTokens)
	}
	if tr.Attributes != `{"purpose":"worker"}` {
		t.Fatalf("attributes = %q", tr.Attributes)
	}
}

func TestBuildIngestedTraceValidation(t *testing.T) {
	now := time.Now()
	cases := []ingestSpan{
		{SpanID: ""},
		{SpanID: "sp_1", StartTime: now.UnixMilli(), EndTime: now.Add(-time.Second).UnixMilli()},
		{SpanID: "sp_1", Kind: "banana", StartTime: now.UnixMilli()},
		{SpanID: "sp_1", AgentRole: "boss", StartTime: now.UnixMilli()},
		{SpanID: "sp_1", Status: "weird", StartTime: now.UnixMilli()},
		{SpanID: "sp_1", Attributes: json.RawMessage(`not-json`), StartTime: now.UnixMilli()},
	}
	for i, in := range cases {
		if _, err := buildIngestedTrace("prj_1", "", in, now); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

type ingestTraceRepo struct {
	upserted []*repo.Trace
}

func (r *ingestTraceRepo) Create(context.Context, *repo.Trace) error        { return nil }
func (r *ingestTraceRepo) BatchCreate(context.Context, []*repo.Trace) error { return nil }
func (r *ingestTraceRepo) Upsert(_ context.Context, t *repo.Trace) error {
	r.upserted = append(r.upserted, t)
	return nil
}
func (r *ingestTraceRepo) GetByID(context.Context, string) (*repo.Trace, error) {
	return nil, repo.ErrNotFound
}
func (r *ingestTraceRepo) List(context.Context, repo.ListTraceFilter) ([]*repo.Trace, int64, error) {
	return nil, 0, nil
}
func (r *ingestTraceRepo) ListGroups(context.Context, repo.ListTraceGroupFilter) ([]*repo.TraceGroup, int64, error) {
	return nil, 0, nil
}
func (r *ingestTraceRepo) UsageByDimension(context.Context, string, int64, int64, string) ([]repo.UsageStat, error) {
	return nil, nil
}
func (r *ingestTraceRepo) Facets(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}
func (r *ingestTraceRepo) DeleteByProject(context.Context, string) (int64, error) { return 0, nil }

type ingestProjectRepo struct{ project *repo.Project }

func (r *ingestProjectRepo) Create(context.Context, *repo.Project) error { return nil }
func (r *ingestProjectRepo) GetByID(context.Context, string) (*repo.Project, error) {
	return r.project, nil
}
func (r *ingestProjectRepo) GetByProjectKey(_ context.Context, key string) (*repo.Project, error) {
	if r.project != nil && r.project.ProjectKey == key {
		return r.project, nil
	}
	return nil, repo.ErrNotFound
}
func (r *ingestProjectRepo) List(context.Context) ([]*repo.Project, error) { return nil, nil }
func (r *ingestProjectRepo) Update(context.Context, *repo.Project) error   { return nil }
func (r *ingestProjectRepo) UpdateProjectKey(context.Context, string, string) error {
	return nil
}
func (r *ingestProjectRepo) Delete(context.Context, string) error { return nil }

type ingestProjectCache struct{}

func (ingestProjectCache) Get(context.Context, string) (*repo.Project, bool, error) {
	return nil, false, nil
}
func (ingestProjectCache) Set(context.Context, string, *repo.Project) error { return nil }
func (ingestProjectCache) Delete(context.Context, string) error             { return nil }
func (ingestProjectCache) Close() error                                     { return nil }

func TestIngestSpansAcceptsProjectKey(t *testing.T) {
	traceRepo := &ingestTraceRepo{}
	h := &Handlers{
		Resolver: project.NewResolver(
			&ingestProjectRepo{project: &repo.Project{ID: "prj_1", ProjectKey: "sk-test"}},
			ingestProjectCache{},
		),
		Traces: traceRepo,
	}
	body := `{"trace_name":"feishu.turn","spans":[{"trace_id":"task_1","span_id":"sp_worker_1","kind":"agent","name":"worker.run","agent_name":"worker","agent_role":"main","agent_path":"/main/worker","start_time":1700000000000,"end_time":1700000001500}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ingest/spans", strings.NewReader(body))
	req.Header.Set("X-AILens-Project-Key", "sk-test")
	rec := httptest.NewRecorder()

	h.IngestSpans(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(traceRepo.upserted) != 1 {
		t.Fatalf("upserted = %d", len(traceRepo.upserted))
	}
	got := traceRepo.upserted[0]
	if got.ProjectID != "prj_1" || got.TraceName != "feishu.turn" {
		t.Fatalf("trace = %+v", got)
	}
	if got.SpanKind != "agent" || got.AgentRole != "main" || got.LatencyMs != 1500 {
		t.Fatalf("span = %+v", got)
	}
}

func TestIngestSpansRequiresProjectKey(t *testing.T) {
	h := &Handlers{Traces: &ingestTraceRepo{}}
	req := httptest.NewRequest(http.MethodPost, "/api/ingest/spans", strings.NewReader(`{"spans":[]}`))
	rec := httptest.NewRecorder()
	h.IngestSpans(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

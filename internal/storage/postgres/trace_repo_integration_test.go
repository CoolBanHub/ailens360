package postgres

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CoolBanHub/ailens360/internal/partition"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

// TestTraceRepoSpanTree is an opt-in integration test. It needs a disposable
// Postgres database because it creates/drops partitions.
//
//	AILENS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/ailens?sslmode=disable' \
//	  go test ./internal/storage/postgres -run TestTraceRepoSpanTree -v
func TestTraceRepoSpanTree(t *testing.T) {
	dsn := os.Getenv("AILENS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AILENS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Open(ctx, dsn, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := partition.New(pool, partition.Config{PreCreate: 1}, slog.Default(), nil, nil).Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	traceRepo := NewTraceRepo(pool)
	const projectID = "prj_span_tree_test"
	if _, err := pool.Exec(ctx, `DELETE FROM traces WHERE project_id=$1`, projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM traces WHERE project_id=$1`, projectID)
	})

	start := time.Now().Add(-3 * time.Second).Truncate(time.Millisecond)
	upsert := func(tr *repo.Trace) {
		t.Helper()
		if err := traceRepo.Upsert(ctx, tr); err != nil {
			t.Fatalf("upsert %s: %v", tr.ID, err)
		}
	}
	upsert(&repo.Trace{
		ID: "sp_root", TraceID: "task_1", ProjectID: projectID,
		SpanKind: "agent", SpanName: "worker.run", AgentID: "mtunion.worker",
		AgentName: "worker", AgentRole: "main", AgentPath: "/main/worker",
		Status: "success", LatencyMs: 3000, CreatedAt: start,
		Attributes: `{"turn":1}`,
	})
	upsert(&repo.Trace{
		ID: "tr_1", TraceID: "task_1", ParentSpanID: "sp_root", ProjectID: projectID,
		SpanKind: "llm", SpanName: "worker.llm", Model: "gpt-4o",
		AgentID: "mtunion.worker", AgentName: "worker", AgentRole: "main", AgentPath: "/main/worker",
		Status: "success", LatencyMs: 1000, CreatedAt: start.Add(500 * time.Millisecond),
		InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostUSD: 0.01,
	})
	upsert(&repo.Trace{
		ID: "sp_sub", TraceID: "task_1", ParentSpanID: "sp_root", ProjectID: projectID,
		SpanKind: "agent", SpanName: "researcher.run", AgentID: "mtunion.researcher",
		AgentName: "researcher", AgentRole: "sub", AgentPath: "/main/worker/researcher",
		Status: "success", LatencyMs: 1500, CreatedAt: start.Add(800 * time.Millisecond),
	})
	upsert(&repo.Trace{
		ID: "tr_2", TraceID: "task_1", ParentSpanID: "sp_sub", ProjectID: projectID,
		SpanKind: "llm", SpanName: "researcher.llm", Model: "gpt-4o",
		AgentID: "mtunion.researcher", AgentName: "researcher", AgentRole: "sub", AgentPath: "/main/worker/researcher",
		Status: "success", LatencyMs: 700, CreatedAt: start.Add(1000 * time.Millisecond),
		InputTokens: 20, OutputTokens: 10, TotalTokens: 30, CostUSD: 0.02,
	})

	// Idempotent close of the root span: same id + start_time, new end/status.
	upsert(&repo.Trace{
		ID: "sp_root", TraceID: "task_1", ProjectID: projectID,
		SpanKind: "agent", SpanName: "worker.run", AgentName: "worker", AgentRole: "main",
		AgentPath: "/main/worker", Status: "error", LatencyMs: 3200, CreatedAt: start,
	})

	spans, total, err := traceRepo.List(ctx, repo.ListTraceFilter{ProjectID: projectID, TraceID: "task_1", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(spans) != 4 {
		t.Fatalf("spans = %d/%d, want 4", len(spans), total)
	}
	var root *repo.Trace
	for _, s := range spans {
		if s.ID == "sp_root" {
			root = s
		}
	}
	if root == nil || root.LatencyMs != 3200 || root.Status != "error" {
		t.Fatalf("upsert did not update root: %+v", root)
	}

	groups, gtotal, err := traceRepo.ListGroups(ctx, repo.ListTraceGroupFilter{ProjectID: projectID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if gtotal != 1 || len(groups) != 1 {
		t.Fatalf("groups = %d/%d, want 1", len(groups), gtotal)
	}
	g := groups[0]
	if g.SpanCount != 2 {
		t.Fatalf("span_count = %d, want 2 llm calls", g.SpanCount)
	}
	if g.AgentCount != 2 || !strings.Contains(g.Agents, "worker") || !strings.Contains(g.Agents, "researcher") {
		t.Fatalf("agents = %d/%q", g.AgentCount, g.Agents)
	}
	if g.TotalTokens != 45 || g.CostUSD != 0.03 {
		t.Fatalf("usage = %d tok / %f usd", g.TotalTokens, g.CostUSD)
	}
	if g.LatencyMs < 3000 {
		t.Fatalf("latency = %d, want wall-clock >= 3000", g.LatencyMs)
	}
	if g.Status != "error" {
		t.Fatalf("status = %q, want error from agent span", g.Status)
	}

	usage, err := traceRepo.UsageByDimension(ctx, "agent", 0, 0, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 2 {
		t.Fatalf("usage rows = %d, want 2 (agent/tool spans excluded)", len(usage))
	}
	for _, u := range usage {
		if u.Calls != 1 {
			t.Fatalf("agent %q calls = %d, want 1", u.Key, u.Calls)
		}
	}

	// Create + BatchCreate still use the full 46-column insert path; the
	// collector's hot path is CopyFrom, so exercise both.
	if err := traceRepo.Create(ctx, &repo.Trace{
		ID: "tr_create", TraceID: "task_create", ProjectID: projectID,
		Model: "gpt-4o", Status: "success", CreatedAt: start,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := traceRepo.BatchCreate(ctx, []*repo.Trace{
		{ID: "tr_batch_1", TraceID: "task_batch", ProjectID: projectID, Model: "gpt-4o", Status: "success", CreatedAt: start},
		{ID: "tr_batch_2", TraceID: "task_batch", ProjectID: projectID, Model: "gpt-4o", Status: "success", CreatedAt: start},
	}); err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if _, n, err := traceRepo.List(ctx, repo.ListTraceFilter{ProjectID: projectID, TraceID: "task_batch", Limit: 10}); err != nil || n != 2 {
		t.Fatalf("batch rows = %d, err=%v", n, err)
	}
	if got, err := traceRepo.GetByID(ctx, "tr_create"); err != nil || got.ID != "tr_create" || got.SpanKind != "llm" {
		t.Fatalf("get by id = %+v, err=%v", got, err)
	}

	models, hasAny, err := traceRepo.Facets(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAny || len(models) != 1 || models[0] != "gpt-4o" {
		t.Fatalf("facets = %v/%v", models, hasAny)
	}
}

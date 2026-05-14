package collector

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

type recordingTraceRepo struct {
	mu     sync.Mutex
	traces []*repo.Trace
}

func (r *recordingTraceRepo) Create(context.Context, *repo.Trace) error { return nil }

func (r *recordingTraceRepo) BatchCreate(_ context.Context, traces []*repo.Trace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.traces = append(r.traces, traces...)
	return nil
}

func (r *recordingTraceRepo) GetByID(context.Context, string) (*repo.Trace, error) {
	return nil, repo.ErrNotFound
}

func (r *recordingTraceRepo) List(context.Context, repo.ListTraceFilter) ([]*repo.Trace, int64, error) {
	return nil, 0, nil
}

func (r *recordingTraceRepo) ListGroups(context.Context, repo.ListTraceGroupFilter) ([]*repo.TraceGroup, int64, error) {
	return nil, 0, nil
}

func (r *recordingTraceRepo) UsageByDimension(context.Context, string, int64, int64, string) ([]repo.UsageStat, error) {
	return nil, nil
}

func (r *recordingTraceRepo) Facets(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}

func (r *recordingTraceRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.traces)
}

func TestStopDrainsSubmittedEvents(t *testing.T) {
	traces := &recordingTraceRepo{}
	p := New(Config{BufferSize: 8, BatchSize: 10}, slog.Default(), traces, pricing.NewCatalog(), tokenizer.New(), nil)
	p.Start(context.Background())

	for i := 0; i < 5; i++ {
		p.Submit(&stream.Event{TraceID: "tr_test", ProjectID: "prj_1"})
	}
	p.Stop()

	if got := traces.count(); got != 5 {
		t.Fatalf("persisted traces = %d, want 5", got)
	}
}

package collector

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

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

func TestTransformerCarriesBodyKeysAndEstimatesOutputTokens(t *testing.T) {
	tx := NewTransformer(slog.Default(), pricing.NewCatalog(), tokenizer.New())
	ev := &stream.Event{
		TraceID:          "tr_1",
		Model:            "gpt-4o-mini",
		ProjectID:        "prj_1",
		Status:           "success",
		StatusCode:       200,
		ResponseText:     "hello world from the model",
		RequestBodyKey:   "prj_1/202605/tr_1/request.json",
		RequestBodySize:  123,
		ResponseBodyKey:  "prj_1/202605/tr_1/response.bin",
		ResponseBodySize: 456,
		Timeline: stream.Timeline{
			RequestIn:   time.Now(),
			ResponseOut: time.Now().Add(50 * time.Millisecond),
		},
	}

	tr := tx.Transform(ev)

	if tr.ID != "tr_1" {
		t.Errorf("ID = %q", tr.ID)
	}
	if tr.RequestBodyKey != "prj_1/202605/tr_1/request.json" || tr.RequestBodySize != 123 {
		t.Errorf("request body key/size mismatch: %q/%d", tr.RequestBodyKey, tr.RequestBodySize)
	}
	if tr.ResponseBodyKey != "prj_1/202605/tr_1/response.bin" || tr.ResponseBodySize != 456 {
		t.Errorf("response body key/size mismatch: %q/%d", tr.ResponseBodyKey, tr.ResponseBodySize)
	}
	if tr.OutputTokens == 0 {
		t.Error("expected estimated output tokens > 0 when usage missing")
	}
	if !tr.TokensEstimated {
		t.Error("expected TokensEstimated=true after fallback")
	}
	if tr.LatencyMs <= 0 {
		t.Errorf("latency_ms = %d", tr.LatencyMs)
	}
}

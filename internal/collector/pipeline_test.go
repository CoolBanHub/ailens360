package collector

import (
	"log/slog"
	"testing"
	"time"

	"github.com/CoolBanHub/ailens360/internal/pricing"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/tokenizer"
)

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

func TestTransformerCarriesSpanTreeFields(t *testing.T) {
	tx := NewTransformer(slog.Default(), pricing.NewCatalog(), tokenizer.New())
	ev := &stream.Event{
		TraceID:      "tr_span",
		LogicTraceID: "task_123",
		Model:        "gpt-4o-mini",
		ProjectID:    "prj_1",
		Status:       "success",
		ParentSpanID: "sp_worker_1",
		SpanKind:     "llm",
		SpanName:     "worker.llm",
		AgentID:      "mtunion.worker",
		AgentName:    "worker",
		AgentRole:    "main",
		AgentPath:    "/main/worker",
		Timeline:     stream.Timeline{RequestIn: time.Now(), ResponseOut: time.Now().Add(time.Millisecond)},
	}

	tr := tx.Transform(ev)
	if tr.ParentSpanID != "sp_worker_1" || tr.SpanKind != "llm" || tr.SpanName != "worker.llm" {
		t.Fatalf("span fields = %q/%q/%q", tr.ParentSpanID, tr.SpanKind, tr.SpanName)
	}
	if tr.AgentName != "worker" || tr.AgentRole != "main" || tr.AgentPath != "/main/worker" {
		t.Fatalf("agent fields = %+v", tr)
	}
}

func TestTransformerDefaultsToLegacyLLMSpan(t *testing.T) {
	tx := NewTransformer(slog.Default(), pricing.NewCatalog(), tokenizer.New())
	ev := &stream.Event{
		TraceID:   "tr_legacy",
		Model:     "gpt-4o-mini",
		ProjectID: "prj_1",
		Status:    "success",
		Timeline:  stream.Timeline{RequestIn: time.Now(), ResponseOut: time.Now()},
	}
	tr := tx.Transform(ev)
	if tr.SpanKind != "llm" || tr.SpanName != "llm" {
		t.Fatalf("legacy defaults = %q/%q", tr.SpanKind, tr.SpanName)
	}
	if tr.TraceID != "tr_legacy" {
		t.Fatalf("logic trace = %q", tr.TraceID)
	}
}

package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/cache"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

func TestParseProxyPath(t *testing.T) {
	cases := []struct {
		in             string
		wantUpstream   string
		wantProjectKey string
		wantOK         bool
	}{
		{"/https://api.openai.com/v1/chat/completions",
			"https://api.openai.com/v1/chat/completions", "", true},
		{"/https://api.anthropic.com/v1/messages",
			"https://api.anthropic.com/v1/messages", "", true},
		{"/http://localhost:11434/v1/chat/completions",
			"http://localhost:11434/v1/chat/completions", "", true},
		{"/sk-abc/https://api.openai.com/v1/chat/completions",
			"https://api.openai.com/v1/chat/completions", "sk-abc", true},
		{"/p/https://api.openai.com/v1/chat/completions", "", "", false}, // non-sk prefix rejected
		{"/", "", "", false},                       // no upstream
		{"/healthz", "", "", false},                // not a proxy path
		{"/ftp://example.com/file", "", "", false}, // wrong scheme
	}
	for _, c := range cases {
		up, projectKey, ok := parseProxyPath(c.in)
		if ok != c.wantOK || up != c.wantUpstream || projectKey != c.wantProjectKey {
			t.Errorf("parseProxyPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, up, projectKey, ok, c.wantUpstream, c.wantProjectKey, c.wantOK)
		}
	}
}

func TestParseProxyTarget(t *testing.T) {
	cases := []struct {
		name             string
		target           string
		headerProjectKey string
		wantUpstream     string
		wantProjectKey   string
		wantRawQuery     string
	}{
		{
			name:             "header key",
			target:           "/https://api.openai.com/v1/chat/completions?foo=bar",
			headerProjectKey: "sk-header",
			wantUpstream:     "https://api.openai.com/v1/chat/completions",
			wantProjectKey:   "sk-header",
			wantRawQuery:     "foo=bar",
		},
		{
			name:           "path key",
			target:         "/sk-path/https://api.openai.com/v1/chat/completions?foo=bar",
			wantUpstream:   "https://api.openai.com/v1/chat/completions",
			wantProjectKey: "sk-path",
			wantRawQuery:   "foo=bar",
		},
		{
			name:           "query key stripped from upstream query",
			target:         "/https://api.openai.com/v1/chat/completions?foo=bar&sk=sk-query",
			wantUpstream:   "https://api.openai.com/v1/chat/completions",
			wantProjectKey: "sk-query",
			wantRawQuery:   "foo=bar",
		},
		{
			name:             "header key keeps sk query for upstream",
			target:           "/https://api.openai.com/v1/chat/completions?foo=bar&sk=upstream-sk",
			headerProjectKey: "sk-header",
			wantUpstream:     "https://api.openai.com/v1/chat/completions",
			wantProjectKey:   "sk-header",
			wantRawQuery:     "foo=bar&sk=upstream-sk",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, c.target, nil)
			if c.headerProjectKey != "" {
				req.Header.Set("X-AILens-Project-Key", c.headerProjectKey)
			}

			got, ok := parseProxyTarget(req)
			if !ok {
				t.Fatal("parseProxyTarget ok = false, want true")
			}
			if got.upstreamRaw != c.wantUpstream || got.projectKey != c.wantProjectKey || got.rawQuery != c.wantRawQuery {
				t.Fatalf("parseProxyTarget = (%q, %q, %q), want (%q, %q, %q)",
					got.upstreamRaw, got.projectKey, got.rawQuery,
					c.wantUpstream, c.wantProjectKey, c.wantRawQuery)
			}
		})
	}
}

func TestServeRejectsRequestBodyOverConfiguredLimit(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h := NewHandler(Deps{
		Resolver: project.NewResolver(
			&proxyProjectRepo{project: &repo.Project{ID: "prj_1", ProjectKey: "abc"}},
			&proxyProjectCache{project: &repo.Project{ID: "prj_1", ProjectKey: "abc"}},
		),
		Sink:      noopSink{},
		BodyStore: noopStore{},
		RawLimit:  1024,
		MaxBody:   4,
	})
	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL, strings.NewReader("12345"))
	req.Header.Set("X-AILens-Project-Key", "abc")
	rec := httptest.NewRecorder()

	h.serve(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := atomic.LoadInt32(&upstreamHits); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
}

func TestServeAcceptsPathProjectKey(t *testing.T) {
	var gotPath string
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	h := newTestHandler("sk-path")
	req := httptest.NewRequest(http.MethodPost, "/sk-path/"+upstream.URL+"/v1/chat/completions?foo=bar", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	h.serve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/chat/completions")
	}
	if gotQuery != "foo=bar" {
		t.Fatalf("upstream query = %q, want %q", gotQuery, "foo=bar")
	}
}

func TestServeAcceptsQueryProjectKeyAndStripsItFromUpstream(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	h := newTestHandler("sk-query")
	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/chat/completions?foo=bar&sk=sk-query", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	h.serve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotQuery != "foo=bar" {
		t.Fatalf("upstream query = %q, want %q", gotQuery, "foo=bar")
	}
}

func TestServeUsesMetadataSessionIDAsTraceFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	sink := &captureSink{}
	h := newTestHandlerWithSink("sk-test", sink)
	body := `{"model":"glm-5.2","messages":[],"metadata":{"user_id":"{\"device_id\":\"dev_1\",\"account_uuid\":\"\",\"session_id\":\"sess_1\"}"}}`
	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-AILens-Project-Key", "sk-test")
	rec := httptest.NewRecorder()

	h.serve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if sink.ev == nil {
		t.Fatal("no event submitted")
	}
	if sink.ev.SessionID != "sess_1" {
		t.Fatalf("session_id = %q, want sess_1", sink.ev.SessionID)
	}
	if sink.ev.LogicTraceID != "sess_1" {
		t.Fatalf("logic_trace_id = %q, want sess_1", sink.ev.LogicTraceID)
	}
	if sink.ev.UserID != "dev_1" {
		t.Fatalf("user_id = %q, want dev_1", sink.ev.UserID)
	}
}

func TestServeDoesNotMarkCompletedResponsesStreamAborted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"model":"gpt-5.5","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"model":"gpt-5.5","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`+"\n\n")
	}))
	defer upstream.Close()

	sink := &captureSink{}
	h := newTestHandlerWithSink("sk-test", sink)
	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true}`))
	req.Header.Set("X-AILens-Project-Key", "sk-test")
	rec := httptest.NewRecorder()

	h.serve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if sink.ev == nil {
		t.Fatal("no event submitted")
	}
	if sink.ev.Status != "success" || sink.ev.StreamStatus != "completed" {
		t.Fatalf("event status = %s/%s, want success/completed; err=%q", sink.ev.Status, sink.ev.StreamStatus, sink.ev.ErrorMsg)
	}
	if sink.ev.FinishReason != "completed" {
		t.Fatalf("finish_reason = %q, want completed", sink.ev.FinishReason)
	}
	if sink.ev.ErrorMsg != "" {
		t.Fatalf("error msg = %q, want empty", sink.ev.ErrorMsg)
	}
}

type noopSink struct{}

func (noopSink) Submit(*stream.Event) {}

type captureSink struct {
	ev *stream.Event
}

func (s *captureSink) Submit(ev *stream.Event) { s.ev = ev }

type noopStore struct{}

func (noopStore) UploadBytes(context.Context, string, []byte, string) (int64, error) {
	return 0, nil
}
func (noopStore) NewStreamingUploader(context.Context, string, string) (bodystore.StreamingUploader, error) {
	return discardUploader{}, nil
}
func (noopStore) Get(context.Context, string) (io.ReadCloser, bodystore.ObjectMeta, error) {
	return nil, bodystore.ObjectMeta{}, errors.New("noop")
}
func (noopStore) PresignGet(context.Context, string) (string, error) {
	return "", errors.New("noop")
}
func (noopStore) EnsureBucket(context.Context) error         { return nil }
func (noopStore) DeletePrefix(context.Context, string) error { return nil }

type discardUploader struct{}

func (discardUploader) Write(p []byte) (int, error) { return io.Discard.Write(p) }
func (discardUploader) Close() error                { return nil }
func (discardUploader) BytesWritten() int64         { return 0 }

func newTestHandler(projectKey string) *Handler {
	return newTestHandlerWithSink(projectKey, noopSink{})
}

func newTestHandlerWithSink(projectKey string, sink Submitter) *Handler {
	return NewHandler(Deps{
		Resolver: project.NewResolver(
			&proxyProjectRepo{project: &repo.Project{ID: "prj_1", ProjectKey: projectKey}},
			&proxyProjectCache{project: &repo.Project{ID: "prj_1", ProjectKey: projectKey}},
		),
		Sink:      sink,
		BodyStore: noopStore{},
		RawLimit:  1024,
		MaxBody:   1024,
	})
}

type proxyProjectRepo struct {
	project *repo.Project
}

func (r *proxyProjectRepo) Create(context.Context, *repo.Project) error { return nil }
func (r *proxyProjectRepo) GetByID(context.Context, string) (*repo.Project, error) {
	return r.project, nil
}
func (r *proxyProjectRepo) List(context.Context) ([]*repo.Project, error)          { return nil, nil }
func (r *proxyProjectRepo) Update(context.Context, *repo.Project) error            { return nil }
func (r *proxyProjectRepo) UpdateProjectKey(context.Context, string, string) error { return nil }
func (r *proxyProjectRepo) Delete(context.Context, string) error                   { return nil }
func (r *proxyProjectRepo) GetByProjectKey(_ context.Context, key string) (*repo.Project, error) {
	if r.project != nil && r.project.ProjectKey == key {
		return r.project, nil
	}
	return nil, repo.ErrNotFound
}

type proxyProjectCache struct {
	project *repo.Project
}

var _ cache.Cache[*repo.Project] = (*proxyProjectCache)(nil)

func (c *proxyProjectCache) Get(context.Context, string) (*repo.Project, bool, error) {
	return c.project, c.project != nil, nil
}
func (c *proxyProjectCache) Set(context.Context, string, *repo.Project) error { return nil }
func (c *proxyProjectCache) Delete(context.Context, string) error             { return nil }
func (c *proxyProjectCache) Close() error                                     { return nil }

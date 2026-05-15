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
		in           string
		wantUpstream string
		wantOK       bool
	}{
		{"/https://api.openai.com/v1/chat/completions",
			"https://api.openai.com/v1/chat/completions", true},
		{"/https://api.anthropic.com/v1/messages",
			"https://api.anthropic.com/v1/messages", true},
		{"/http://localhost:11434/v1/chat/completions",
			"http://localhost:11434/v1/chat/completions", true},
		{"/p/https://api.openai.com/v1/chat/completions", "", false}, // old shape rejected
		{"/", "", false},                       // no upstream
		{"/healthz", "", false},                // not a proxy path
		{"/ftp://example.com/file", "", false}, // wrong scheme
	}
	for _, c := range cases {
		up, ok := parseProxyPath(c.in)
		if ok != c.wantOK || up != c.wantUpstream {
			t.Errorf("parseProxyPath(%q) = (%q, %v), want (%q, %v)",
				c.in, up, ok, c.wantUpstream, c.wantOK)
		}
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

type noopSink struct{}

func (noopSink) Submit(*stream.Event) {}

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
func (noopStore) EnsureBucket(context.Context) error { return nil }

type discardUploader struct{}

func (discardUploader) Write(p []byte) (int, error) { return io.Discard.Write(p) }
func (discardUploader) Close() error                { return nil }
func (discardUploader) BytesWritten() int64         { return 0 }

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

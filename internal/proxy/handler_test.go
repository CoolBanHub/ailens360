package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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
		{"/p/https://api.openai.com/v1/chat/completions",
			"https://api.openai.com/v1/chat/completions", true},
		{"/p/https://api.anthropic.com/v1/messages",
			"https://api.anthropic.com/v1/messages", true},
		{"/p/http://localhost:11434/v1/chat/completions",
			"http://localhost:11434/v1/chat/completions", true},
		{"/p/", "", false},                          // no upstream
		{"/u/abc/foo", "", false},                   // wrong prefix
		{"/p/https:/host/x", "https:/host/x", true}, // collapsed // — handler will reject as not absolute
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
		Sink:     noopSink{},
		RawLimit: 1024,
		MaxBody:  4,
	})
	req := httptest.NewRequest(http.MethodPost, "/p/"+upstream.URL, strings.NewReader("12345"))
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

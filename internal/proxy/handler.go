package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/proxy/intercept"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/pkg/shortid"
)

type Handler struct {
	logger     *slog.Logger
	resolver   *project.Resolver
	sink       EventSink
	rawLimit   int
	maxReqBody int64
	httpClient *http.Client
}

type Deps struct {
	Logger   *slog.Logger
	Resolver *project.Resolver
	Sink     EventSink
	RawLimit int
	MaxBody  int64
	Timeout  time.Duration
}

func NewHandler(d Deps) *Handler {
	limit := d.RawLimit
	if limit <= 0 {
		limit = 256 << 10
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Handler{
		logger:     d.Logger,
		resolver:   d.Resolver,
		sink:       d.Sink,
		rawLimit:   limit,
		maxReqBody: d.MaxBody,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Mount registers the proxy route. We avoid chi's `{var}` capture for the
// upstream URL — chi's wildcard normalises path segments and would collapse the
// `//` in `https://` — so the handler parses r.URL.Path directly.
func (h *Handler) Mount(mux interface {
	HandleFunc(pattern string, h http.HandlerFunc)
}) {
	mux.HandleFunc("/p/*", h.serve)
}

// parseProxyPath extracts the embedded upstream URL from a proxy request path.
// The expected shape is `/p/{upstream_url}` where {upstream_url} starts with a
// scheme such as `https://`. The project the request belongs to is identified
// out-of-band via the X-AILens-Project-Key header, not the path.
func parseProxyPath(p string) (upstream string, ok bool) {
	if !strings.HasPrefix(p, "/p/") {
		return "", false
	}
	rest := p[len("/p/"):]
	if rest == "" {
		return "", false
	}
	return rest, true
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	tl := stream.Timeline{RequestIn: time.Now()}

	upstreamRaw, ok := parseProxyPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_path",
			"expected /p/{upstream_url}, e.g. /p/https://api.openai.com/v1/chat/completions")
		return
	}

	projectKey := r.Header.Get("X-AILens-Project-Key")
	if projectKey == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing_project_key",
			"missing X-AILens-Project-Key header")
		return
	}

	proj, err := h.resolver.Resolve(r.Context(), projectKey)
	if err != nil {
		switch {
		case errors.Is(err, project.ErrProjectNotFound):
			writeJSONError(w, http.StatusNotFound, "project_not_found", "no such project")
		default:
			h.logger.Error("resolve project", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "resolve_failed", err.Error())
		}
		return
	}

	// Preserve the query string the client attached (e.g. ?key=... for Gemini).
	if r.URL.RawQuery != "" {
		upstreamRaw += "?" + r.URL.RawQuery
	}

	upstreamURL, err := url.Parse(upstreamRaw)
	if err != nil || !upstreamURL.IsAbs() || upstreamURL.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_upstream",
			"upstream URL must be absolute with scheme (e.g. https://api.openai.com/...)")
		return
	}

	// Extract telemetry metadata from internal headers BEFORE they get stripped
	// off the outbound request. These promote the X-AILens-* convention to
	// first-class trace fields so the UI can filter by user / session.
	userID := r.Header.Get("X-AILens-User")
	sessionID := r.Header.Get("X-AILens-Session")
	tags := r.Header.Get("X-AILens-Tag")
	logicTraceID := r.Header.Get("X-AILens-Trace-Id")
	traceName := r.Header.Get("X-AILens-Trace-Name")

	// Snapshot request headers + body before sending. Limit body size.
	var reqBodyCopy []byte
	if r.Body != nil {
		body := r.Body
		if h.maxReqBody > 0 {
			body = http.MaxBytesReader(w, r.Body, h.maxReqBody)
		}
		limited := io.LimitReader(body, int64(h.rawLimit)+1)
		buf, err := io.ReadAll(limited)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				_ = r.Body.Close()
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request_body_too_large",
					fmt.Sprintf("request body exceeds configured limit of %d bytes", maxErr.Limit))
				return
			}
			h.logger.Warn("read request body", "err", err)
		}
		reqBodyCopy = buf
		if len(reqBodyCopy) > h.rawLimit {
			reqBodyCopy = reqBodyCopy[:h.rawLimit]
		}
		// Build outgoing body: we need the full original, so combine with any unread tail.
		var fullBody bytes.Buffer
		fullBody.Write(buf)
		if _, err := io.Copy(&fullBody, body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				_ = r.Body.Close()
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request_body_too_large",
					fmt.Sprintf("request body exceeds configured limit of %d bytes", maxErr.Limit))
				return
			}
			h.logger.Warn("read request body", "err", err)
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(fullBody.Bytes()))
		r.ContentLength = int64(fullBody.Len())
	}

	model, isStream := peekModelAndStream(reqBodyCopy)

	// Build outbound request: point it at the embedded upstream URL.
	outReq := r.Clone(context.WithoutCancel(r.Context()))
	outReq.RequestURI = ""
	outReq.URL = upstreamURL
	outReq.Host = upstreamURL.Host
	outReq.Header.Del("X-Forwarded-Host")
	// Strip Accept-Encoding so Go's Transport auto-adds gzip AND auto-decompresses
	// the upstream response into plain bytes. That way the trace stores readable
	// JSON / SSE instead of raw gzip; the client receives plain bytes too (the
	// Transport also clears Content-Encoding/Content-Length on the response).
	outReq.Header.Del("Accept-Encoding")
	stripInternalHeaders(outReq.Header)

	tl.UpstreamRequestOut = time.Now()
	resp, err := h.httpClient.Do(outReq)
	if err != nil {
		h.logger.Error("upstream request failed", "err", err, "host", upstreamURL.Host)
		writeJSONError(w, http.StatusBadGateway, "upstream_error", err.Error())
		h.emitError(reqBodyCopy, model, isStream, proj.ID, userID, sessionID, tags, logicTraceID, traceName, tl, err, 0)
		return
	}
	defer resp.Body.Close()
	tl.UpstreamFirstByte = time.Now()

	// Copy response headers and status.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	parser := stream.NewParserForHost(upstreamURL.Host)
	cw := intercept.NewCapturingWriter(w, h.rawLimit, nil)

	contentType := resp.Header.Get("Content-Type")
	streaming := isStream || strings.Contains(strings.ToLower(contentType), "text/event-stream")

	if streaming {
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			firstTokenCb := func(ts time.Time) { tl.FirstToken = ts }
			parser.Feed(pr, &tl, firstTokenCb)
		}()
		mw := io.MultiWriter(cw, pw)
		if _, err := io.Copy(mw, resp.Body); err != nil {
			h.logger.Warn("stream copy interrupted", "err", err)
		}
		_ = pw.Close()
		<-done
		cw.Flush()
		tl.UpstreamDone = time.Now()
	} else {
		if _, err := io.Copy(cw, resp.Body); err != nil {
			h.logger.Warn("response copy interrupted", "err", err)
		}
		tl.UpstreamDone = time.Now()
	}
	tl.ResponseOut = time.Now()

	body, bytesTotal, _, statusCode := cw.Snapshot()
	if statusCode == 0 {
		statusCode = resp.StatusCode
	}

	ev := &stream.Event{
		TraceID:         "tr_" + shortid.MustNew(16),
		IsStream:        streaming,
		Model:           model,
		StatusCode:      statusCode,
		ProjectID:       proj.ID,
		UserID:          userID,
		SessionID:       sessionID,
		Tags:            tags,
		LogicTraceID:    logicTraceID,
		TraceName:       traceName,
		RequestHeaders:  cloneHeader(r.Header),
		RequestBody:     reqBodyCopy,
		RequestPath:     upstreamURL.String(),
		ResponseHeaders: cloneHeader(resp.Header),
		ResponseBody:    body,
		BytesStreamed:   bytesTotal,
		Timeline:        tl,
	}
	if streaming {
		parser.Finalize(ev)
	} else {
		parser.ParseNonStream(body, ev)
	}

	if statusCode >= 400 {
		ev.Status = "error"
		ev.StreamStatus = "errored"
	} else {
		ev.Status = "success"
		ev.StreamStatus = "completed"
	}
	if streaming && tl.LastToken.IsZero() && statusCode < 400 {
		ev.StreamStatus = "aborted"
		ev.Status = "aborted"
	}

	h.sink.Submit(ev)
}

func (h *Handler) emitError(reqBody []byte, model string, isStream bool, projectID, userID, sessionID, tags, logicTraceID, traceName string, tl stream.Timeline, e error, status int) {
	tl.ResponseOut = time.Now()
	ev := &stream.Event{
		TraceID:      "tr_" + shortid.MustNew(16),
		IsStream:     isStream,
		Model:        model,
		StatusCode:   status,
		Status:       "error",
		StreamStatus: "errored",
		ErrorMsg:     e.Error(),
		ProjectID:    projectID,
		UserID:       userID,
		SessionID:    sessionID,
		Tags:         tags,
		LogicTraceID: logicTraceID,
		TraceName:    traceName,
		RequestBody:  reqBody,
		Timeline:     tl,
	}
	h.sink.Submit(ev)
}

func peekModelAndStream(body []byte) (model string, stream bool) {
	if len(body) == 0 {
		return "", false
	}
	var probe struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", false
	}
	return probe.Model, probe.Stream
}

func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		// Redact sensitive headers — clients send their real upstream API key here
		// in pass-through mode and we never want to persist it to traces.
		if strings.EqualFold(k, "Authorization") ||
			strings.EqualFold(k, "Cookie") ||
			strings.EqualFold(k, "X-Api-Key") ||
			strings.EqualFold(k, "X-Goog-Api-Key") ||
			strings.EqualFold(k, "X-AILens-Project-Key") {
			out[k] = []string{"[redacted]"}
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func stripInternalHeaders(h http.Header) {
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "x-ailens-") {
			h.Del(k)
		}
	}
}

func writeJSONError(w http.ResponseWriter, code int, kind, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    kind,
			"message": msg,
		},
	})
}

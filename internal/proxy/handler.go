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
	"sync/atomic"
	"time"

	"github.com/CoolBanHub/ailens360/internal/bodystore"
	"github.com/CoolBanHub/ailens360/internal/project"
	"github.com/CoolBanHub/ailens360/internal/proxy/intercept"
	"github.com/CoolBanHub/ailens360/internal/proxy/stream"
	"github.com/CoolBanHub/ailens360/pkg/shortid"
)

// Submitter is the minimal sink interface the handler depends on. The
// production implementation is *StreamSink; tests inject a no-op.
type Submitter interface {
	Submit(*stream.Event)
}

type Handler struct {
	logger     *slog.Logger
	resolver   *project.Resolver
	sink       Submitter
	store      bodystore.Store
	rawLimit   int
	maxReqBody int64
	httpClient *http.Client
}

type Deps struct {
	Logger    *slog.Logger
	Resolver  *project.Resolver
	Sink      Submitter
	BodyStore bodystore.Store
	RawLimit  int
	MaxBody   int64
	Timeout   time.Duration
}

func NewHandler(d Deps) *Handler {
	limit := d.RawLimit
	if limit <= 0 {
		limit = 8 << 20
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Handler{
		logger:     d.Logger,
		resolver:   d.Resolver,
		sink:       d.Sink,
		store:      d.BodyStore,
		rawLimit:   limit,
		maxReqBody: d.MaxBody,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Mount registers the catch-all route. The handler treats any path that begins
// with `/http://` or `/https://` as a proxy request; anything else (including
// `/healthz` registered on the same router) is left to the caller.
func (h *Handler) Mount(mux interface {
	HandleFunc(pattern string, h http.HandlerFunc)
}) {
	mux.HandleFunc("/*", h.serve)
}

// parseProxyPath extracts the embedded upstream URL from a proxy request path.
// The expected shape is `/<scheme>://<rest>` where scheme is http or https.
// The project the request belongs to is identified out-of-band via the
// X-AILens-Project-Key header.
func parseProxyPath(p string) (upstream string, ok bool) {
	if len(p) == 0 || p[0] != '/' {
		return "", false
	}
	rest := p[1:]
	if !strings.HasPrefix(rest, "http://") && !strings.HasPrefix(rest, "https://") {
		return "", false
	}
	return rest, true
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	tl := stream.Timeline{RequestIn: time.Now()}

	upstreamRaw, ok := parseProxyPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_path",
			"expected /<scheme>://<upstream>, e.g. /https://api.openai.com/v1/chat/completions")
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

	traceID := "tr_" + shortid.MustNew(16)

	// Read the request body once into memory (bounded by maxReqBody).
	reqBody, err := h.readRequestBody(r, w)
	if err != nil {
		// readRequestBody has already written the error response.
		return
	}

	model, isStream := peekModelAndStream(reqBody)

	// Kick off the request body upload in parallel with the upstream call.
	// Whatever finishes last gets joined back at event-build time. The key is
	// reserved deterministically up front so the trace event can refer to it
	// even before the upload completes.
	reqKey := bodystore.Key(proj.ID, time.Now(), traceID, bodystore.PartRequest, "json")
	reqUpload := h.uploadRequestAsync(r.Context(), reqKey, reqBody)

	// Build outbound request: point it at the embedded upstream URL.
	outReq := r.Clone(context.WithoutCancel(r.Context()))
	outReq.RequestURI = ""
	outReq.URL = upstreamURL
	outReq.Host = upstreamURL.Host
	outReq.Body = io.NopCloser(bytes.NewReader(reqBody))
	outReq.ContentLength = int64(len(reqBody))
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
		reqResult := reqUpload.wait()
		h.emitError(traceID, reqBody, reqResult, model, isStream, proj.ID, userID, sessionID, tags, logicTraceID, traceName, tl, err, 0, r.Header)
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
	contentType := resp.Header.Get("Content-Type")
	streaming := isStream || strings.Contains(strings.ToLower(contentType), "text/event-stream")

	// Streaming responses skip the snapshot buffer (would waste RAM); non-stream
	// responses keep a buffer up to rawLimit so the parser has bytes to parse.
	bufLimit := h.rawLimit
	if streaming {
		bufLimit = 0
	}
	cw := intercept.NewCapturingWriter(w, bufLimit, nil)

	// Open the response body uploader. If MinIO is unavailable, NewStreamingUploader
	// returns an error and we fall back to fail-open (no key in event).
	respKey := bodystore.Key(proj.ID, time.Now(), traceID, bodystore.PartResponse, "bin")
	respUploader, respUploaderErr := h.store.NewStreamingUploader(context.Background(), respKey, contentType)
	if respUploaderErr != nil {
		h.logger.Warn("response body uploader unavailable", "err", respUploaderErr, "trace_id", traceID)
	}

	// Compose tees: always client (cw); body store if available; parser pipe if streaming.
	writers := []io.Writer{cw}
	if respUploader != nil {
		writers = append(writers, &swallowingWriter{w: respUploader})
	}
	var pw *io.PipeWriter
	var parserDone chan struct{}
	if streaming {
		var pr *io.PipeReader
		pr, pw = io.Pipe()
		parserDone = make(chan struct{})
		go func() {
			defer close(parserDone)
			firstTokenCb := func(ts time.Time) { tl.FirstToken = ts }
			parser.Feed(pr, &tl, firstTokenCb)
		}()
		writers = append(writers, pw)
	}
	mw := io.MultiWriter(writers...)
	if _, err := io.Copy(mw, resp.Body); err != nil {
		h.logger.Warn("response copy interrupted", "err", err, "trace_id", traceID)
	}
	if pw != nil {
		_ = pw.Close()
	}
	if parserDone != nil {
		<-parserDone
	}
	cw.Flush()
	tl.UpstreamDone = time.Now()

	var respKeyFinal string
	var respSize int64
	if respUploader != nil {
		if err := respUploader.Close(); err != nil {
			h.logger.Warn("response body upload failed", "err", err, "trace_id", traceID)
		} else {
			respKeyFinal = respKey
			respSize = respUploader.BytesWritten()
		}
	}
	tl.ResponseOut = time.Now()

	body, bytesTotal, _, statusCode := cw.Snapshot()
	if statusCode == 0 {
		statusCode = resp.StatusCode
	}

	reqResult := reqUpload.wait()

	ev := &stream.Event{
		TraceID:          traceID,
		IsStream:         streaming,
		Model:            model,
		StatusCode:       statusCode,
		ProjectID:        proj.ID,
		UserID:           userID,
		SessionID:        sessionID,
		Tags:             tags,
		LogicTraceID:     logicTraceID,
		TraceName:        traceName,
		RequestHeaders:   cloneHeader(r.Header),
		RequestPath:      upstreamURL.String(),
		ResponseHeaders:  cloneHeader(resp.Header),
		RequestBodyKey:   reqResult.key,
		RequestBodySize:  reqResult.size,
		ResponseBodyKey:  respKeyFinal,
		ResponseBodySize: respSize,
		BytesStreamed:    bytesTotal,
		Timeline:         tl,
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

// readRequestBody reads at most maxReqBody bytes; if the cap is exceeded it
// writes a 413 response and returns an error so the caller can bail. The
// returned slice is the full body — there's no separate "preserved tail" any
// more because we always need the whole body for upstream + uploading.
func (h *Handler) readRequestBody(r *http.Request, w http.ResponseWriter) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body := r.Body
	if h.maxReqBody > 0 {
		body = http.MaxBytesReader(w, r.Body, h.maxReqBody)
	}
	buf, err := io.ReadAll(body)
	_ = r.Body.Close()
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request_body_too_large",
				fmt.Sprintf("request body exceeds configured limit of %d bytes", maxErr.Limit))
			return nil, err
		}
		h.logger.Warn("read request body", "err", err)
		return buf, nil
	}
	return buf, nil
}

type uploadResult struct {
	key  string
	size int64
}

type asyncUpload struct {
	doneCh chan uploadResult
}

func (u *asyncUpload) wait() uploadResult {
	if u == nil || u.doneCh == nil {
		return uploadResult{}
	}
	return <-u.doneCh
}

// uploadRequestAsync starts an upload of the buffered request body in a
// goroutine and returns a handle that .wait()s for the result. If the body is
// empty, returns a nil handle (wait() will return a zero uploadResult).
func (h *Handler) uploadRequestAsync(ctx context.Context, key string, body []byte) *asyncUpload {
	if len(body) == 0 || h.store == nil {
		return nil
	}
	u := &asyncUpload{doneCh: make(chan uploadResult, 1)}
	// Detach from the inbound request's context so an early client cancel
	// doesn't kill the upload — the bodystore enforces its own timeout.
	uploadCtx := context.Background()
	_ = ctx
	go func() {
		size, err := h.store.UploadBytes(uploadCtx, key, body, "application/json")
		if err != nil {
			h.logger.Warn("request body upload failed", "err", err, "key", key)
			u.doneCh <- uploadResult{}
			return
		}
		u.doneCh <- uploadResult{key: key, size: size}
	}()
	return u
}

// swallowingWriter prevents errors from the body-store writer from propagating
// up through io.MultiWriter (which short-circuits on first error). The client
// must keep receiving bytes even if MinIO is sick.
type swallowingWriter struct {
	w       io.Writer
	dropped atomic.Bool
}

func (s *swallowingWriter) Write(p []byte) (int, error) {
	if s.dropped.Load() {
		return len(p), nil
	}
	if _, err := s.w.Write(p); err != nil {
		s.dropped.Store(true)
	}
	return len(p), nil
}

func (h *Handler) emitError(traceID string, reqBody []byte, reqResult uploadResult, model string, isStream bool, projectID, userID, sessionID, tags, logicTraceID, traceName string, tl stream.Timeline, e error, status int, hdr http.Header) {
	_ = reqBody
	tl.ResponseOut = time.Now()
	ev := &stream.Event{
		TraceID:         traceID,
		IsStream:        isStream,
		Model:           model,
		StatusCode:      status,
		Status:          "error",
		StreamStatus:    "errored",
		ErrorMsg:        e.Error(),
		ProjectID:       projectID,
		UserID:          userID,
		SessionID:       sessionID,
		Tags:            tags,
		LogicTraceID:    logicTraceID,
		TraceName:       traceName,
		RequestHeaders:  cloneHeader(hdr),
		RequestBodyKey:  reqResult.key,
		RequestBodySize: reqResult.size,
		Timeline:        tl,
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

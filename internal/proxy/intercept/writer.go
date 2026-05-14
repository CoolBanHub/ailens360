package intercept

import (
	"bytes"
	"net/http"
)

// CapturingWriter wraps http.ResponseWriter and tees the body into a bounded buffer
// while preserving the streaming write+flush behaviour required by SSE.
//
// It implements http.Flusher and exposes the captured bytes via Snapshot().
type CapturingWriter struct {
	http.ResponseWriter
	flusher    http.Flusher
	buf        bytes.Buffer
	limit      int
	bytesTotal int64
	truncated  bool
	statusCode int
	onChunk    func(p []byte)
}

func NewCapturingWriter(w http.ResponseWriter, limit int, onChunk func(p []byte)) *CapturingWriter {
	cw := &CapturingWriter{ResponseWriter: w, limit: limit, onChunk: onChunk}
	if f, ok := w.(http.Flusher); ok {
		cw.flusher = f
	}
	return cw
}

func (c *CapturingWriter) WriteHeader(statusCode int) {
	c.statusCode = statusCode
	c.ResponseWriter.WriteHeader(statusCode)
}

func (c *CapturingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	if n > 0 {
		c.bytesTotal += int64(n)
		if c.onChunk != nil {
			c.onChunk(p[:n])
		}
		if c.limit > 0 && c.buf.Len() < c.limit {
			room := c.limit - c.buf.Len()
			if n <= room {
				c.buf.Write(p[:n])
			} else {
				c.buf.Write(p[:room])
				c.truncated = true
			}
		} else if c.limit > 0 {
			c.truncated = true
		}
	}
	return n, err
}

func (c *CapturingWriter) Flush() {
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

func (c *CapturingWriter) Snapshot() (body []byte, bytesTotal int64, truncated bool, status int) {
	return c.buf.Bytes(), c.bytesTotal, c.truncated, c.statusCode
}

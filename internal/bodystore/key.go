package bodystore

import (
	"fmt"
	"time"
)

// Part names the half of the trace each object holds.
type Part string

const (
	PartRequest  Part = "request"
	PartResponse Part = "response"
)

// Key builds the canonical object key. Layout is "{project}/{YYYYMM}/{trace}/{part}.{ext}":
// project scoping makes per-tenant lifecycle policies trivial; the YYYYMM bucket
// matches the PG partition cadence so archive cycles can be aligned.
//
// ext is "json" for request bodies and small JSON responses, "bin" for streaming
// or compressed bodies — callers decide what's accurate; this helper only
// concatenates.
func Key(projectID string, t time.Time, traceID string, part Part, ext string) string {
	if t.IsZero() {
		t = time.Now()
	}
	return fmt.Sprintf("%s/%s/%s/%s.%s",
		projectID,
		t.UTC().Format("200601"),
		traceID,
		string(part),
		ext,
	)
}

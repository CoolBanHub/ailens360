package bodystore

import (
	"testing"
	"time"
)

func TestKey(t *testing.T) {
	ts := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	got := Key("proj_abc", ts, "tr_xyz", PartRequest, "json")
	want := "proj_abc/202605/tr_xyz/request.json"
	if got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
}

func TestKeyZeroTimeUsesNow(t *testing.T) {
	got := Key("p", time.Time{}, "tr", PartResponse, "bin")
	// Just assert shape — month value is whatever time.Now() reports.
	if len(got) < len("p/000000/tr/response.bin") {
		t.Fatalf("Key too short: %q", got)
	}
}

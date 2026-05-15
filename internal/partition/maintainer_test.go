package partition

import (
	"testing"
	"time"
)

func TestPartitionName(t *testing.T) {
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if got, want := partitionName(ts), "traces_202605"; got != want {
		t.Errorf("partitionName = %q, want %q", got, want)
	}
}

func TestParsePartitionMonth(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"traces_202605", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"traces_202612", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), true},
		{"projects", time.Time{}, false},
		{"traces_foo", time.Time{}, false},
		{"traces_2026", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := parsePartitionMonth(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAddMonthsWrapsYear(t *testing.T) {
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got := addMonths(base, 3) // Feb 2027
	want := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("addMonths +3 = %v, want %v", got, want)
	}
	got2 := addMonths(base, -12) // Nov 2025
	want2 := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	if !got2.Equal(want2) {
		t.Errorf("addMonths -12 = %v, want %v", got2, want2)
	}
}

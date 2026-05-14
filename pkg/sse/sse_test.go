package sse

import (
	"io"
	"strings"
	"testing"
)

func TestScannerBasicEvents(t *testing.T) {
	in := "data: hello\n\ndata: {\"x\":1}\n\n"
	sc := NewScanner(strings.NewReader(in))
	want := []string{"hello", `{"x":1}`}
	for i, w := range want {
		ev, err := sc.Next()
		if err != nil && err != io.EOF {
			t.Fatalf("event %d: %v", i, err)
		}
		if ev == nil {
			t.Fatalf("event %d: nil", i)
		}
		if string(ev.Data) != w {
			t.Fatalf("event %d: want %q got %q", i, w, ev.Data)
		}
	}
}

func TestScannerSkipsCommentsAndKeepsCRLF(t *testing.T) {
	in := ":heartbeat\r\ndata: a\r\n\r\n"
	sc := NewScanner(strings.NewReader(in))
	ev, err := sc.Next()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if string(ev.Data) != "a" {
		t.Fatalf("want %q got %q", "a", ev.Data)
	}
}

func TestScannerMultiLineData(t *testing.T) {
	in := "data: line1\ndata: line2\n\n"
	sc := NewScanner(strings.NewReader(in))
	ev, _ := sc.Next()
	if string(ev.Data) != "line1\nline2" {
		t.Fatalf("want concatenated multi-line, got %q", ev.Data)
	}
}

func TestScannerEventTypes(t *testing.T) {
	in := "event: message_start\ndata: {}\n\nevent: content_block_delta\ndata: {\"a\":1}\n\n"
	sc := NewScanner(strings.NewReader(in))
	want := []string{"message_start", "content_block_delta"}
	for _, w := range want {
		ev, err := sc.Next()
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if ev.Event != w {
			t.Fatalf("want event %q got %q", w, ev.Event)
		}
	}
}

func TestScannerEOF(t *testing.T) {
	sc := NewScanner(strings.NewReader(""))
	_, err := sc.Next()
	if err != io.EOF {
		t.Fatalf("want io.EOF got %v", err)
	}
}

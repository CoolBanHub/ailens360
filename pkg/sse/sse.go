package sse

import (
	"bufio"
	"bytes"
	"io"
)

// Event is a single SSE event with the lines preceding the blank-line terminator.
type Event struct {
	ID    string
	Event string
	Data  []byte // joined data: lines with newlines; trailing newline stripped
	Raw   []byte // original bytes including the terminating blank line
}

// Scanner reads SSE events from an io.Reader.
// It tolerates "\n" and "\r\n" line endings.
type Scanner struct {
	r   *bufio.Reader
	err error
}

func NewScanner(r io.Reader) *Scanner {
	return &Scanner{r: bufio.NewReaderSize(r, 16*1024)}
}

// Next blocks until a full event is read or the stream ends.
// Returns (nil, io.EOF) at end of stream.
func (s *Scanner) Next() (*Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	var (
		ev      Event
		raw     bytes.Buffer
		data    bytes.Buffer
		hasData bool
	)
	for {
		line, err := s.r.ReadBytes('\n')
		if len(line) > 0 {
			raw.Write(line)
		}
		if err != nil {
			if err == io.EOF && raw.Len() == 0 {
				s.err = io.EOF
				return nil, io.EOF
			}
			s.err = err
			if raw.Len() == 0 {
				return nil, err
			}
		}
		// strip CR/LF
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			// event terminator
			if hasData {
				d := data.Bytes()
				// trim final \n if present
				if len(d) > 0 && d[len(d)-1] == '\n' {
					d = d[:len(d)-1]
				}
				ev.Data = append([]byte(nil), d...)
			}
			ev.Raw = append([]byte(nil), raw.Bytes()...)
			return &ev, nil
		}
		if bytes.HasPrefix(trimmed, []byte(":")) {
			continue // comment
		}
		field, value := splitField(trimmed)
		switch string(field) {
		case "id":
			ev.ID = string(value)
		case "event":
			ev.Event = string(value)
		case "data":
			data.Write(value)
			data.WriteByte('\n')
			hasData = true
		}
		if s.err != nil {
			return &ev, s.err
		}
	}
}

func splitField(line []byte) (field, value []byte) {
	idx := bytes.IndexByte(line, ':')
	if idx < 0 {
		return line, nil
	}
	field = line[:idx]
	value = line[idx+1:]
	// per spec, a single leading space after the colon is stripped
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value
}

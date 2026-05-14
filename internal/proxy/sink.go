package proxy

import "github.com/CoolBanHub/ailens360/internal/proxy/stream"

// EventSink receives finalized proxy events for downstream processing (persist, metrics, ws).
type EventSink interface {
	Submit(*stream.Event)
}

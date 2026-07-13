package telemetry

import (
	"sync"

	"github.com/google/uuid"
)

// eventBuffer is the per-subscriber channel size. A subscriber that falls
// this far behind starts losing updates rather than blocking anyone else —
// see Hub.Publish.
const eventBuffer = 32

// EventType distinguishes the two kinds of messages a commuter WS receives
// after its initial snapshot.
type EventType string

const (
	EventUpdate  EventType = "update"
	EventOffline EventType = "offline"
)

// Event is one fan-out message, published per route.
type Event struct {
	Type      EventType   `json:"type"`
	Vehicle   VehicleView `json:"vehicle,omitempty"`
	VehicleID string      `json:"vehicle_id,omitempty"`
}

// Subscriber is one commuter WS connection's mailbox for a route.
type Subscriber struct {
	RouteID uuid.UUID
	ch      chan Event
}

// Hub fans out telemetry events to commuters subscribed per route_id.
// Publish is non-blocking: a slow/stuck subscriber has updates dropped for
// it rather than stalling driver ingestion or other subscribers. This is
// the core concurrency property the stage asks for — driver position
// updates (the ingestion path) never wait on commuter delivery.
type Hub struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[*Subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[uuid.UUID]map[*Subscriber]struct{})}
}

// Subscribe registers a new mailbox for a route. Callers must Unsubscribe
// when done (e.g. via defer) to avoid leaking the entry.
func (h *Hub) Subscribe(routeID uuid.UUID) *Subscriber {
	sub := &Subscriber{RouteID: routeID, ch: make(chan Event, eventBuffer)}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[routeID] == nil {
		h.subs[routeID] = make(map[*Subscriber]struct{})
	}
	h.subs[routeID][sub] = struct{}{}
	return sub
}

func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[sub.RouteID]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(h.subs, sub.RouteID)
		}
	}
}

// Publish fans an event out to every subscriber of a route. Each send is
// non-blocking (buffered channel + select/default): a subscriber whose
// mailbox is full has this update dropped for it instead of blocking the
// publisher, which is always the driver ingestion path.
func (h *Hub) Publish(routeID uuid.UUID, evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subs[routeID] {
		select {
		case sub.ch <- evt:
		default:
			// Slow consumer — drop rather than block. The commuter's next
			// REST snapshot or next successful event will resync it.
		}
	}
}

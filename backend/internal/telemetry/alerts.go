package telemetry

import (
	"sync"

	"github.com/google/uuid"
)

// alertBuffer is the per-driver mailbox size, mirroring Hub's eventBuffer.
const alertBuffer = 8

// AlertMessage is a server-pushed message delivered to one specific driver's
// WebSocket connection — unlike Hub's Event (fanned out to every commuter
// subscribed to a route), an alert targets exactly one driver. Stage 6's
// stop-request is the first user of this; the fields are generic enough for
// future driver-directed pushes. RequestID/StopID/RouteID are stringified
// uuids, matching VehicleView's convention.
type AlertMessage struct {
	Type        string `json:"type"`
	RequestID   string `json:"request_id"`
	RouteID     string `json:"route_id"`
	StopID      string `json:"stop_id"`
	StopName    string `json:"stop_name"`
	RequestedAt string `json:"requested_at"`
}

// DriverAlertSub is one driver WS connection's mailbox for pushed alerts.
type DriverAlertSub struct {
	DriverID uuid.UUID
	ch       chan AlertMessage
}

// DriverAlertHub delivers alerts to a specific online driver's WebSocket
// connection(s), keyed by driver id rather than Hub's route id. Send uses
// the same non-blocking, drop-on-full-mailbox discipline as Hub.Publish —
// a caller publishing a stop-request alert must never block on a slow or
// stuck driver connection.
type DriverAlertHub struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[*DriverAlertSub]struct{}
}

func NewDriverAlertHub() *DriverAlertHub {
	return &DriverAlertHub{subs: make(map[uuid.UUID]map[*DriverAlertSub]struct{})}
}

// Subscribe registers a new mailbox for a driver. Callers must Unsubscribe
// when the connection ends (e.g. via defer) to avoid leaking the entry.
func (h *DriverAlertHub) Subscribe(driverID uuid.UUID) *DriverAlertSub {
	sub := &DriverAlertSub{DriverID: driverID, ch: make(chan AlertMessage, alertBuffer)}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[driverID] == nil {
		h.subs[driverID] = make(map[*DriverAlertSub]struct{})
	}
	h.subs[driverID][sub] = struct{}{}
	return sub
}

func (h *DriverAlertHub) Unsubscribe(sub *DriverAlertSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[sub.DriverID]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(h.subs, sub.DriverID)
		}
	}
}

// Send delivers an alert to every currently-connected WS mailbox for a
// driver (normally at most one — a driver has at most one active /ws/driver
// connection in this MVP). Returns whether at least one mailbox received it,
// so callers can distinguish "delivered" from "driver isn't actually
// connected right now" (e.g. it went offline between the online check and
// the send).
func (h *DriverAlertHub) Send(driverID uuid.UUID, msg AlertMessage) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	delivered := false
	for sub := range h.subs[driverID] {
		select {
		case sub.ch <- msg:
			delivered = true
		default:
			// Slow consumer — drop rather than block the publisher.
		}
	}
	return delivered
}

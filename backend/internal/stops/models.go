// Package stops implements Stage 6's request-a-stop feature: a commuter
// waiting at a stop asks to be picked up, and the nearest approaching online
// driver on that route gets a live alert pushed to their existing /ws/driver
// connection (Stage 4's WebSocket infrastructure). It is a telemetry-shaped
// feature — no money or crypto involved — built on top of Stage 1 (auth),
// Stage 3 (routes/stops), and Stage 4 (live vehicle state/hub) rather than
// duplicating any of them.
package stops

import (
	"time"

	"github.com/google/uuid"
)

// Status is a stop-request's lifecycle state.
type Status string

const (
	// StatusPending: matched to a driver and delivered, awaiting ack.
	StatusPending Status = "pending"
	// StatusUnmatched: no approaching online driver was available (or the
	// matched driver's connection wasn't actually reachable at send time).
	StatusUnmatched Status = "unmatched"
	// StatusAcknowledged: the matched driver has acked the request.
	StatusAcknowledged Status = "acknowledged"
)

// Request is one commuter's pickup request at a stop on a route.
//
// SCOPE HONESTY (per CLAUDE.md): held in memory only (see Store) — resets on
// server restart, same accepted MVP trade-off as telemetry.VehicleStateStore.
type Request struct {
	ID              uuid.UUID
	CommuterID      uuid.UUID
	RouteID         uuid.UUID
	StopID          uuid.UUID
	StopName        string
	RequestedAt     time.Time
	Status          Status
	MatchedDriverID *uuid.UUID // nil if Status == StatusUnmatched
	AckedAt         *time.Time
}

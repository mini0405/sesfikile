// Package boarding is Stage 5: the fare-on-scan hero flow. It fuses
// identity (Stage 1, auth), the wallet ledger (Stage 2, the fare charge
// itself), routing (Stage 3, pricing a leg), and telemetry (Stage 4, seat
// state) rather than reimplementing any of them.
//
// SCOPE HONESTY: the QR code itself is generated/scanned client-side in a
// later frontend stage. This package produces and verifies the signed token
// a QR would carry — tested here as a raw token over HTTP. Boarding assumes
// the commuter is physically present; this proves the cryptographic and
// financial flow, not proximity/geofencing.
package boarding

import (
	"time"

	"github.com/google/uuid"
)

// PassPayload is the signed content of a boarding pass — everything needed
// to verify and charge a boarding without a further DB round-trip to
// re-derive the fare. FareCents is fixed at issue time (Stage 3 routing) and
// carried inside the signed payload so it can't be tampered with in transit.
type PassPayload struct {
	CommuterID uuid.UUID `json:"commuter_id"`
	RouteID    uuid.UUID `json:"route_id"`
	FromStopID uuid.UUID `json:"from_stop_id"`
	ToStopID   uuid.UUID `json:"to_stop_id"`
	FareCents  int64     `json:"fare_cents"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Nonce is a unique per-pass identifier, used as the wallet ledger's
	// idempotency_key on scan — this is what guarantees a replayed scan of
	// the same pass charges exactly once (see Handlers.ScanPass).
	Nonce string `json:"nonce"`
}

func (p PassPayload) Expired(now time.Time) bool {
	return now.After(p.ExpiresAt)
}

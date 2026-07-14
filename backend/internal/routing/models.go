package routing

import (
	"time"

	"github.com/google/uuid"
)

// Stop's Latitude/Longitude are nullable: real for every hand-seeded stop
// (cmd/seed), but always NULL for a catalogue-imported stop (see
// internal/catalogue) since the source CSV carries no coordinates at all —
// only rank names. Use CoordinatesKnown() rather than checking either field
// directly.
type Stop struct {
	ID        uuid.UUID
	Name      string
	Latitude  *float64
	Longitude *float64
	CreatedAt time.Time
}

// CoordinatesKnown reports whether this stop has a real, usable map
// position. Any read path that renders a map or matches live telemetry
// against a stop (e.g. internal/stops' request-a-stop matching) must check
// this first — a coordinate-less stop must never be treated as if it were
// at (0, 0).
func (s Stop) CoordinatesKnown() bool {
	return s.Latitude != nil && s.Longitude != nil
}

// Source distinguishes cmd/seed's hand-seeded demo corridors ("seed", the
// tested baseline) from cmd/importcatalogue's real-but-unverified City of
// Cape Town rows ("catalogue") — see internal/catalogue. The two constants
// below are the only valid values (enforced by a DB CHECK constraint).
const (
	SourceSeed      = "seed"
	SourceCatalogue = "catalogue"
)

type Route struct {
	ID              uuid.UUID
	Name            string
	AssociationName string
	Source          string
	CreatedAt       time.Time
}

// RouteLeg is one directed edge of a route, walked in increasing Sequence
// order. FareCents is the fare for riding this single leg.
//
// DistanceMeters/FareEstimated are only ever set for a catalogue-imported
// leg (internal/catalogue): DistanceMeters is the source CSV's own
// SHAPE_Length (nil for a hand-seeded leg, which has no such measurement),
// and FareEstimated marks FareCents as computed from that distance
// (internal/catalogue.EstimateFareCents) rather than a real association
// tariff. Always false/nil for every hand-seeded leg.
type RouteLeg struct {
	ID             uuid.UUID
	RouteID        uuid.UUID
	FromStopID     uuid.UUID
	ToStopID       uuid.UUID
	Sequence       int
	FareCents      int64
	DistanceMeters *float64
	FareEstimated  bool
	CreatedAt      time.Time
}

// RouteWithLegs is a route and its legs ordered by Sequence.
type RouteWithLegs struct {
	Route Route
	Legs  []RouteLeg
}

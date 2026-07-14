package routing

import (
	"time"

	"github.com/google/uuid"
)

// Source distinguishes cmd/seed's hand-seeded demo corridors ("seed", the
// tested baseline) from cmd/importcatalogue's real-but-unverified City of
// Cape Town rows ("catalogue") — see internal/catalogue. The two constants
// below are the only valid values (enforced by a DB CHECK constraint on
// both stops.source and routes.source).
const (
	SourceSeed      = "seed"
	SourceCatalogue = "catalogue"
)

// Stop's Latitude/Longitude are nullable. Every hand-seeded stop (cmd/seed)
// has real coordinates. A catalogue-imported stop (see internal/catalogue)
// now ALSO gets a coordinate as of the GeoJSON upgrade — an APPROXIMATE one,
// the median of every endpoint position its rank name appears at across the
// source file (see catalogue.medianCoordinate), not a surveyed position —
// so Latitude/Longitude being non-nil does not by itself mean "exact." Use
// CoordinatesKnown() rather than checking either field directly, and Source
// to tell an approximate (catalogue) coordinate from a real (seed) one.
// A stop can still end up coordinate-less in principle (e.g. a defensive
// fallback if a future import ever lacks geometry) — CoordinatesKnown()
// covers that case too.
type Stop struct {
	ID        uuid.UUID
	Name      string
	Latitude  *float64
	Longitude *float64
	Source    string
	CreatedAt time.Time
}

// CoordinatesKnown reports whether this stop has ANY usable map position
// (exact or approximate). Any read path that renders a map or matches live
// telemetry against a stop (e.g. internal/stops' request-a-stop matching)
// must check this first — a coordinate-less stop must never be treated as
// if it were at (0, 0).
func (s Stop) CoordinatesKnown() bool {
	return s.Latitude != nil && s.Longitude != nil
}

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
// leg (internal/catalogue): DistanceMeters is computed from the route's real
// geometry (nil for a hand-seeded leg, which has no such measurement), and
// FareEstimated marks FareCents as computed from that distance
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

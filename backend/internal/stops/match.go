package stops

import (
	"math"

	"github.com/google/uuid"
)

// RouteStop is one stop's position in a route's ordered stop sequence
// (index 0 = the first stop the route departs from, increasing in the
// direction the route is walked — routes are directional, per Stage 3).
type RouteStop struct {
	StopID uuid.UUID
	Lat    float64
	Lng    float64
}

// DriverPosition is one online driver's live telemetry, as needed for the
// approaching-driver match — a trimmed view of telemetry.VehicleState so
// this package's matching logic stays pure (no telemetry import, easy to
// unit test with synthetic data).
type DriverPosition struct {
	DriverID  uuid.UUID
	VehicleID uuid.UUID
	Lat       float64
	Lng       float64
}

// Candidate is a driver who qualifies to be alerted for a stop-request.
type Candidate struct {
	DriverID       uuid.UUID
	VehicleID      uuid.UUID
	DistanceMeters float64
}

// StopSequenceIndex returns stopID's 0-based position within a route's
// ordered stop list, or ok=false if the stop isn't on that route.
func StopSequenceIndex(routeStops []RouteStop, stopID uuid.UUID) (int, bool) {
	for i, s := range routeStops {
		if s.StopID == stopID {
			return i, true
		}
	}
	return 0, false
}

// nearestStopIndex approximates "where is this driver right now, in terms
// of route progress" as the index of the geographically nearest stop to
// their last reported lat/lng.
//
// APPROXIMATION (documented per CLAUDE.md "SCOPE HONESTY"): this is nearest
// stop by straight-line (haversine) distance, not true map-matching or
// geofencing along the corridor's actual road path. A driver who has just
// pulled away from a stop but is still physically closer to it than to the
// next one will read as "at" that stop rather than "just past" it. That
// slop is acceptable for an MVP demo — it only matters near the boundary
// between two adjacent stops, and errs toward still alerting a driver who
// has *just* passed a stop rather than silently skipping them.
func nearestStopIndex(routeStops []RouteStop, lat, lng float64) int {
	best := 0
	bestDist := math.MaxFloat64
	for i, s := range routeStops {
		d := haversineMeters(lat, lng, s.Lat, s.Lng)
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// FindApproachingDriver picks the one driver to alert for a stop-request,
// out of the online drivers currently on the requested route.
//
// Rule (kept deliberately simple for the MVP):
//  1. "Approaching" = a driver's nearestStopIndex is <= the requested stop's
//     index in the route's ordered stop sequence, i.e. they have not yet
//     passed it. A driver whose nearest stop is beyond the target does not
//     qualify.
//  2. Among qualifying drivers, the physically nearest one (haversine
//     distance from their live position to the stop's own lat/lng) is
//     chosen. Only a single driver is alerted per request — simplest
//     behavior to reason about for a first cut of this feature; alerting
//     every qualifying driver would be the natural extension if a single
//     alerted driver turns out to be an unreliable pickup in practice.
//
// Returns ok=false if no online driver on the route qualifies.
func FindApproachingDriver(drivers []DriverPosition, routeStops []RouteStop, targetStop RouteStop) (Candidate, bool) {
	targetIndex, ok := StopSequenceIndex(routeStops, targetStop.StopID)
	if !ok {
		return Candidate{}, false
	}

	var (
		best  Candidate
		found bool
	)
	for _, d := range drivers {
		if nearestStopIndex(routeStops, d.Lat, d.Lng) > targetIndex {
			continue // already passed the requested stop
		}
		dist := haversineMeters(d.Lat, d.Lng, targetStop.Lat, targetStop.Lng)
		if !found || dist < best.DistanceMeters {
			best = Candidate{DriverID: d.DriverID, VehicleID: d.VehicleID, DistanceMeters: dist}
			found = true
		}
	}
	return best, found
}

package routing

import "github.com/google/uuid"

// Segment is one leg of a search result ridden on a single route.
type Segment struct {
	RouteID   uuid.UUID
	RouteName string
	Legs      []RouteLeg
	FareCents int64
}

// SearchResult is the best path found between an origin and destination
// stop. Transfers is the number of route changes (0 for a direct ride, 1 for
// a single interchange).
type SearchResult struct {
	Transfers      int
	TotalFareCents int64
	Segments       []Segment
}

// routeStopOrder returns, for a route's ordered legs, the sequence of stops
// walked (from_stop of the first leg, then each leg's to_stop) and the
// position of each stop within that sequence. A route is only walkable in
// increasing Sequence order, matching a real minibus taxi corridor that runs
// in one fixed direction.
func routeStopOrder(legs []RouteLeg) (stops []uuid.UUID, position map[uuid.UUID]int) {
	if len(legs) == 0 {
		return nil, map[uuid.UUID]int{}
	}
	stops = make([]uuid.UUID, 0, len(legs)+1)
	stops = append(stops, legs[0].FromStopID)
	for _, l := range legs {
		stops = append(stops, l.ToStopID)
	}
	position = make(map[uuid.UUID]int, len(stops))
	for i, s := range stops {
		position[s] = i
	}
	return stops, position
}

// directSegment returns the segment riding route r from stop origin to stop
// dest, if both stops are on r with origin reachable before dest.
func directSegment(r RouteWithLegs, origin, dest uuid.UUID) (Segment, bool) {
	_, position := routeStopOrder(r.Legs)
	posOrigin, okOrigin := position[origin]
	posDest, okDest := position[dest]
	if !okOrigin || !okDest || posOrigin >= posDest {
		return Segment{}, false
	}

	legs := r.Legs[posOrigin:posDest]
	var fare int64
	for _, l := range legs {
		fare += l.FareCents
	}
	return Segment{RouteID: r.Route.ID, RouteName: r.Route.Name, Legs: legs, FareCents: fare}, true
}

// FareForSegment computes the fare for a single ride along legs (one
// route's ordered legs) from fromStopID to toStopID, in increasing sequence
// order — the same direct-ride rule Search uses for a single route. Used by
// boarding (Stage 5) to price a pass against one specific route rather than
// searching across all routes. ok=false if the stops aren't both on this
// route with fromStopID reachable before toStopID.
func FareForSegment(legs []RouteLeg, fromStopID, toStopID uuid.UUID) (fareCents int64, ok bool) {
	seg, ok := directSegment(RouteWithLegs{Legs: legs}, fromStopID, toStopID)
	if !ok {
		return 0, false
	}
	return seg.FareCents, true
}

// Search finds the best path from origin to dest across the given routes.
// Ordering: fewest transfers first (a direct ride always beats any
// transfer), then lowest total fare. The MVP supports at most one transfer
// (one interchange) — see CLAUDE.md stage 3 brief.
//
// Routes are one-directional by design: a return trip along the same
// corridor is modeled as its own separate route (see
// internal/routing/seed_data.go's ForwardCorridors/reverseRoute) rather than
// this search treating a route's legs as walkable in both directions. Real
// minibus taxi associations typically dispatch each direction as a distinct
// route from a distinct rank, so this matches the real-world structure
// rather than simplifying it — see docs/PROGRESS.md's Stage 3 decision.
func Search(routes []RouteWithLegs, origin, dest uuid.UUID) (SearchResult, bool) {
	if origin == dest {
		return SearchResult{}, false
	}

	var bestDirect *Segment
	for _, r := range routes {
		seg, ok := directSegment(r, origin, dest)
		if !ok {
			continue
		}
		if bestDirect == nil || seg.FareCents < bestDirect.FareCents {
			s := seg
			bestDirect = &s
		}
	}
	if bestDirect != nil {
		return SearchResult{
			Transfers:      0,
			TotalFareCents: bestDirect.FareCents,
			Segments:       []Segment{*bestDirect},
		}, true
	}

	var best *SearchResult
	for _, r1 := range routes {
		stops1, pos1 := routeStopOrder(r1.Legs)
		originPos, ok := pos1[origin]
		if !ok {
			continue
		}
		for _, r2 := range routes {
			if r2.Route.ID == r1.Route.ID {
				continue
			}
			_, pos2 := routeStopOrder(r2.Legs)
			destPos, ok := pos2[dest]
			if !ok {
				continue
			}

			for _, interchange := range stops1 {
				ip1, ok := pos1[interchange]
				if !ok || ip1 <= originPos {
					continue
				}
				ip2, ok := pos2[interchange]
				if !ok || ip2 >= destPos {
					continue
				}

				seg1, ok := directSegment(r1, origin, interchange)
				if !ok {
					continue
				}
				seg2, ok := directSegment(r2, interchange, dest)
				if !ok {
					continue
				}

				total := seg1.FareCents + seg2.FareCents
				if best == nil || total < best.TotalFareCents {
					best = &SearchResult{
						Transfers:      1,
						TotalFareCents: total,
						Segments:       []Segment{seg1, seg2},
					}
				}
			}
		}
	}
	if best != nil {
		return *best, true
	}

	return SearchResult{}, false
}

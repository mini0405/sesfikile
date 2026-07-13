package routing

import (
	"context"
	"errors"
	"fmt"
)

// StopSeed is a hand-picked stop on a Cape Town taxi corridor: name plus
// approximate lat/long. See CLAUDE.md SCOPE HONESTY — this is a
// representative demo sample, not association-approved data.
type StopSeed struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// LegSeed is one leg of a route: the stop names it runs between (by name,
// resolved to ids during seeding) and its fixed fare in cents.
type LegSeed struct {
	FromStop  string
	ToStop    string
	FareCents int64
}

type RouteSeed struct {
	Name            string
	AssociationName string
	Legs            []LegSeed
}

const CPTAssociation = "Cape Town Minibus Taxi Association"

var SeedStops = []StopSeed{
	{"Cape Town Station", -33.9249, 18.4241},
	{"Woodstock", -33.9281, 18.4464},
	{"Athlone", -33.9667, 18.5000},
	{"Mitchells Plain Town Centre", -34.0333, 18.6167},
	{"Khayelitsha Site C", -34.0333, 18.6667},
	{"Khayelitsha Town Centre", -34.0500, 18.6833},
	{"Claremont", -33.9833, 18.4667},
	{"Wynberg", -34.0000, 18.4667},
	{"Parow", -33.9167, 18.5833},
	{"Bellville Station", -33.9000, 18.6333},
	{"Retreat", -34.0333, 18.4833},
	{"Muizenberg", -34.1083, 18.4700},
}

// ForwardCorridors are the four Cape Town taxi corridors this demo models,
// one direction each. "Athlone" is shared between corridors 1 and 2, and
// "Wynberg" between corridors 2 and 4, so multi-hop search has real
// interchanges to find.
//
// SeedRoutes (below) adds each corridor's return trip as its own separate
// route via reverseRoute, rather than making search treat a route's legs as
// bidirectional: real minibus taxi associations typically dispatch each
// direction as a distinct route from a distinct rank, so two route rows is
// the more faithful model, not a simplification (see docs/PROGRESS.md Stage
// 3 decision). Interchange stops reported by cmd/seed's SEEDED DATA summary
// are computed from ForwardCorridors only — a corridor and its own return
// trip share every stop by construction and don't represent a genuine
// network crossing.
var ForwardCorridors = []RouteSeed{
	{
		Name:            "Cape Town CBD - Khayelitsha",
		AssociationName: CPTAssociation,
		Legs: []LegSeed{
			{"Cape Town Station", "Woodstock", 800},
			{"Woodstock", "Athlone", 700},
			{"Athlone", "Mitchells Plain Town Centre", 900},
			{"Mitchells Plain Town Centre", "Khayelitsha Site C", 600},
			{"Khayelitsha Site C", "Khayelitsha Town Centre", 500},
		},
	},
	{
		Name:            "Athlone - Wynberg",
		AssociationName: CPTAssociation,
		Legs: []LegSeed{
			{"Athlone", "Claremont", 600},
			{"Claremont", "Wynberg", 500},
		},
	},
	{
		Name:            "Cape Town CBD - Bellville",
		AssociationName: CPTAssociation,
		Legs: []LegSeed{
			{"Cape Town Station", "Parow", 700},
			{"Parow", "Bellville Station", 400},
		},
	},
	{
		Name:            "Wynberg - Muizenberg",
		AssociationName: CPTAssociation,
		Legs: []LegSeed{
			{"Wynberg", "Retreat", 400},
			{"Retreat", "Muizenberg", 400},
		},
	},
}

// reverseRouteNames maps each forward corridor's name to its return trip's
// name.
var reverseRouteNames = map[string]string{
	"Cape Town CBD - Khayelitsha": "Khayelitsha - Cape Town CBD",
	"Athlone - Wynberg":           "Wynberg - Athlone",
	"Cape Town CBD - Bellville":   "Bellville - Cape Town CBD",
	"Wynberg - Muizenberg":        "Muizenberg - Wynberg",
}

// reverseRoute builds a corridor's return-trip route: same stops, legs
// walked in reverse order. Fares are mirrored for now (each leg keeps the
// fare of its forward counterpart) — real minibus taxi fares can differ by
// direction (e.g. peak-direction pricing), which this hand-seeded demo
// doesn't attempt to model (see CLAUDE.md SCOPE HONESTY).
func reverseRoute(forward RouteSeed) RouteSeed {
	n := len(forward.Legs)
	legs := make([]LegSeed, n)
	for i, l := range forward.Legs {
		legs[n-1-i] = LegSeed{FromStop: l.ToStop, ToStop: l.FromStop, FareCents: l.FareCents}
	}
	return RouteSeed{
		Name:            reverseRouteNames[forward.Name],
		AssociationName: forward.AssociationName,
		Legs:            legs,
	}
}

// SeedRoutes is every route SeedCorridors seeds: the four forward corridors
// plus their return trips (see reverseRoute).
var SeedRoutes = buildSeedRoutes()

func buildSeedRoutes() []RouteSeed {
	routes := make([]RouteSeed, 0, len(ForwardCorridors)*2)
	routes = append(routes, ForwardCorridors...)
	for _, fwd := range ForwardCorridors {
		routes = append(routes, reverseRoute(fwd))
	}
	return routes
}

// SeedCorridors seeds SeedStops and SeedRoutes if not already present. Safe
// to call repeatedly: stops and routes are matched by name (no DB
// uniqueness constraint on either — this is the seed's own idempotency
// check), and a route's legs are only inserted the first time that route
// has none.
func SeedCorridors(ctx context.Context, repo *Repo) error {
	for _, s := range SeedStops {
		if _, err := getOrCreateStop(ctx, repo, s); err != nil {
			return fmt.Errorf("seed stop %s: %w", s.Name, err)
		}
	}

	for _, rs := range SeedRoutes {
		route, err := getOrCreateRoute(ctx, repo, rs.Name, rs.AssociationName)
		if err != nil {
			return fmt.Errorf("seed route %s: %w", rs.Name, err)
		}

		existingLegs, err := repo.ListLegsForRoute(ctx, route.ID)
		if err != nil {
			return fmt.Errorf("list legs for route %s: %w", rs.Name, err)
		}
		if len(existingLegs) > 0 {
			continue
		}

		for i, l := range rs.Legs {
			fromStop, err := repo.GetStopByName(ctx, l.FromStop)
			if err != nil {
				return fmt.Errorf("look up stop %s: %w", l.FromStop, err)
			}
			toStop, err := repo.GetStopByName(ctx, l.ToStop)
			if err != nil {
				return fmt.Errorf("look up stop %s: %w", l.ToStop, err)
			}
			if _, err := repo.CreateRouteLeg(ctx, route.ID, fromStop.ID, toStop.ID, i+1, l.FareCents); err != nil {
				return fmt.Errorf("seed leg %s -> %s on route %s: %w", l.FromStop, l.ToStop, rs.Name, err)
			}
		}
	}
	return nil
}

func getOrCreateStop(ctx context.Context, repo *Repo, s StopSeed) (Stop, error) {
	existing, err := repo.GetStopByName(ctx, s.Name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Stop{}, err
	}
	return repo.CreateStop(ctx, s.Name, s.Latitude, s.Longitude)
}

func getOrCreateRoute(ctx context.Context, repo *Repo, name, associationName string) (Route, error) {
	existing, err := repo.GetRouteByName(ctx, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Route{}, err
	}
	return repo.CreateRoute(ctx, name, associationName)
}

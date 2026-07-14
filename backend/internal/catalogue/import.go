package catalogue

import (
	"context"
	"errors"
	"fmt"
	"io"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/routing"
)

// CatalogueAssociationName labels every catalogue-imported route's
// association_name. The source CSV (City of Cape Town open data) carries no
// association attribution at all — unlike cmd/seed's hand-seeded demo
// corridors, which use the real "Cape Town Minibus Taxi Association" name.
// Kept visibly distinct so nobody mistakes a catalogue route for
// association-verified data.
const CatalogueAssociationName = "City of Cape Town open data (unverified, no association attribution)"

// Stats reports what one Import run did — both for cmd/importcatalogue's
// printed summary and for tests.
type Stats struct {
	ParseStats
	UniqueRanks    int // distinct canonical rank names seen this run
	RoutesImported int // newly created this run
	RoutesExisting int // already present from a prior run (idempotent skip)
	StopsCreated   int // newly created this run
	StopsExisting  int // already present (reused, possibly from cmd/seed's own baseline)
}

// Import parses csvData (see ParseCSV) and idempotently loads it into the
// existing routes/stops/route_legs model, tagged routing.SourceCatalogue.
// Every imported route is a single leg (the source data has only
// endpoints, no intermediate stops); every imported stop has NO
// coordinates (routing.Repo.CreateStopNoCoordinates — the source data has
// none); every leg's fare is EstimateFareCents's output, flagged
// fare_estimated.
//
// Re-running Import with the same CSV is a no-op for rows already
// imported: each route's name embeds the source CSV's own OBJECTID (see
// routeName), so GetRouteByName finds the exact same route on a re-run and
// nothing is duplicated.
func Import(ctx context.Context, repo *routing.Repo, csvData io.Reader, fareModel config.CatalogueFareModel) (Stats, error) {
	rows, parseStats, err := ParseCSV(csvData)
	if err != nil {
		return Stats{}, fmt.Errorf("parse csv: %w", err)
	}

	stats := Stats{ParseStats: parseStats}
	stopCache := map[string]routing.Stop{}
	ranksSeen := map[string]bool{}

	getOrCreateStop := func(name string) (routing.Stop, error) {
		if s, ok := stopCache[name]; ok {
			return s, nil
		}
		existing, err := repo.GetStopByName(ctx, name)
		if err == nil {
			stopCache[name] = existing
			stats.StopsExisting++
			return existing, nil
		}
		if !errors.Is(err, routing.ErrNotFound) {
			return routing.Stop{}, err
		}
		created, err := repo.CreateStopNoCoordinates(ctx, name)
		if err != nil {
			return routing.Stop{}, err
		}
		stopCache[name] = created
		stats.StopsCreated++
		return created, nil
	}

	for _, row := range rows {
		origin := Normalize(row.Origin)
		dest := Normalize(row.Destination)
		ranksSeen[origin] = true
		ranksSeen[dest] = true

		name := routeName(origin, dest, row.ObjectID)

		if _, err := repo.GetRouteByName(ctx, name); err == nil {
			stats.RoutesExisting++
			continue // already imported this exact source row
		} else if !errors.Is(err, routing.ErrNotFound) {
			return stats, fmt.Errorf("look up route %q: %w", name, err)
		}

		fromStop, err := getOrCreateStop(origin)
		if err != nil {
			return stats, fmt.Errorf("stop %q: %w", origin, err)
		}
		toStop, err := getOrCreateStop(dest)
		if err != nil {
			return stats, fmt.Errorf("stop %q: %w", dest, err)
		}

		route, err := repo.CreateCatalogueRoute(ctx, name, CatalogueAssociationName)
		if err != nil {
			return stats, fmt.Errorf("create route %q: %w", name, err)
		}

		fareCents := EstimateFareCents(row.DistanceMeters, fareModel)
		if _, err := repo.CreateCatalogueRouteLeg(ctx, route.ID, fromStop.ID, toStop.ID, fareCents, row.DistanceMeters); err != nil {
			return stats, fmt.Errorf("create leg for route %q: %w", name, err)
		}
		stats.RoutesImported++
	}

	stats.UniqueRanks = len(ranksSeen)
	return stats, nil
}

// routeName builds a catalogue route's stable, human-legible, traceable
// name: "<ORIGIN> - <DESTINATION> (CoCT #<OBJECTID>)". Embedding the source
// CSV's own OBJECTID makes re-import trivially idempotent (GetRouteByName
// finds the identical name on a re-run of the same row) and keeps every
// catalogue route individually traceable back to its exact source row —
// including the ~220 origin/destination pairs that appear more than once in
// the source data with a different distance each time (kept as distinct
// routes, per the source data, never deduplicated away).
func routeName(origin, dest string, objectID int) string {
	return fmt.Sprintf("%s - %s (CoCT #%d)", origin, dest, objectID)
}

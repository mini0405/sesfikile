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
// association_name. The source dataset (City of Cape Town open data)
// carries no association attribution at all — unlike cmd/seed's
// hand-seeded demo corridors, which use the real "Cape Town Minibus Taxi
// Association" name. Kept visibly distinct so nobody mistakes a catalogue
// route for association-verified data.
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

// normalizedRow is a Row with its canonical (post-Normalize) origin/dest
// names attached, computed once up front so both the endpoint/median pass
// and the create pass agree exactly on every rank's canonical name.
type normalizedRow struct {
	Row
	origin string
	dest   string
}

// Import parses geojsonData (see ParseGeoJSON) and idempotently loads it
// into the existing routes/stops/route_legs model, tagged
// routing.SourceCatalogue:
//   - Every imported route is a single leg (the source data has only
//     endpoints, no intermediate stops).
//   - Every imported stop gets an APPROXIMATE coordinate: the median of
//     every endpoint position its canonical rank name appears at anywhere
//     in the file (see medianCoordinate in geo.go and rankCoordinates
//     below) — computed in a first pass over the whole file so a rank's
//     coordinate never depends on which row happens to create its stop.
//   - Every route's full polyline is stored via
//     routing.Repo.CreateRouteGeometry, for later display (not rendered by
//     any frontend in this pass).
//   - Every leg's fare is EstimateFareCents's output (distance computed
//     from the real geometry — see ParseGeoJSON), flagged fare_estimated.
//
// Re-running Import with the same file is a no-op for rows already
// imported: each route's name embeds the source dataset's own OBJECTID
// (see routeName), so GetRouteByName finds the exact same route on a
// re-run and nothing is duplicated.
func Import(ctx context.Context, repo *routing.Repo, geojsonData io.Reader, fareModel config.CatalogueFareModel) (Stats, error) {
	rows, parseStats, err := ParseGeoJSON(geojsonData)
	if err != nil {
		return Stats{}, fmt.Errorf("parse geojson: %w", err)
	}

	stats := Stats{ParseStats: parseStats}

	normalized, rankCoordinates := prepareRows(rows)

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

		var created routing.Stop
		if coord, ok := rankCoordinates[name]; ok {
			// coord is [lon, lat] (GeoJSON order); CreateCatalogueStop takes
			// (latitude, longitude).
			created, err = repo.CreateCatalogueStop(ctx, name, coord[1], coord[0])
		} else {
			// Defensive fallback only — every canonical rank that reaches
			// this point came from at least one valid row's endpoint, so
			// rankCoordinates always has an entry for it in practice.
			created, err = repo.CreateStopNoCoordinates(ctx, name)
		}
		if err != nil {
			return routing.Stop{}, err
		}
		stopCache[name] = created
		stats.StopsCreated++
		return created, nil
	}

	for _, row := range normalized {
		ranksSeen[row.origin] = true
		ranksSeen[row.dest] = true

		name := routeName(row.origin, row.dest, row.ObjectID)

		if _, err := repo.GetRouteByName(ctx, name); err == nil {
			stats.RoutesExisting++
			continue // already imported this exact source row
		} else if !errors.Is(err, routing.ErrNotFound) {
			return stats, fmt.Errorf("look up route %q: %w", name, err)
		}

		fromStop, err := getOrCreateStop(row.origin)
		if err != nil {
			return stats, fmt.Errorf("stop %q: %w", row.origin, err)
		}
		toStop, err := getOrCreateStop(row.dest)
		if err != nil {
			return stats, fmt.Errorf("stop %q: %w", row.dest, err)
		}

		route, err := repo.CreateCatalogueRoute(ctx, name, CatalogueAssociationName)
		if err != nil {
			return stats, fmt.Errorf("create route %q: %w", name, err)
		}

		fareCents := EstimateFareCents(row.DistanceMeters, fareModel)
		if _, err := repo.CreateCatalogueRouteLeg(ctx, route.ID, fromStop.ID, toStop.ID, fareCents, row.DistanceMeters); err != nil {
			return stats, fmt.Errorf("create leg for route %q: %w", name, err)
		}

		if len(row.Points) > 0 {
			if err := repo.CreateRouteGeometry(ctx, route.ID, row.Points); err != nil {
				return stats, fmt.Errorf("store geometry for route %q: %w", name, err)
			}
		}

		stats.RoutesImported++
	}

	stats.UniqueRanks = len(ranksSeen)
	return stats, nil
}

// prepareRows normalizes every row's origin/destination once, then builds
// rankCoordinates: for every canonical rank name, the median (see
// medianCoordinate) of every endpoint position it appears at across the
// WHOLE file — as an origin (that row's FIRST polyline point) or as a
// destination (that row's LAST polyline point), regardless of which row
// eventually creates its stop. This first pass is what makes a rank's
// coordinate reflect all of its appearances, not just the first one seen.
func prepareRows(rows []Row) ([]normalizedRow, map[string][2]float64) {
	normalized := make([]normalizedRow, 0, len(rows))
	endpointSamples := map[string][][2]float64{}

	for _, row := range rows {
		origin := Normalize(row.Origin)
		dest := Normalize(row.Destination)
		normalized = append(normalized, normalizedRow{Row: row, origin: origin, dest: dest})

		if len(row.Points) == 0 {
			continue
		}
		endpointSamples[origin] = append(endpointSamples[origin], row.Points[0])
		endpointSamples[dest] = append(endpointSamples[dest], row.Points[len(row.Points)-1])
	}

	rankCoordinates := make(map[string][2]float64, len(endpointSamples))
	for rank, samples := range endpointSamples {
		lon, lat := medianCoordinate(samples)
		rankCoordinates[rank] = [2]float64{lon, lat}
	}

	return normalized, rankCoordinates
}

// routeName builds a catalogue route's stable, human-legible, traceable
// name: "<ORIGIN> - <DESTINATION> (CoCT #<OBJECTID>)". Embedding the source
// dataset's own OBJECTID makes re-import trivially idempotent (GetRouteByName
// finds the identical name on a re-run of the same row) and keeps every
// catalogue route individually traceable back to its exact source row —
// including the ~220 origin/destination pairs that appear more than once in
// the source data with a different distance each time (kept as distinct
// routes, per the source data, never deduplicated away).
func routeName(origin, dest string, objectID int) string {
	return fmt.Sprintf("%s - %s (CoCT #%d)", origin, dest, objectID)
}

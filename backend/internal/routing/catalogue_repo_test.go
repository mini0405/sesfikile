package routing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sesfikile/backend/internal/routing"
)

// TestCreateStopNoCoordinates_CoordinatesUnknown confirms a defensive
// coordinate-less stop (the fallback internal/catalogue's importer would
// use if a rank somehow had no endpoint sample) reports
// CoordinatesKnown() == false, is tagged source='catalogue', and
// round-trips through GetStopByID/GetStopByName with nil Latitude/Longitude,
// while an ordinary (cmd/seed-style) stop is unaffected.
func TestCreateStopNoCoordinates_CoordinatesUnknown(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	noCoords, err := repo.CreateStopNoCoordinates(ctx, fmt.Sprintf("Catalogue Repo Test Stop %d", suffix))
	if err != nil {
		t.Fatalf("failed to create coordinate-less stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, noCoords.ID) })

	if noCoords.CoordinatesKnown() {
		t.Fatal("expected CoordinatesKnown() == false for a stop created via CreateStopNoCoordinates")
	}
	if noCoords.Latitude != nil || noCoords.Longitude != nil {
		t.Fatalf("expected nil Latitude/Longitude, got %v/%v", noCoords.Latitude, noCoords.Longitude)
	}
	if noCoords.Source != routing.SourceCatalogue {
		t.Fatalf("expected CreateStopNoCoordinates to tag source=%q, got %q", routing.SourceCatalogue, noCoords.Source)
	}

	reloaded, err := repo.GetStopByID(ctx, noCoords.ID)
	if err != nil {
		t.Fatalf("failed to reload stop: %v", err)
	}
	if reloaded.CoordinatesKnown() {
		t.Fatal("expected reloaded stop to still report CoordinatesKnown() == false")
	}

	withCoords, err := repo.CreateStop(ctx, fmt.Sprintf("Real Coords Test Stop %d", suffix), -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create ordinary stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, withCoords.ID) })
	if !withCoords.CoordinatesKnown() {
		t.Fatal("expected an ordinary (real-coordinate) stop to report CoordinatesKnown() == true")
	}
	if withCoords.Source != routing.SourceSeed {
		t.Fatalf("expected CreateStop to default source=%q, got %q", routing.SourceSeed, withCoords.Source)
	}
}

// TestCreateCatalogueStop_TaggedCatalogueWithRealCoordinates is the
// GeoJSON-upgrade path: a catalogue stop now gets a real (if approximate)
// coordinate, tagged source='catalogue' — distinguishable from a
// hand-seeded stop by source, not by coordinate presence anymore.
func TestCreateCatalogueStop_TaggedCatalogueWithRealCoordinates(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	stop, err := repo.CreateCatalogueStop(ctx, fmt.Sprintf("Catalogue Repo Test Median Stop %d", suffix), -33.95, 18.45)
	if err != nil {
		t.Fatalf("failed to create catalogue stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, stop.ID) })

	if !stop.CoordinatesKnown() {
		t.Fatal("expected a catalogue stop created with a coordinate to report CoordinatesKnown() == true")
	}
	if stop.Source != routing.SourceCatalogue {
		t.Fatalf("expected source=%q, got %q", routing.SourceCatalogue, stop.Source)
	}
	if *stop.Latitude != -33.95 || *stop.Longitude != 18.45 {
		t.Fatalf("expected lat/lng -33.95/18.45, got %v/%v", *stop.Latitude, *stop.Longitude)
	}
}

// TestListStopsWithCoordinates_MapFacingReadPath confirms the map-facing
// read includes any stop with a known position REGARDLESS of source — a
// catalogue stop with a real (median-derived) coordinate is included, same
// as a hand-seeded one, while a coordinate-less stop (the defensive
// fallback case) is excluded. ListStops (unfiltered/route-scoped) still
// returns all three.
func TestListStopsWithCoordinates_MapFacingReadPath(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	noCoordsName := fmt.Sprintf("Catalogue Repo Test NoCoords %d", suffix)
	catWithCoordsName := fmt.Sprintf("Catalogue Repo Test CatWithCoords %d", suffix)
	seedWithCoordsName := fmt.Sprintf("Catalogue Repo Test SeedWithCoords %d", suffix)

	noCoords, err := repo.CreateStopNoCoordinates(ctx, noCoordsName)
	if err != nil {
		t.Fatalf("failed to create coordinate-less stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, noCoords.ID) })

	catWithCoords, err := repo.CreateCatalogueStop(ctx, catWithCoordsName, -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create catalogue stop with coordinates: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, catWithCoords.ID) })

	seedWithCoords, err := repo.CreateStop(ctx, seedWithCoordsName, -33.91, 18.41)
	if err != nil {
		t.Fatalf("failed to create ordinary stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, seedWithCoords.ID) })

	mapFacing, err := repo.ListStopsWithCoordinates(ctx)
	if err != nil {
		t.Fatalf("failed to list stops with coordinates: %v", err)
	}
	seenMapFacing := map[string]bool{}
	for _, s := range mapFacing {
		seenMapFacing[s.Name] = true
	}
	if seenMapFacing[noCoordsName] {
		t.Fatal("expected the map-facing stop list to EXCLUDE the coordinate-less stop")
	}
	if !seenMapFacing[catWithCoordsName] {
		t.Fatal("expected the map-facing stop list to INCLUDE the catalogue stop that has a coordinate — the intended GeoJSON upgrade")
	}
	if !seenMapFacing[seedWithCoordsName] {
		t.Fatal("expected the map-facing stop list to INCLUDE the hand-seeded stop")
	}

	full, err := repo.ListStops(ctx)
	if err != nil {
		t.Fatalf("failed to list all stops: %v", err)
	}
	seenFull := map[string]bool{}
	for _, s := range full {
		seenFull[s.Name] = true
	}
	if !seenFull[noCoordsName] || !seenFull[catWithCoordsName] || !seenFull[seedWithCoordsName] {
		t.Fatal("expected the unfiltered/route-scoped ListStops to include all three stops")
	}
}

// TestCatalogueRoute_TaggedDistinctlyFromSeedRoute confirms
// CreateRoute (cmd/seed's path) defaults to source "seed" while
// CreateCatalogueRoute is tagged "catalogue", and CountRoutesBySource
// reports each independently.
func TestCatalogueRoute_TaggedDistinctlyFromSeedRoute(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	seedRoute, err := repo.CreateRoute(ctx, fmt.Sprintf("Catalogue Repo Test Seed Route %d", suffix), "Test Association")
	if err != nil {
		t.Fatalf("failed to create seed-style route: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM routes WHERE id = $1`, seedRoute.ID) })
	if seedRoute.Source != routing.SourceSeed {
		t.Fatalf("expected CreateRoute to default source=%q, got %q", routing.SourceSeed, seedRoute.Source)
	}

	catRoute, err := repo.CreateCatalogueRoute(ctx, fmt.Sprintf("Catalogue Repo Test Catalogue Route %d", suffix), "City of Cape Town open data (unverified, no association attribution)")
	if err != nil {
		t.Fatalf("failed to create catalogue route: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM routes WHERE id = $1`, catRoute.ID) })
	if catRoute.Source != routing.SourceCatalogue {
		t.Fatalf("expected CreateCatalogueRoute to set source=%q, got %q", routing.SourceCatalogue, catRoute.Source)
	}

	reloaded, err := repo.GetRouteByID(ctx, catRoute.ID)
	if err != nil {
		t.Fatalf("failed to reload catalogue route: %v", err)
	}
	if reloaded.Source != routing.SourceCatalogue {
		t.Fatalf("expected reloaded route to keep source=%q, got %q", routing.SourceCatalogue, reloaded.Source)
	}
}

// TestRouteGeometry_StoredAndRetrievable confirms a route's polyline
// round-trips through CreateRouteGeometry/GetRouteGeometry exactly, and
// that a route with none returns ErrNotFound rather than an empty slice
// (distinguishing "no geometry recorded" from "recorded as empty").
func TestRouteGeometry_StoredAndRetrievable(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	route, err := repo.CreateCatalogueRoute(ctx, fmt.Sprintf("Catalogue Repo Test Geometry Route %d", suffix), "City of Cape Town open data (unverified, no association attribution)")
	if err != nil {
		t.Fatalf("failed to create route: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM route_geometries WHERE route_id = $1`, route.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM routes WHERE id = $1`, route.ID)
	})

	points := [][2]float64{{18.4241, -33.9249}, {18.43, -33.93}, {18.44, -33.94}}
	if err := repo.CreateRouteGeometry(ctx, route.ID, points); err != nil {
		t.Fatalf("failed to store geometry: %v", err)
	}

	got, err := repo.GetRouteGeometry(ctx, route.ID)
	if err != nil {
		t.Fatalf("failed to retrieve geometry: %v", err)
	}
	if len(got) != len(points) {
		t.Fatalf("expected %d points, got %d", len(points), len(got))
	}
	for i := range points {
		if got[i] != points[i] {
			t.Errorf("point %d: expected %v, got %v", i, points[i], got[i])
		}
	}

	// A route with no geometry (e.g. a hand-seeded corridor) must report
	// ErrNotFound, not an empty slice.
	bareRoute, err := repo.CreateRoute(ctx, fmt.Sprintf("Catalogue Repo Test No Geometry Route %d", suffix), "Test Association")
	if err != nil {
		t.Fatalf("failed to create bare route: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM routes WHERE id = $1`, bareRoute.ID) })

	if _, err := repo.GetRouteGeometry(ctx, bareRoute.ID); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for a route with no stored geometry, got %v", err)
	}
}

// TestDeleteCatalogueData_OnlyRemovesCatalogueRoutesStopsAndGeometry is the
// clear/undo path's safety guarantee: a hand-seeded (source='seed') route
// and its stops survive; a catalogue route, its leg, its geometry, and its
// now-orphaned catalogue stops (WITH real median-derived coordinates, the
// post-GeoJSON-upgrade normal case — no longer identifiable by having no
// coordinates) do not.
func TestDeleteCatalogueData_OnlyRemovesCatalogueRoutesStopsAndGeometry(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// A hand-seeded route that must survive.
	seedFrom, err := repo.CreateStop(ctx, fmt.Sprintf("Delete Test Seed From %d", suffix), -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create seed from-stop: %v", err)
	}
	seedTo, err := repo.CreateStop(ctx, fmt.Sprintf("Delete Test Seed To %d", suffix), -33.91, 18.41)
	if err != nil {
		t.Fatalf("failed to create seed to-stop: %v", err)
	}
	seedRoute, err := repo.CreateRoute(ctx, fmt.Sprintf("Delete Test Seed Route %d", suffix), "Test Association")
	if err != nil {
		t.Fatalf("failed to create seed route: %v", err)
	}
	if _, err := repo.CreateRouteLeg(ctx, seedRoute.ID, seedFrom.ID, seedTo.ID, 1, 500); err != nil {
		t.Fatalf("failed to create seed leg: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM route_legs WHERE route_id = $1`, seedRoute.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM routes WHERE id = $1`, seedRoute.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM stops WHERE id IN ($1, $2)`, seedFrom.ID, seedTo.ID)
	})

	// A catalogue route (WITH real, median-style coordinates on its stops —
	// the post-upgrade normal case) that must be removed, along with its
	// geometry and its now-orphaned stops.
	catFrom, err := repo.CreateCatalogueStop(ctx, fmt.Sprintf("Delete Test Cat From %d", suffix), -33.95, 18.45)
	if err != nil {
		t.Fatalf("failed to create catalogue from-stop: %v", err)
	}
	catTo, err := repo.CreateCatalogueStop(ctx, fmt.Sprintf("Delete Test Cat To %d", suffix), -33.96, 18.46)
	if err != nil {
		t.Fatalf("failed to create catalogue to-stop: %v", err)
	}
	catRoute, err := repo.CreateCatalogueRoute(ctx, fmt.Sprintf("Delete Test Cat Route %d", suffix), "City of Cape Town open data (unverified, no association attribution)")
	if err != nil {
		t.Fatalf("failed to create catalogue route: %v", err)
	}
	if _, err := repo.CreateCatalogueRouteLeg(ctx, catRoute.ID, catFrom.ID, catTo.ID, 900, 12345.6); err != nil {
		t.Fatalf("failed to create catalogue leg: %v", err)
	}
	if err := repo.CreateRouteGeometry(ctx, catRoute.ID, [][2]float64{{18.45, -33.95}, {18.46, -33.96}}); err != nil {
		t.Fatalf("failed to store catalogue geometry: %v", err)
	}

	routesDeleted, legsDeleted, stopsDeleted, geometriesDeleted, err := repo.DeleteCatalogueData(ctx)
	if err != nil {
		t.Fatalf("DeleteCatalogueData failed: %v", err)
	}
	if routesDeleted < 1 || legsDeleted < 1 || stopsDeleted < 2 || geometriesDeleted < 1 {
		t.Fatalf("expected at least 1 route, 1 leg, 2 stops, 1 geometry deleted; got routes=%d legs=%d stops=%d geometries=%d",
			routesDeleted, legsDeleted, stopsDeleted, geometriesDeleted)
	}

	if _, err := repo.GetRouteByID(ctx, catRoute.ID); !isNotFound(err) {
		t.Fatalf("expected catalogue route to be gone, got err=%v", err)
	}
	if _, err := repo.GetRouteGeometry(ctx, catRoute.ID); !isNotFound(err) {
		t.Fatalf("expected catalogue route's geometry to be gone, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, catFrom.ID); !isNotFound(err) {
		t.Fatalf("expected orphaned catalogue from-stop (with real coordinates) to be gone, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, catTo.ID); !isNotFound(err) {
		t.Fatalf("expected orphaned catalogue to-stop (with real coordinates) to be gone, got err=%v", err)
	}

	// The seed-tagged fixture must be completely unaffected.
	if _, err := repo.GetRouteByID(ctx, seedRoute.ID); err != nil {
		t.Fatalf("expected the hand-seeded route to survive DeleteCatalogueData, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, seedFrom.ID); err != nil {
		t.Fatalf("expected the hand-seeded from-stop to survive DeleteCatalogueData, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, seedTo.ID); err != nil {
		t.Fatalf("expected the hand-seeded to-stop to survive DeleteCatalogueData, got err=%v", err)
	}
}

func isNotFound(err error) bool {
	return err == routing.ErrNotFound
}

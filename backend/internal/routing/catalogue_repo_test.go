package routing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sesfikile/backend/internal/routing"
)

// TestCreateStopNoCoordinates_CoordinatesUnknown confirms a catalogue-style
// stop reports CoordinatesKnown() == false and round-trips through
// GetStopByID/GetStopByName with nil Latitude/Longitude, while an ordinary
// (cmd/seed-style) stop is unaffected.
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
}

// TestListStopsWithCoordinates_ExcludesCoordinatelessStops is the
// map-facing read path's core guarantee: a coordinate-less
// (catalogue-style) stop never appears in ListStopsWithCoordinates, while a
// real-coordinate stop does — and ListStops (the unfiltered/route-scoped
// variant) still returns both.
func TestListStopsWithCoordinates_ExcludesCoordinatelessStops(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	noCoordsName := fmt.Sprintf("Catalogue Repo Test NoCoords %d", suffix)
	withCoordsName := fmt.Sprintf("Catalogue Repo Test WithCoords %d", suffix)

	noCoords, err := repo.CreateStopNoCoordinates(ctx, noCoordsName)
	if err != nil {
		t.Fatalf("failed to create coordinate-less stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, noCoords.ID) })

	withCoords, err := repo.CreateStop(ctx, withCoordsName, -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create ordinary stop: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM stops WHERE id = $1`, withCoords.ID) })

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
	if !seenMapFacing[withCoordsName] {
		t.Fatal("expected the map-facing stop list to INCLUDE the real-coordinate stop")
	}

	full, err := repo.ListStops(ctx)
	if err != nil {
		t.Fatalf("failed to list all stops: %v", err)
	}
	seenFull := map[string]bool{}
	for _, s := range full {
		seenFull[s.Name] = true
	}
	if !seenFull[noCoordsName] || !seenFull[withCoordsName] {
		t.Fatal("expected the unfiltered/route-scoped ListStops to still include both stops")
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

// TestDeleteCatalogueData_OnlyRemovesCatalogueRoutesAndOrphanedStops is the
// clear/undo path's safety guarantee: a hand-seeded (source='seed') route
// and its stops survive; a catalogue route, its leg, and its now-orphaned
// coordinate-less stops do not.
func TestDeleteCatalogueData_OnlyRemovesCatalogueRoutesAndOrphanedStops(t *testing.T) {
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

	// A catalogue route that must be removed, along with its now-orphaned
	// coordinate-less stops.
	catFrom, err := repo.CreateStopNoCoordinates(ctx, fmt.Sprintf("Delete Test Cat From %d", suffix))
	if err != nil {
		t.Fatalf("failed to create catalogue from-stop: %v", err)
	}
	catTo, err := repo.CreateStopNoCoordinates(ctx, fmt.Sprintf("Delete Test Cat To %d", suffix))
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

	routesDeleted, legsDeleted, stopsDeleted, err := repo.DeleteCatalogueData(ctx)
	if err != nil {
		t.Fatalf("DeleteCatalogueData failed: %v", err)
	}
	if routesDeleted < 1 || legsDeleted < 1 || stopsDeleted < 2 {
		t.Fatalf("expected at least 1 route, 1 leg, 2 stops deleted; got routes=%d legs=%d stops=%d", routesDeleted, legsDeleted, stopsDeleted)
	}

	if _, err := repo.GetRouteByID(ctx, catRoute.ID); !isNotFound(err) {
		t.Fatalf("expected catalogue route to be gone, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, catFrom.ID); !isNotFound(err) {
		t.Fatalf("expected orphaned catalogue from-stop to be gone, got err=%v", err)
	}
	if _, err := repo.GetStopByID(ctx, catTo.ID); !isNotFound(err) {
		t.Fatalf("expected orphaned catalogue to-stop to be gone, got err=%v", err)
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

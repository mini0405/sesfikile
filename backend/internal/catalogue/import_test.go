package catalogue_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/catalogue"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/routing"
)

// setup connects to a real Postgres and applies migrations, skipping (not
// failing) the test if none is reachable — matching every other DB-backed
// test in this repo.
func setup(t *testing.T) (*routing.Repo, *pgxpool.Pool) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil || pool.Ping(ctx) != nil {
		t.Skip("skipping test: no reachable Postgres database")
	}
	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}
	t.Cleanup(pool.Close)

	return routing.NewRepo(pool), pool
}

func testFareModel() config.CatalogueFareModel {
	return config.CatalogueFareModel{BaseCents: 500, PerKmCents: 150, MinFareCents: 600, MaxFareCents: 6000}
}

// syntheticCSV builds a small CSV fixture whose every rank name embeds a
// fresh per-run suffix, so this suite never collides with (or needs to
// touch) real backend/data/taxi_routes.csv data, cmd/seed's baseline, or a
// prior test run's own rows. Cleaned up precisely via cleanupSuffix, never
// via a blanket "delete all catalogue data" — this runs against the shared
// dev Postgres, and a developer may have a real catalogue import loaded
// that this suite must not touch.
func syntheticCSV(suffix string) string {
	rows := []string{
		fmt.Sprintf("1,ORIGIN A %s,DEST B %s,5000.0", suffix, suffix), // directional pair, forward
		fmt.Sprintf("2,DEST B %s,ORIGIN A %s,5000.0", suffix, suffix), // directional pair, reverse — distinct
		fmt.Sprintf("3,ORIGIN A %s,DEST B %s,7000.0", suffix, suffix), // same pair, different distance — distinct
		fmt.Sprintf("4,,BLANK DEST %s,1000.0", suffix),                // blank origin — dropped
	}
	return "OBJECTID,ORGN,DSTN,SHAPE_Length\n" + strings.Join(rows, "\n") + "\n"
}

// cleanupSuffix deletes exactly the rows a syntheticCSV(suffix) import could
// have created: route_legs for routes whose from/to stop names contain the
// suffix, those routes, and those stops — precise, not a blanket sweep.
func cleanupSuffix(t *testing.T, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	ctx := context.Background()
	pattern := "%" + suffix + "%"
	_, _ = pool.Exec(ctx, `
		DELETE FROM route_legs
		WHERE from_stop_id IN (SELECT id FROM stops WHERE name LIKE $1)
		   OR to_stop_id IN (SELECT id FROM stops WHERE name LIKE $1)`, pattern)
	_, _ = pool.Exec(ctx, `DELETE FROM routes WHERE name LIKE $1`, pattern)
	_, _ = pool.Exec(ctx, `DELETE FROM stops WHERE name LIKE $1`, pattern)
}

func TestImport_ParsesAndTagsCatalogueSource(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })

	stats, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticCSV(suffix)), testFareModel())
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if stats.TotalDataRows != 4 {
		t.Errorf("expected 4 total data rows, got %d", stats.TotalDataRows)
	}
	if stats.BlankDropped != 1 {
		t.Errorf("expected 1 blank row dropped, got %d", stats.BlankDropped)
	}
	if stats.RoutesImported != 3 {
		t.Errorf("expected 3 routes imported (the blank row dropped), got %d", stats.RoutesImported)
	}

	route, err := repo.GetRouteByName(ctx, fmt.Sprintf("ORIGIN A %s - DEST B %s (CoCT #1)", suffix, suffix))
	if err != nil {
		t.Fatalf("expected the imported route to exist: %v", err)
	}
	if route.Source != routing.SourceCatalogue {
		t.Errorf("expected source=%q, got %q", routing.SourceCatalogue, route.Source)
	}

	legs, err := repo.ListLegsForRoute(ctx, route.ID)
	if err != nil || len(legs) != 1 {
		t.Fatalf("expected exactly 1 leg for a catalogue route, got %d (err=%v)", len(legs), err)
	}
	leg := legs[0]
	if !leg.FareEstimated {
		t.Error("expected the imported leg's fare to be flagged fare_estimated=true")
	}
	if leg.DistanceMeters == nil || *leg.DistanceMeters != 5000.0 {
		t.Errorf("expected distance_meters=5000.0, got %v", leg.DistanceMeters)
	}
	wantFare := catalogue.EstimateFareCents(5000.0, testFareModel())
	if leg.FareCents != wantFare {
		t.Errorf("expected fare_cents=%d (from EstimateFareCents), got %d", wantFare, leg.FareCents)
	}

	fromStop, err := repo.GetStopByID(ctx, leg.FromStopID)
	if err != nil {
		t.Fatalf("failed to load from-stop: %v", err)
	}
	if fromStop.CoordinatesKnown() {
		t.Error("expected a catalogue-imported stop to have NO known coordinates")
	}
}

// TestImport_DirectionalAndDuplicateDistanceRoutesKeptDistinct is the
// brief's explicit non-negotiable: A->B and B->A are distinct routes, and
// two rows sharing an origin/destination pair but a different distance are
// ALSO kept as distinct routes (traced back to their own OBJECTID), never
// deduplicated away.
func TestImport_DirectionalAndDuplicateDistanceRoutesKeptDistinct(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })

	if _, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticCSV(suffix)), testFareModel()); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	forward := fmt.Sprintf("ORIGIN A %s - DEST B %s (CoCT #1)", suffix, suffix)
	reverse := fmt.Sprintf("DEST B %s - ORIGIN A %s (CoCT #2)", suffix, suffix)
	dup := fmt.Sprintf("ORIGIN A %s - DEST B %s (CoCT #3)", suffix, suffix)

	for _, name := range []string{forward, reverse, dup} {
		if _, err := repo.GetRouteByName(ctx, name); err != nil {
			t.Errorf("expected route %q to exist: %v", name, err)
		}
	}

	// The forward route and the duplicate-pair route (#1 vs #3) must be
	// genuinely different route rows, not the same one reused.
	r1, _ := repo.GetRouteByName(ctx, forward)
	r3, _ := repo.GetRouteByName(ctx, dup)
	if r1.ID == r3.ID {
		t.Fatal("expected the same origin/destination pair with a different source row to create a DISTINCT route, not reuse the first one")
	}
}

// TestImport_Idempotent confirms re-running Import against the identical
// CSV creates nothing new — every row is recognized as already imported via
// its OBJECTID-embedding route name.
func TestImport_Idempotent(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })
	csvText := syntheticCSV(suffix)

	first, err := catalogue.Import(ctx, repo, strings.NewReader(csvText), testFareModel())
	if err != nil {
		t.Fatalf("first Import failed: %v", err)
	}

	second, err := catalogue.Import(ctx, repo, strings.NewReader(csvText), testFareModel())
	if err != nil {
		t.Fatalf("second Import failed: %v", err)
	}

	if second.RoutesImported != 0 {
		t.Fatalf("expected 0 newly-imported routes on the second run, got %d", second.RoutesImported)
	}
	if second.RoutesExisting != first.RoutesImported {
		t.Fatalf("expected the second run's RoutesExisting (%d) to equal the first run's RoutesImported (%d)", second.RoutesExisting, first.RoutesImported)
	}
	if second.StopsCreated != 0 {
		t.Fatalf("expected 0 newly-created stops on the second run, got %d", second.StopsCreated)
	}
}

// TestImport_DoesNotAffectSeedBaseline is this feature's core non-negotiable:
// importing the catalogue must never change cmd/seed's own hand-seeded
// route count.
func TestImport_DoesNotAffectSeedBaseline(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := routing.SeedCorridors(ctx, repo); err != nil {
		t.Fatalf("failed to seed baseline corridors: %v", err)
	}
	seedCountBefore, err := repo.CountRoutesBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed routes: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })
	if _, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticCSV(suffix)), testFareModel()); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	seedCountAfter, err := repo.CountRoutesBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed routes after import: %v", err)
	}
	if seedCountAfter != seedCountBefore {
		t.Fatalf("expected the seed-tagged route count to stay exactly %d after a catalogue import, got %d", seedCountBefore, seedCountAfter)
	}
}

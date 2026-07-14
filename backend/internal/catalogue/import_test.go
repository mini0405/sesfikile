package catalogue_test

import (
	"context"
	"encoding/json"
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

// Minimal GeoJSON structs mirroring the real source shape (see
// internal/catalogue/geojson.go's unexported equivalents) — kept separate
// and test-local since this is an external test package; the JSON wire
// shape is the stable contract being tested, not the internal Go types.
type testFeatureCollection struct {
	Type     string        `json:"type"`
	Features []testFeature `json:"features"`
}

type testFeature struct {
	Type       string         `json:"type"`
	Properties testProperties `json:"properties"`
	Geometry   testGeometry   `json:"geometry"`
}

type testProperties struct {
	ObjectID    int     `json:"OBJECTID"`
	Origin      string  `json:"ORGN"`
	Destination string  `json:"DSTN"`
	ShapeLength float64 `json:"SHAPE_Length"`
}

type testGeometry struct {
	Type        string         `json:"type"`
	Coordinates [][][2]float64 `json:"coordinates"`
}

func syntheticGeoJSON(features []testFeature) string {
	fc := testFeatureCollection{Type: "FeatureCollection", Features: features}
	data, err := json.Marshal(fc)
	if err != nil {
		panic(err) // test fixture construction — a marshal failure here is a test bug
	}
	return string(data)
}

// simpleFeature builds a 2-point straight-line feature (origin -> dest),
// enough geometry for ParseGeoJSON's endpoint/distance logic to exercise
// fully without needing hundreds of realistic shape points.
func simpleFeature(objectID int, origin, dest string, originLon, originLat, destLon, destLat float64) testFeature {
	return testFeature{
		Type:       "Feature",
		Properties: testProperties{ObjectID: objectID, Origin: origin, Destination: dest, ShapeLength: 0.01},
		Geometry: testGeometry{
			Type:        "MultiLineString",
			Coordinates: [][][2]float64{{{originLon, originLat}, {destLon, destLat}}},
		},
	}
}

// syntheticFeatures builds the same small fixture set every test below
// shares: a directional pair, a same-pair-different-distance duplicate, and
// a blank-origin row — every rank name embeds a fresh per-run suffix so
// this suite never collides with (or needs to touch) real
// backend/data/taxi_routes.json data, cmd/seed's baseline, or a prior test
// run's own rows. Cleaned up precisely via cleanupSuffix, never via a
// blanket "delete all catalogue data" — this runs against the shared dev
// Postgres, and a developer may have a real import loaded that this suite
// must not touch.
func syntheticFeatures(suffix string) []testFeature {
	originA := "ORIGIN A " + suffix
	destB := "DEST B " + suffix
	blankDest := "BLANK DEST " + suffix

	return []testFeature{
		simpleFeature(1, originA, destB, 18.40, -33.90, 18.45, -33.95), // directional pair, forward
		simpleFeature(2, destB, originA, 18.45, -33.95, 18.40, -33.90), // directional pair, reverse — distinct
		simpleFeature(3, originA, destB, 18.40, -33.90, 18.50, -34.00), // same pair, different distance — distinct
		simpleFeature(4, "", blankDest, 0, 0, 18.4, -33.9),             // blank origin — dropped
	}
}

func syntheticCSV(suffix string) string {
	return syntheticGeoJSON(syntheticFeatures(suffix))
}

// cleanupSuffix deletes exactly the rows a syntheticFeatures(suffix) import
// could have created: geometry + legs for routes whose from/to stop names
// contain the suffix, those routes, and those stops — precise, not a
// blanket sweep.
func cleanupSuffix(t *testing.T, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	ctx := context.Background()
	pattern := "%" + suffix + "%"
	_, _ = pool.Exec(ctx, `
		DELETE FROM route_geometries
		WHERE route_id IN (
			SELECT rl.route_id FROM route_legs rl
			JOIN stops s ON s.id = rl.from_stop_id OR s.id = rl.to_stop_id
			WHERE s.name LIKE $1
		)`, pattern)
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
	if leg.DistanceMeters == nil || *leg.DistanceMeters <= 0 {
		t.Errorf("expected a positive computed distance_meters, got %v", leg.DistanceMeters)
	}
	wantFare := catalogue.EstimateFareCents(*leg.DistanceMeters, testFareModel())
	if leg.FareCents != wantFare {
		t.Errorf("expected fare_cents=%d (from EstimateFareCents), got %d", wantFare, leg.FareCents)
	}

	// Post-GeoJSON-upgrade: a catalogue stop now HAS a (median-derived,
	// approximate) coordinate, tagged source='catalogue'.
	fromStop, err := repo.GetStopByID(ctx, leg.FromStopID)
	if err != nil {
		t.Fatalf("failed to load from-stop: %v", err)
	}
	if !fromStop.CoordinatesKnown() {
		t.Error("expected a catalogue-imported stop to HAVE a known (approximate) coordinate")
	}
	if fromStop.Source != routing.SourceCatalogue {
		t.Errorf("expected the stop's source=%q, got %q", routing.SourceCatalogue, fromStop.Source)
	}

	// The route's polyline must be stored and retrievable.
	points, err := repo.GetRouteGeometry(ctx, route.ID)
	if err != nil {
		t.Fatalf("expected the route's geometry to be stored: %v", err)
	}
	if len(points) != 2 {
		t.Errorf("expected 2 stored points (the synthetic fixture's straight line), got %d", len(points))
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
// data creates nothing new — every row is recognized as already imported
// via its OBJECTID-embedding route name.
func TestImport_Idempotent(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })
	data := syntheticCSV(suffix)

	first, err := catalogue.Import(ctx, repo, strings.NewReader(data), testFareModel())
	if err != nil {
		t.Fatalf("first Import failed: %v", err)
	}

	second, err := catalogue.Import(ctx, repo, strings.NewReader(data), testFareModel())
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
// route or stop count.
func TestImport_DoesNotAffectSeedBaseline(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := routing.SeedCorridors(ctx, repo); err != nil {
		t.Fatalf("failed to seed baseline corridors: %v", err)
	}
	seedRouteCountBefore, err := repo.CountRoutesBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed routes: %v", err)
	}
	seedStopCountBefore, err := repo.CountStopsBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed stops: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })
	if _, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticCSV(suffix)), testFareModel()); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	seedRouteCountAfter, err := repo.CountRoutesBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed routes after import: %v", err)
	}
	if seedRouteCountAfter != seedRouteCountBefore {
		t.Fatalf("expected the seed-tagged route count to stay exactly %d after a catalogue import, got %d", seedRouteCountBefore, seedRouteCountAfter)
	}

	seedStopCountAfter, err := repo.CountStopsBySource(ctx, routing.SourceSeed)
	if err != nil {
		t.Fatalf("failed to count seed stops after import: %v", err)
	}
	if seedStopCountAfter != seedStopCountBefore {
		t.Fatalf("expected the seed-tagged stop count to stay exactly %d after a catalogue import, got %d", seedStopCountBefore, seedStopCountAfter)
	}
}

// TestImport_EveryCreatedStopHasACoordinate is the "no orphans in either
// direction" guarantee: every stop a catalogue import creates has a known
// coordinate (median-derived), and — since every canonical rank name that
// gets a stop came from at least one parsed row's endpoint — no rank is
// ever left stop-less or coordinate-less by a normal import.
func TestImport_EveryCreatedStopHasACoordinate(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })

	if _, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticCSV(suffix)), testFareModel()); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	for _, name := range []string{"ORIGIN A " + suffix, "DEST B " + suffix} {
		stop, err := repo.GetStopByName(ctx, name)
		if err != nil {
			t.Fatalf("expected stop %q to exist: %v", name, err)
		}
		if !stop.CoordinatesKnown() {
			t.Errorf("expected stop %q to have a known coordinate, got none", name)
		}
	}
}

func TestImport_MedianCoordinateResistsOutliers(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupSuffix(t, pool, suffix) })

	rank := "MEDIAN TEST RANK " + suffix
	dest := "MEDIAN TEST DEST " + suffix

	// A tight cluster of endpoint samples around (18.400, -33.900), plus one
	// wild outlier nowhere near Cape Town — mirrors a real rank (e.g.
	// Khayelitsha) whose routes depart from genuinely spread-out points.
	features := []testFeature{
		simpleFeature(1, rank, dest, 18.398, -33.898, 18.5, -34.0),
		simpleFeature(2, rank, dest, 18.399, -33.899, 18.5, -34.0),
		simpleFeature(3, rank, dest, 18.400, -33.900, 18.5, -34.0),
		simpleFeature(4, rank, dest, 18.401, -33.901, 18.5, -34.0),
		simpleFeature(5, rank, dest, 18.402, -33.902, 18.5, -34.0),
		simpleFeature(6, rank, dest, 25.0, -29.0, 18.5, -34.0), // outlier
	}

	if _, err := catalogue.Import(ctx, repo, strings.NewReader(syntheticGeoJSON(features)), testFareModel()); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	stop, err := repo.GetStopByName(ctx, rank)
	if err != nil {
		t.Fatalf("expected rank stop to exist: %v", err)
	}
	if !stop.CoordinatesKnown() {
		t.Fatal("expected the rank stop to have a coordinate")
	}

	lon, lat := *stop.Longitude, *stop.Latitude
	// Median of {18.398, 18.399, 18.400, 18.401, 18.402, 25.0} = (18.400+18.401)/2 = 18.4005
	if diff := lon - 18.4005; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected median longitude near 18.4005 (the cluster), got %v", lon)
	}
	if diff := lat - (-33.9005); diff > 0.01 || diff < -0.01 {
		t.Errorf("expected median latitude near -33.9005 (the cluster), got %v", lat)
	}

	// The MEAN would have been dragged far toward the outlier — confirm the
	// actual (median) result is nowhere near it.
	meanLon := (18.398 + 18.399 + 18.400 + 18.401 + 18.402 + 25.0) / 6
	if diff := lon - meanLon; diff < 0.5 && diff > -0.5 {
		t.Errorf("stop longitude (%v) is suspiciously close to the MEAN (%v) — expected the median to resist the outlier", lon, meanLon)
	}
}

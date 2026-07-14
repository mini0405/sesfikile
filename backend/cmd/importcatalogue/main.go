// Command importcatalogue is an OPT-IN loader for the real City of Cape Town
// taxi-routes open dataset (backend/data/taxi_routes.json, a GeoJSON
// FeatureCollection — 1466 features of real origin/destination rank names,
// route distances, AND full polyline geometry) into the existing
// routes/stops/route_legs model, tagged source='catalogue' so it never
// touches or is confused with cmd/seed's 8-corridor/12-stop hand-seeded demo
// baseline (source='seed'). See docs/PROGRESS.md's "Real route catalogue
// import: GeoJSON upgrade" entry for exactly what's real vs approximate vs
// estimated vs missing, backend/data/README.md for how to obtain the file,
// and internal/catalogue for the importer itself.
//
// PROVENANCE: City of Cape Town open data (Copyright: Western Cape
// Government, Department of Transport and Public Works). Source API (for
// reference only — never fetched at runtime):
// https://citymaps.capetown.gov.za/agsext/rest/services/Theme_Based/ODP_SPLIT_6/FeatureServer/11
//
// Usage:
//
//	go run ./cmd/importcatalogue                          # uses data/taxi_routes.json
//	go run ./cmd/importcatalogue -geojson path/to/file.json
//
// Idempotent — safe to re-run; rows already imported (matched by the source
// dataset's own OBJECTID, embedded in each route's name) are skipped, not
// duplicated. Pairs with cmd/clearcatalogue to undo without disturbing the
// seeded baseline.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/catalogue"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/routing"
)

func main() {
	geojsonPath := flag.String("geojson", "data/taxi_routes.json", "path to the City of Cape Town taxi-routes GeoJSON")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply migrations: %v\n", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	f, err := os.Open(*geojsonPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %s: %v\n(see backend/data/README.md for how to obtain this file — it is not committed/fetched automatically)\n", *geojsonPath, err)
		os.Exit(1)
	}
	defer f.Close()

	repo := routing.NewRepo(pool)
	stats, err := catalogue.Import(ctx, repo, f, cfg.CatalogueFare)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Real Cape Town route catalogue import — OPT-IN, tagged source='catalogue'")
	fmt.Println("===========================================================================")
	fmt.Println()
	fmt.Println("Source: City of Cape Town open data (\"SL_CGIS_TAXI_RTS\"). Copyright: Western")
	fmt.Println("Cape Government, Department of Transport and Public Works.")
	fmt.Println()
	fmt.Println("REAL: origin/destination rank names, route distances (computed from the")
	fmt.Println("actual polyline geometry), and each route's full polyline (stored for later")
	fmt.Println("display — see GET /routes/{id}/geometry).")
	fmt.Println("APPROXIMATE: every stop's coordinate is the MEDIAN of every endpoint position")
	fmt.Println("its rank name appears at across the whole dataset — a derived centroid, not a")
	fmt.Println("surveyed rank position.")
	fmt.Println("ESTIMATED: every fare below (distance-derived, NOT an actual association")
	fmt.Println("tariff — see fare_estimated on each leg).")
	fmt.Println("MISSING: named intermediate stops (a polyline's vertices are shape points, not")
	fmt.Println("boarding stops — each route stays a single origin->destination leg) and")
	fmt.Println("association sign-off.")
	fmt.Println()
	fmt.Printf("Source rows read:        %d\n", stats.TotalDataRows)
	fmt.Printf("Blank rows dropped:      %d\n", stats.BlankDropped)
	fmt.Printf("Unique ranks (folded):   %d\n", stats.UniqueRanks)
	fmt.Printf("Routes imported (new):   %d\n", stats.RoutesImported)
	fmt.Printf("Routes already present:  %d\n", stats.RoutesExisting)
	fmt.Printf("Stops created (new):     %d\n", stats.StopsCreated)
	fmt.Printf("Stops already present:   %d\n", stats.StopsExisting)
	fmt.Println()
	fmt.Println("Run `go run ./cmd/clearcatalogue -apply` to undo and restore the clean")
	fmt.Println("8-corridor/12-stop seeded baseline.")
}

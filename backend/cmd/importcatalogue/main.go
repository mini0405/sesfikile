// Command importcatalogue is an OPT-IN loader for the real City of Cape Town
// taxi-routes open-data CSV (backend/data/taxi_routes.csv, ~1466 rows of
// real origin/destination rank names + route distances) into the existing
// routes/stops/route_legs model, tagged source='catalogue' so it never
// touches or is confused with cmd/seed's 8-corridor/12-stop hand-seeded demo
// baseline (source='seed'). See docs/PROGRESS.md's "Real route catalogue
// import (opt-in)" entry for exactly what's real vs estimated vs missing,
// and internal/catalogue for the importer itself.
//
// Usage:
//
//	go run ./cmd/importcatalogue                      # uses data/taxi_routes.csv
//	go run ./cmd/importcatalogue -csv path/to/file.csv
//
// Idempotent — safe to re-run; rows already imported (matched by the source
// CSV's own OBJECTID, embedded in each route's name) are skipped, not
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
	csvPath := flag.String("csv", "data/taxi_routes.csv", "path to the City of Cape Town taxi-routes CSV")
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

	f, err := os.Open(*csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %s: %v\n", *csvPath, err)
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
	fmt.Println("REAL: origin/destination rank names, route distances (from the City of Cape")
	fmt.Println("Town open dataset). ESTIMATED: every fare below (distance-derived, NOT an")
	fmt.Println("actual association tariff — see fare_estimated on each leg). MISSING: stop")
	fmt.Println("coordinates (so these routes never appear on the live map or in telemetry),")
	fmt.Println("intermediate stops (each route is a single origin->destination leg), and")
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

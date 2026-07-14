// Command clearcatalogue removes every source='catalogue' route/leg/stop
// added by cmd/importcatalogue, restoring the database to the clean
// cmd/seed 8-corridor/12-stop baseline. SAFE: it only ever touches rows
// tagged source='catalogue' (routes) and coordinate-less orphaned stops —
// see routing.Repo.DeleteCatalogueData for the exact scoping guarantee.
// Mirrors cmd/cleanup's shape: defaults to a DRY RUN; pass -apply to
// actually delete.
//
// Usage:
//
//	go run ./cmd/clearcatalogue           # dry run: show what would be removed
//	go run ./cmd/clearcatalogue -apply    # actually delete it
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/catalogue"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/routing"
)

func main() {
	apply := flag.Bool("apply", false, "actually delete the catalogue-imported rows (default is a dry run)")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := routing.NewRepo(pool)

	routeCount, err := repo.CountRoutesBySource(ctx, routing.SourceCatalogue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to count catalogue routes: %v\n", err)
		os.Exit(1)
	}
	stopCount, err := repo.CountStopsWithoutCoordinates(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to count coordinate-less stops: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d catalogue route(s) and %d coordinate-less stop(s) (candidates for removal once orphaned by the routes above).\n", routeCount, stopCount)

	if !*apply {
		fmt.Println("\nDRY RUN — no rows deleted. Re-run with -apply to delete them and restore the clean 8-corridor/12-stop seeded baseline.")
		return
	}

	if routeCount == 0 && stopCount == 0 {
		fmt.Println("\nNothing to delete.")
		return
	}

	stats, err := catalogue.Clear(ctx, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clear failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nDeleted %d route_leg(s), %d route(s), %d stop(s).\n", stats.LegsDeleted, stats.RoutesDeleted, stats.StopsDeleted)
}

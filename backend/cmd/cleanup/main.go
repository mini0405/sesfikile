// Command cleanup removes leftover test-generated routes/stops from the
// persistent dev Postgres — rows created by DB-backed integration tests
// (internal/{boarding,stops,telemetry}) that run against the shared dev
// database rather than a disposable one — and also sweeps long-expired
// boarding_pass_codes rows (short boarding codes, see internal/boarding/
// passstore.go). The latter already happens opportunistically on every
// code-based scan (PassStore.Lookup), so this is only needed to reclaim
// codes that were issued but never scanned again — the common case. It is
// SAFE and IDEMPOTENT:
//
//   - It only ever matches routes/stops whose names match the exact,
//     hand-verified junk-name patterns below (see docs/PROGRESS.md's
//     "Housekeeping" entry for how they were identified) — never the real
//     Cape Town corridors or cmd/seed's other data.
//   - A stop is only deleted if, after the matched junk routes' legs are
//     removed, no route_legs row references it anymore — so a stop can
//     never be deleted while still in use by a real route, even if some
//     future stop happened to share a junk-looking name.
//   - route_legs are deleted before their parent routes (respects the FK),
//     and nothing here touches ledger_transactions/ledger_postings, so the
//     zero-sum ledger trigger is never in play — routes/stops carry no FK
//     from the ledger (fare metadata stores vehicle_id, not route/stop ids).
//   - Defaults to a DRY RUN: it always shows exactly what would be removed
//     first. Pass -apply to actually delete.
//
// Usage:
//
//	go run ./cmd/cleanup            # dry run: show what would be deleted
//	go run ./cmd/cleanup -apply     # actually delete it
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/boarding"
	"sesfikile/backend/internal/config"
)

// routeNamePatterns are the exact SQL LIKE patterns of test-generated route
// names observed accumulating in the dev database — one pattern per
// integration-test fixture that (pre-fix) didn't clean up after itself. See
// docs/PROGRESS.md's "Housekeeping — test-data isolation + cleanup" entry
// for how these were identified (a GROUP BY name query against the live dev
// DB, not guessed).
var routeNamePatterns = []string{
	"Boarding Test Route %",   // internal/boarding/boarding_test.go seedFixture
	"Stops Test Route %",      // internal/stops/integration_test.go seedRoute
	"Telemetry Test Route %",  // internal/telemetry/integration_test.go seedDriverOnRoute
	"Telemetry Other Route %", // internal/telemetry/integration_test.go TestDriverUpdatePropagatesToCommuterOnSameRoute
}

// stopNamePatterns are the matching stop-name patterns from the same
// fixtures. A stop is only ever deleted if it ALSO has zero remaining
// route_legs references after the junk routes above are removed (see the
// query in run()) — matching one of these patterns alone is not sufficient.
var stopNamePatterns = []string{
	"Boarding Test Origin %",
	"Boarding Test Dest %",
	"Stops Test Origin %",
	"Stops Test Mid %",
	"Stops Test Dest %",
	"Telemetry Test Origin %",
	"Telemetry Test Dest %",
}

func main() {
	apply := flag.Bool("apply", false, "actually delete the matched rows (default is a dry run)")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := run(ctx, pool, *apply); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup failed: %v\n", err)
		os.Exit(1)
	}
}

// sweepExpiredBoardingCodes reports (dry run) or deletes (apply) long-expired
// boarding_pass_codes rows — see PassStore's sweepGrace doc comment for why a
// grace period beyond the pass TTL is used rather than deleting the instant a
// code expires (it would otherwise race a driver's scan and turn an
// informative "pass has expired" 410 into an indistinguishable "unknown
// code" 401).
func sweepExpiredBoardingCodes(ctx context.Context, pool *pgxpool.Pool, apply bool) error {
	if !apply {
		var count int
		if err := pool.QueryRow(ctx, boarding.ExpiredCodesCountQuery, boarding.SweepCutoff()).Scan(&count); err != nil {
			return err
		}
		fmt.Printf("Boarding codes: %d expired row(s) would be swept (older than the %s grace period).\n", count, boarding.SweepGrace)
		return nil
	}

	passStore := boarding.NewPassStore(pool)
	deleted, err := passStore.CleanupExpired(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Boarding codes: deleted %d expired row(s).\n", deleted)
	return nil
}

type matchedRoute struct {
	ID   uuid.UUID
	Name string
}

type matchedStop struct {
	ID   uuid.UUID
	Name string
}

func run(ctx context.Context, pool *pgxpool.Pool, apply bool) error {
	if err := sweepExpiredBoardingCodes(ctx, pool, apply); err != nil {
		return fmt.Errorf("failed to sweep expired boarding codes: %w", err)
	}

	routes, err := findMatchedRoutes(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to query matched routes: %w", err)
	}
	routeIDs := make([]uuid.UUID, len(routes))
	for i, r := range routes {
		routeIDs[i] = r.ID
	}

	stops, err := findOrphanableStops(ctx, pool, routeIDs)
	if err != nil {
		return fmt.Errorf("failed to query candidate stops: %w", err)
	}

	fmt.Printf("Matched %d junk route(s):\n", len(routes))
	printRouteSample(routes)
	fmt.Printf("\nMatched %d stop(s) that would become orphaned (no longer referenced by any route_legs) once those routes are removed:\n", len(stops))
	printStopSample(stops)

	if !apply {
		fmt.Println("\nDRY RUN — no rows deleted. Re-run with -apply to delete the rows listed above.")
		return nil
	}

	if len(routes) == 0 && len(stops) == 0 {
		fmt.Println("\nNothing to delete.")
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// boarding_pass_codes.route_id FKs to routes(id) (short boarding codes,
	// see internal/boarding/passstore.go) — a matched junk route can have
	// leftover code rows from short-lived test passes, which would otherwise
	// block the route delete below with a foreign-key violation. These are
	// always long-expired by the time cleanup runs (junk routes only come
	// from finished test runs), so deleting them here is safe regardless of
	// PassStore's own sweepGrace window.
	if _, err := tx.Exec(ctx, `DELETE FROM boarding_pass_codes WHERE route_id = ANY($1)`, routeIDs); err != nil {
		return fmt.Errorf("failed to delete boarding_pass_codes: %w", err)
	}

	legsTag, err := tx.Exec(ctx, `DELETE FROM route_legs WHERE route_id = ANY($1)`, routeIDs)
	if err != nil {
		return fmt.Errorf("failed to delete route_legs: %w", err)
	}
	routesTag, err := tx.Exec(ctx, `DELETE FROM routes WHERE id = ANY($1)`, routeIDs)
	if err != nil {
		return fmt.Errorf("failed to delete routes: %w", err)
	}

	// Re-resolve orphanable stops inside the same transaction, after the
	// junk routes' legs are gone — a stop is deleted only if it matches a
	// junk pattern AND no route_legs row (real or otherwise) references it
	// anymore. This is deliberately re-queried rather than reusing the
	// dry-run stop list, so it stays correct even if something else
	// referenced one of those stops between the dry-run read and this
	// transaction.
	stopsTag, err := tx.Exec(ctx, `
		DELETE FROM stops
		WHERE name LIKE ANY($1::text[])
		AND NOT EXISTS (
			SELECT 1 FROM route_legs rl
			WHERE rl.from_stop_id = stops.id OR rl.to_stop_id = stops.id
		)`, stopNamePatterns)
	if err != nil {
		return fmt.Errorf("failed to delete orphaned stops: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	fmt.Printf("\nDeleted %d route_legs row(s), %d route(s), %d stop(s).\n", legsTag.RowsAffected(), routesTag.RowsAffected(), stopsTag.RowsAffected())
	return nil
}

func findMatchedRoutes(ctx context.Context, pool *pgxpool.Pool) ([]matchedRoute, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name FROM routes WHERE name LIKE ANY($1::text[]) ORDER BY name`,
		routeNamePatterns,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []matchedRoute{}
	for rows.Next() {
		var r matchedRoute
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, rows.Err()
}

// findOrphanableStops returns junk-pattern-matching stops that reference no
// route_legs row outside of routeIDs — i.e. stops that would have zero
// remaining references once routeIDs' legs are deleted. `<> ALL(routeIDs)`
// is vacuously true for every row when routeIDs is empty, so this also
// correctly reports "not referenced by any route" in that case.
func findOrphanableStops(ctx context.Context, pool *pgxpool.Pool, routeIDs []uuid.UUID) ([]matchedStop, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.id, s.name FROM stops s
		WHERE s.name LIKE ANY($1::text[])
		AND NOT EXISTS (
			SELECT 1 FROM route_legs rl
			WHERE (rl.from_stop_id = s.id OR rl.to_stop_id = s.id)
			AND rl.route_id <> ALL($2::uuid[])
		)
		ORDER BY s.name`,
		stopNamePatterns, routeIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stops := []matchedStop{}
	for rows.Next() {
		var s matchedStop
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, err
		}
		stops = append(stops, s)
	}
	return stops, rows.Err()
}

const sampleLimit = 20

func printRouteSample(routes []matchedRoute) {
	for i, r := range routes {
		if i >= sampleLimit {
			fmt.Printf("  ... and %d more\n", len(routes)-sampleLimit)
			break
		}
		fmt.Printf("  %s  id=%s\n", r.Name, r.ID)
	}
}

func printStopSample(stops []matchedStop) {
	for i, s := range stops {
		if i >= sampleLimit {
			fmt.Printf("  ... and %d more\n", len(stops)-sampleLimit)
			break
		}
		fmt.Printf("  %s  id=%s\n", s.Name, s.ID)
	}
}

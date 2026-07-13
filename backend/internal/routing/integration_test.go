package routing_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/routing"
)

// setup connects to a real Postgres and applies migrations, skipping (not
// failing) the test if none is reachable — matching the Stage 0-2 tests'
// approach.
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

// seedTestRoutes creates a small fixture independent of cmd/seed's data:
// R1: A -(100)-> B -(200)-> I (interchange)
// R2: I -(300)-> C
// so a direct search (A->B), a multi-hop search (A->C), and a no-path
// search (C->A, against the routes' fixed direction) all have something
// real to exercise. Rows are cleaned up via t.Cleanup since this runs
// against the shared dev database, not a disposable one.
func seedTestRoutes(t *testing.T, repo *routing.Repo, pool *pgxpool.Pool) (stopIDs map[string]uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	name := func(s string) string { return fmt.Sprintf("%s-%d", s, suffix) }

	stopA, err := repo.CreateStop(ctx, name("A"), -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create stop A: %v", err)
	}
	stopB, err := repo.CreateStop(ctx, name("B"), -33.91, 18.41)
	if err != nil {
		t.Fatalf("failed to create stop B: %v", err)
	}
	stopI, err := repo.CreateStop(ctx, name("I"), -33.92, 18.42)
	if err != nil {
		t.Fatalf("failed to create stop I: %v", err)
	}
	stopC, err := repo.CreateStop(ctx, name("C"), -33.93, 18.43)
	if err != nil {
		t.Fatalf("failed to create stop C: %v", err)
	}

	r1, err := repo.CreateRoute(ctx, name("Route1"), "Test Association")
	if err != nil {
		t.Fatalf("failed to create route 1: %v", err)
	}
	if _, err := repo.CreateRouteLeg(ctx, r1.ID, stopA.ID, stopB.ID, 1, 100); err != nil {
		t.Fatalf("failed to create leg A->B: %v", err)
	}
	if _, err := repo.CreateRouteLeg(ctx, r1.ID, stopB.ID, stopI.ID, 2, 200); err != nil {
		t.Fatalf("failed to create leg B->I: %v", err)
	}

	r2, err := repo.CreateRoute(ctx, name("Route2"), "Test Association")
	if err != nil {
		t.Fatalf("failed to create route 2: %v", err)
	}
	if _, err := repo.CreateRouteLeg(ctx, r2.ID, stopI.ID, stopC.ID, 1, 300); err != nil {
		t.Fatalf("failed to create leg I->C: %v", err)
	}

	// This is a shared dev database (not a disposable per-test one), so
	// clean up the fixture rows this test created rather than leaving them
	// to pollute cmd/seed's SEEDED DATA output.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM route_legs WHERE route_id IN ($1, $2)`, r1.ID, r2.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM routes WHERE id IN ($1, $2)`, r1.ID, r2.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM stops WHERE id IN ($1, $2, $3, $4)`, stopA.ID, stopB.ID, stopI.ID, stopC.ID)
	})

	return map[string]uuid.UUID{
		"A": stopA.ID,
		"B": stopB.ID,
		"I": stopI.ID,
		"C": stopC.ID,
	}
}

func TestRepoSearch_Direct(t *testing.T) {
	repo, pool := setup(t)
	ids := seedTestRoutes(t, repo, pool)

	routes, err := repo.AllRoutesWithLegs(context.Background())
	if err != nil {
		t.Fatalf("failed to load routes: %v", err)
	}

	result, ok := routing.Search(routes, ids["A"], ids["B"])
	if !ok {
		t.Fatal("expected a direct path A->B")
	}
	if result.Transfers != 0 {
		t.Errorf("expected 0 transfers, got %d", result.Transfers)
	}
	if result.TotalFareCents != 100 {
		t.Errorf("expected fare 100, got %d", result.TotalFareCents)
	}
}

func TestRepoSearch_MultiHop(t *testing.T) {
	repo, pool := setup(t)
	ids := seedTestRoutes(t, repo, pool)

	routes, err := repo.AllRoutesWithLegs(context.Background())
	if err != nil {
		t.Fatalf("failed to load routes: %v", err)
	}

	result, ok := routing.Search(routes, ids["A"], ids["C"])
	if !ok {
		t.Fatal("expected a multi-hop path A->C via interchange I")
	}
	if result.Transfers != 1 {
		t.Errorf("expected 1 transfer, got %d", result.Transfers)
	}
	if result.TotalFareCents != 600 { // 100 + 200 + 300
		t.Errorf("expected fare 600, got %d", result.TotalFareCents)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
}

func TestRepoSearch_NoPath(t *testing.T) {
	repo, pool := setup(t)
	ids := seedTestRoutes(t, repo, pool)

	routes, err := repo.AllRoutesWithLegs(context.Background())
	if err != nil {
		t.Fatalf("failed to load routes: %v", err)
	}

	// Routes only run forward (increasing sequence), so the reverse
	// direction (C back to A) has no path even though A can reach C.
	_, ok := routing.Search(routes, ids["C"], ids["A"])
	if ok {
		t.Fatal("expected no path from C back to A")
	}
}

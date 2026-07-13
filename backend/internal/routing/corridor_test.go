package routing_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"sesfikile/backend/internal/routing"
)

// These tests exercise the real hand-seeded demo corridors (routing.SeedRoutes
// — the same data cmd/seed writes, including each corridor's return-trip
// route added per the Stage 3 "return trips are separate routes" decision),
// not a synthetic fixture. SeedCorridors is idempotent, so this is safe to
// run against the shared dev database and — unlike seedTestRoutes in
// integration_test.go — doesn't need cleanup: it's the same persistent demo
// data cmd/seed itself writes.

func seedRealCorridors(t *testing.T, repo *routing.Repo) []routing.RouteWithLegs {
	t.Helper()
	if err := routing.SeedCorridors(context.Background(), repo); err != nil {
		t.Fatalf("failed to seed corridors: %v", err)
	}
	routes, err := repo.AllRoutesWithLegs(context.Background())
	if err != nil {
		t.Fatalf("failed to load routes: %v", err)
	}
	return routes
}

func stopID(t *testing.T, repo *routing.Repo, name string) uuid.UUID {
	t.Helper()
	s, err := repo.GetStopByName(context.Background(), name)
	if err != nil {
		t.Fatalf("failed to look up seeded stop %q: %v", name, err)
	}
	return s.ID
}

func TestRealCorridors_DirectFareUnaffectedByReturnRoutes(t *testing.T) {
	repo, _ := setup(t)
	routes := seedRealCorridors(t, repo)

	ctStation := stopID(t, repo, "Cape Town Station")
	khayelitsha := stopID(t, repo, "Khayelitsha Town Centre")

	result, ok := routing.Search(routes, ctStation, khayelitsha)
	if !ok {
		t.Fatal("expected a direct path Cape Town Station -> Khayelitsha Town Centre")
	}
	if result.Transfers != 0 {
		t.Errorf("expected 0 transfers, got %d", result.Transfers)
	}
	if result.TotalFareCents != 3500 { // 800+700+900+600+500
		t.Errorf("expected fare 3500, got %d", result.TotalFareCents)
	}
}

func TestRealCorridors_ReturnTripNowSucceeds(t *testing.T) {
	repo, _ := setup(t)
	routes := seedRealCorridors(t, repo)

	khayelitsha := stopID(t, repo, "Khayelitsha Town Centre")
	ctStation := stopID(t, repo, "Cape Town Station")

	// Before the "Khayelitsha - Cape Town CBD" return route existed, this
	// direction 404'd — the forward corridor only ran
	// Cape Town Station -> Khayelitsha.
	result, ok := routing.Search(routes, khayelitsha, ctStation)
	if !ok {
		t.Fatal("expected the return-trip route to provide a direct path Khayelitsha Town Centre -> Cape Town Station")
	}
	if result.Transfers != 0 {
		t.Errorf("expected 0 transfers, got %d", result.Transfers)
	}
	if result.TotalFareCents != 3500 { // fares are mirrored, so the return trip costs the same
		t.Errorf("expected mirrored fare 3500, got %d", result.TotalFareCents)
	}
}

func TestRealCorridors_BellvilleKhayelitshaNowConnectedViaReturnRoute(t *testing.T) {
	repo, _ := setup(t)
	routes := seedRealCorridors(t, repo)

	khayelitsha := stopID(t, repo, "Khayelitsha Town Centre")
	bellville := stopID(t, repo, "Bellville Station")

	// Before return routes existed, this pair was the stage's disconnected
	// (no-path) example. Adding "Khayelitsha - Cape Town CBD" creates a
	// genuine 1-transfer path via the Cape Town Station interchange, so
	// this pair is deliberately no longer used as the no-path test — see
	// TestRealCorridors_StillNoPathBeyondOneTransfer for the replacement.
	result, ok := routing.Search(routes, khayelitsha, bellville)
	if !ok {
		t.Fatal("expected a 1-transfer path Khayelitsha Town Centre -> Bellville Station via Cape Town Station")
	}
	if result.Transfers != 1 {
		t.Errorf("expected 1 transfer, got %d", result.Transfers)
	}
	wantFare := int64(3500 + 1100) // Khayelitsha->CT Station (3500) + CT Station->Bellville (700+400)
	if result.TotalFareCents != wantFare {
		t.Errorf("expected fare %d, got %d", wantFare, result.TotalFareCents)
	}
}

func TestRealCorridors_StillNoPathBeyondOneTransfer(t *testing.T) {
	repo, _ := setup(t)
	routes := seedRealCorridors(t, repo)

	khayelitsha := stopID(t, repo, "Khayelitsha Town Centre")
	muizenberg := stopID(t, repo, "Muizenberg")

	// Khayelitsha and Muizenberg sit on opposite ends of the corridor
	// network and remain 2 transfers apart even with return routes in
	// place (the Khayelitsha/Cape Town CBD corridor doesn't share a stop
	// with the Athlone-Wynberg or Wynberg-Muizenberg corridors) — this is
	// a genuine no-path case under the MVP's 1-transfer cap.
	_, ok := routing.Search(routes, khayelitsha, muizenberg)
	if ok {
		t.Fatal("expected no path within a single transfer between Khayelitsha Town Centre and Muizenberg")
	}
}

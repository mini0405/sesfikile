package stops_test

import (
	"context"
	"net/http"
	"testing"
)

// TestRequestStop_CatalogueRouteWithRealCoordinatesStillRejected is the
// GeoJSON-upgrade-era safety net: since internal/catalogue's importer now
// gives catalogue stops a real (median-derived) coordinate, the OLD
// coordinate-based guard alone would no longer block a catalogue route.
// RequestStop must reject it anyway, by explicitly checking the route's
// Source — live stop-request matching still has no place on a route with
// no real vehicles, regardless of whether its stops happen to have a
// coordinate. Built directly against routing.Repo rather than the
// importer, so this test doesn't depend on internal/catalogue.
func TestRequestStop_CatalogueRouteWithRealCoordinatesStillRejected(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	suffix := uniqueSuffix()

	origin, err := env.routing.CreateCatalogueStop(ctx, "Catalogue Test Origin WithCoords "+suffix, -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create catalogue origin stop: %v", err)
	}
	dest, err := env.routing.CreateCatalogueStop(ctx, "Catalogue Test Dest WithCoords "+suffix, -33.95, 18.45)
	if err != nil {
		t.Fatalf("failed to create catalogue dest stop: %v", err)
	}
	if !origin.CoordinatesKnown() || !dest.CoordinatesKnown() {
		t.Fatal("expected these catalogue stops to have known coordinates — that's the scenario this test targets")
	}

	route, err := env.routing.CreateCatalogueRoute(ctx, "Catalogue Test Route WithCoords "+suffix, "City of Cape Town open data (unverified, no association attribution)")
	if err != nil {
		t.Fatalf("failed to create catalogue route: %v", err)
	}
	if _, err := env.routing.CreateCatalogueRouteLeg(ctx, route.ID, origin.ID, dest.ID, 900, 12345.6); err != nil {
		t.Fatalf("failed to create catalogue leg: %v", err)
	}

	commuterTok := seedCommuter(t, env)

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.ID.String(),
		"stop_id":  dest.ID.String(),
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a catalogue route even though its stops have real coordinates, got %d: %+v", status, body)
	}
}

// TestRequestStop_CoordinatelessRouteRejected covers the (now-defensive,
// still-valid) coordinate-based guard directly: a route whose stops have no
// known coordinates at all (routing.Repo.CreateStopNoCoordinates —
// possible in principle even outside the catalogue importer) must be
// cleanly rejected for live stop-request matching, not silently treated as
// if its stops were at (0, 0). Built directly against routing.Repo rather
// than the importer, so this test doesn't depend on internal/catalogue.
func TestRequestStop_CoordinatelessRouteRejected(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	suffix := uniqueSuffix()

	origin, err := env.routing.CreateStopNoCoordinates(ctx, "Catalogue Test Origin "+suffix)
	if err != nil {
		t.Fatalf("failed to create coordinate-less origin stop: %v", err)
	}
	dest, err := env.routing.CreateStopNoCoordinates(ctx, "Catalogue Test Dest "+suffix)
	if err != nil {
		t.Fatalf("failed to create coordinate-less dest stop: %v", err)
	}
	if origin.CoordinatesKnown() || dest.CoordinatesKnown() {
		t.Fatal("expected freshly created catalogue-style stops to report CoordinatesKnown() == false")
	}

	route, err := env.routing.CreateCatalogueRoute(ctx, "Catalogue Test Route "+suffix, "City of Cape Town open data (unverified, no association attribution)")
	if err != nil {
		t.Fatalf("failed to create catalogue route: %v", err)
	}
	if _, err := env.routing.CreateCatalogueRouteLeg(ctx, route.ID, origin.ID, dest.ID, 900, 12345.6); err != nil {
		t.Fatalf("failed to create catalogue leg: %v", err)
	}

	commuterTok := seedCommuter(t, env)

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.ID.String(),
		"stop_id":  dest.ID.String(),
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a coordinate-less (catalogue) route, got %d: %+v", status, body)
	}
}

package stops_test

import (
	"context"
	"net/http"
	"testing"
)

// TestRequestStop_CoordinatelessRouteRejected covers the map/telemetry
// safety net for catalogue-imported routes (internal/catalogue): a route
// whose stops have no known coordinates (routing.Repo.CreateStopNoCoordinates
// / CreateCatalogueRoute — exactly what cmd/importcatalogue creates) must be
// cleanly rejected for live stop-request matching, not silently treated as
// if its stops were at (0, 0). Built directly against routing.Repo rather
// than the CSV importer, so this test doesn't depend on internal/catalogue.
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

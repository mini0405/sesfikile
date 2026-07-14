package routing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"sesfikile/backend/internal/routing"
)

// stopsTestRouter wires routing.Handlers into a bare chi router — /stops is
// public (no auth), same reasoning as /routes (Stage 3): reference data a
// commuter should be able to read before logging in.
func stopsTestRouter(repo *routing.Repo) *httptest.Server {
	handlers := routing.NewHandlers(repo)
	r := chi.NewRouter()
	r.Get("/stops", handlers.ListStops)
	r.Get("/routes/{id}", handlers.GetRoute)
	server := httptest.NewServer(r)
	return server
}

func getStops(t *testing.T, server *httptest.Server, path string) (int, []map[string]any) {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response as a JSON array: %v", err)
	}
	return resp.StatusCode, body
}

// TestListStops_ReturnsSeededStops confirms GET /stops (no filter) returns
// every seeded stop, including known corridor stops by name.
func TestListStops_ReturnsSeededStops(t *testing.T) {
	repo, _ := setup(t)
	seedRealCorridors(t, repo)

	server := stopsTestRouter(repo)
	defer server.Close()

	status, stops := getStops(t, server, "/stops")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if len(stops) == 0 {
		t.Fatal("expected a non-empty stop list after seeding corridors")
	}

	names := map[string]bool{}
	for _, s := range stops {
		names[s["name"].(string)] = true
	}
	if !names["Cape Town Station"] {
		t.Error("expected seeded stop \"Cape Town Station\" in the unfiltered stop list")
	}
}

// TestListStops_EmptyRouteReturnsEmptyArray is Gap 1's array-not-null
// requirement exercised end-to-end: a route with zero legs must serialize
// its stop list as [], not null.
func TestListStops_EmptyRouteReturnsEmptyArray(t *testing.T) {
	repo, pool := setup(t)

	route, err := repo.CreateRoute(context.Background(), "Empty Route "+uuid.NewString(), "Test Association")
	if err != nil {
		t.Fatalf("failed to create route: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM routes WHERE id = $1`, route.ID)
	})

	server := stopsTestRouter(repo)
	defer server.Close()

	resp, err := http.Get(server.URL + "/stops?route_id=" + route.ID.String())
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("expected the empty-route stop list to serialize as [], got %s", string(raw))
	}
}

// TestListStops_FilteredByRoute_ReturnsOrderedStops confirms route_id
// filtering returns stops in physical sequence order, not alphabetical.
func TestListStops_FilteredByRoute_ReturnsOrderedStops(t *testing.T) {
	repo, pool := setup(t)
	ids := seedTestRoutes(t, repo, pool)

	// seedTestRoutes' Route1 runs A -> B -> I (see integration_test.go).
	routes, err := repo.AllRoutesWithLegs(context.Background())
	if err != nil {
		t.Fatalf("failed to load routes: %v", err)
	}
	var route1ID string
	for _, rt := range routes {
		if len(rt.Legs) == 2 && rt.Legs[0].FromStopID == ids["A"] {
			route1ID = rt.Route.ID.String()
			break
		}
	}
	if route1ID == "" {
		t.Fatal("failed to find seeded Route1 among loaded routes")
	}

	server := stopsTestRouter(repo)
	defer server.Close()

	status, stops := getStops(t, server, "/stops?route_id="+route1ID)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, stops)
	}
	if len(stops) != 3 {
		t.Fatalf("expected 3 ordered stops (A, B, I), got %d: %+v", len(stops), stops)
	}
	if stops[0]["id"] != ids["A"].String() || stops[1]["id"] != ids["B"].String() || stops[2]["id"] != ids["I"].String() {
		t.Fatalf("expected stops in sequence order A, B, I; got %+v", stops)
	}
}

// TestListStops_UnknownRoute404s confirms a nonexistent route_id 404s
// rather than silently returning an empty (or worse, unfiltered) list.
func TestListStops_UnknownRoute404s(t *testing.T) {
	repo, _ := setup(t)

	server := stopsTestRouter(repo)
	defer server.Close()

	resp, err := http.Get(server.URL + "/stops?route_id=" + "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown route_id, got %d", resp.StatusCode)
	}
}

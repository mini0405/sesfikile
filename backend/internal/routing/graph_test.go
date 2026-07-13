package routing

import (
	"testing"

	"github.com/google/uuid"
)

// buildRoute constructs a RouteWithLegs from a list of stop names and
// per-leg fares, e.g. buildRoute("R1", "A", 100, "B", 200, "C") builds
// A -(100)-> B -(200)-> C. stopIDs maps stop name -> id, populated/reused
// across calls so the same name always resolves to the same id.
func buildRoute(routeName string, stopIDs map[string]uuid.UUID, parts ...any) RouteWithLegs {
	stopAt := func(name string) uuid.UUID {
		id, ok := stopIDs[name]
		if !ok {
			id = uuid.New()
			stopIDs[name] = id
		}
		return id
	}

	route := Route{ID: uuid.New(), Name: routeName}
	var legs []RouteLeg
	// parts alternates: name, fare, name, fare, name, ...
	for i := 0; i+2 < len(parts); i += 2 {
		from := stopAt(parts[i].(string))
		fare := int64(parts[i+1].(int))
		to := stopAt(parts[i+2].(string))
		legs = append(legs, RouteLeg{
			ID:         uuid.New(),
			RouteID:    route.ID,
			FromStopID: from,
			ToStopID:   to,
			Sequence:   i/2 + 1,
			FareCents:  fare,
		})
	}
	return RouteWithLegs{Route: route, Legs: legs}
}

func TestSearch_Direct(t *testing.T) {
	stopIDs := map[string]uuid.UUID{}
	r1 := buildRoute("R1", stopIDs, "A", 100, "B", 200, "C", 300, "D")

	result, ok := Search([]RouteWithLegs{r1}, stopIDs["A"], stopIDs["C"])
	if !ok {
		t.Fatal("expected a path to be found")
	}
	if result.Transfers != 0 {
		t.Errorf("expected 0 transfers, got %d", result.Transfers)
	}
	if result.TotalFareCents != 300 {
		t.Errorf("expected fare 300 (100+200), got %d", result.TotalFareCents)
	}
	if len(result.Segments) != 1 || len(result.Segments[0].Legs) != 2 {
		t.Fatalf("expected 1 segment with 2 legs, got %+v", result.Segments)
	}
}

func TestSearch_MultiHopViaInterchange(t *testing.T) {
	stopIDs := map[string]uuid.UUID{}
	// R1: A -(100)-> B -(200)-> Interchange
	r1 := buildRoute("R1", stopIDs, "A", 100, "B", 200, "Interchange")
	// R2: Interchange -(300)-> C -(400)-> D
	r2 := buildRoute("R2", stopIDs, "Interchange", 300, "C", 400, "D")

	result, ok := Search([]RouteWithLegs{r1, r2}, stopIDs["A"], stopIDs["D"])
	if !ok {
		t.Fatal("expected a multi-hop path to be found")
	}
	if result.Transfers != 1 {
		t.Errorf("expected 1 transfer, got %d", result.Transfers)
	}
	wantFare := int64(100 + 200 + 300 + 400)
	if result.TotalFareCents != wantFare {
		t.Errorf("expected fare %d, got %d", wantFare, result.TotalFareCents)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].RouteName != "R1" || result.Segments[1].RouteName != "R2" {
		t.Errorf("unexpected segment routes: %+v", result.Segments)
	}
}

func TestSearch_NoPath(t *testing.T) {
	stopIDs := map[string]uuid.UUID{}
	r1 := buildRoute("R1", stopIDs, "A", 100, "B")
	r2 := buildRoute("R2", stopIDs, "C", 100, "D")

	_, ok := Search([]RouteWithLegs{r1, r2}, stopIDs["A"], stopIDs["D"])
	if ok {
		t.Fatal("expected no path between disconnected stops")
	}
}

func TestSearch_DirectionMatters(t *testing.T) {
	// A route only runs forward (increasing sequence) — asking for the
	// reverse direction must not find a path.
	stopIDs := map[string]uuid.UUID{}
	r1 := buildRoute("R1", stopIDs, "A", 100, "B", 200, "C")

	_, ok := Search([]RouteWithLegs{r1}, stopIDs["C"], stopIDs["A"])
	if ok {
		t.Fatal("expected no path when traveling against a route's direction")
	}
}

func TestSearch_PrefersDirectOverTransfer(t *testing.T) {
	stopIDs := map[string]uuid.UUID{}
	// Direct route A -> B costs 1000.
	direct := buildRoute("Direct", stopIDs, "A", 1000, "B")
	// A cheaper-looking transfer path also exists via an interchange, but
	// direct should still win since fewest-transfers beats lowest-fare.
	viaR1 := buildRoute("ViaR1", stopIDs, "A", 10, "X")
	viaR2 := buildRoute("ViaR2", stopIDs, "X", 10, "B")

	result, ok := Search([]RouteWithLegs{direct, viaR1, viaR2}, stopIDs["A"], stopIDs["B"])
	if !ok {
		t.Fatal("expected a path to be found")
	}
	if result.Transfers != 0 {
		t.Errorf("expected the direct (0-transfer) path to be preferred, got %d transfers", result.Transfers)
	}
	if result.TotalFareCents != 1000 {
		t.Errorf("expected direct fare 1000, got %d", result.TotalFareCents)
	}
}

func TestSearch_SameStop(t *testing.T) {
	stopIDs := map[string]uuid.UUID{}
	r1 := buildRoute("R1", stopIDs, "A", 100, "B")

	_, ok := Search([]RouteWithLegs{r1}, stopIDs["A"], stopIDs["A"])
	if ok {
		t.Fatal("expected no path when origin equals destination")
	}
}

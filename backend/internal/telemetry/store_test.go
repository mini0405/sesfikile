package telemetry

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestConcurrentUpdatesAndReads exercises the store from many goroutines at
// once — drivers moving, drivers adjusting seats, and commuters listing —
// against a shared set of vehicles. Run with `go test -race` to verify no
// data races; correctness here is "doesn't crash / race", not specific
// final values, since updates interleave nondeterministically.
func TestConcurrentUpdatesAndReads(t *testing.T) {
	store := NewVehicleStateStore()
	routeID := uuid.New()

	const numVehicles = 5
	vehicleIDs := make([]uuid.UUID, numVehicles)
	for i := range vehicleIDs {
		vehicleIDs[i] = uuid.New()
		store.GoOnline(vehicleIDs[i], routeID, uuid.New(), 16)
	}

	const iterations = 200
	var wg sync.WaitGroup

	// Writers: position updates.
	for _, vID := range vehicleIDs {
		wg.Add(1)
		go func(vehicleID uuid.UUID) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				store.UpdatePosition(vehicleID, float64(i), float64(-i))
			}
		}(vID)
	}

	// Writers: seat adjustments (mix of delta and absolute).
	for _, vID := range vehicleIDs {
		wg.Add(1)
		go func(vehicleID uuid.UUID) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if i%2 == 0 {
					store.AdjustSeats(vehicleID, 1)
				} else {
					store.AdjustSeats(vehicleID, -1)
				}
			}
		}(vID)
	}

	// Readers: route snapshot + individual gets.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				store.ListByRoute(routeID)
				for _, vID := range vehicleIDs {
					store.Get(vID)
				}
			}
		}()
	}

	// A concurrent online/offline churner on its own vehicle to exercise
	// the add/remove path alongside everything else.
	churnID := uuid.New()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			store.GoOnline(churnID, routeID, uuid.New(), 4)
			store.GoOffline(churnID)
		}
	}()

	wg.Wait()

	for _, vID := range vehicleIDs {
		state, ok := store.Get(vID)
		if !ok {
			t.Fatalf("vehicle %s vanished unexpectedly", vID)
		}
		if state.SeatsAvailable < 0 || state.SeatsAvailable > state.SeatsTotal {
			t.Fatalf("vehicle %s seats_available %d out of bounds [0,%d]", vID, state.SeatsAvailable, state.SeatsTotal)
		}
	}

	if _, ok := store.Get(churnID); ok {
		t.Fatal("churn vehicle should have ended offline (absent from store)")
	}
}

func TestSeatClampingNeverExceedsBounds(t *testing.T) {
	store := NewVehicleStateStore()
	vehicleID := uuid.New()
	store.GoOnline(vehicleID, uuid.New(), uuid.New(), 4)

	state, ok := store.AdjustSeats(vehicleID, 100)
	if !ok || state.SeatsAvailable != 4 {
		t.Fatalf("expected clamp to seats_total=4, got %+v (ok=%v)", state, ok)
	}

	state, ok = store.AdjustSeats(vehicleID, -100)
	if !ok || state.SeatsAvailable != 0 {
		t.Fatalf("expected clamp to 0, got %+v (ok=%v)", state, ok)
	}

	state, ok = store.SetSeatsAbsolute(vehicleID, 999)
	if !ok || state.SeatsAvailable != 4 {
		t.Fatalf("expected absolute set clamped to 4, got %+v (ok=%v)", state, ok)
	}

	state, ok = store.SetSeatsAbsolute(vehicleID, -5)
	if !ok || state.SeatsAvailable != 0 {
		t.Fatalf("expected absolute set clamped to 0, got %+v (ok=%v)", state, ok)
	}
}

func TestGoOfflineRemovesFromRouteSnapshot(t *testing.T) {
	store := NewVehicleStateStore()
	routeID := uuid.New()
	vehicleID := uuid.New()

	store.GoOnline(vehicleID, routeID, uuid.New(), 10)
	if len(store.ListByRoute(routeID)) != 1 {
		t.Fatal("expected vehicle to appear in route snapshot while online")
	}

	if _, ok := store.GoOffline(vehicleID); !ok {
		t.Fatal("expected GoOffline to report the vehicle was tracked")
	}
	if len(store.ListByRoute(routeID)) != 0 {
		t.Fatal("expected vehicle to be removed from route snapshot after going offline")
	}
	if _, ok := store.Get(vehicleID); ok {
		t.Fatal("expected vehicle to be absent from the store after going offline")
	}
}

func TestUpdatePositionOnUntrackedVehicleFails(t *testing.T) {
	store := NewVehicleStateStore()
	_, ok := store.UpdatePosition(uuid.New(), 1, 2)
	if ok {
		t.Fatal("expected UpdatePosition on an unknown vehicle to report ok=false")
	}
}

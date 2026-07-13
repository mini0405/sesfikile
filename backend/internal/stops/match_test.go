package stops

import (
	"testing"

	"github.com/google/uuid"
)

// A simple 3-stop straight-line route: A(0,0) -> B(0,1) -> C(0,2), degrees
// used directly as coordinates since haversineMeters only cares about
// relative distance, not real-world position.
func straightRoute() (a, b, c RouteStop) {
	a = RouteStop{StopID: uuid.New(), Lat: 0, Lng: 0}
	b = RouteStop{StopID: uuid.New(), Lat: 0, Lng: 1}
	c = RouteStop{StopID: uuid.New(), Lat: 0, Lng: 2}
	return
}

func TestStopSequenceIndex(t *testing.T) {
	a, b, c := straightRoute()
	route := []RouteStop{a, b, c}

	if idx, ok := StopSequenceIndex(route, b.StopID); !ok || idx != 1 {
		t.Fatalf("expected index 1 for b, got %d (ok=%v)", idx, ok)
	}
	if _, ok := StopSequenceIndex(route, uuid.New()); ok {
		t.Fatal("expected ok=false for a stop not on the route")
	}
}

func TestFindApproachingDriver_ApproachingDriverMatched(t *testing.T) {
	a, b, c := straightRoute()
	route := []RouteStop{a, b, c}

	// Driver's nearest stop is A (index 0), requesting pickup at C (index 2):
	// 0 <= 2, so the driver qualifies as approaching.
	driver := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: a.Lat, Lng: a.Lng}

	candidate, ok := FindApproachingDriver([]DriverPosition{driver}, route, c)
	if !ok {
		t.Fatal("expected a matching driver")
	}
	if candidate.DriverID != driver.DriverID {
		t.Fatalf("expected driver %s matched, got %s", driver.DriverID, candidate.DriverID)
	}
}

func TestFindApproachingDriver_DriverPastStopNotMatched(t *testing.T) {
	a, b, c := straightRoute()
	route := []RouteStop{a, b, c}

	// Driver's nearest stop is C (index 2), requesting pickup at B (index 1):
	// 2 > 1, so the driver has already passed the stop and does not qualify.
	driver := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: c.Lat, Lng: c.Lng}

	if _, ok := FindApproachingDriver([]DriverPosition{driver}, route, b); ok {
		t.Fatal("expected no match: driver has already passed the requested stop")
	}
}

func TestFindApproachingDriver_NearestOfMultipleQualifyingDrivers(t *testing.T) {
	a, b, c := straightRoute()
	route := []RouteStop{a, b, c}

	far := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: a.Lat, Lng: a.Lng}
	near := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: b.Lat, Lng: b.Lng}

	candidate, ok := FindApproachingDriver([]DriverPosition{far, near}, route, c)
	if !ok {
		t.Fatal("expected a matching driver")
	}
	if candidate.DriverID != near.DriverID {
		t.Fatalf("expected the nearer driver %s matched, got %s", near.DriverID, candidate.DriverID)
	}
}

func TestFindApproachingDriver_NoQualifyingDrivers(t *testing.T) {
	a, b, c := straightRoute()
	route := []RouteStop{a, b, c}

	pastDriver := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: c.Lat, Lng: c.Lng}

	if _, ok := FindApproachingDriver([]DriverPosition{pastDriver}, route, a); ok {
		t.Fatal("expected no match when every driver has already passed the stop")
	}
	if _, ok := FindApproachingDriver(nil, route, a); ok {
		t.Fatal("expected no match with zero drivers online")
	}
}

func TestFindApproachingDriver_StopNotOnRoute(t *testing.T) {
	a, b, _ := straightRoute()
	route := []RouteStop{a, b}
	offRoute := RouteStop{StopID: uuid.New(), Lat: 99, Lng: 99}

	driver := DriverPosition{DriverID: uuid.New(), VehicleID: uuid.New(), Lat: a.Lat, Lng: a.Lng}
	if _, ok := FindApproachingDriver([]DriverPosition{driver}, route, offRoute); ok {
		t.Fatal("expected no match when the requested stop isn't on the route")
	}
}

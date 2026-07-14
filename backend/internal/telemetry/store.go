// Package telemetry holds Stage 4's live vehicle state and WebSocket
// fan-out. Positions/seat-state live IN MEMORY only (a VehicleStateStore),
// not in Postgres — they reset on server restart. That's an accepted MVP
// trade-off (see CLAUDE.md / docs/PROGRESS.md): it avoids introducing Redis
// this stage, and telemetry deliberately does not persist a GPS history/track
// (that's Analytics' job later). This store is the single source of truth
// for "is this vehicle online and where is it right now."
package telemetry

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// VehicleState is a snapshot of one vehicle's live telemetry.
type VehicleState struct {
	VehicleID      uuid.UUID
	RouteID        uuid.UUID
	DriverID       uuid.UUID
	Lat            float64
	Lng            float64
	SeatsTotal     int
	SeatsAvailable int
	Online         bool
	LastUpdated    time.Time
}

// VehicleStateStore is a concurrency-safe, in-memory map of vehicle_id ->
// VehicleState. All access is guarded by a single RWMutex; state values are
// copied in and out so callers never share mutable state with the store.
type VehicleStateStore struct {
	mu       sync.RWMutex
	vehicles map[uuid.UUID]VehicleState
}

func NewVehicleStateStore() *VehicleStateStore {
	return &VehicleStateStore{vehicles: make(map[uuid.UUID]VehicleState)}
}

// GoOnline marks a vehicle online for a route/driver. If the vehicle was
// already tracked (e.g. a reconnect), its last-known seats_available is
// preserved rather than reset to seatsTotal.
func (s *VehicleStateStore) GoOnline(vehicleID, routeID, driverID uuid.UUID, seatsTotal int) VehicleState {
	s.mu.Lock()
	defer s.mu.Unlock()

	seatsAvailable := seatsTotal
	if existing, ok := s.vehicles[vehicleID]; ok {
		seatsAvailable = clamp(existing.SeatsAvailable, 0, seatsTotal)
	}

	state := VehicleState{
		VehicleID:      vehicleID,
		RouteID:        routeID,
		DriverID:       driverID,
		SeatsTotal:     seatsTotal,
		SeatsAvailable: seatsAvailable,
		Online:         true,
		LastUpdated:    time.Now(),
	}
	if existing, ok := s.vehicles[vehicleID]; ok {
		state.Lat = existing.Lat
		state.Lng = existing.Lng
	}
	s.vehicles[vehicleID] = state
	return state
}

// GoOffline removes a vehicle from live state entirely — offline vehicles
// must not appear in route snapshots, so "offline" is modeled as "absent"
// rather than as an online=false row.
func (s *VehicleStateStore) GoOffline(vehicleID uuid.UUID) (VehicleState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.vehicles[vehicleID]
	if !ok {
		return VehicleState{}, false
	}
	delete(s.vehicles, vehicleID)
	state.Online = false
	return state, true
}

// UpdatePosition moves an already-online vehicle. Returns ok=false if the
// vehicle isn't currently tracked (e.g. it went offline concurrently).
func (s *VehicleStateStore) UpdatePosition(vehicleID uuid.UUID, lat, lng float64) (VehicleState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.vehicles[vehicleID]
	if !ok {
		return VehicleState{}, false
	}
	state.Lat = lat
	state.Lng = lng
	state.LastUpdated = time.Now()
	s.vehicles[vehicleID] = state
	return state, true
}

// AdjustSeats applies a signed delta to seats_available, clamped to
// [0, seats_total]. Returns ok=false if the vehicle isn't currently tracked.
func (s *VehicleStateStore) AdjustSeats(vehicleID uuid.UUID, delta int) (VehicleState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.vehicles[vehicleID]
	if !ok {
		return VehicleState{}, false
	}
	state.SeatsAvailable = clamp(state.SeatsAvailable+delta, 0, state.SeatsTotal)
	state.LastUpdated = time.Now()
	s.vehicles[vehicleID] = state
	return state, true
}

// SetSeatsAbsolute sets seats_available directly, clamped to
// [0, seats_total]. Returns ok=false if the vehicle isn't currently tracked.
func (s *VehicleStateStore) SetSeatsAbsolute(vehicleID uuid.UUID, seats int) (VehicleState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.vehicles[vehicleID]
	if !ok {
		return VehicleState{}, false
	}
	state.SeatsAvailable = clamp(seats, 0, state.SeatsTotal)
	state.LastUpdated = time.Now()
	s.vehicles[vehicleID] = state
	return state, true
}

// Get returns the current state of one vehicle.
func (s *VehicleStateStore) Get(vehicleID uuid.UUID) (VehicleState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.vehicles[vehicleID]
	return state, ok
}

// ListByRoute returns every currently-online vehicle on a route. Order is
// unspecified (map iteration).
func (s *VehicleStateStore) ListByRoute(routeID uuid.UUID) []VehicleState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []VehicleState{}
	for _, state := range s.vehicles {
		if state.RouteID == routeID {
			result = append(result, state)
		}
	}
	return result
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

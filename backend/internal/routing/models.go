package routing

import (
	"time"

	"github.com/google/uuid"
)

type Stop struct {
	ID        uuid.UUID
	Name      string
	Latitude  float64
	Longitude float64
	CreatedAt time.Time
}

type Route struct {
	ID              uuid.UUID
	Name            string
	AssociationName string
	CreatedAt       time.Time
}

// RouteLeg is one directed edge of a route, walked in increasing Sequence
// order. FareCents is the fare for riding this single leg.
type RouteLeg struct {
	ID         uuid.UUID
	RouteID    uuid.UUID
	FromStopID uuid.UUID
	ToStopID   uuid.UUID
	Sequence   int
	FareCents  int64
	CreatedAt  time.Time
}

// RouteWithLegs is a route and its legs ordered by Sequence.
type RouteWithLegs struct {
	Route Route
	Legs  []RouteLeg
}

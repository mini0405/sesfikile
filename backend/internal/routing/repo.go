package routing

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) CreateStop(ctx context.Context, name string, latitude, longitude float64) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`INSERT INTO stops (name, latitude, longitude) VALUES ($1, $2, $3)
		 RETURNING id, name, latitude, longitude, created_at`,
		name, latitude, longitude,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.CreatedAt)
	return s, err
}

func (r *Repo) GetStopByName(ctx context.Context, name string) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, latitude, longitude, created_at FROM stops WHERE name = $1`,
		name,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Stop{}, ErrNotFound
		}
		return Stop{}, err
	}
	return s, nil
}

func (r *Repo) GetStopByID(ctx context.Context, id uuid.UUID) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, latitude, longitude, created_at FROM stops WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Stop{}, ErrNotFound
		}
		return Stop{}, err
	}
	return s, nil
}

func (r *Repo) ListStops(ctx context.Context) ([]Stop, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, latitude, longitude, created_at FROM stops ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stops := []Stop{}
	for rows.Next() {
		var s Stop
		if err := rows.Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.CreatedAt); err != nil {
			return nil, err
		}
		stops = append(stops, s)
	}
	return stops, rows.Err()
}

func (r *Repo) CreateRoute(ctx context.Context, name, associationName string) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`INSERT INTO routes (name, association_name) VALUES ($1, $2)
		 RETURNING id, name, association_name, created_at`,
		name, associationName,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.CreatedAt)
	return rt, err
}

func (r *Repo) GetRouteByName(ctx context.Context, name string) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, association_name, created_at FROM routes WHERE name = $1`,
		name,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Route{}, ErrNotFound
		}
		return Route{}, err
	}
	return rt, nil
}

func (r *Repo) GetRouteByID(ctx context.Context, id uuid.UUID) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, association_name, created_at FROM routes WHERE id = $1`,
		id,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Route{}, ErrNotFound
		}
		return Route{}, err
	}
	return rt, nil
}

func (r *Repo) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, association_name, created_at FROM routes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []Route{}
	for rows.Next() {
		var rt Route
		if err := rows.Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, rt)
	}
	return routes, rows.Err()
}

func (r *Repo) CreateRouteLeg(ctx context.Context, routeID, fromStopID, toStopID uuid.UUID, sequence int, fareCents int64) (RouteLeg, error) {
	var l RouteLeg
	err := r.pool.QueryRow(ctx,
		`INSERT INTO route_legs (route_id, from_stop_id, to_stop_id, sequence, fare_cents)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, route_id, from_stop_id, to_stop_id, sequence, fare_cents, created_at`,
		routeID, fromStopID, toStopID, sequence, fareCents,
	).Scan(&l.ID, &l.RouteID, &l.FromStopID, &l.ToStopID, &l.Sequence, &l.FareCents, &l.CreatedAt)
	return l, err
}

// ListLegsForRoute returns a route's legs ordered by Sequence.
func (r *Repo) ListLegsForRoute(ctx context.Context, routeID uuid.UUID) ([]RouteLeg, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, route_id, from_stop_id, to_stop_id, sequence, fare_cents, created_at
		 FROM route_legs WHERE route_id = $1 ORDER BY sequence`,
		routeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	legs := []RouteLeg{}
	for rows.Next() {
		var l RouteLeg
		if err := rows.Scan(&l.ID, &l.RouteID, &l.FromStopID, &l.ToStopID, &l.Sequence, &l.FareCents, &l.CreatedAt); err != nil {
			return nil, err
		}
		legs = append(legs, l)
	}
	return legs, rows.Err()
}

// AllRoutesWithLegs loads every route and its ordered legs in one go. The
// seeded dataset is small (a handful of routes/legs), so loading it whole
// into memory and searching in Go (see graph.go) is simpler than expressing
// the path search as SQL.
func (r *Repo) AllRoutesWithLegs(ctx context.Context) ([]RouteWithLegs, error) {
	routes, err := r.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]RouteWithLegs, 0, len(routes))
	for _, rt := range routes {
		legs, err := r.ListLegsForRoute(ctx, rt.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, RouteWithLegs{Route: rt, Legs: legs})
	}
	return result, nil
}

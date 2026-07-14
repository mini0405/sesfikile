package routing

import (
	"context"
	"encoding/json"
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

// CreateStop inserts a hand-seeded stop (cmd/seed) — source defaults to
// 'seed' via the column's DB default. Use CreateCatalogueStop for a
// catalogue-imported stop with a real (if approximate) coordinate, or
// CreateStopNoCoordinates for one with none.
func (r *Repo) CreateStop(ctx context.Context, name string, latitude, longitude float64) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`INSERT INTO stops (name, latitude, longitude) VALUES ($1, $2, $3)
		 RETURNING id, name, latitude, longitude, source, created_at`,
		name, latitude, longitude,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt)
	return s, err
}

// CreateCatalogueStop inserts a stop tagged source='catalogue' with a real
// (APPROXIMATE — see internal/catalogue.medianCoordinate) coordinate. Used
// by internal/catalogue's importer for every rank it creates a stop for.
func (r *Repo) CreateCatalogueStop(ctx context.Context, name string, latitude, longitude float64) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`INSERT INTO stops (name, latitude, longitude, source) VALUES ($1, $2, $3, 'catalogue')
		 RETURNING id, name, latitude, longitude, source, created_at`,
		name, latitude, longitude,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt)
	return s, err
}

// CreateStopNoCoordinates inserts a stop tagged source='catalogue' with NO
// known position — a defensive fallback for internal/catalogue's importer
// (every rank it processes has at least one endpoint sample in practice, so
// this path is not expected to be hit by a real import). Stop.CoordinatesKnown()
// reports false for a stop created this way.
func (r *Repo) CreateStopNoCoordinates(ctx context.Context, name string) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`INSERT INTO stops (name, latitude, longitude, source) VALUES ($1, NULL, NULL, 'catalogue')
		 RETURNING id, name, latitude, longitude, source, created_at`,
		name,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt)
	return s, err
}

func (r *Repo) GetStopByName(ctx context.Context, name string) (Stop, error) {
	var s Stop
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, latitude, longitude, source, created_at FROM stops WHERE name = $1`,
		name,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt)
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
		`SELECT id, name, latitude, longitude, source, created_at FROM stops WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Stop{}, ErrNotFound
		}
		return Stop{}, err
	}
	return s, nil
}

func (r *Repo) ListStops(ctx context.Context) ([]Stop, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, latitude, longitude, source, created_at FROM stops ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stops := []Stop{}
	for rows.Next() {
		var s Stop
		if err := rows.Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		stops = append(stops, s)
	}
	return stops, rows.Err()
}

// ListStopsWithCoordinates is the map-facing read path: every stop with a
// known position, regardless of source. Before the GeoJSON upgrade this
// excluded every catalogue stop (they had none); now a catalogue stop's
// APPROXIMATE (median-derived) coordinate makes it map-eligible too — the
// intended upgrade. Use this wherever a caller is about to place a stop on
// a map; use ListStops (or the route-scoped GET /stops?route_id=) for
// browse/search uses that don't need a position.
func (r *Repo) ListStopsWithCoordinates(ctx context.Context) ([]Stop, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, latitude, longitude, source, created_at FROM stops WHERE latitude IS NOT NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stops := []Stop{}
	for rows.Next() {
		var s Stop
		if err := rows.Scan(&s.ID, &s.Name, &s.Latitude, &s.Longitude, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		stops = append(stops, s)
	}
	return stops, rows.Err()
}

// CountStopsWithoutCoordinates reports how many stops currently have no
// known position — after the GeoJSON upgrade this should normally be 0 even
// with the catalogue loaded (every rank gets a median-derived coordinate);
// non-zero would mean the defensive CreateStopNoCoordinates fallback was
// actually hit. Used by cmd/clearcatalogue's dry-run report.
func (r *Repo) CountStopsWithoutCoordinates(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM stops WHERE latitude IS NULL`).Scan(&n)
	return n, err
}

// CountStopsBySource reports how many stops are tagged the given source
// ("seed" or "catalogue") — used by cmd/clearcatalogue's dry-run report and
// by tests confirming the seeded baseline's stop count is unaffected by an
// import.
func (r *Repo) CountStopsBySource(ctx context.Context, source string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM stops WHERE source = $1`, source).Scan(&n)
	return n, err
}

// CreateRoute inserts a hand-seeded route (cmd/seed) — source defaults to
// 'seed' via the column's DB default. Use CreateCatalogueRoute for a
// catalogue-imported route.
func (r *Repo) CreateRoute(ctx context.Context, name, associationName string) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`INSERT INTO routes (name, association_name) VALUES ($1, $2)
		 RETURNING id, name, association_name, source, created_at`,
		name, associationName,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.Source, &rt.CreatedAt)
	return rt, err
}

// CreateCatalogueRoute inserts a route tagged source='catalogue' — used only
// by internal/catalogue's importer. Distinguishable from every hand-seeded
// route via Route.Source, and removable independently via
// DeleteCatalogueData without touching cmd/seed's baseline.
func (r *Repo) CreateCatalogueRoute(ctx context.Context, name, associationName string) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`INSERT INTO routes (name, association_name, source) VALUES ($1, $2, 'catalogue')
		 RETURNING id, name, association_name, source, created_at`,
		name, associationName,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.Source, &rt.CreatedAt)
	return rt, err
}

func (r *Repo) GetRouteByName(ctx context.Context, name string) (Route, error) {
	var rt Route
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, association_name, source, created_at FROM routes WHERE name = $1`,
		name,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.Source, &rt.CreatedAt)
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
		`SELECT id, name, association_name, source, created_at FROM routes WHERE id = $1`,
		id,
	).Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.Source, &rt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Route{}, ErrNotFound
		}
		return Route{}, err
	}
	return rt, nil
}

func (r *Repo) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, association_name, source, created_at FROM routes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []Route{}
	for rows.Next() {
		var rt Route
		if err := rows.Scan(&rt.ID, &rt.Name, &rt.AssociationName, &rt.Source, &rt.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, rt)
	}
	return routes, rows.Err()
}

// CountRoutesBySource reports how many routes are tagged the given source
// ("seed" or "catalogue") — used by cmd/clearcatalogue's dry-run report and
// by tests confirming the seeded baseline's route count is unaffected by an
// import.
func (r *Repo) CountRoutesBySource(ctx context.Context, source string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM routes WHERE source = $1`, source).Scan(&n)
	return n, err
}

// DeleteCatalogueData removes every catalogue-imported route (source =
// 'catalogue'), its geometry and leg(s), then any stop tagged
// source='catalogue' left with zero remaining route_legs references as a
// result. SAFE and idempotent — a hand-seeded route/stop (source = 'seed')
// is never matched by any of these deletes, so cmd/seed's baseline is
// structurally unreachable by this method regardless of what else exists in
// the database.
//
// Stops are scoped by source, not by "has no coordinates" — since the
// GeoJSON upgrade, a catalogue stop DOES have a (median-derived,
// approximate) coordinate, so coordinate-presence alone can no longer tell
// a catalogue stop from a hand-seeded one; source is the only reliable
// signal now (see stops.source's migration comment).
func (r *Repo) DeleteCatalogueData(ctx context.Context) (routesDeleted, legsDeleted, stopsDeleted, geometriesDeleted int64, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer tx.Rollback(ctx)

	geometriesTag, err := tx.Exec(ctx, `
		DELETE FROM route_geometries
		WHERE route_id IN (SELECT id FROM routes WHERE source = 'catalogue')`)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	legsTag, err := tx.Exec(ctx, `
		DELETE FROM route_legs
		WHERE route_id IN (SELECT id FROM routes WHERE source = 'catalogue')`)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	routesTag, err := tx.Exec(ctx, `DELETE FROM routes WHERE source = 'catalogue'`)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	stopsTag, err := tx.Exec(ctx, `
		DELETE FROM stops
		WHERE source = 'catalogue'
		AND NOT EXISTS (
			SELECT 1 FROM route_legs rl
			WHERE rl.from_stop_id = stops.id OR rl.to_stop_id = stops.id
		)`)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, 0, err
	}
	return routesTag.RowsAffected(), legsTag.RowsAffected(), stopsTag.RowsAffected(), geometriesTag.RowsAffected(), nil
}

// CreateRouteLeg inserts a hand-seeded leg (cmd/seed) — distance_meters
// stays NULL and fare_estimated stays false via each column's DB default.
// Use CreateCatalogueRouteLeg for a catalogue-imported leg.
func (r *Repo) CreateRouteLeg(ctx context.Context, routeID, fromStopID, toStopID uuid.UUID, sequence int, fareCents int64) (RouteLeg, error) {
	var l RouteLeg
	err := r.pool.QueryRow(ctx,
		`INSERT INTO route_legs (route_id, from_stop_id, to_stop_id, sequence, fare_cents)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, route_id, from_stop_id, to_stop_id, sequence, fare_cents, distance_meters, fare_estimated, created_at`,
		routeID, fromStopID, toStopID, sequence, fareCents,
	).Scan(&l.ID, &l.RouteID, &l.FromStopID, &l.ToStopID, &l.Sequence, &l.FareCents, &l.DistanceMeters, &l.FareEstimated, &l.CreatedAt)
	return l, err
}

// CreateCatalogueRouteLeg inserts a catalogue-imported route's single leg
// (sequence is always 1 — the source data has only endpoints, no
// intermediate stops), tagged fare_estimated = true and carrying the source
// CSV's own distance measurement. Used only by internal/catalogue.
func (r *Repo) CreateCatalogueRouteLeg(ctx context.Context, routeID, fromStopID, toStopID uuid.UUID, fareCents int64, distanceMeters float64) (RouteLeg, error) {
	var l RouteLeg
	err := r.pool.QueryRow(ctx,
		`INSERT INTO route_legs (route_id, from_stop_id, to_stop_id, sequence, fare_cents, distance_meters, fare_estimated)
		 VALUES ($1, $2, $3, 1, $4, $5, true)
		 RETURNING id, route_id, from_stop_id, to_stop_id, sequence, fare_cents, distance_meters, fare_estimated, created_at`,
		routeID, fromStopID, toStopID, fareCents, distanceMeters,
	).Scan(&l.ID, &l.RouteID, &l.FromStopID, &l.ToStopID, &l.Sequence, &l.FareCents, &l.DistanceMeters, &l.FareEstimated, &l.CreatedAt)
	return l, err
}

// ListLegsForRoute returns a route's legs ordered by Sequence.
func (r *Repo) ListLegsForRoute(ctx context.Context, routeID uuid.UUID) ([]RouteLeg, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, route_id, from_stop_id, to_stop_id, sequence, fare_cents, distance_meters, fare_estimated, created_at
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
		if err := rows.Scan(&l.ID, &l.RouteID, &l.FromStopID, &l.ToStopID, &l.Sequence, &l.FareCents, &l.DistanceMeters, &l.FareEstimated, &l.CreatedAt); err != nil {
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

// CreateRouteGeometry stores a route's full display polyline: points is an
// ordered slice of [lon, lat] pairs (WGS84, GeoJSON coordinate order),
// marshaled to a flat JSONB array (see the 000006 migration's doc comment
// for why JSONB over a native geometry column). Used only by
// internal/catalogue's importer — a hand-seeded route never gets one.
// ON CONFLICT DO NOTHING makes a re-import idempotent alongside
// CreateCatalogueRoute's own name-based idempotency check.
func (r *Repo) CreateRouteGeometry(ctx context.Context, routeID uuid.UUID, points [][2]float64) error {
	data, err := json.Marshal(points)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO route_geometries (route_id, geometry, point_count) VALUES ($1, $2, $3)
		 ON CONFLICT (route_id) DO NOTHING`,
		routeID, data, len(points),
	)
	return err
}

// GetRouteGeometry returns a route's stored display polyline ([lon, lat]
// pairs, WGS84, in path order), or ErrNotFound if the route has none (every
// hand-seeded route, and any catalogue route imported before geometry
// storage existed).
func (r *Repo) GetRouteGeometry(ctx context.Context, routeID uuid.UUID) ([][2]float64, error) {
	var data []byte
	err := r.pool.QueryRow(ctx,
		`SELECT geometry FROM route_geometries WHERE route_id = $1`, routeID,
	).Scan(&data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var points [][2]float64
	if err := json.Unmarshal(data, &points); err != nil {
		return nil, err
	}
	return points, nil
}

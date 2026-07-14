package catalogue

import (
	"context"

	"sesfikile/backend/internal/routing"
)

// ClearStats reports what Clear removed.
type ClearStats struct {
	RoutesDeleted     int64
	LegsDeleted       int64
	StopsDeleted      int64
	GeometriesDeleted int64
}

// Clear removes every catalogue-imported route/leg/geometry
// (routing.SourceCatalogue) and any stop tagged source='catalogue' left
// with zero remaining references as a result — see
// routing.Repo.DeleteCatalogueData for the exact scoping guarantee that
// makes this safe to run at any time without touching cmd/seed's
// hand-seeded baseline (a "seed"-sourced route or stop is structurally
// unreachable by this operation). Idempotent: calling it when nothing was
// ever imported returns all zeros, not an error.
func Clear(ctx context.Context, repo *routing.Repo) (ClearStats, error) {
	routesDeleted, legsDeleted, stopsDeleted, geometriesDeleted, err := repo.DeleteCatalogueData(ctx)
	if err != nil {
		return ClearStats{}, err
	}
	return ClearStats{
		RoutesDeleted:     routesDeleted,
		LegsDeleted:       legsDeleted,
		StopsDeleted:      stopsDeleted,
		GeometriesDeleted: geometriesDeleted,
	}, nil
}

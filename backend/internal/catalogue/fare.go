package catalogue

import (
	"math"

	"sesfikile/backend/internal/config"
)

// EstimateFareCents derives an INDICATIVE fare in cents from a route's
// straight-line distance in metres: base + per-kilometre rate, rounded to
// the nearest cent, clamped to [model.MinFareCents, model.MaxFareCents].
//
// This is NOT a real association tariff — the source CSV
// (backend/data/taxi_routes.csv) carries no fare data whatsoever. Every
// fare this produces is stored with fare_estimated = true
// (routing.RouteLeg/route_legs) and must be presented everywhere as
// "estimated from distance, not an actual association fare." Real fares
// require association tariff data, which does not exist as an input to
// this MVP.
func EstimateFareCents(distanceMeters float64, model config.CatalogueFareModel) int64 {
	km := distanceMeters / 1000
	fare := model.BaseCents + int64(math.Round(float64(model.PerKmCents)*km))

	if fare < model.MinFareCents {
		fare = model.MinFareCents
	}
	if fare > model.MaxFareCents {
		fare = model.MaxFareCents
	}
	return fare
}

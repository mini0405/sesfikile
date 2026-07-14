-- Opt-in real Cape Town route catalogue import (backend/data/taxi_routes.csv,
-- City of Cape Town open data — cmd/importcatalogue). Purely additive to
-- Stage 3's schema; nothing here changes cmd/seed's hand-seeded 8/12
-- baseline, which keeps using every column's default value unchanged.
--
-- SCOPE HONESTY: the source CSV carries real origin/destination rank names
-- and route distances, but NO coordinates for any rank (only endpoints, no
-- intermediate stops, no fares). Catalogue-imported stops are therefore
-- coordinate-less by construction (latitude/longitude NULL) and can never
-- appear on the live map or in telemetry/stop-request matching, which both
-- require a real position — see internal/routing.Stop.CoordinatesKnown and
-- internal/stops.Handlers.loadRouteStops. Catalogue-imported route legs
-- carry an ESTIMATED fare derived from distance (internal/catalogue.
-- EstimateFareCents), never a real association tariff — fare_estimated
-- flags this on every such leg.

-- latitude/longitude are NULL for catalogue-imported stops (unknown
-- position); the CHECK keeps the pair consistent (both known or both
-- unknown) so nothing can end up with only half a coordinate.
ALTER TABLE stops ALTER COLUMN latitude DROP NOT NULL;
ALTER TABLE stops ALTER COLUMN longitude DROP NOT NULL;
ALTER TABLE stops ADD CONSTRAINT stops_coordinates_paired CHECK (
    (latitude IS NULL) = (longitude IS NULL)
);

-- 'seed' = cmd/seed's hand-seeded demo corridors (the tested baseline);
-- 'catalogue' = cmd/importcatalogue's real-but-unverified City of Cape Town
-- rows. Every existing route defaults to 'seed', unchanged.
ALTER TABLE routes ADD COLUMN source TEXT NOT NULL DEFAULT 'seed'
    CHECK (source IN ('seed', 'catalogue'));
CREATE INDEX routes_source_idx ON routes (source);

-- distance_meters is the source CSV's SHAPE_Length, kept for traceability;
-- NULL for hand-seeded legs, which have no such measurement.
-- fare_estimated flags a leg whose fare_cents was computed by
-- internal/catalogue.EstimateFareCents (distance-derived), not a real
-- association tariff. Every existing leg defaults to false, unchanged.
ALTER TABLE route_legs ADD COLUMN distance_meters DOUBLE PRECISION;
ALTER TABLE route_legs ADD COLUMN fare_estimated BOOLEAN NOT NULL DEFAULT false;

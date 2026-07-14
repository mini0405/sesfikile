-- The GeoJSON upgrade to the opt-in route catalogue importer — see
-- internal/catalogue and docs/PROGRESS.md's "Real route catalogue import:
-- GeoJSON upgrade" entry. Two additions:
--
-- 1. Real polyline geometry per catalogue route. One row per route that has
--    geometry; a hand-seeded route (source='seed') never gets one.
--
--    JSONB — a flat JSON array of [lon, lat] pairs, WGS84, in path order —
--    rather than PostGIS/a native geometry column: every feature in the
--    source dataset is a MultiLineString with exactly one LineString part
--    (verified against the real file), so flattening to a plain point array
--    loses nothing, and this MVP only ever needs to read a route's polyline
--    back whole for display — no spatial queries (ST_Intersects,
--    nearest-neighbour, simplification, etc.) are performed against it.
--    Reusing JSONB avoids a new Postgres extension and a new Go dependency
--    for a need this simple.
CREATE TABLE route_geometries (
    route_id    UUID PRIMARY KEY REFERENCES routes(id),
    geometry    JSONB NOT NULL,
    point_count INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 2. stops.source, mirroring routes.source. Before this upgrade every
--    catalogue-imported stop had NULL coordinates, so "clear the catalogue"
--    could safely find them via `latitude IS NULL`. Now that catalogue
--    stops get a real (median-derived, approximate) coordinate, that check
--    can no longer tell a catalogue stop from a hand-seeded one — a stop's
--    own provenance needs to be tracked explicitly, the same way a route's
--    already is, so cmd/clearcatalogue keeps restoring the exact clean
--    seeded baseline rather than leaving orphaned catalogue stops behind.
ALTER TABLE stops ADD COLUMN source TEXT NOT NULL DEFAULT 'seed'
    CHECK (source IN ('seed', 'catalogue'));
CREATE INDEX stops_source_idx ON stops (source);

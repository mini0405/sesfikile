ALTER TABLE route_legs DROP COLUMN fare_estimated;
ALTER TABLE route_legs DROP COLUMN distance_meters;

DROP INDEX IF EXISTS routes_source_idx;
ALTER TABLE routes DROP COLUMN source;

ALTER TABLE stops DROP CONSTRAINT stops_coordinates_paired;
-- Only safe if no catalogue rows remain (all stops still have real
-- coordinates) — run cmd/clearcatalogue -apply before migrating down.
ALTER TABLE stops ALTER COLUMN longitude SET NOT NULL;
ALTER TABLE stops ALTER COLUMN latitude SET NOT NULL;

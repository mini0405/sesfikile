DROP INDEX IF EXISTS stops_source_idx;
ALTER TABLE stops DROP COLUMN source;

DROP TABLE route_geometries;

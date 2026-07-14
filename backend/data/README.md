# backend/data — opt-in route catalogue source data

Source data for the opt-in real Cape Town route catalogue importer
(`internal/catalogue`, `cmd/importcatalogue`). Nothing here is read by
`cmd/seed`, `cmd/server`, or any other part of the app — it only matters if
you choose to run `cmd/importcatalogue`. See `docs/PROGRESS.md`'s "Real
route catalogue import" entries for the full write-up.

## Provenance

**City of Cape Town open data**, dataset `SL_CGIS_TAXI_RTS` (minibus taxi
routes). **Copyright: Western Cape Government, Department of Transport and
Public Works.**

Source API (reference only — the importer never fetches this at runtime;
both files below must already exist locally):

```
https://citymaps.capetown.gov.za/agsext/rest/services/Theme_Based/ODP_SPLIT_6/FeatureServer/11
```

The live API serves EPSG:3857 (Web Mercator). Both files in this directory
are one-time exports already reprojected to WGS84 (lon/lat) — query the
FeatureServer with `outSR=4326` (and `f=geojson` for the GeoJSON form) to
reproduce them.

## Files

- **`taxi_routes.json`** (GeoJSON FeatureCollection, ~16MB, **not committed
  — see below**) — the current importer's input. 1466 features, each with
  `properties.{OBJECTID, ORGN, DSTN, SHAPE_Length}` plus a `MultiLineString`
  geometry (the route's real polyline, ~394 points/route on average).
  `cmd/importcatalogue` defaults to reading this file as
  `data/taxi_routes.json` (i.e. run from `backend/`).
- **`taxi_routes.csv`** (~70KB, committed) — the original CSV export with
  the same `OBJECTID, ORGN, DSTN, SHAPE_Length` attributes but **no
  geometry**. The CSV-only importer this file originally fed has been
  **retired** (see docs/PROGRESS.md's GeoJSON-upgrade entry) in favour of
  the GeoJSON above, which is a strict superset of this data. Kept here as
  a small, harmless historical/reference artifact — nothing in the codebase
  reads it anymore.

## Why the GeoJSON isn't committed

`taxi_routes.json` is gitignored (see the root `.gitignore`). At ~16MB it's
large for a git repository — especially one with no cloud/LFS setup — and
it's static reference data that's trivial to re-obtain from the source API
above (or from wherever this copy originally came from) rather than
carrying it in every clone and every future commit's history. The small CSV
(~70KB) is comfortably committed since it costs nothing; the GeoJSON isn't.

If you need it: place a copy at `backend/data/taxi_routes.json` before
running `go run ./cmd/importcatalogue`. The command will error clearly
(pointing back here) if the file is missing.

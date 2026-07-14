// Package catalogue is the OPT-IN importer for the real City of Cape Town
// taxi-routes open dataset (backend/data/taxi_routes.json — see
// cmd/importcatalogue). It is entirely additive to cmd/seed's hand-seeded
// 8-corridor/12-stop demo baseline: every row this package creates is
// tagged routing.SourceCatalogue, is removable independently via Clear, and
// never runs unless cmd/importcatalogue is invoked by hand.
//
// PROVENANCE: City of Cape Town open data, "SL_CGIS_TAXI_RTS"
// (Copyright: Western Cape Government, Department of Transport and Public
// Works). Source API (for reference only — this package does NOT fetch it
// at runtime; the GeoJSON must already exist locally, see
// backend/data/README.md):
// https://citymaps.capetown.gov.za/agsext/rest/services/Theme_Based/ODP_SPLIT_6/FeatureServer/11
// The live API serves EPSG:3857 (Web Mercator); backend/data/taxi_routes.json
// is a one-time export already reprojected to WGS84 (CRS84, lon/lat) —
// that's the whole reason this package reads a local file instead of
// calling the API.
//
// SCOPE HONESTY (per CLAUDE.md):
//   - REAL: origin rank name, destination rank name, and (as of this
//     upgrade) each route's real polyline geometry and endpoint-derived rank
//     coordinates. Directional pairs (A->B and B->A) and "VIA <x>" variants
//     are distinct real routes and are kept distinct, never deduplicated.
//   - APPROXIMATE: rank coordinates are the MEDIAN of every endpoint
//     position a rank appears at across the whole dataset (see
//     medianCoordinate in geo.go) — a derived centroid, not a surveyed rank
//     position. A large area like Khayelitsha has routes departing from
//     genuinely different points spread over ~20km; the median snaps to the
//     main cluster rather than being dragged toward an unrepresentative
//     average.
//   - NOT PRESENT: named intermediate stops (a polyline's vertices are shape
//     points, not boarding stops — every imported route stays a single leg),
//     fares (see fare.go's distance-derived ESTIMATE), and association
//     sign-off.
package catalogue

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Row is one parsed, not-yet-normalized feature from the source GeoJSON.
// ObjectID is the source dataset's own feature identifier — carried through
// into the imported route's name (see routeName in import.go) so a
// re-import is trivially idempotent and every imported route stays
// traceable back to its exact source feature. Points is the feature's full
// ordered polyline (lon, lat pairs, WGS84) — Points[0] is the origin rank's
// location, Points[len(Points)-1] is the destination rank's location (see
// import.go's endpoint/median handling), and the whole slice is stored
// verbatim as the route's display geometry (routing.Repo.CreateRouteGeometry).
type Row struct {
	ObjectID       int
	Origin         string
	Destination    string
	DistanceMeters float64
	Points         [][2]float64 // [lon, lat], WGS84 — GeoJSON coordinate order
}

// ParseStats reports what happened while reading the source file,
// independent of any database work.
type ParseStats struct {
	TotalDataRows int // features read
	BlankDropped  int // features dropped because ORGN or DSTN was blank/whitespace
}

type geoJSONFeatureCollection struct {
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Properties geoJSONProperties `json:"properties"`
	Geometry   geoJSONGeometry   `json:"geometry"`
}

type geoJSONProperties struct {
	ObjectID    int     `json:"OBJECTID"`
	Origin      string  `json:"ORGN"`
	Destination string  `json:"DSTN"`
	ShapeLength float64 `json:"SHAPE_Length"`
}

// geoJSONGeometry is deliberately narrow: every feature in this dataset is a
// MultiLineString with exactly one LineString part (verified against the
// real file — see docs/PROGRESS.md), so Coordinates is a
// slice-of-parts-of-points ([part][point][lon,lat]) and ParseGeoJSON simply
// concatenates every part's points in order. If a future export ever had
// more than one part, this would treat the join between parts as a
// continuous line for distance purposes — acceptable here since it never
// happens in the actual data, not a general-purpose GeoJSON reader.
type geoJSONGeometry struct {
	Type        string         `json:"type"`
	Coordinates [][][2]float64 `json:"coordinates"`
}

// ParseGeoJSON reads the City of Cape Town taxi-routes GeoJSON
// FeatureCollection: OBJECTID/ORGN/DSTN properties (identical attributes to
// the retired CSV importer) plus a MultiLineString geometry per feature.
//
// Features where ORGN or DSTN is blank/whitespace-only are dropped (counted
// in ParseStats.BlankDropped, never returned as a Row) — same rule as the
// retired CSV importer. A handful of features have an identical origin and
// destination — a rank-internal loop route; these are kept, not dropped,
// since they're a real (if unusual) route in the source data.
//
// DistanceMeters is NOT read from the SHAPE_Length property: cross-checking
// this GeoJSON export against the original CSV (same OBJECTID, e.g. #1
// BELLVILLE->DURBANVILLE: CSV SHAPE_Length 12918.67, GeoJSON SHAPE_Length
// 0.1006) showed the GeoJSON's SHAPE_Length is in decimal degrees
// (unprojected CRS84 path length), not metres. Rather than depend on an
// attribute in the wrong unit — or on having the CSV around to cross-check
// against — DistanceMeters is computed directly from the real geometry by
// summing haversine distance along consecutive polyline vertices
// (polylineLengthMeters in geo.go), which is an honest, self-contained
// real-world measurement and needs no unit-conversion guesswork.
func ParseGeoJSON(r io.Reader) ([]Row, ParseStats, error) {
	var fc geoJSONFeatureCollection
	if err := json.NewDecoder(r).Decode(&fc); err != nil {
		return nil, ParseStats{}, fmt.Errorf("parse geojson: %w", err)
	}

	var (
		rows  []Row
		stats ParseStats
	)
	for i, feature := range fc.Features {
		stats.TotalDataRows++

		origin := strings.TrimSpace(feature.Properties.Origin)
		dest := strings.TrimSpace(feature.Properties.Destination)
		if origin == "" || dest == "" {
			stats.BlankDropped++
			continue
		}

		points := make([][2]float64, 0, 400)
		for _, part := range feature.Geometry.Coordinates {
			points = append(points, part...)
		}
		if len(points) < 2 {
			return nil, ParseStats{}, fmt.Errorf("feature %d (OBJECTID %d): geometry has fewer than 2 points", i, feature.Properties.ObjectID)
		}

		rows = append(rows, Row{
			ObjectID:       feature.Properties.ObjectID,
			Origin:         origin,
			Destination:    dest,
			DistanceMeters: polylineLengthMeters(points),
			Points:         points,
		})
	}

	return rows, stats, nil
}

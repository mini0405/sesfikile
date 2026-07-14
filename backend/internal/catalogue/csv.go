// Package catalogue is the OPT-IN importer for the real City of Cape Town
// taxi-routes open-data CSV (backend/data/taxi_routes.csv, ~1466 rows —
// see cmd/importcatalogue). It is entirely additive to cmd/seed's
// hand-seeded 8-corridor/12-stop demo baseline: every row this package
// creates is tagged routing.SourceCatalogue, is removable independently via
// Clear, and never runs unless cmd/importcatalogue is invoked by hand.
//
// SCOPE HONESTY (per CLAUDE.md):
//   - REAL: origin rank name, destination rank name, route distance in
//     metres (SHAPE_Length). Directional pairs (A->B and B->A) and "VIA <x>"
//     variants are distinct real routes and are kept distinct, never
//     deduplicated.
//   - NOT PRESENT: intermediate stops (only endpoints — every imported route
//     is a single leg), fares, and coordinates (no lat/lng anywhere in the
//     source). See fare.go for the estimated-fare model and
//     routing.Stop.CoordinatesKnown for the coordinate gap's consequences
//     (no map, no telemetry, no live stop-request matching).
package catalogue

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Row is one parsed, not-yet-normalized record from the source CSV.
// ObjectID is the source dataset's own row identifier — carried through
// into the imported route's name (see routeName in import.go) so a re-import
// is trivially idempotent and every imported route stays traceable back to
// its exact source row.
type Row struct {
	ObjectID       int
	Origin         string
	Destination    string
	DistanceMeters float64
}

// ParseStats reports what happened while reading the CSV, independent of
// any database work.
type ParseStats struct {
	TotalDataRows int // data rows read, excluding the header
	BlankDropped  int // rows dropped because ORGN or DSTN was blank/whitespace
}

// ParseCSV reads the City of Cape Town taxi-routes CSV: OBJECTID, ORGN
// (origin rank), DSTN (destination rank), SHAPE_Length (route length in
// metres) columns, read positionally (the header row itself is skipped, not
// validated by name). It tolerates a leading UTF-8 BOM (present in the
// source file) and relies on encoding/csv for RFC 4180-compliant
// quoted-comma handling — several rank names embed a literal comma (e.g.
// "FABRIEKS AREA,ATLANTIS") — rather than any hand-rolled splitting.
//
// Rows where ORGN or DSTN is blank/whitespace-only are dropped (counted in
// ParseStats.BlankDropped, never returned as a Row). A handful of source
// rows have an identical origin and destination — a rank-internal loop
// route; these are kept, not dropped, since they're a real (if unusual)
// route in the source data, not a parsing error.
func ParseCSV(r io.Reader) ([]Row, ParseStats, error) {
	br := bufio.NewReader(r)
	if bom, err := br.Peek(3); err == nil && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		_, _ = br.Discard(3)
	}

	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1 // tolerant; each row's shape is validated below instead

	records, err := cr.ReadAll()
	if err != nil {
		return nil, ParseStats{}, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) == 0 {
		return nil, ParseStats{}, fmt.Errorf("csv has no rows")
	}

	var (
		rows  []Row
		stats ParseStats
	)
	for i, rec := range records {
		if i == 0 {
			continue // header row (OBJECTID, ORGN, DSTN, SHAPE_Length)
		}
		if len(rec) < 4 {
			return nil, ParseStats{}, fmt.Errorf("row %d: expected at least 4 columns, got %d", i+1, len(rec))
		}
		stats.TotalDataRows++

		origin := strings.TrimSpace(rec[1])
		dest := strings.TrimSpace(rec[2])
		if origin == "" || dest == "" {
			stats.BlankDropped++
			continue
		}

		objectID, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		if err != nil {
			return nil, ParseStats{}, fmt.Errorf("row %d: invalid OBJECTID %q: %w", i+1, rec[0], err)
		}
		distance, err := strconv.ParseFloat(strings.TrimSpace(rec[3]), 64)
		if err != nil {
			return nil, ParseStats{}, fmt.Errorf("row %d: invalid SHAPE_Length %q: %w", i+1, rec[3], err)
		}

		rows = append(rows, Row{ObjectID: objectID, Origin: origin, Destination: dest, DistanceMeters: distance})
	}

	return rows, stats, nil
}

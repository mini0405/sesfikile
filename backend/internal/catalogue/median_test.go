package catalogue

import (
	"math"
	"os"
	"testing"
)

// TestMedianCoordinate_ResistsOutliers is the brief's explicit
// non-negotiable: given a tight cluster of endpoint samples plus one wild
// outlier, medianCoordinate must land near the cluster, not be dragged
// toward the outlier the way a plain mean would be.
func TestMedianCoordinate_ResistsOutliers(t *testing.T) {
	samples := [][2]float64{
		{18.398, -33.898},
		{18.399, -33.899},
		{18.400, -33.900},
		{18.401, -33.901},
		{18.402, -33.902},
		{25.0, -29.0}, // wild outlier, nowhere near Cape Town
	}

	lon, lat := medianCoordinate(samples)

	// 6 samples -> median is the average of the 3rd and 4th sorted values:
	// lons sorted: 18.398, 18.399, 18.400, 18.401, 18.402, 25.0 -> (18.400+18.401)/2
	wantLon, wantLat := 18.4005, -33.9005
	if math.Abs(lon-wantLon) > 0.001 {
		t.Errorf("expected median longitude ~%v, got %v", wantLon, lon)
	}
	if math.Abs(lat-wantLat) > 0.001 {
		t.Errorf("expected median latitude ~%v, got %v", wantLat, lat)
	}

	var meanLon float64
	for _, s := range samples {
		meanLon += s[0]
	}
	meanLon /= float64(len(samples))

	if math.Abs(lon-meanLon) < 0.5 {
		t.Errorf("median result (%v) is suspiciously close to the mean (%v) — expected the median to resist the outlier", lon, meanLon)
	}
}

func TestMedianCoordinate_SinglePoint(t *testing.T) {
	lon, lat := medianCoordinate([][2]float64{{18.4241, -33.9249}})
	if lon != 18.4241 || lat != -33.9249 {
		t.Errorf("expected the single sample back exactly, got %v/%v", lon, lat)
	}
}

func TestMedianCoordinate_EvenCount(t *testing.T) {
	lon, _ := medianCoordinate([][2]float64{{18.0, -33.0}, {19.0, -34.0}})
	if lon != 18.5 {
		t.Errorf("expected the average of 18.0 and 19.0 (18.5) for an even-sized sample, got %v", lon)
	}
}

// TestPrepareRows_KnownRankGetsSaneCapeTownCoordinate is a live check
// against the real source file (skips if not present, same pattern as
// normalize_test.go's real-data audit): a well-known rank should end up
// with a coordinate that actually falls within greater Cape Town, not some
// nonsensical value. Pure computation only — no database involved, so this
// can never touch or risk a developer's real loaded catalogue.
func TestPrepareRows_KnownRankGetsSaneCapeTownCoordinate(t *testing.T) {
	f, err := os.Open("../../data/taxi_routes.json")
	if err != nil {
		t.Skipf("skipping: source GeoJSON not found: %v", err)
	}
	defer f.Close()

	rows, _, err := ParseGeoJSON(f)
	if err != nil {
		t.Fatalf("failed to parse geojson: %v", err)
	}

	_, rankCoordinates := prepareRows(rows)

	for _, rank := range []string{"WYNBERG", "CAPE TOWN STATION"} {
		coord, ok := rankCoordinates[rank]
		if !ok {
			t.Errorf("expected %q to have a computed coordinate", rank)
			continue
		}
		lon, lat := coord[0], coord[1]
		// Greater Cape Town's rough bounding box.
		if lat > -33.4 || lat < -34.6 || lon < 18.0 || lon > 19.2 {
			t.Errorf("expected %q's coordinate to fall within greater Cape Town, got lon=%v lat=%v", rank, lon, lat)
		}
	}
}

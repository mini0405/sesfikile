package catalogue

import (
	"math"
	"sort"
)

const earthRadiusMeters = 6371000.0

// haversineMeters returns the great-circle distance between two WGS84
// points (lon, lat, in that order — GeoJSON's coordinate order) in metres.
func haversineMeters(lon1, lat1, lon2, lat2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(p1)*math.Cos(p2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

// polylineLengthMeters sums the haversine distance between every consecutive
// pair of points along a route's polyline — see ParseGeoJSON's doc comment
// for why this replaces the source file's own (wrongly-unitted)
// SHAPE_Length property.
func polylineLengthMeters(points [][2]float64) float64 {
	var total float64
	for i := 1; i < len(points); i++ {
		total += haversineMeters(points[i-1][0], points[i-1][1], points[i][0], points[i][1])
	}
	return total
}

// medianCoordinate computes a representative (lon, lat) for a set of
// endpoint samples using the MEDIAN of their longitudes and the MEDIAN of
// their latitudes independently — NOT the mean. A rank can appear as an
// endpoint across many routes with real spread (e.g. "KHAYELITSHA" spans
// ~20km of genuinely different departure points across the dataset); the
// mean of such a spread can land in an unrepresentative or even invalid
// location (e.g. the middle of a spread that isn't actually where the rank
// is), while the per-axis median snaps to wherever the bulk of the samples
// actually cluster and is far less sensitive to a handful of outliers.
//
// This is the per-axis coordinate median (independently sorting longitudes
// and latitudes), not the true geometric median (which would minimise the
// sum of distances to every sample via an iterative algorithm like
// Weiszfeld's) — the simpler per-axis version is what was asked for and is
// more than sufficient for an approximate rank centroid.
func medianCoordinate(samples [][2]float64) (lon, lat float64) {
	lons := make([]float64, len(samples))
	lats := make([]float64, len(samples))
	for i, s := range samples {
		lons[i] = s[0]
		lats[i] = s[1]
	}
	sort.Float64s(lons)
	sort.Float64s(lats)
	return median(lons), median(lats)
}

// median assumes sorted is already sorted ascending.
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

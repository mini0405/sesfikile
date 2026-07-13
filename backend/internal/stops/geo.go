package stops

import "math"

const earthRadiusMeters = 6371000

// haversineMeters is the great-circle distance between two lat/lng points,
// used both to approximate a driver's progress along a route (nearest stop)
// and to rank qualifying drivers by physical distance to the requested stop.
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

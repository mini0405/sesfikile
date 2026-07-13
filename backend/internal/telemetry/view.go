package telemetry

// VehicleView is the JSON-serializable projection of a VehicleState sent
// over REST and WebSocket.
type VehicleView struct {
	VehicleID      string  `json:"vehicle_id"`
	RouteID        string  `json:"route_id"`
	Lat            float64 `json:"lat"`
	Lng            float64 `json:"lng"`
	SeatsTotal     int     `json:"seats_total"`
	SeatsAvailable int     `json:"seats_available"`
	LastUpdated    string  `json:"last_updated"`
}

func toView(s VehicleState) VehicleView {
	return VehicleView{
		VehicleID:      s.VehicleID.String(),
		RouteID:        s.RouteID.String(),
		Lat:            s.Lat,
		Lng:            s.Lng,
		SeatsTotal:     s.SeatsTotal,
		SeatsAvailable: s.SeatsAvailable,
		LastUpdated:    s.LastUpdated.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
}

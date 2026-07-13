package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// Pinger is satisfied by *db.DB (and by fakes in tests).
type Pinger interface {
	Ping(ctx context.Context) error
}

func healthHandler(pinger Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := pinger.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "degraded",
				"db":     "down",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"db":     "ok",
		})
	}
}

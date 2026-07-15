package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"sesfikile/backend/internal/analytics"
	"sesfikile/backend/internal/boarding"
	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/stops"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

func NewRouter(pinger Pinger, identityHandlers *identity.Handlers, tokens identity.TokenIssuer, walletHandlers *wallet.Handlers, routingHandlers *routing.Handlers, telemetryHandlers *telemetry.Handlers, boardingHandlers *boarding.Handlers, stopsHandlers *stops.Handlers, fuelHandlers *fuel.Handlers, analyticsHandlers *analytics.Handlers) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Dev-only permissive CORS: the Stage 9a driver web app (Vite dev server,
	// a different origin than this API) needs to call these endpoints from
	// the browser. No cloud/production deployment exists yet for this MVP,
	// so a wide-open allow-all is acceptable here; tighten this to a real
	// allowlist before any non-local deployment.
	r.Use(devCORS)

	r.Get("/health", healthHandler(pinger))

	r.Post("/auth/register", identityHandlers.Register)
	r.Post("/auth/login", identityHandlers.Login)

	r.Get("/routes", routingHandlers.ListRoutes)
	r.Get("/routes/search", routingHandlers.Search)
	r.Get("/routes/geometries", routingHandlers.ListRouteGeometries)
	r.Get("/routes/{id}", routingHandlers.GetRoute)
	r.Get("/routes/{id}/geometry", routingHandlers.GetRouteGeometry)
	r.Get("/stops", routingHandlers.ListStops)

	// /ws/driver validates its own JWT on the handshake (query param or
	// header — see telemetry.bearerToken) rather than via RequireAuth,
	// since it needs to authenticate before the HTTP->WS upgrade completes.
	// /ws/commuter and the REST snapshot are intentionally public — see
	// telemetry.Handlers.CommuterWS's doc comment.
	r.Get("/ws/driver", telemetryHandlers.DriverWS)
	r.Get("/ws/commuter", telemetryHandlers.CommuterWS)
	r.Get("/telemetry/vehicles", telemetryHandlers.VehiclesSnapshot)

	// /fuel/viu/* are the MOCK VIU/pump endpoints (see internal/fuel/viu_mock.go)
	// — deliberately public, since a real device sits behind hardware-level
	// authentication, not a user JWT, and modeling that is out of scope for
	// this MVP simulation.
	r.Post("/fuel/viu/authorize", fuelHandlers.AuthorizePump)
	r.Post("/fuel/viu/confirm", fuelHandlers.ConfirmPump)

	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Get("/me", identityHandlers.Me)
		r.Get("/wallet/balance", walletHandlers.Balance)

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleCommuter))
			r.Post("/wallet/topup", walletHandlers.Topup)
			r.Get("/wallet/transactions", walletHandlers.Transactions)
			r.Post("/boarding/pass", boardingHandlers.IssuePass)
			r.Post("/stops/request", stopsHandlers.RequestStop)
		})

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleDriver))
			r.Post("/fare/charge", walletHandlers.ChargeFare)
			r.Post("/telemetry/seats", telemetryHandlers.UpdateSeats)
			r.Post("/boarding/scan", boardingHandlers.ScanPass)
			r.Post("/stops/request/{id}/ack", stopsHandlers.AckRequest)
		})

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleOwner))
			r.Post("/fuel/allocate", fuelHandlers.Allocate)
			r.Get("/fuel/balance", fuelHandlers.Balance)
			r.Post("/fuel/vehicle/quota", fuelHandlers.FundVehicleQuota)
			r.Get("/fuel/vehicle/quota", fuelHandlers.VehicleQuota)

			r.Get("/owner/summary", analyticsHandlers.Summary)
			r.Get("/owner/vehicles", analyticsHandlers.Vehicles)
			r.Get("/owner/drivers", analyticsHandlers.Drivers)
			r.Get("/owner/revenue-vs-fuel", analyticsHandlers.RevenueVsFuel)
			r.Get("/owner/ledger", analyticsHandlers.Ledger)
		})
	})

	return r
}

// devCORS is a dev-only, allow-all CORS middleware (see the comment where
// it's registered in NewRouter). It reflects the request's Origin rather
// than a hardcoded "*" so it also works for requests that need credentials
// mode, and handles the browser's OPTIONS preflight for the JSON POST
// endpoints (/auth/login, /boarding/scan, etc.) that carry an Authorization
// header and Content-Type: application/json.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

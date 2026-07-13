package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"sesfikile/backend/internal/boarding"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

func NewRouter(pinger Pinger, identityHandlers *identity.Handlers, tokens identity.TokenIssuer, walletHandlers *wallet.Handlers, routingHandlers *routing.Handlers, telemetryHandlers *telemetry.Handlers, boardingHandlers *boarding.Handlers) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler(pinger))

	r.Post("/auth/register", identityHandlers.Register)
	r.Post("/auth/login", identityHandlers.Login)

	r.Get("/routes", routingHandlers.ListRoutes)
	r.Get("/routes/search", routingHandlers.Search)
	r.Get("/routes/{id}", routingHandlers.GetRoute)

	// /ws/driver validates its own JWT on the handshake (query param or
	// header — see telemetry.bearerToken) rather than via RequireAuth,
	// since it needs to authenticate before the HTTP->WS upgrade completes.
	// /ws/commuter and the REST snapshot are intentionally public — see
	// telemetry.Handlers.CommuterWS's doc comment.
	r.Get("/ws/driver", telemetryHandlers.DriverWS)
	r.Get("/ws/commuter", telemetryHandlers.CommuterWS)
	r.Get("/telemetry/vehicles", telemetryHandlers.VehiclesSnapshot)

	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Get("/me", identityHandlers.Me)
		r.Get("/wallet/balance", walletHandlers.Balance)

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleCommuter))
			r.Post("/wallet/topup", walletHandlers.Topup)
			r.Post("/boarding/pass", boardingHandlers.IssuePass)
		})

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleDriver))
			r.Post("/fare/charge", walletHandlers.ChargeFare)
			r.Post("/telemetry/seats", telemetryHandlers.UpdateSeats)
			r.Post("/boarding/scan", boardingHandlers.ScanPass)
		})
	})

	return r
}

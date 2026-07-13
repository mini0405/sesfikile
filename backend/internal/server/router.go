package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/wallet"
)

func NewRouter(pinger Pinger, identityHandlers *identity.Handlers, tokens identity.TokenIssuer, walletHandlers *wallet.Handlers) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler(pinger))

	r.Post("/auth/register", identityHandlers.Register)
	r.Post("/auth/login", identityHandlers.Login)

	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Get("/me", identityHandlers.Me)
		r.Get("/wallet/balance", walletHandlers.Balance)

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleCommuter))
			r.Post("/wallet/topup", walletHandlers.Topup)
		})

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireRole(identity.RoleDriver))
			r.Post("/fare/charge", walletHandlers.ChargeFare)
		})
	})

	return r
}

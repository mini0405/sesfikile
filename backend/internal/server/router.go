package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"sesfikile/backend/internal/identity"
)

func NewRouter(pinger Pinger, identityHandlers *identity.Handlers, tokens identity.TokenIssuer) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler(pinger))

	r.Post("/auth/register", identityHandlers.Register)
	r.Post("/auth/login", identityHandlers.Login)

	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Get("/me", identityHandlers.Me)
	})

	return r
}

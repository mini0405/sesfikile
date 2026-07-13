package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/server"
	"sesfikile/backend/internal/wallet"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := config.Load()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		logger.Warn("skipping migrations: database not reachable or migration failed", "error", err)
	} else {
		logger.Info("migrations applied")
	}

	database, err := db.New(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to initialize db pool", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	tokens := identity.NewTokenIssuer(cfg.JWTSecret)
	identityRepo := identity.NewRepo(database.Pool)
	identityHandlers := identity.NewHandlers(identityRepo, tokens)

	walletRepo := wallet.NewRepo(database.Pool)
	if err := walletRepo.EnsureSystemAccounts(context.Background()); err != nil {
		logger.Warn("skipping system account setup: database not reachable or setup failed", "error", err)
	} else {
		logger.Info("system accounts ready")
	}
	walletHandlers := wallet.NewHandlers(walletRepo, cfg.FareSplit)

	routingRepo := routing.NewRepo(database.Pool)
	routingHandlers := routing.NewHandlers(routingRepo)

	router := server.NewRouter(database, identityHandlers, tokens, walletHandlers, routingHandlers)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()
	logger.Info("shutdown signal received, shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"sesfikile/backend/internal/analytics"
	"sesfikile/backend/internal/boarding"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/stops"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error {
	return f.err
}

func testRouter(pinger Pinger) chi.Router {
	tokens := identity.NewTokenIssuer("test-secret")
	handlers := identity.NewHandlers(identity.NewRepo(nil), tokens)
	walletHandlers := wallet.NewHandlers(wallet.NewRepo(nil), config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65})
	routingHandlers := routing.NewHandlers(routing.NewRepo(nil))
	identityRepo := identity.NewRepo(nil)
	routingRepo := routing.NewRepo(nil)
	telemetryStore := telemetry.NewVehicleStateStore()
	telemetryHub := telemetry.NewHub()
	driverAlerts := telemetry.NewDriverAlertHub()
	telemetryHandlers := telemetry.NewHandlers(telemetryStore, telemetryHub, driverAlerts, identityRepo, routingRepo, tokens)
	boardingHandlers := boarding.NewHandlers(routingRepo, wallet.NewRepo(nil), identityRepo, telemetryStore, telemetryHub, boarding.NewSigner("test-boarding-secret"), 3*time.Minute, config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65})
	stopsHandlers := stops.NewHandlers(stops.NewStore(), routingRepo, telemetryStore, driverAlerts, identityRepo)
	fuelRepo := fuel.NewRepo(nil, wallet.NewRepo(nil))
	fuelHandlers := fuel.NewHandlers(fuelRepo, 30, 2200)
	analyticsHandlers := analytics.NewHandlers(analytics.NewRepo(nil, wallet.NewRepo(nil), fuelRepo), identityRepo, routingRepo, fuelRepo, telemetryStore)
	return NewRouter(pinger, handlers, tokens, walletHandlers, routingHandlers, telemetryHandlers, boardingHandlers, stopsHandlers, fuelHandlers, analyticsHandlers)
}

func TestHealthHandler_Healthy(t *testing.T) {
	r := testRouter(fakePinger{err: nil})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "ok" || body["db"] != "ok" {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestHealthHandler_Degraded(t *testing.T) {
	r := testRouter(fakePinger{err: errors.New("connection refused")})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "degraded" || body["db"] != "down" {
		t.Errorf("unexpected body: %+v", body)
	}
}

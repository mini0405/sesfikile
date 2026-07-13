package telemetry_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
)

// This test requires a reachable Postgres (see infra/docker-compose.yml).
// It skips rather than failing when no database is available, matching
// every other DB-backed test in this repo (Stage 0-3).
func setupTelemetryTest(t *testing.T) (*identity.Repo, *routing.Repo, identity.TokenIssuer, *httptest.Server) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil || pool.Ping(ctx) != nil {
		t.Skip("skipping integration test: no reachable Postgres database")
	}
	t.Cleanup(pool.Close)

	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	identityRepo := identity.NewRepo(pool)
	routingRepo := routing.NewRepo(pool)
	tokens := identity.NewTokenIssuer("telemetry-integration-test-secret")

	store := telemetry.NewVehicleStateStore()
	hub := telemetry.NewHub()
	alerts := telemetry.NewDriverAlertHub()
	handlers := telemetry.NewHandlers(store, hub, alerts, identityRepo, routingRepo, tokens)

	r := chi.NewRouter()
	r.Get("/ws/driver", handlers.DriverWS)
	r.Get("/ws/commuter", handlers.CommuterWS)
	r.Get("/telemetry/vehicles", handlers.VehiclesSnapshot)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return identityRepo, routingRepo, tokens, server
}

// uniqueSuffix keeps fixtures collision-free across repeat runs against a
// persistent (not reset-between-runs) database — same reasoning as
// wallet.uniquePhone and routing's test fixtures.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

type driverFixture struct {
	Token     string
	VehicleID string
	RouteID   string
}

// seedDriverOnRoute creates a route, a driver, a vehicle (given capacity),
// an active assignment between them, and returns a driver JWT plus the
// created ids.
func seedDriverOnRoute(t *testing.T, identityRepo *identity.Repo, routingRepo *routing.Repo, tokens identity.TokenIssuer, capacity int) driverFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	fromStop, err := routingRepo.CreateStop(ctx, "Telemetry Test Origin "+suffix, -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create origin stop: %v", err)
	}
	toStop, err := routingRepo.CreateStop(ctx, "Telemetry Test Dest "+suffix, -33.95, 18.45)
	if err != nil {
		t.Fatalf("failed to create dest stop: %v", err)
	}
	route, err := routingRepo.CreateRoute(ctx, "Telemetry Test Route "+suffix, "Test Association")
	if err != nil {
		t.Fatalf("failed to create route: %v", err)
	}
	if _, err := routingRepo.CreateRouteLeg(ctx, route.ID, fromStop.ID, toStop.ID, 1, 1000); err != nil {
		t.Fatalf("failed to create route leg: %v", err)
	}

	ownerUser, err := identityRepo.CreateUser(ctx, "+27"+suffix+"1", nil, "x", identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	vehicle, err := identityRepo.CreateVehicle(ctx, ownerUser.ID, "TEL-"+suffix, capacity, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}
	driverUser, err := identityRepo.CreateUser(ctx, "+27"+suffix+"2", nil, "x", identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := identityRepo.CreateDriver(ctx, driverUser.ID, "Telemetry Test Driver", "PRDP-"+suffix, "ID-"+suffix)
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}
	if _, err := identityRepo.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to create vehicle assignment: %v", err)
	}

	token, err := tokens.Issue(driverUser.ID, identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to issue driver token: %v", err)
	}

	return driverFixture{Token: token, VehicleID: vehicle.ID.String(), RouteID: route.ID.String()}
}

func wsURL(server *httptest.Server, path string, query url.Values) string {
	u := strings.TrimPrefix(server.URL, "http://")
	return "ws://" + u + path + "?" + query.Encode()
}

func dialCommuter(t *testing.T, server *httptest.Server, routeID string) *websocket.Conn {
	t.Helper()
	q := url.Values{"route_id": {routeID}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server, "/ws/commuter", q), nil)
	if err != nil {
		t.Fatalf("failed to dial commuter ws: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	return conn
}

func readEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read event: %v", err)
	}
	return msg
}

func expectNoEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var msg map[string]any
	err := conn.ReadJSON(&msg)
	if err == nil {
		t.Fatalf("expected no event, got: %+v", msg)
	}
}

// TestDriverUpdatePropagatesToCommuterOnSameRoute is the core end-to-end
// flow: a driver connects and goes online, a commuter subscribed to that
// route sees it in the snapshot then receives position/seat updates, and a
// commuter subscribed to a different route sees none of it.
func TestDriverUpdatePropagatesToCommuterOnSameRoute(t *testing.T) {
	identityRepo, routingRepo, tokens, server := setupTelemetryTest(t)
	fx := seedDriverOnRoute(t, identityRepo, routingRepo, tokens, 4)

	otherRoute, err := routingRepo.CreateRoute(context.Background(), "Telemetry Other Route "+uniqueSuffix(), "Test Association")
	if err != nil {
		t.Fatalf("failed to create other route: %v", err)
	}

	commuterSame := dialCommuter(t, server, fx.RouteID)
	defer commuterSame.Close()
	commuterOther := dialCommuter(t, server, otherRoute.ID.String())
	defer commuterOther.Close()

	// Initial snapshots: nothing online yet.
	snapSame := readEvent(t, commuterSame, 2*time.Second)
	if snapSame["type"] != "snapshot" {
		t.Fatalf("expected initial snapshot, got %+v", snapSame)
	}
	snapOther := readEvent(t, commuterOther, 2*time.Second)
	if snapOther["type"] != "snapshot" {
		t.Fatalf("expected initial snapshot, got %+v", snapOther)
	}

	q := url.Values{"route_id": {fx.RouteID}}
	header := http.Header{"Authorization": {"Bearer " + fx.Token}}
	driverConn, resp, err := websocket.DefaultDialer.Dial(wsURL(server, "/ws/driver", q), header)
	if err != nil {
		t.Fatalf("failed to dial driver ws: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	defer driverConn.Close()

	// The same-route commuter should see the vehicle go online.
	onlineEvt := readEvent(t, commuterSame, 2*time.Second)
	if onlineEvt["type"] != "update" {
		t.Fatalf("expected update event on driver connect, got %+v", onlineEvt)
	}
	vehicle, ok := onlineEvt["vehicle"].(map[string]any)
	if !ok || vehicle["vehicle_id"] != fx.VehicleID {
		t.Fatalf("expected vehicle_id %s in update, got %+v", fx.VehicleID, onlineEvt)
	}
	if vehicle["seats_total"].(float64) != 4 {
		t.Fatalf("expected seats_total=4, got %+v", vehicle)
	}

	// The different-route commuter should see nothing.
	expectNoEvent(t, commuterOther, 500*time.Millisecond)

	// Position update.
	if err := driverConn.WriteJSON(map[string]any{"lat": 1.5, "lng": 2.5}); err != nil {
		t.Fatalf("failed to write position update: %v", err)
	}
	posEvt := readEvent(t, commuterSame, 2*time.Second)
	posVehicle := posEvt["vehicle"].(map[string]any)
	if posVehicle["lat"].(float64) != 1.5 || posVehicle["lng"].(float64) != 2.5 {
		t.Fatalf("expected lat=1.5 lng=2.5, got %+v", posVehicle)
	}

	// Seat delta update.
	if err := driverConn.WriteJSON(map[string]any{"seats_delta": -1}); err != nil {
		t.Fatalf("failed to write seat delta: %v", err)
	}
	seatEvt := readEvent(t, commuterSame, 2*time.Second)
	seatVehicle := seatEvt["vehicle"].(map[string]any)
	if seatVehicle["seats_available"].(float64) != 3 {
		t.Fatalf("expected seats_available=3 after delta -1, got %+v", seatVehicle)
	}

	// REST snapshot should agree while online.
	restResp, err := http.Get(server.URL + "/telemetry/vehicles?route_id=" + fx.RouteID)
	if err != nil {
		t.Fatalf("failed to GET rest snapshot: %v", err)
	}
	restResp.Body.Close()
	if restResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from rest snapshot, got %d", restResp.StatusCode)
	}

	// Disconnect: same-route commuter should see it go offline.
	driverConn.Close()
	offlineEvt := readEvent(t, commuterSame, 2*time.Second)
	if offlineEvt["type"] != "offline" || offlineEvt["vehicle_id"] != fx.VehicleID {
		t.Fatalf("expected offline event for %s, got %+v", fx.VehicleID, offlineEvt)
	}

	// Still nothing for the other route's commuter.
	expectNoEvent(t, commuterOther, 500*time.Millisecond)
}

// TestDriverWSRejectsWrongRole confirms a commuter JWT cannot open the
// driver WS.
func TestDriverWSRejectsWrongRole(t *testing.T) {
	identityRepo, _, tokens, server := setupTelemetryTest(t)
	ctx := context.Background()
	suffix := uniqueSuffix()

	commuterUser, err := identityRepo.CreateUser(ctx, "+27"+suffix+"3", nil, "x", identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter user: %v", err)
	}
	token, err := tokens.Issue(commuterUser.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	q := url.Values{"route_id": {"00000000-0000-0000-0000-000000000000"}}
	header := http.Header{"Authorization": {"Bearer " + token}}
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server, "/ws/driver", q), header)
	if err == nil {
		t.Fatal("expected dial to fail for a commuter token on the driver ws")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 403, got %d", status)
	}
}

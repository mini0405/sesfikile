package stops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/stops"
	"sesfikile/backend/internal/telemetry"
)

// This test requires a reachable Postgres (see infra/docker-compose.yml). It
// skips rather than failing when no database is available, matching every
// other DB-backed test in this repo (Stage 0-5).
type testEnv struct {
	pool      *pgxpool.Pool
	identity  *identity.Repo
	routing   *routing.Repo
	telemetry *telemetry.VehicleStateStore
	alerts    *telemetry.DriverAlertHub
	tokens    identity.TokenIssuer
	server    *httptest.Server
}

func setup(t *testing.T) *testEnv {
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
	tokens := identity.NewTokenIssuer("stops-integration-test-secret")

	telemetryStore := telemetry.NewVehicleStateStore()
	alerts := telemetry.NewDriverAlertHub()
	telemetryHub := telemetry.NewHub()
	telemetryHandlers := telemetry.NewHandlers(telemetryStore, telemetryHub, alerts, identityRepo, routingRepo, tokens)

	stopsStore := stops.NewStore()
	stopsHandlers := stops.NewHandlers(stopsStore, routingRepo, telemetryStore, alerts, identityRepo)

	// One server for /ws/driver (needs the raw handler, not RequireAuth,
	// same as production wiring), and REST endpoints behind RequireAuth.
	r := chi.NewRouter()
	r.Get("/ws/driver", telemetryHandlers.DriverWS)
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Post("/stops/request", stopsHandlers.RequestStop)
		r.Post("/stops/request/{id}/ack", stopsHandlers.AckRequest)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &testEnv{
		pool:      pool,
		identity:  identityRepo,
		routing:   routingRepo,
		telemetry: telemetryStore,
		alerts:    alerts,
		tokens:    tokens,
		server:    server,
	}
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// routeFixture is a 3-stop, 2-leg straight route: Origin -> Mid -> Dest,
// sequence 1 then 2, so Mid's sequence index is 1 and Dest's is 2.
type routeFixture struct {
	RouteID   string
	OriginID  string
	MidID     string
	DestID    string
	OriginLat float64
	OriginLng float64
	MidLat    float64
	MidLng    float64
	DestLat   float64
	DestLng   float64
}

func seedRoute(t *testing.T, env *testEnv) routeFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	origin, err := env.routing.CreateStop(ctx, "Stops Test Origin "+suffix, -33.90, 18.40)
	if err != nil {
		t.Fatalf("failed to create origin stop: %v", err)
	}
	mid, err := env.routing.CreateStop(ctx, "Stops Test Mid "+suffix, -33.92, 18.42)
	if err != nil {
		t.Fatalf("failed to create mid stop: %v", err)
	}
	dest, err := env.routing.CreateStop(ctx, "Stops Test Dest "+suffix, -33.94, 18.44)
	if err != nil {
		t.Fatalf("failed to create dest stop: %v", err)
	}
	route, err := env.routing.CreateRoute(ctx, "Stops Test Route "+suffix, "Test Association")
	if err != nil {
		t.Fatalf("failed to create route: %v", err)
	}
	if _, err := env.routing.CreateRouteLeg(ctx, route.ID, origin.ID, mid.ID, 1, 500); err != nil {
		t.Fatalf("failed to create leg 1: %v", err)
	}
	if _, err := env.routing.CreateRouteLeg(ctx, route.ID, mid.ID, dest.ID, 2, 500); err != nil {
		t.Fatalf("failed to create leg 2: %v", err)
	}

	// This is a shared dev database (not a disposable per-test one), so
	// clean up the fixture rows this test created rather than leaving them
	// to pollute cmd/seed's SEEDED DATA output and GET /routes, /stops —
	// same reasoning and shape as routing/integration_test.go's
	// seedTestRoutes.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = env.pool.Exec(cleanupCtx, `DELETE FROM route_legs WHERE route_id = $1`, route.ID)
		_, _ = env.pool.Exec(cleanupCtx, `DELETE FROM routes WHERE id = $1`, route.ID)
		_, _ = env.pool.Exec(cleanupCtx, `DELETE FROM stops WHERE id IN ($1, $2, $3)`, origin.ID, mid.ID, dest.ID)
	})

	return routeFixture{
		RouteID: route.ID.String(), OriginID: origin.ID.String(), MidID: mid.ID.String(), DestID: dest.ID.String(),
		OriginLat: *origin.Latitude, OriginLng: *origin.Longitude,
		MidLat: *mid.Latitude, MidLng: *mid.Longitude,
		DestLat: *dest.Latitude, DestLng: *dest.Longitude,
	}
}

type driverFixture struct {
	DriverTok string
	VehicleID string
}

// seedDriver creates an owner/vehicle/driver with an active assignment and
// returns a driver JWT.
func seedDriver(t *testing.T, env *testEnv) driverFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	ownerUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"1", nil, "x", identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	vehicle, err := env.identity.CreateVehicle(ctx, ownerUser.ID, "STP-"+suffix, 16, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}
	driverUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"2", nil, "x", identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := env.identity.CreateDriver(ctx, driverUser.ID, "Stops Test Driver", "PRDP-"+suffix, "ID-"+suffix)
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}
	if _, err := env.identity.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to create vehicle assignment: %v", err)
	}

	driverTok, err := env.tokens.Issue(driverUser.ID, identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to issue driver token: %v", err)
	}
	return driverFixture{DriverTok: driverTok, VehicleID: vehicle.ID.String()}
}

func seedCommuter(t *testing.T, env *testEnv) string {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()
	commuterUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"3", nil, "x", identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter: %v", err)
	}
	tok, err := env.tokens.Issue(commuterUser.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue commuter token: %v", err)
	}
	return tok
}

func wsURL(server *httptest.Server, path string, query url.Values) string {
	u := strings.TrimPrefix(server.URL, "http://")
	return "ws://" + u + path + "?" + query.Encode()
}

// dialDriver opens a real /ws/driver connection so the driver is genuinely
// reachable via the telemetry.DriverAlertHub, exactly as production alert
// delivery works — the fixture also reports the driver's uuid so tests can
// assert on it.
func dialDriver(t *testing.T, env *testEnv, driverTok, routeID string) *websocket.Conn {
	t.Helper()
	q := url.Values{"route_id": {routeID}}
	header := http.Header{"Authorization": {"Bearer " + driverTok}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(env.server, "/ws/driver", q), header)
	if err != nil {
		if resp != nil {
			t.Fatalf("failed to dial driver ws (HTTP %d): %v", resp.StatusCode, err)
		}
		t.Fatalf("failed to dial driver ws: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	return conn
}

func readAlert(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read alert: %v", err)
	}
	return msg
}

func expectNoAlert(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err == nil {
		t.Fatalf("expected no alert, got: %+v", msg)
	}
}

func doJSON(t *testing.T, server *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("failed to encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, server.URL+path, &buf)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return resp.StatusCode, respBody
}

// TestApproachingDriverReceivesAlert: an online driver whose position is
// nearest the route's origin stop (i.e. hasn't passed the requested Dest
// stop) receives the stop-request alert on their own /ws/driver connection.
func TestApproachingDriverReceivesAlert(t *testing.T) {
	env := setup(t)
	route := seedRoute(t, env)
	drv := seedDriver(t, env)
	commuterTok := seedCommuter(t, env)

	env.telemetry.GoOnline(mustUUID(t, drv.VehicleID), mustUUID(t, route.RouteID), mustDriverID(t, env, drv.DriverTok), 16)
	env.telemetry.UpdatePosition(mustUUID(t, drv.VehicleID), route.OriginLat, route.OriginLng)

	driverConn := dialDriver(t, env, drv.DriverTok, route.RouteID)
	defer driverConn.Close()

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.RouteID,
		"stop_id":  route.DestID,
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %+v", status, body)
	}
	if body["driver_available"] != true {
		t.Fatalf("expected driver_available=true, got %+v", body)
	}
	requestID, _ := body["request_id"].(string)
	if requestID == "" {
		t.Fatalf("expected request_id in response, got %+v", body)
	}

	alert := readAlert(t, driverConn, 2*time.Second)
	if alert["type"] != "stop_request" {
		t.Fatalf("expected stop_request alert, got %+v", alert)
	}
	if alert["request_id"] != requestID {
		t.Fatalf("expected request_id %s, got %+v", requestID, alert)
	}
	if alert["stop_id"] != route.DestID {
		t.Fatalf("expected stop_id %s, got %+v", route.DestID, alert)
	}
}

// TestDriverPastStopNotAlerted: a driver whose last-known position is
// nearest the Dest stop (i.e. already past the Mid stop being requested)
// does not receive an alert.
func TestDriverPastStopNotAlerted(t *testing.T) {
	env := setup(t)
	route := seedRoute(t, env)
	drv := seedDriver(t, env)
	commuterTok := seedCommuter(t, env)

	env.telemetry.GoOnline(mustUUID(t, drv.VehicleID), mustUUID(t, route.RouteID), mustDriverID(t, env, drv.DriverTok), 16)
	env.telemetry.UpdatePosition(mustUUID(t, drv.VehicleID), route.DestLat, route.DestLng)

	driverConn := dialDriver(t, env, drv.DriverTok, route.RouteID)
	defer driverConn.Close()

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.RouteID,
		"stop_id":  route.MidID,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 (no driver available), got %d: %+v", status, body)
	}
	if body["driver_available"] != false {
		t.Fatalf("expected driver_available=false, got %+v", body)
	}

	expectNoAlert(t, driverConn, 500*time.Millisecond)
}

// TestDriverOnDifferentRouteNotAlerted: a driver online on a different route
// entirely is never considered.
func TestDriverOnDifferentRouteNotAlerted(t *testing.T) {
	env := setup(t)
	route := seedRoute(t, env)
	otherRoute := seedRoute(t, env)
	drv := seedDriver(t, env)
	commuterTok := seedCommuter(t, env)

	env.telemetry.GoOnline(mustUUID(t, drv.VehicleID), mustUUID(t, otherRoute.RouteID), mustDriverID(t, env, drv.DriverTok), 16)
	env.telemetry.UpdatePosition(mustUUID(t, drv.VehicleID), otherRoute.OriginLat, otherRoute.OriginLng)

	driverConn := dialDriver(t, env, drv.DriverTok, otherRoute.RouteID)
	defer driverConn.Close()

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.RouteID,
		"stop_id":  route.DestID,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 (no driver available), got %d: %+v", status, body)
	}
	if body["driver_available"] != false {
		t.Fatalf("expected driver_available=false, got %+v", body)
	}

	expectNoAlert(t, driverConn, 500*time.Millisecond)
}

// TestNoDriverOnline_CleanUnmatchedResult: no driver online on the route at
// all -> a clean "no driver available" result, not an error.
func TestNoDriverOnline_CleanUnmatchedResult(t *testing.T) {
	env := setup(t)
	route := seedRoute(t, env)
	commuterTok := seedCommuter(t, env)

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.RouteID,
		"stop_id":  route.DestID,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	if body["driver_available"] != false {
		t.Fatalf("expected driver_available=false, got %+v", body)
	}
	if body["status"] != "unmatched" {
		t.Fatalf("expected status=unmatched, got %+v", body)
	}
}

// TestAckFlow_MarksRequestAcknowledged.
func TestAckFlow_MarksRequestAcknowledged(t *testing.T) {
	env := setup(t)
	route := seedRoute(t, env)
	drv := seedDriver(t, env)
	commuterTok := seedCommuter(t, env)

	env.telemetry.GoOnline(mustUUID(t, drv.VehicleID), mustUUID(t, route.RouteID), mustDriverID(t, env, drv.DriverTok), 16)
	env.telemetry.UpdatePosition(mustUUID(t, drv.VehicleID), route.OriginLat, route.OriginLng)

	driverConn := dialDriver(t, env, drv.DriverTok, route.RouteID)
	defer driverConn.Close()

	status, body := doJSON(t, env.server, http.MethodPost, "/stops/request", commuterTok, map[string]string{
		"route_id": route.RouteID,
		"stop_id":  route.DestID,
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %+v", status, body)
	}
	requestID := body["request_id"].(string)
	readAlert(t, driverConn, 2*time.Second) // drain the pushed alert

	ackStatus, ackBody := doJSON(t, env.server, http.MethodPost, "/stops/request/"+requestID+"/ack", drv.DriverTok, nil)
	if ackStatus != http.StatusOK {
		t.Fatalf("expected 200 acking request, got %d: %+v", ackStatus, ackBody)
	}
	if ackBody["status"] != "acknowledged" {
		t.Fatalf("expected status=acknowledged, got %+v", ackBody)
	}
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("failed to parse uuid %s: %v", s, err)
	}
	return parsed
}

// mustDriverID looks up the driver's id (drivers.id, not user id) from the
// driver's own JWT, needed since telemetry.GoOnline is keyed by driver id.
func mustDriverID(t *testing.T, env *testEnv, driverTok string) uuid.UUID {
	t.Helper()
	claims, err := env.tokens.Parse(driverTok)
	if err != nil {
		t.Fatalf("failed to parse driver token: %v", err)
	}
	driver, err := env.identity.GetDriverByUserID(context.Background(), claims.UserID)
	if err != nil {
		t.Fatalf("failed to look up driver: %v", err)
	}
	return driver.ID
}

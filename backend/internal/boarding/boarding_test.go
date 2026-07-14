package boarding_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/boarding"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

// This test requires a reachable Postgres (see infra/docker-compose.yml). It
// skips rather than failing when no database is available, matching every
// other DB-backed test in this repo (Stage 0-4).
type testEnv struct {
	pool      *pgxpool.Pool
	identity  *identity.Repo
	routing   *routing.Repo
	wallet    *wallet.Repo
	telemetry *telemetry.VehicleStateStore
	hub       *telemetry.Hub
	tokens    identity.TokenIssuer
	server    *httptest.Server
	fareSplit config.FareSplit
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
	walletRepo := wallet.NewRepo(pool)
	if err := walletRepo.EnsureSystemAccounts(context.Background()); err != nil {
		t.Fatalf("failed to ensure system accounts: %v", err)
	}
	tokens := identity.NewTokenIssuer("boarding-integration-test-secret")
	store := telemetry.NewVehicleStateStore()
	hub := telemetry.NewHub()
	fareSplit := config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65}

	signer := boarding.NewSigner("boarding-integration-test-hmac-secret")
	handlers := boarding.NewHandlers(routingRepo, walletRepo, identityRepo, store, hub, signer, 3*time.Minute, fareSplit)

	r := chi.NewRouter()
	r.Post("/boarding/pass", withClaims(tokens, handlers.IssuePass))
	r.Post("/boarding/scan", withClaims(tokens, handlers.ScanPass))

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &testEnv{
		pool:      pool,
		identity:  identityRepo,
		routing:   routingRepo,
		wallet:    walletRepo,
		telemetry: store,
		hub:       hub,
		tokens:    tokens,
		server:    server,
		fareSplit: fareSplit,
	}
}

// withClaims wraps a boarding handler with identity.RequireAuth so tests can
// drive the real HTTP handlers with a bearer token, same as production —
// the router.go wiring puts every /boarding/* route behind RequireAuth +
// RequireRole, but RequireRole isn't needed here since each handler is only
// ever called with the right role of token in these tests.
func withClaims(tokens identity.TokenIssuer, next http.HandlerFunc) http.HandlerFunc {
	handler := identity.RequireAuth(tokens)(next)
	return handler.ServeHTTP
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

type fixture struct {
	CommuterID  string
	CommuterTok string
	DriverTok   string
	VehicleID   string
	RouteID     string
	FromStopID  string
	ToStopID    string
	FareCents   int64
}

// seedFixture creates a route with one leg, an owner+vehicle+driver with an
// active assignment, and a commuter with the given starting balance. The
// driver's vehicle is marked online on the route in the telemetry store (as
// /ws/driver would do on a real connection), so ScanPass's "driver online +
// assigned + on this pass's route" check passes without needing a real
// WebSocket connection in these tests.
func seedFixture(t *testing.T, env *testEnv, commuterBalanceCents, fareCents int64) fixture {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	fromStop, err := env.routing.CreateStop(ctx, "Boarding Test Origin "+suffix, -33.9, 18.4)
	if err != nil {
		t.Fatalf("failed to create origin stop: %v", err)
	}
	toStop, err := env.routing.CreateStop(ctx, "Boarding Test Dest "+suffix, -33.95, 18.45)
	if err != nil {
		t.Fatalf("failed to create dest stop: %v", err)
	}
	route, err := env.routing.CreateRoute(ctx, "Boarding Test Route "+suffix, "Test Association")
	if err != nil {
		t.Fatalf("failed to create route: %v", err)
	}
	if _, err := env.routing.CreateRouteLeg(ctx, route.ID, fromStop.ID, toStop.ID, 1, fareCents); err != nil {
		t.Fatalf("failed to create route leg: %v", err)
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
		_, _ = env.pool.Exec(cleanupCtx, `DELETE FROM stops WHERE id IN ($1, $2)`, fromStop.ID, toStop.ID)
	})

	ownerUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"1", nil, "x", identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	vehicle, err := env.identity.CreateVehicle(ctx, ownerUser.ID, "BRD-"+suffix, 16, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}
	driverUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"2", nil, "x", identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := env.identity.CreateDriver(ctx, driverUser.ID, "Boarding Test Driver", "PRDP-"+suffix, "ID-"+suffix)
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}
	if _, err := env.identity.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to create vehicle assignment: %v", err)
	}

	commuterUser, err := env.identity.CreateUser(ctx, "+27"+suffix+"3", nil, "x", identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter: %v", err)
	}
	if commuterBalanceCents > 0 {
		if _, _, err := env.wallet.Topup(ctx, commuterUser.ID, commuterBalanceCents); err != nil {
			t.Fatalf("failed to top up commuter: %v", err)
		}
	}

	commuterTok, err := env.tokens.Issue(commuterUser.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue commuter token: %v", err)
	}
	driverTok, err := env.tokens.Issue(driverUser.ID, identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to issue driver token: %v", err)
	}

	// Mark the driver's vehicle online on this route, as /ws/driver would.
	env.telemetry.GoOnline(vehicle.ID, route.ID, driver.ID, 16)

	return fixture{
		CommuterID:  commuterUser.ID.String(),
		CommuterTok: commuterTok,
		DriverTok:   driverTok,
		VehicleID:   vehicle.ID.String(),
		RouteID:     route.ID.String(),
		FromStopID:  fromStop.ID.String(),
		ToStopID:    toStop.ID.String(),
		FareCents:   fareCents,
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

func issuePass(t *testing.T, env *testEnv, fx fixture) (token string, fareCents int64) {
	t.Helper()
	status, body := doJSON(t, env.server, http.MethodPost, "/boarding/pass", fx.CommuterTok, map[string]string{
		"route_id":     fx.RouteID,
		"from_stop_id": fx.FromStopID,
		"to_stop_id":   fx.ToStopID,
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201 issuing pass, got %d: %+v", status, body)
	}
	tok, _ := body["pass_token"].(string)
	if tok == "" {
		t.Fatalf("expected pass_token in response, got %+v", body)
	}
	return tok, int64(body["fare_cents"].(float64))
}

func scanPass(t *testing.T, env *testEnv, driverTok, passToken string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, env.server, http.MethodPost, "/boarding/scan", driverTok, map[string]string{
		"pass_token": passToken,
	})
}

func (env *testEnv) commuterBalance(t *testing.T, commuterID string) int64 {
	t.Helper()
	u, err := parseUUID(commuterID)
	if err != nil {
		t.Fatalf("failed to parse commuter id: %v", err)
	}
	acc, err := env.wallet.GetOrCreateAccount(context.Background(), env.pool, &u, wallet.AccountCommuterWallet)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	bal, err := env.wallet.AccountBalance(context.Background(), env.pool, acc.ID)
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	return bal
}

func (env *testEnv) vehicleSeats(vehicleID string) (int, bool) {
	u, err := parseUUID(vehicleID)
	if err != nil {
		return 0, false
	}
	state, ok := env.telemetry.Get(u)
	if !ok {
		return 0, false
	}
	return state.SeatsAvailable, true
}

func TestHappyPath_IssueScanChargeSeatDecrement(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)

	passToken, fareCents := issuePass(t, env, fx)
	if fareCents != 1500 {
		t.Fatalf("expected fare_cents 1500, got %d", fareCents)
	}

	seatsBefore, ok := env.vehicleSeats(fx.VehicleID)
	if !ok {
		t.Fatalf("expected vehicle to be tracked")
	}

	status, body := scanPass(t, env, fx.DriverTok, passToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 on fresh scan, got %d: %+v", status, body)
	}
	if body["replayed"] != false {
		t.Fatalf("expected replayed=false on fresh scan, got %+v", body)
	}
	if int64(body["fare_cents"].(float64)) != 1500 {
		t.Fatalf("expected fare_cents 1500, got %+v", body)
	}
	platform := int64(body["platform_cents"].(float64))
	driverCents := int64(body["driver_cents"].(float64))
	owner := int64(body["owner_cents"].(float64))
	if platform+driverCents+owner != 1500 {
		t.Fatalf("split %d+%d+%d does not sum to fare 1500", platform, driverCents, owner)
	}
	seatsRemaining := int(body["seats_remaining"].(float64))
	if seatsRemaining != seatsBefore-1 {
		t.Fatalf("expected seats_remaining %d, got %d", seatsBefore-1, seatsRemaining)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000-1500 {
		t.Fatalf("expected commuter balance %d, got %d", 10000-1500, gotBalance)
	}

	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsBefore-1 {
		t.Fatalf("expected store seats %d, got %d", seatsBefore-1, gotSeats)
	}
}

func TestTamperedPass_Rejected(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)

	passToken, _ := issuePass(t, env, fx)
	// Flip a byte in the middle of the signature segment — not the very
	// last character, since a base64url encoding's final character can
	// carry unused padding bits that don't affect the decoded byte value.
	tampered := []byte(passToken)
	mid := len(tampered) - 4
	tampered[mid] ^= 0x01

	seatsBefore, _ := env.vehicleSeats(fx.VehicleID)

	status, body := scanPass(t, env, fx.DriverTok, string(tampered))
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered pass, got %d: %+v", status, body)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000 {
		t.Fatalf("expected no charge, balance still 10000, got %d", gotBalance)
	}
	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsBefore {
		t.Fatalf("expected no seat change, got %d want %d", gotSeats, seatsBefore)
	}
}

func TestExpiredPass_Rejected(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)

	// Issue a pass through a handler wired with a near-zero TTL so it's
	// already expired by the time we scan it, without sleeping in the test.
	shortSigner := boarding.NewSigner("boarding-integration-test-hmac-secret")
	shortHandlers := boarding.NewHandlers(env.routing, env.wallet, env.identity, env.telemetry, env.hub, shortSigner, 1*time.Nanosecond, env.fareSplit)
	shortRouter := chi.NewRouter()
	shortRouter.Post("/boarding/pass", withClaims(env.tokens, shortHandlers.IssuePass))
	shortServer := httptest.NewServer(shortRouter)
	defer shortServer.Close()

	status, body := doJSON(t, shortServer, http.MethodPost, "/boarding/pass", fx.CommuterTok, map[string]string{
		"route_id":     fx.RouteID,
		"from_stop_id": fx.FromStopID,
		"to_stop_id":   fx.ToStopID,
	})
	if status != http.StatusCreated {
		t.Fatalf("expected 201 issuing short-TTL pass, got %d: %+v", status, body)
	}
	passToken := body["pass_token"].(string)

	time.Sleep(10 * time.Millisecond) // guarantee we're past the 1ns TTL

	seatsBefore, _ := env.vehicleSeats(fx.VehicleID)

	scanStatus, scanBody := scanPass(t, env, fx.DriverTok, passToken)
	if scanStatus != http.StatusGone {
		t.Fatalf("expected 410 for expired pass, got %d: %+v", scanStatus, scanBody)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000 {
		t.Fatalf("expected no charge, balance still 10000, got %d", gotBalance)
	}
	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsBefore {
		t.Fatalf("expected no seat change, got %d want %d", gotSeats, seatsBefore)
	}
}

func TestDoubleScan_IdempotentReplay(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)
	passToken, _ := issuePass(t, env, fx)

	status1, body1 := scanPass(t, env, fx.DriverTok, passToken)
	if status1 != http.StatusCreated || body1["replayed"] != false {
		t.Fatalf("expected fresh charge on first scan, got %d: %+v", status1, body1)
	}
	txn1 := body1["transaction_id"]
	seatsAfterFirst, _ := env.vehicleSeats(fx.VehicleID)

	status2, body2 := scanPass(t, env, fx.DriverTok, passToken)
	if status2 != http.StatusOK {
		t.Fatalf("expected 200 on replayed scan, got %d: %+v", status2, body2)
	}
	if body2["replayed"] != true {
		t.Fatalf("expected replayed=true on second scan, got %+v", body2)
	}
	if body2["transaction_id"] != txn1 {
		t.Fatalf("expected same transaction id, got %v and %v", txn1, body2["transaction_id"])
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000-1500 {
		t.Fatalf("expected wallet debited exactly once (%d), got %d", 10000-1500, gotBalance)
	}

	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsAfterFirst {
		t.Fatalf("expected seats unchanged by replay (%d), got %d", seatsAfterFirst, gotSeats)
	}
	seatsRemaining2 := int(body2["seats_remaining"].(float64))
	if seatsRemaining2 != seatsAfterFirst {
		t.Fatalf("expected replay's reported seats_remaining %d, got %d", seatsAfterFirst, seatsRemaining2)
	}
}

func TestInsufficientFunds_Rejected(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 500, 1500) // balance < fare
	passToken, _ := issuePass(t, env, fx)

	seatsBefore, _ := env.vehicleSeats(fx.VehicleID)

	status, body := scanPass(t, env, fx.DriverTok, passToken)
	if status != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for insufficient funds, got %d: %+v", status, body)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 500 {
		t.Fatalf("expected wallet unchanged at 500, got %d", gotBalance)
	}
	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsBefore {
		t.Fatalf("expected no seat change, got %d want %d", gotSeats, seatsBefore)
	}
}

func TestWrongDriverRoute_Rejected(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)
	passToken, _ := issuePass(t, env, fx)

	// A second driver, online but on a different route entirely.
	otherFx := seedFixture(t, env, 10000, 900)

	seatsBefore, _ := env.vehicleSeats(fx.VehicleID)

	status, body := scanPass(t, env, otherFx.DriverTok, passToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for wrong driver/route, got %d: %+v", status, body)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000 {
		t.Fatalf("expected no charge, balance still 10000, got %d", gotBalance)
	}
	gotSeats, _ := env.vehicleSeats(fx.VehicleID)
	if gotSeats != seatsBefore {
		t.Fatalf("expected no seat change, got %d want %d", gotSeats, seatsBefore)
	}
}

func TestDriverOffline_Rejected(t *testing.T) {
	env := setup(t)
	fx := seedFixture(t, env, 10000, 1500)
	passToken, _ := issuePass(t, env, fx)

	// Take the driver's vehicle offline (as a closed /ws/driver connection
	// would) before scanning.
	vehicleUUID, err := parseUUID(fx.VehicleID)
	if err != nil {
		t.Fatalf("failed to parse vehicle id: %v", err)
	}
	env.telemetry.GoOffline(vehicleUUID)

	status, body := scanPass(t, env, fx.DriverTok, passToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for offline driver, got %d: %+v", status, body)
	}

	gotBalance := env.commuterBalance(t, fx.CommuterID)
	if gotBalance != 10000 {
		t.Fatalf("expected no charge, balance still 10000, got %d", gotBalance)
	}
}

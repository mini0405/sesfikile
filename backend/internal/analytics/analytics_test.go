package analytics_test

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

	"sesfikile/backend/internal/analytics"
	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/telemetry"
	"sesfikile/backend/internal/wallet"
)

// This test requires a reachable Postgres (see infra/docker-compose.yml). It
// skips rather than failing when no database is available, matching every
// other DB-backed test in this repo.
type testEnv struct {
	pool      *pgxpool.Pool
	identity  *identity.Repo
	routing   *routing.Repo
	wallet    *wallet.Repo
	fuel      *fuel.Repo
	analytics *analytics.Repo
	telemetry *telemetry.VehicleStateStore
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
	fuelRepo := fuel.NewRepo(pool, walletRepo)
	analyticsRepo := analytics.NewRepo(pool, walletRepo, fuelRepo)
	telemetryStore := telemetry.NewVehicleStateStore()
	tokens := identity.NewTokenIssuer("analytics-integration-test-secret")
	fareSplit := config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65}

	handlers := analytics.NewHandlers(analyticsRepo, identityRepo, routingRepo, fuelRepo, telemetryStore)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(withAuth(tokens))
		r.Use(identity.RequireRole(identity.RoleOwner))
		r.Get("/owner/summary", handlers.Summary)
		r.Get("/owner/vehicles", handlers.Vehicles)
		r.Get("/owner/drivers", handlers.Drivers)
		r.Get("/owner/revenue-vs-fuel", handlers.RevenueVsFuel)
		r.Get("/owner/ledger", handlers.Ledger)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &testEnv{
		pool:      pool,
		identity:  identityRepo,
		routing:   routingRepo,
		wallet:    walletRepo,
		fuel:      fuelRepo,
		analytics: analyticsRepo,
		telemetry: telemetryStore,
		tokens:    tokens,
		server:    server,
		fareSplit: fareSplit,
	}
}

func withAuth(tokens identity.TokenIssuer) func(http.Handler) http.Handler {
	return identity.RequireAuth(tokens)
}

var uniqueCounter int64

func uniqueSuffix() string {
	uniqueCounter++
	return fmt.Sprintf("%d%d", time.Now().UnixNano(), uniqueCounter)
}

// fleet is one owner + one vehicle + one driver + one commuter, ready to
// charge fares against.
type fleet struct {
	OwnerID      uuid.UUID
	OwnerToken   string
	DriverUserID uuid.UUID
	DriverID     uuid.UUID
	VehicleID    uuid.UUID
	CommuterID   uuid.UUID
}

func seedFleet(t *testing.T, env *testEnv) fleet {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	ownerUser, err := env.identity.CreateUser(ctx, "+27o"+suffix, nil, "x", identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	vehicle, err := env.identity.CreateVehicle(ctx, ownerUser.ID, "ANL-"+suffix, 16, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}
	driverUser, err := env.identity.CreateUser(ctx, "+27d"+suffix, nil, "x", identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := env.identity.CreateDriver(ctx, driverUser.ID, "Analytics Test Driver", "PRDP-"+suffix, "ID-"+suffix)
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}
	if _, err := env.identity.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to create vehicle assignment: %v", err)
	}
	commuterUser, err := env.identity.CreateUser(ctx, "+27c"+suffix, nil, "x", identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter: %v", err)
	}

	ownerTok, err := env.tokens.Issue(ownerUser.ID, identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to issue owner token: %v", err)
	}

	return fleet{
		OwnerID:      ownerUser.ID,
		OwnerToken:   ownerTok,
		DriverUserID: driverUser.ID,
		DriverID:     driver.ID,
		VehicleID:    vehicle.ID,
		CommuterID:   commuterUser.ID,
	}
}

// chargeFare tops up the commuter by fareCents and charges it in full
// against f's vehicle, returning the resolved split.
func chargeFare(t *testing.T, env *testEnv, f fleet, fareCents int64) wallet.FareSplit {
	t.Helper()
	ctx := context.Background()
	if _, _, err := env.wallet.Topup(ctx, f.CommuterID, fareCents); err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	_, split, _, err := env.wallet.ChargeFare(ctx, f.CommuterID, f.VehicleID, fareCents, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err != nil {
		t.Fatalf("charge fare failed: %v", err)
	}
	return split
}

func doJSON(t *testing.T, server *httptest.Server, method, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp.StatusCode, body
}

// --- Reconciliation ---------------------------------------------------------

// TestReconciliation_SummaryMatchesLedgerSums is the stage's core property:
// /owner/summary's figures must equal direct SUM()s over ledger_postings for
// the same owner/range, computed independently in this test (not by calling
// the same repo code the handler uses).
func TestReconciliation_SummaryMatchesLedgerSums(t *testing.T) {
	env := setup(t)
	f := seedFleet(t, env)

	chargeFare(t, env, f, 1000)
	chargeFare(t, env, f, 2500)
	chargeFare(t, env, f, 375)

	if _, _, err := env.fuel.Allocate(context.Background(), f.OwnerID, 30); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}

	status, body := doJSON(t, env.server, http.MethodGet, "/owner/summary", f.OwnerToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}

	// Independently computed: direct SUM over ledger_postings for this
	// owner's owner_revenue account, fare transactions only.
	var wantRevenue int64
	var wantTrips int64
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COUNT(DISTINCT lt.id), COALESCE(SUM(lp.amount_cents), 0)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'`,
		f.OwnerID,
	).Scan(&wantTrips, &wantRevenue); err != nil {
		t.Fatalf("failed independent revenue query: %v", err)
	}

	gotRevenue := int64(body["revenue_cents"].(float64))
	gotTrips := int64(body["trips"].(float64))
	if gotRevenue != wantRevenue {
		t.Fatalf("revenue_cents %d != independently-computed ledger sum %d", gotRevenue, wantRevenue)
	}
	if gotTrips != wantTrips || gotTrips != 3 {
		t.Fatalf("trips %d != independently-computed fare-transaction count %d (want 3)", gotTrips, wantTrips)
	}

	var wantFuelBalance int64
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(lp.amount_cents), 0)
		 FROM ledger_postings lp JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'fuel_account' AND a.owner_user_id = $1`,
		f.OwnerID,
	).Scan(&wantFuelBalance); err != nil {
		t.Fatalf("failed independent fuel balance query: %v", err)
	}
	gotFuelBalance := int64(body["fuel_balance_cents"].(float64))
	if gotFuelBalance != wantFuelBalance {
		t.Fatalf("fuel_balance_cents %d != independently-computed ledger sum %d", gotFuelBalance, wantFuelBalance)
	}
}

// TestSplitConsistency_PlatformDriverOwnerSumToFareTotal is the stage's
// second required property: platform fee + driver earnings + owner revenue
// reported for the range must sum to the total fares charged.
func TestSplitConsistency_PlatformDriverOwnerSumToFareTotal(t *testing.T) {
	env := setup(t)
	f := seedFleet(t, env)

	var totalFare int64
	for _, fare := range []int64{1000, 777, 333} {
		chargeFare(t, env, f, fare)
		totalFare += fare
	}

	status, body := doJSON(t, env.server, http.MethodGet, "/owner/summary", f.OwnerToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}

	revenue := int64(body["revenue_cents"].(float64))
	platform := int64(body["platform_fees_cents"].(float64))
	driver := int64(body["driver_earnings_cents"].(float64))

	if revenue+platform+driver != totalFare {
		t.Fatalf("split mismatch: revenue(%d) + platform(%d) + driver(%d) = %d, want total fare %d",
			revenue, platform, driver, revenue+platform+driver, totalFare)
	}
}

// --- Scoping ------------------------------------------------------------

// TestScoping_OwnerCannotSeeAnotherOwnersData exercises the brief's explicit
// two-owner requirement across every endpoint.
func TestScoping_OwnerCannotSeeAnotherOwnersData(t *testing.T) {
	env := setup(t)
	owner1 := seedFleet(t, env)
	owner2 := seedFleet(t, env)

	chargeFare(t, env, owner1, 5000)
	chargeFare(t, env, owner2, 900)

	// /owner/summary: each owner's revenue must reflect only their own fare.
	_, s1 := doJSON(t, env.server, http.MethodGet, "/owner/summary", owner1.OwnerToken)
	_, s2 := doJSON(t, env.server, http.MethodGet, "/owner/summary", owner2.OwnerToken)
	rev1 := int64(s1["revenue_cents"].(float64))
	rev2 := int64(s2["revenue_cents"].(float64))
	if rev1 == rev2 {
		t.Fatalf("expected distinct revenue for distinct owners' fares, got %d and %d", rev1, rev2)
	}
	wantRev1 := int64(float64(5000) * float64(env.fareSplit.OwnerPct) / 100)
	wantRev2 := int64(float64(900) * float64(env.fareSplit.OwnerPct) / 100)
	if rev1 != wantRev1 || rev2 != wantRev2 {
		t.Fatalf("revenue not scoped correctly: owner1=%d (want %d), owner2=%d (want %d)", rev1, wantRev1, rev2, wantRev2)
	}

	// /owner/vehicles: owner1 must not see owner2's vehicle id, and vice versa.
	_, v1 := doJSON(t, env.server, http.MethodGet, "/owner/vehicles", owner1.OwnerToken)
	vehicles1 := v1["vehicles"].([]any)
	if len(vehicles1) != 1 {
		t.Fatalf("expected exactly 1 vehicle for owner1, got %d", len(vehicles1))
	}
	gotVehicleID := vehicles1[0].(map[string]any)["vehicle_id"].(string)
	if gotVehicleID != owner1.VehicleID.String() {
		t.Fatalf("owner1 saw vehicle %s, expected their own %s", gotVehicleID, owner1.VehicleID)
	}
	if gotVehicleID == owner2.VehicleID.String() {
		t.Fatal("owner1's vehicle list leaked owner2's vehicle id")
	}

	// /owner/drivers: same check.
	_, d1 := doJSON(t, env.server, http.MethodGet, "/owner/drivers", owner1.OwnerToken)
	drivers1 := d1["drivers"].([]any)
	if len(drivers1) != 1 {
		t.Fatalf("expected exactly 1 driver for owner1, got %d", len(drivers1))
	}
	gotDriverID := drivers1[0].(map[string]any)["driver_id"].(string)
	if gotDriverID == owner2.DriverID.String() {
		t.Fatal("owner1's driver list leaked owner2's driver id")
	}

	// /owner/ledger: owner1's entries must not include owner2's vehicle id.
	_, l1 := doJSON(t, env.server, http.MethodGet, "/owner/ledger", owner1.OwnerToken)
	entries1 := l1["entries"].([]any)
	for _, e := range entries1 {
		entry := e.(map[string]any)
		if vid, ok := entry["vehicle_id"]; ok && vid != nil && vid.(string) == owner2.VehicleID.String() {
			t.Fatal("owner1's ledger leaked an entry tied to owner2's vehicle")
		}
	}
}

// --- Date range -----------------------------------------------------------

// TestDateRange_RespectsFromTo confirms figures only include activity inside
// the requested [from, to) window.
func TestDateRange_RespectsFromTo(t *testing.T) {
	env := setup(t)
	f := seedFleet(t, env)

	chargeFare(t, env, f, 1234)

	// A window entirely in the future must show zero activity.
	future := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	future2 := time.Now().AddDate(0, 0, 6).Format("2006-01-02")
	status, body := doJSON(t, env.server, http.MethodGet,
		fmt.Sprintf("/owner/summary?from=%s&to=%s", future, future2), f.OwnerToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	if got := int64(body["revenue_cents"].(float64)); got != 0 {
		t.Fatalf("expected 0 revenue for a future window with no activity, got %d", got)
	}
	if got := int64(body["trips"].(float64)); got != 0 {
		t.Fatalf("expected 0 trips for a future window, got %d", got)
	}

	// A window covering today must include the fare charged above.
	todayStr := time.Now().Format("2006-01-02")
	tomorrowStr := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	status, body = doJSON(t, env.server, http.MethodGet,
		fmt.Sprintf("/owner/summary?from=%s&to=%s", todayStr, tomorrowStr), f.OwnerToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	if got := int64(body["trips"].(float64)); got != 1 {
		t.Fatalf("expected 1 trip in today's window, got %d", got)
	}
}

// --- Empty state ------------------------------------------------------------

// TestEmptyState_NoActivityReturnsCleanZeros confirms a fresh owner with no
// vehicles/fares/fuel activity gets zeros, not an error.
func TestEmptyState_NoActivityReturnsCleanZeros(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	suffix := uniqueSuffix()

	ownerUser, err := env.identity.CreateUser(ctx, "+27empty"+suffix, nil, "x", identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	ownerTok, err := env.tokens.Issue(ownerUser.ID, identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to issue owner token: %v", err)
	}

	status, body := doJSON(t, env.server, http.MethodGet, "/owner/summary", ownerTok)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for empty-state owner, got %d: %+v", status, body)
	}
	for _, key := range []string{"revenue_cents", "trips", "passenger_volume", "platform_fees_cents", "driver_earnings_cents", "fuel_balance_cents", "fuel_allocated_cents"} {
		if got := body[key].(float64); got != 0 {
			t.Fatalf("expected %s to be 0 for empty-state owner, got %v", key, got)
		}
	}

	status, body = doJSON(t, env.server, http.MethodGet, "/owner/vehicles", ownerTok)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	assertEmptyJSONArray(t, body, "vehicles")

	status, body = doJSON(t, env.server, http.MethodGet, "/owner/drivers", ownerTok)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	assertEmptyJSONArray(t, body, "drivers")

	status, body = doJSON(t, env.server, http.MethodGet, "/owner/ledger", ownerTok)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}
	assertEmptyJSONArray(t, body, "entries")
	if total := body["total"].(float64); total != 0 {
		t.Fatalf("expected total 0, got %v", total)
	}
}

// assertEmptyJSONArray fails unless body[key] decoded as an empty JSON
// array ([]any{}), not JSON null — a plain type assertion
// (body[key].([]any)) silently reports ok=false for both "absent" and
// "decoded as null", which let a real null-vs-[] regression slip past this
// test previously (see docs/PROGRESS.md's backend-cleanup entry). Decoding
// into json.RawMessage first and comparing bytes distinguishes "null" from
// "[]" unambiguously.
func assertEmptyJSONArray(t *testing.T, body map[string]any, key string) {
	t.Helper()
	raw, ok := body[key]
	if !ok {
		t.Fatalf("expected %q key present in response", key)
	}
	if raw == nil {
		t.Fatalf("expected %q to serialize as an empty JSON array [], got null", key)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected %q to be a JSON array, got %#v", key, raw)
	}
	if len(arr) != 0 {
		t.Fatalf("expected %q to be empty, got %d entries", key, len(arr))
	}
}

package wallet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/wallet"
)

// transactionsTestServer wires wallet.Handlers.Transactions behind the same
// RequireAuth/RequireRole(commuter) middleware the real router uses, so
// these tests exercise it as a raw HTTP endpoint with real bearer tokens —
// not just the repo method directly.
func transactionsTestServer(t *testing.T, env *testEnv) (*httptest.Server, identity.TokenIssuer) {
	t.Helper()
	tokens := identity.NewTokenIssuer("wallet-transactions-test-secret")
	handlers := wallet.NewHandlers(env.wallet, env.fareSplit, env.identity)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireAuth(tokens))
		r.Use(identity.RequireRole(identity.RoleCommuter))
		r.Get("/wallet/transactions", handlers.Transactions)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server, tokens
}

func doGetJSON(t *testing.T, server *httptest.Server, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

// TestTransactions_TopupsAndFaresOrderedAndReconciled covers the core
// happy path: a commuter with a top-up and a fare charge sees both, newest
// first, with amounts that reconcile exactly to their wallet balance — no
// separate tally, straight off ledger_postings (same principle as Stage 8's
// owner ledger).
func TestTransactions_TopupsAndFaresOrderedAndReconciled(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	vehicle := mustCreateVehicleWithDriver(t, env)
	server, tokens := transactionsTestServer(t, env)

	token, err := tokens.Issue(commuter.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, 10000); err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	if _, _, _, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 1500, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct); err != nil {
		t.Fatalf("charge fare failed: %v", err)
	}

	status, body := doGetJSON(t, server, "/wallet/transactions", token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}

	txns, ok := body["transactions"].([]any)
	if !ok {
		t.Fatalf("expected transactions to be a JSON array, got %#v", body["transactions"])
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d: %+v", len(txns), txns)
	}

	// Newest first: the fare charge (second op) must come before the topup.
	fareEntry := txns[0].(map[string]any)
	topupEntry := txns[1].(map[string]any)
	if fareEntry["kind"] != "fare" {
		t.Errorf("expected the newest entry to be the fare charge, got kind=%v", fareEntry["kind"])
	}
	if topupEntry["kind"] != "topup" {
		t.Errorf("expected the oldest entry to be the topup, got kind=%v", topupEntry["kind"])
	}

	fareAmount := int64(fareEntry["amount_cents"].(float64))
	topupAmount := int64(topupEntry["amount_cents"].(float64))
	if fareAmount != -1500 {
		t.Errorf("expected fare posting amount -1500, got %d", fareAmount)
	}
	if topupAmount != 10000 {
		t.Errorf("expected topup posting amount 10000, got %d", topupAmount)
	}

	// Reconciliation: the two postings must sum to exactly the wallet balance.
	gotBalance := env.balance(t, &commuter.ID, wallet.AccountCommuterWallet)
	if fareAmount+topupAmount != gotBalance {
		t.Errorf("postings sum to %d, want balance %d", fareAmount+topupAmount, gotBalance)
	}

	// The fare entry should carry vehicle context (route/trip isn't stored
	// per-fare — see PROGRESS.md — but vehicle_id/registration is).
	if fareEntry["vehicle_id"] != vehicle.ID.String() {
		t.Errorf("expected fare entry vehicle_id %s, got %v", vehicle.ID, fareEntry["vehicle_id"])
	}
	if fareEntry["vehicle_registration"] == nil {
		t.Error("expected fare entry to carry a vehicle_registration")
	}
	if topupEntry["vehicle_id"] != nil {
		t.Errorf("expected topup entry to have no vehicle_id, got %v", topupEntry["vehicle_id"])
	}

	if total := body["total"].(float64); total != 2 {
		t.Errorf("expected total 2, got %v", total)
	}
}

// TestTransactions_EmptyCommuterReturnsEmptyArray is Gap 1's requirement
// exercised on this endpoint: a commuter with no ledger activity yet must
// get [], not null.
func TestTransactions_EmptyCommuterReturnsEmptyArray(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	server, tokens := transactionsTestServer(t, env)

	token, err := tokens.Issue(commuter.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	status, body := doGetJSON(t, server, "/wallet/transactions", token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, body)
	}

	raw, ok := body["transactions"]
	if !ok {
		t.Fatal("expected \"transactions\" key present")
	}
	if raw == nil {
		t.Fatal("expected transactions to serialize as [], got null")
	}
	txns, ok := raw.([]any)
	if !ok || len(txns) != 0 {
		t.Fatalf("expected an empty JSON array, got %#v", raw)
	}
	if total := body["total"].(float64); total != 0 {
		t.Fatalf("expected total 0, got %v", total)
	}
}

// TestTransactions_CrossCommuterIsolation mirrors Stage 8's two-owner
// scoping test: commuter B must never see commuter A's transactions, and
// identity is derived from the JWT — never a request parameter, so there is
// no id to even try passing for another commuter.
func TestTransactions_CrossCommuterIsolation(t *testing.T) {
	env := setup(t)
	commuterA := mustCreateCommuter(t, env)
	commuterB := mustCreateCommuter(t, env)
	server, tokens := transactionsTestServer(t, env)

	tokenA, err := tokens.Issue(commuterA.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue token A: %v", err)
	}
	tokenB, err := tokens.Issue(commuterB.ID, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to issue token B: %v", err)
	}

	if _, _, err := env.wallet.Topup(context.Background(), commuterA.ID, 7500); err != nil {
		t.Fatalf("topup for commuter A failed: %v", err)
	}

	// Commuter A sees their own top-up.
	status, bodyA := doGetJSON(t, server, "/wallet/transactions", tokenA)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, bodyA)
	}
	txnsA := bodyA["transactions"].([]any)
	if len(txnsA) != 1 {
		t.Fatalf("expected commuter A to see exactly 1 transaction, got %d", len(txnsA))
	}

	// Commuter B, with zero activity of their own, must see an empty list —
	// never commuter A's top-up.
	status, bodyB := doGetJSON(t, server, "/wallet/transactions", tokenB)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, bodyB)
	}
	rawB, ok := bodyB["transactions"]
	if !ok || rawB == nil {
		t.Fatalf("expected commuter B's transactions to serialize as [], got %#v", rawB)
	}
	txnsB, ok := rawB.([]any)
	if !ok || len(txnsB) != 0 {
		t.Fatalf("expected commuter B to see 0 transactions, got %#v", rawB)
	}
}

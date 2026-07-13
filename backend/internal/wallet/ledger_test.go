package wallet_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/wallet"
)

// testEnv holds everything a test needs against a real Postgres. All tests
// in this file skip (rather than fail) when no DB is reachable, matching
// Stage 0/1's approach.
type testEnv struct {
	pool      *pgxpool.Pool
	identity  *identity.Repo
	wallet    *wallet.Repo
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
		t.Skip("skipping test: no reachable Postgres database")
	}

	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	env := &testEnv{
		pool:      pool,
		identity:  identity.NewRepo(pool),
		wallet:    wallet.NewRepo(pool),
		fareSplit: config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65},
	}
	t.Cleanup(pool.Close)

	if err := env.wallet.EnsureSystemAccounts(context.Background()); err != nil {
		t.Fatalf("failed to ensure system accounts: %v", err)
	}

	return env
}

var uniqueCounter int64

// uniquePhone returns a value unique both within this test binary run and
// across repeated runs against the same (persistent, not reset-between-runs)
// database. A plain in-process counter restarting at 1 every run would
// collide with rows a previous run already left behind; combining a
// per-call nanosecond timestamp with an atomic counter avoids that (and the
// atomic counter alone guards against two calls landing in the same
// nanosecond).
func uniquePhone(prefix string) string {
	n := atomic.AddInt64(&uniqueCounter, 1)
	return fmt.Sprintf("+27%d%d%s", time.Now().UnixNano(), n, prefix)
}

func mustCreateCommuter(t *testing.T, env *testEnv) identity.User {
	t.Helper()
	hash, err := identity.HashPassword("Test1234!")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	u, err := env.identity.CreateUser(context.Background(), uniquePhone("c"), nil, hash, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter: %v", err)
	}
	return u
}

// mustCreateVehicleWithDriver creates an owner, a vehicle, a driver, and an
// active assignment between them, returning the vehicle.
func mustCreateVehicleWithDriver(t *testing.T, env *testEnv) identity.Vehicle {
	t.Helper()
	ctx := context.Background()

	ownerHash, err := identity.HashPassword("Test1234!")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	owner, err := env.identity.CreateUser(ctx, uniquePhone("o"), nil, ownerHash, identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	driverUser, err := env.identity.CreateUser(ctx, uniquePhone("d"), nil, ownerHash, identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := env.identity.CreateDriver(ctx, driverUser.ID, "Test Driver", uniquePhone("prdp"), uniquePhone("id"))
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}

	vehicle, err := env.identity.CreateVehicle(ctx, owner.ID, uniquePhone("REG"), 16, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}

	if _, err := env.identity.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to assign driver to vehicle: %v", err)
	}

	return vehicle
}

func (env *testEnv) balance(t *testing.T, accountOwner *uuid.UUID, accountType wallet.AccountType) int64 {
	t.Helper()
	ctx := context.Background()
	acc, err := env.wallet.GetOrCreateAccount(ctx, env.pool, accountOwner, accountType)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	bal, err := env.wallet.AccountBalance(ctx, env.pool, acc.ID)
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	return bal
}

func TestTopupThenBalance(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)

	_, balance, err := env.wallet.Topup(context.Background(), commuter.ID, 5000)
	if err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	if balance != 5000 {
		t.Fatalf("expected balance 5000, got %d", balance)
	}

	got := env.balance(t, &commuter.ID, wallet.AccountCommuterWallet)
	if got != 5000 {
		t.Fatalf("expected balance 5000, got %d", got)
	}
}

// TestSplitSumsToFare checks the fare split always sums to exactly
// fare_cents, including amounts that don't divide evenly across the default
// 10/25/65 split.
func TestSplitSumsToFare(t *testing.T) {
	env := setup(t)

	amounts := []int64{100, 999, 1000, 1, 3, 7, 12345, 999999, 2}

	for _, fareCents := range amounts {
		fareCents := fareCents
		t.Run(fmt.Sprintf("fare_%d", fareCents), func(t *testing.T) {
			commuter := mustCreateCommuter(t, env)
			vehicle := mustCreateVehicleWithDriver(t, env)

			if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, fareCents); err != nil {
				t.Fatalf("topup failed: %v", err)
			}

			_, split, replayed, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, fareCents, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
			if err != nil {
				t.Fatalf("charge failed: %v", err)
			}
			if replayed {
				t.Fatal("expected a fresh charge, got replayed")
			}

			sum := split.PlatformCents + split.DriverCents + split.OwnerCents
			if sum != fareCents {
				t.Fatalf("split %+v sums to %d, want %d", split, sum, fareCents)
			}
		})
	}
}

// TestLedgerInvariant checks that every transaction's postings sum to zero.
func TestLedgerInvariant(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	vehicle := mustCreateVehicleWithDriver(t, env)

	if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, 10000); err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	txn, _, _, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 2345, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err != nil {
		t.Fatalf("charge failed: %v", err)
	}

	var total int64
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount_cents), 0) FROM ledger_postings WHERE transaction_id = $1`,
		txn.ID,
	).Scan(&total); err != nil {
		t.Fatalf("failed to sum postings: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected postings to sum to zero, got %d", total)
	}
}

func TestIdempotentFareCharge(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	vehicle := mustCreateVehicleWithDriver(t, env)

	if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, 10000); err != nil {
		t.Fatalf("topup failed: %v", err)
	}

	key := uuid.NewString()

	txn1, _, replayed1, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 1500, key, env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err != nil {
		t.Fatalf("first charge failed: %v", err)
	}
	if replayed1 {
		t.Fatal("expected first charge to not be a replay")
	}

	txn2, _, replayed2, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 1500, key, env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err != nil {
		t.Fatalf("replayed charge failed: %v", err)
	}
	if !replayed2 {
		t.Fatal("expected second charge with same idempotency key to be reported as replayed")
	}
	if txn1.ID != txn2.ID {
		t.Fatalf("expected same transaction id, got %s and %s", txn1.ID, txn2.ID)
	}

	got := env.balance(t, &commuter.ID, wallet.AccountCommuterWallet)
	if got != 10000-1500 {
		t.Fatalf("expected balance debited exactly once (%d), got %d", 10000-1500, got)
	}
}

func TestInsufficientFundsRejected(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	vehicle := mustCreateVehicleWithDriver(t, env)

	if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, 1000); err != nil {
		t.Fatalf("topup failed: %v", err)
	}

	_, _, _, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 5000, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err == nil {
		t.Fatal("expected insufficient funds error")
	}

	got := env.balance(t, &commuter.ID, wallet.AccountCommuterWallet)
	if got != 1000 {
		t.Fatalf("expected balance unchanged at 1000, got %d", got)
	}
}

// TestConcurrentChargesOnlyOneSucceeds fires two concurrent fare charges
// against a wallet that can only afford one, and checks exactly one
// succeeds while the balance never goes negative.
func TestConcurrentChargesOnlyOneSucceeds(t *testing.T) {
	env := setup(t)
	commuter := mustCreateCommuter(t, env)
	vehicle := mustCreateVehicleWithDriver(t, env)

	if _, _, err := env.wallet.Topup(context.Background(), commuter.ID, 1000); err != nil {
		t.Fatalf("topup failed: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _, err := env.wallet.ChargeFare(context.Background(), commuter.ID, vehicle.ID, 1000, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
			results[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, wallet.ErrInsufficientFunds) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful charge, got %d", successes)
	}

	got := env.balance(t, &commuter.ID, wallet.AccountCommuterWallet)
	if got != 0 {
		t.Fatalf("expected balance 0 after exactly one charge, got %d", got)
	}
	if got < 0 {
		t.Fatalf("balance went negative: %d", got)
	}
}

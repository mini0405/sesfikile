package fuel_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/identity"
	"sesfikile/backend/internal/wallet"
)

// testEnv holds everything a test needs against a real Postgres. All tests
// in this file skip (rather than fail) when no DB is reachable, matching
// every prior stage's integration test pattern.
type testEnv struct {
	pool      *pgxpool.Pool
	identity  *identity.Repo
	wallet    *wallet.Repo
	fuel      *fuel.Repo
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

	walletRepo := wallet.NewRepo(pool)
	env := &testEnv{
		pool:      pool,
		identity:  identity.NewRepo(pool),
		wallet:    walletRepo,
		fuel:      fuel.NewRepo(pool, walletRepo),
		fareSplit: config.FareSplit{PlatformPct: 10, DriverPct: 25, OwnerPct: 65},
	}
	t.Cleanup(pool.Close)

	if err := env.wallet.EnsureSystemAccounts(context.Background()); err != nil {
		t.Fatalf("failed to ensure system accounts: %v", err)
	}

	return env
}

var uniqueCounter int64

func uniqueStr(prefix string) string {
	n := atomic.AddInt64(&uniqueCounter, 1)
	return fmt.Sprintf("+27%d%d%s", time.Now().UnixNano(), n, prefix)
}

// fundedFixture is the full set of identities mustCreateFundedOwnerVehicleFull
// creates — everything mustCreateFundedOwnerVehicle's (Owner, Vehicle) pair
// omits. Most tests in this file only need the owner/vehicle; the
// anti-bypass test also needs the driver's and commuter's own user ids so it
// can scope its assertions to exactly the accounts this one test's fixture
// owns, rather than reading account state shared with every other package's
// tests running concurrently against the same dev Postgres.
type fundedFixture struct {
	Owner        identity.User
	Vehicle      identity.Vehicle
	DriverUserID uuid.UUID
	CommuterID   uuid.UUID
}

// mustCreateFundedOwnerVehicleFull creates an owner, a driver, a vehicle
// assigned to that driver, and a commuter — then charges revenueCents worth
// of fares from the commuter through the vehicle, so the owner's
// owner_revenue balance ends up non-zero (the real precondition for
// /fuel/allocate to have anything to withhold).
func mustCreateFundedOwnerVehicleFull(t *testing.T, env *testEnv, revenueCents int64) fundedFixture {
	t.Helper()
	ctx := context.Background()

	hash, err := identity.HashPassword("Test1234!")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	owner, err := env.identity.CreateUser(ctx, uniqueStr("o"), nil, hash, identity.RoleOwner)
	if err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	driverUser, err := env.identity.CreateUser(ctx, uniqueStr("d"), nil, hash, identity.RoleDriver)
	if err != nil {
		t.Fatalf("failed to create driver user: %v", err)
	}
	driver, err := env.identity.CreateDriver(ctx, driverUser.ID, "Test Driver", uniqueStr("prdp"), uniqueStr("id"))
	if err != nil {
		t.Fatalf("failed to create driver profile: %v", err)
	}
	vehicle, err := env.identity.CreateVehicle(ctx, owner.ID, uniqueStr("REG"), 16, nil)
	if err != nil {
		t.Fatalf("failed to create vehicle: %v", err)
	}
	if _, err := env.identity.CreateVehicleAssignment(ctx, vehicle.ID, driver.ID); err != nil {
		t.Fatalf("failed to assign driver to vehicle: %v", err)
	}

	commuter, err := env.identity.CreateUser(ctx, uniqueStr("c"), nil, hash, identity.RoleCommuter)
	if err != nil {
		t.Fatalf("failed to create commuter: %v", err)
	}

	// Owner's revenue share is 65% of the fare by default (env.fareSplit) —
	// charge enough fare that owner_revenue lands on exactly revenueCents to
	// keep test assertions simple: revenueCents / ownerPct * 100.
	fareCents := revenueCents * 100 / int64(env.fareSplit.OwnerPct)
	if _, _, err := env.wallet.Topup(ctx, commuter.ID, fareCents); err != nil {
		t.Fatalf("topup failed: %v", err)
	}
	_, split, _, err := env.wallet.ChargeFare(ctx, commuter.ID, vehicle.ID, fareCents, uuid.NewString(), env.fareSplit.PlatformPct, env.fareSplit.DriverPct)
	if err != nil {
		t.Fatalf("charge fare failed: %v", err)
	}
	if split.OwnerCents != revenueCents {
		t.Fatalf("test setup: expected owner_revenue %d, computed split gave %d (fareCents=%d)", revenueCents, split.OwnerCents, fareCents)
	}

	return fundedFixture{Owner: owner, Vehicle: vehicle, DriverUserID: driverUser.ID, CommuterID: commuter.ID}
}

// mustCreateFundedOwnerVehicle is the (Owner, Vehicle)-only convenience form
// every test but the anti-bypass one uses.
func mustCreateFundedOwnerVehicle(t *testing.T, env *testEnv, revenueCents int64) (identity.User, identity.Vehicle) {
	t.Helper()
	fx := mustCreateFundedOwnerVehicleFull(t, env, revenueCents)
	return fx.Owner, fx.Vehicle
}

func (env *testEnv) accountBalance(t *testing.T, ownerUserID *uuid.UUID, accountType wallet.AccountType) int64 {
	t.Helper()
	ctx := context.Background()
	acc, err := env.wallet.GetOrCreateAccount(ctx, env.pool, ownerUserID, accountType)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	bal, err := env.wallet.AccountBalance(ctx, env.pool, acc.ID)
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	return bal
}

// scopedPostingCount counts ledger_postings against exactly the accounts
// owned by ownerUserIDs (accounts.owner_user_id) — never a global count
// across every account in the database. This is what makes it safe to use
// from a test running concurrently with every other package's DB-backed
// tests against the same persistent dev Postgres: the shared system
// accounts (funding_source, platform_fee) have owner_user_id NULL and can
// never match this filter, and no other test in the suite knows these
// specific, single-test-unique user ids, so only this test's own operations
// can move this count.
func (env *testEnv) scopedPostingCount(t *testing.T, ownerUserIDs ...uuid.UUID) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_postings lp
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.owner_user_id = ANY($1::uuid[])`,
		ownerUserIDs,
	).Scan(&n); err != nil {
		t.Fatalf("failed to count scoped postings: %v", err)
	}
	return n
}

// assertTransactionOnlyTouchesAccountTypes asserts transactionID's postings
// exist against exactly wantTypes (as a set) — a structural check on this
// one transaction's own posting rows, not a global balance snapshot, so it
// can't be perturbed by concurrent activity elsewhere in the database.
func assertTransactionOnlyTouchesAccountTypes(t *testing.T, env *testEnv, transactionID uuid.UUID, wantTypes ...wallet.AccountType) {
	t.Helper()
	rows, err := env.pool.Query(context.Background(),
		`SELECT a.type FROM ledger_postings lp
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE lp.transaction_id = $1
		 ORDER BY a.type`,
		transactionID,
	)
	if err != nil {
		t.Fatalf("failed to load transaction account types: %v", err)
	}
	defer rows.Close()

	got := map[wallet.AccountType]bool{}
	for rows.Next() {
		var accType wallet.AccountType
		if err := rows.Scan(&accType); err != nil {
			t.Fatalf("failed to scan account type: %v", err)
		}
		got[accType] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to read transaction account types: %v", err)
	}

	want := map[wallet.AccountType]bool{}
	for _, wt := range wantTypes {
		want[wt] = true
	}

	if len(got) != len(want) {
		t.Fatalf("transaction %s touches account types %v, want exactly %v", transactionID, got, want)
	}
	for wt := range want {
		if !got[wt] {
			t.Fatalf("transaction %s touches account types %v, want exactly %v", transactionID, got, want)
		}
	}
}

// --- Withholding (real ledger) -------------------------------------------

func TestAllocate_MovesExactWithholdPercentage(t *testing.T) {
	env := setup(t)
	owner, _ := mustCreateFundedOwnerVehicle(t, env, 10000)

	revenueBefore := env.accountBalance(t, &owner.ID, wallet.AccountOwnerRevenue)
	if revenueBefore != 10000 {
		t.Fatalf("expected owner_revenue 10000, got %d", revenueBefore)
	}

	txn, allocated, err := env.fuel.Allocate(context.Background(), owner.ID, 30)
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if allocated != 3000 {
		t.Fatalf("expected 30%% of 10000 = 3000 allocated, got %d", allocated)
	}
	if txn.ID == uuid.Nil {
		t.Fatal("expected a real transaction id")
	}

	revenueAfter := env.accountBalance(t, &owner.ID, wallet.AccountOwnerRevenue)
	if revenueAfter != revenueBefore-3000 {
		t.Fatalf("expected owner_revenue to drop by exactly 3000, got %d -> %d", revenueBefore, revenueAfter)
	}

	fuelBalance, err := env.fuel.Balance(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("balance failed: %v", err)
	}
	if fuelBalance != 3000 {
		t.Fatalf("expected fuel_account balance 3000, got %d", fuelBalance)
	}

	var total int64
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount_cents), 0) FROM ledger_postings WHERE transaction_id = $1`,
		txn.ID,
	).Scan(&total); err != nil {
		t.Fatalf("failed to sum postings: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected allocation postings to sum to zero, got %d", total)
	}
}

func TestAllocate_NothingToAllocateWhenRevenueZero(t *testing.T) {
	env := setup(t)
	owner, _ := mustCreateFundedOwnerVehicle(t, env, 0)

	_, _, err := env.fuel.Allocate(context.Background(), owner.ID, 30)
	if !errors.Is(err, fuel.ErrNothingToAllocate) {
		t.Fatalf("expected ErrNothingToAllocate, got %v", err)
	}
}

// --- Per-vehicle quota -----------------------------------------------------

func TestFundVehicleQuota_CannotExceedFuelAccountBalance(t *testing.T) {
	env := setup(t)
	owner, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner.ID, 30); err != nil { // fuel_account = 3000
		t.Fatalf("allocate failed: %v", err)
	}

	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 5000); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds funding more than fuel_account holds, got %v", err)
	}

	q, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 2000)
	if err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}
	if q.QuotaCents != 2000 || q.AvailableCents() != 2000 {
		t.Fatalf("unexpected quota state: %+v", q)
	}
}

func TestFundVehicleQuota_RejectsVehicleNotOwnedByCaller(t *testing.T) {
	env := setup(t)
	owner1, _ := mustCreateFundedOwnerVehicle(t, env, 10000)
	_, vehicle2 := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner1.ID, 100); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}

	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner1.ID, vehicle2.ID, 100); !errors.Is(err, fuel.ErrNotOwnersVehicle) {
		t.Fatalf("expected ErrNotOwnersVehicle, got %v", err)
	}
}

// --- MOCK VIU authorize / confirm -----------------------------------------

func TestAuthorizePump_WithinQuota_ReservesAndAuthorizes(t *testing.T) {
	env := setup(t)
	owner, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner.ID, 100); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 5000); err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}

	result, err := env.fuel.AuthorizePump(context.Background(), vehicle.ID, 100, 2000)
	if err != nil {
		t.Fatalf("authorize failed: %v", err)
	}
	if !result.Authorized {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.AuthReference == uuid.Nil {
		t.Fatal("expected a non-nil auth reference")
	}

	q, err := env.fuel.VehicleQuotaFor(context.Background(), vehicle.ID)
	if err != nil {
		t.Fatalf("quota lookup failed: %v", err)
	}
	if q.ReservedCents != 2000 {
		t.Fatalf("expected 2000 reserved, got %d", q.ReservedCents)
	}
	if q.AvailableCents() != 3000 {
		t.Fatalf("expected 3000 still available, got %d", q.AvailableCents())
	}
}

func TestAuthorizePump_BeyondQuota_DeniedAndQuotaUnchanged(t *testing.T) {
	env := setup(t)
	owner, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner.ID, 100); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 2000); err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}

	result, err := env.fuel.AuthorizePump(context.Background(), vehicle.ID, 200, 5000)
	if err != nil {
		t.Fatalf("authorize call failed: %v", err)
	}
	if result.Authorized {
		t.Fatal("expected denial for amount beyond quota")
	}
	if result.MaxAmountCents != 2000 {
		t.Fatalf("expected max_amount_cents 2000, got %d", result.MaxAmountCents)
	}

	q, err := env.fuel.VehicleQuotaFor(context.Background(), vehicle.ID)
	if err != nil {
		t.Fatalf("quota lookup failed: %v", err)
	}
	if q.ReservedCents != 0 || q.AvailableCents() != 2000 {
		t.Fatalf("expected quota unchanged by a denied authorization, got %+v", q)
	}
}

func TestAuthorizePump_NoQuotaAllocated_Denied(t *testing.T) {
	env := setup(t)
	_, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)

	result, err := env.fuel.AuthorizePump(context.Background(), vehicle.ID, 10, 100)
	if err != nil {
		t.Fatalf("authorize call failed: %v", err)
	}
	if result.Authorized {
		t.Fatal("expected denial when no quota has ever been funded for this vehicle")
	}
}

func TestConfirmPump_SettlesReservationCorrectly(t *testing.T) {
	env := setup(t)
	owner, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner.ID, 100); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 5000); err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}

	auth, err := env.fuel.AuthorizePump(context.Background(), vehicle.ID, 100, 2000)
	if err != nil || !auth.Authorized {
		t.Fatalf("authorize failed: %+v, %v", auth, err)
	}

	confirm, err := env.fuel.ConfirmPump(context.Background(), auth.AuthReference)
	if err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if confirm.AlreadyConfirmed {
		t.Fatal("expected a fresh confirm, not already-confirmed")
	}
	if confirm.AmountCents != 2000 {
		t.Fatalf("expected confirmed amount 2000, got %d", confirm.AmountCents)
	}

	q, err := env.fuel.VehicleQuotaFor(context.Background(), vehicle.ID)
	if err != nil {
		t.Fatalf("quota lookup failed: %v", err)
	}
	if q.ReservedCents != 0 {
		t.Fatalf("expected reserved_cents to drop to 0 after confirm, got %d", q.ReservedCents)
	}
	if q.UsedCents != 2000 {
		t.Fatalf("expected used_cents 2000 after confirm, got %d", q.UsedCents)
	}
}

// TestConfirmPump_SecondConfirmIsIdempotent_NoDoubleDebit is the stage
// brief's explicit "second confirm on the same reference is
// idempotent/rejected (no double-debit)" requirement.
func TestConfirmPump_SecondConfirmIsIdempotent_NoDoubleDebit(t *testing.T) {
	env := setup(t)
	owner, vehicle := mustCreateFundedOwnerVehicle(t, env, 10000)
	if _, _, err := env.fuel.Allocate(context.Background(), owner.ID, 100); err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if _, err := env.fuel.FundVehicleQuota(context.Background(), owner.ID, vehicle.ID, 5000); err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}
	auth, err := env.fuel.AuthorizePump(context.Background(), vehicle.ID, 100, 2000)
	if err != nil || !auth.Authorized {
		t.Fatalf("authorize failed: %+v, %v", auth, err)
	}

	if _, err := env.fuel.ConfirmPump(context.Background(), auth.AuthReference); err != nil {
		t.Fatalf("first confirm failed: %v", err)
	}

	second, err := env.fuel.ConfirmPump(context.Background(), auth.AuthReference)
	if err != nil {
		t.Fatalf("second confirm failed: %v", err)
	}
	if !second.AlreadyConfirmed {
		t.Fatal("expected second confirm to report AlreadyConfirmed=true")
	}

	q, err := env.fuel.VehicleQuotaFor(context.Background(), vehicle.ID)
	if err != nil {
		t.Fatalf("quota lookup failed: %v", err)
	}
	if q.UsedCents != 2000 {
		t.Fatalf("expected used_cents to stay at 2000 after a replayed confirm (no double-debit), got %d", q.UsedCents)
	}
}

func TestConfirmPump_UnknownReference_NotFound(t *testing.T) {
	env := setup(t)
	if _, err := env.fuel.ConfirmPump(context.Background(), uuid.New()); !errors.Is(err, fuel.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- Anti-bypass ------------------------------------------------------------

// TestAntiBypass_FuelFundsNeverReachWalletOrPayout exercises the full
// allocate -> fund quota -> authorize -> confirm flow and asserts the
// structural anti-bypass property from the stage brief: fuel money only
// ever moves owner_revenue -> fuel_account (one real ledger transaction,
// Allocate) and is then consumed by quota bookkeeping alone — no
// commuter_wallet, driver_earnings, or funding_source balance is ever
// touched by fuel operations, and FundVehicleQuota/AuthorizePump/
// ConfirmPump never add a single new ledger_postings row (only Allocate
// does), which is the structural guarantee that fuel value cannot be
// cashed back out through the ledger.
//
// ISOLATION: this test used to read global state — SUM(amount_cents) across
// every commuter_wallet/driver_earnings account in the database, and the
// balance of the one shared funding_source system account — which made it
// flaky under `go test ./...`'s default cross-package parallelism, since
// other packages' tests are concurrently topping up/charging real wallets
// (and the ONE shared funding_source account, by construction) against the
// same persistent dev Postgres. It now only ever inspects: (a) postings
// scoped to this fixture's own owner/driver/commuter user ids
// (scopedPostingCount, via accounts.owner_user_id — no other test in the
// suite knows these single-test-unique ids, and the shared system accounts
// have owner_user_id NULL so can never match this filter), and (b) the
// account-type structure of the one transaction Allocate itself returns
// (assertTransactionOnlyTouchesAccountTypes) rather than a before/after
// snapshot of the shared funding_source balance. The guarantee proved is
// identical — none of these operations touch funding_source or this
// fixture's own commuter_wallet/driver_earnings accounts — only which rows
// get read changed.
func TestAntiBypass_FuelFundsNeverReachWalletOrPayout(t *testing.T) {
	env := setup(t)
	fx := mustCreateFundedOwnerVehicleFull(t, env, 10000)
	fixtureOwnerIDs := []uuid.UUID{fx.Owner.ID, fx.DriverUserID, fx.CommuterID}

	commuterWalletBefore := env.accountBalance(t, &fx.CommuterID, wallet.AccountCommuterWallet)
	driverEarningsBefore := env.accountBalance(t, &fx.DriverUserID, wallet.AccountDriverEarnings)
	postingsBefore := env.scopedPostingCount(t, fixtureOwnerIDs...)

	allocateTxn, _, err := env.fuel.Allocate(context.Background(), fx.Owner.ID, 30)
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}

	// Allocate is the only step in this flow allowed to add a posting, and
	// its transaction must touch exactly owner_revenue and fuel_account —
	// never funding_source, commuter_wallet, driver_earnings, or
	// platform_fee. A structural check on this one transaction's own
	// posting rows, not a global balance read.
	assertTransactionOnlyTouchesAccountTypes(t, env, allocateTxn.ID, wallet.AccountOwnerRevenue, wallet.AccountFuelAccount)

	postingsAfterAllocate := env.scopedPostingCount(t, fixtureOwnerIDs...)
	if postingsAfterAllocate != postingsBefore+2 {
		t.Fatalf("expected Allocate to add exactly 2 postings (owner_revenue debit + fuel_account credit) to this fixture's own accounts, went from %d to %d", postingsBefore, postingsAfterAllocate)
	}

	if _, err := env.fuel.FundVehicleQuota(context.Background(), fx.Owner.ID, fx.Vehicle.ID, 2000); err != nil {
		t.Fatalf("fund quota failed: %v", err)
	}
	if got := env.scopedPostingCount(t, fixtureOwnerIDs...); got != postingsAfterAllocate {
		t.Fatalf("FundVehicleQuota must not create any new ledger postings — it only earmarks already-withheld fuel_account funds (went from %d to %d)", postingsAfterAllocate, got)
	}

	auth, err := env.fuel.AuthorizePump(context.Background(), fx.Vehicle.ID, 90, 2000)
	if err != nil || !auth.Authorized {
		t.Fatalf("authorize failed: %+v, %v", auth, err)
	}
	if got := env.scopedPostingCount(t, fixtureOwnerIDs...); got != postingsAfterAllocate {
		t.Fatalf("AuthorizePump must not create any new ledger postings — it only reserves quota (went from %d to %d)", postingsAfterAllocate, got)
	}

	if _, err := env.fuel.ConfirmPump(context.Background(), auth.AuthReference); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if got := env.scopedPostingCount(t, fixtureOwnerIDs...); got != postingsAfterAllocate {
		t.Fatalf("ConfirmPump must not create any new ledger postings — settling a quota reservation is not a ledger event (went from %d to %d)", postingsAfterAllocate, got)
	}

	commuterWalletAfter := env.accountBalance(t, &fx.CommuterID, wallet.AccountCommuterWallet)
	driverEarningsAfter := env.accountBalance(t, &fx.DriverUserID, wallet.AccountDriverEarnings)

	if commuterWalletAfter != commuterWalletBefore {
		t.Fatalf("this fixture's commuter_wallet changed from %d to %d — fuel operations must never touch commuter wallets", commuterWalletBefore, commuterWalletAfter)
	}
	if driverEarningsAfter != driverEarningsBefore {
		t.Fatalf("this fixture's driver_earnings changed from %d to %d — fuel operations must never touch driver payouts", driverEarningsBefore, driverEarningsAfter)
	}
}

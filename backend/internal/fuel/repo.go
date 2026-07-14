package fuel

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/wallet"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrNotOwnersVehicle  = errors.New("vehicle does not belong to this owner")
	ErrNothingToAllocate = errors.New("owner_revenue balance is zero, nothing to allocate")
	ErrInvalidAmount     = errors.New("amount_cents must be positive")
)

// ANTI-BYPASS PROPERTY (the constraint the stage brief requires be
// structural, not just tested): fuel_account and vehicle_fuel_quotas money
// only ever flows in ONE direction — owner_revenue -> fuel_account
// (Allocate, below, a real ledger transfer) -> a vehicle's earmarked quota
// (FundVehicleQuota) -> consumed by a MOCK VIU authorization (viu_mock.go).
// Nothing in this package posts a ledger transaction, updates a quota row,
// or otherwise moves value FROM fuel_account or vehicle_fuel_quotas TOWARD
// commuter_wallet, driver_earnings, owner_revenue, or funding_source —
// there is no such function to call. Fuel value cannot be cashed out.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repo struct {
	pool       *pgxpool.Pool
	walletRepo *wallet.Repo
}

func NewRepo(pool *pgxpool.Pool, walletRepo *wallet.Repo) *Repo {
	return &Repo{pool: pool, walletRepo: walletRepo}
}

// Allocate is the REAL-ledger fuel withholding step: it moves
// withholdPct% of the owner's current owner_revenue balance into their
// fuel_account, as one balanced ledger transaction via
// wallet.Repo.InternalTransfer. It creates no money — only ever moves the
// owner's own money between their own two accounts.
func (r *Repo) Allocate(ctx context.Context, ownerUserID uuid.UUID, withholdPct int) (wallet.LedgerTransaction, int64, error) {
	revenueAcc, err := r.walletRepo.GetOrCreateAccount(ctx, r.pool, &ownerUserID, wallet.AccountOwnerRevenue)
	if err != nil {
		return wallet.LedgerTransaction{}, 0, err
	}
	revenueBalance, err := r.walletRepo.AccountBalance(ctx, r.pool, revenueAcc.ID)
	if err != nil {
		return wallet.LedgerTransaction{}, 0, err
	}

	amountCents := revenueBalance * int64(withholdPct) / 100
	if amountCents <= 0 {
		return wallet.LedgerTransaction{}, 0, ErrNothingToAllocate
	}

	txn, err := r.walletRepo.InternalTransfer(ctx, ownerUserID, wallet.AccountOwnerRevenue, wallet.AccountFuelAccount, amountCents, wallet.KindFuelAllocation, map[string]any{
		"withhold_pct":    withholdPct,
		"revenue_balance": revenueBalance,
		"allocated_cents": amountCents,
	})
	if err != nil {
		return wallet.LedgerTransaction{}, 0, err
	}
	return txn, amountCents, nil
}

// Balance returns the owner's current fuel_account ledger balance —
// SUM(amount_cents) over its postings, same derivation as every other
// account in the ledger (see wallet.Repo.AccountBalance).
func (r *Repo) Balance(ctx context.Context, ownerUserID uuid.UUID) (int64, error) {
	acc, err := r.walletRepo.GetOrCreateAccount(ctx, r.pool, &ownerUserID, wallet.AccountFuelAccount)
	if err != nil {
		return 0, err
	}
	return r.walletRepo.AccountBalance(ctx, r.pool, acc.ID)
}

// vehicleOwner looks up a vehicle's owner_user_id directly (fuel doesn't
// need the rest of identity.Vehicle, so it queries the one column it needs
// rather than taking a dependency on the identity package).
func (r *Repo) vehicleOwner(ctx context.Context, q querier, vehicleID uuid.UUID) (uuid.UUID, error) {
	var ownerUserID uuid.UUID
	err := q.QueryRow(ctx, `SELECT owner_user_id FROM vehicles WHERE id = $1`, vehicleID).Scan(&ownerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return ownerUserID, nil
}

// FundVehicleQuota earmarks amountCents of the owner's fuel_account balance
// to one vehicle, so a MOCK VIU authorization can later draw against it
// (viu_mock.go). This does NOT post a new ledger transaction: the money
// already left owner_revenue for fuel_account in Allocate above, so
// committing a slice of fuel_account's balance to a specific vehicle is
// bookkeeping over already-withheld funds, not a new cross-account
// transfer. It IS checked against fuel_account's real ledger balance minus
// everything already earmarked to other vehicles, so a vehicle's quota can
// never outrun what the owner actually withheld.
func (r *Repo) FundVehicleQuota(ctx context.Context, ownerUserID, vehicleID uuid.UUID, amountCents int64) (VehicleQuota, error) {
	if amountCents <= 0 {
		return VehicleQuota{}, ErrInvalidAmount
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return VehicleQuota{}, err
	}
	defer tx.Rollback(ctx)

	ownerOfVehicle, err := r.vehicleOwner(ctx, tx, vehicleID)
	if err != nil {
		return VehicleQuota{}, err
	}
	if ownerOfVehicle != ownerUserID {
		return VehicleQuota{}, ErrNotOwnersVehicle
	}

	fuelAcc, err := r.walletRepo.GetOrCreateAccount(ctx, tx, &ownerUserID, wallet.AccountFuelAccount)
	if err != nil {
		return VehicleQuota{}, err
	}
	fuelBalance, err := r.walletRepo.AccountBalance(ctx, tx, fuelAcc.ID)
	if err != nil {
		return VehicleQuota{}, err
	}

	var alreadyEarmarked int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(quota_cents), 0) FROM vehicle_fuel_quotas WHERE owner_user_id = $1`,
		ownerUserID,
	).Scan(&alreadyEarmarked); err != nil {
		return VehicleQuota{}, err
	}

	if alreadyEarmarked+amountCents > fuelBalance {
		return VehicleQuota{}, wallet.ErrInsufficientFunds
	}

	var q VehicleQuota
	err = tx.QueryRow(ctx,
		`INSERT INTO vehicle_fuel_quotas (vehicle_id, owner_user_id, quota_cents)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (vehicle_id) DO UPDATE SET quota_cents = vehicle_fuel_quotas.quota_cents + EXCLUDED.quota_cents, updated_at = now()
		 RETURNING vehicle_id, owner_user_id, quota_cents, reserved_cents, used_cents, updated_at`,
		vehicleID, ownerUserID, amountCents,
	).Scan(&q.VehicleID, &q.OwnerUserID, &q.QuotaCents, &q.ReservedCents, &q.UsedCents, &q.UpdatedAt)
	if err != nil {
		return VehicleQuota{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return VehicleQuota{}, err
	}
	return q, nil
}

// VehicleQuotaFor returns a vehicle's current quota row, or a zero-valued
// quota (not an error) if the owner has never funded that vehicle yet.
func (r *Repo) VehicleQuotaFor(ctx context.Context, vehicleID uuid.UUID) (VehicleQuota, error) {
	var q VehicleQuota
	err := r.pool.QueryRow(ctx,
		`SELECT vehicle_id, owner_user_id, quota_cents, reserved_cents, used_cents, updated_at
		 FROM vehicle_fuel_quotas WHERE vehicle_id = $1`,
		vehicleID,
	).Scan(&q.VehicleID, &q.OwnerUserID, &q.QuotaCents, &q.ReservedCents, &q.UsedCents, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VehicleQuota{VehicleID: vehicleID}, nil
		}
		return VehicleQuota{}, err
	}
	return q, nil
}

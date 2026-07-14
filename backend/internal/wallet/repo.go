package wallet

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrNoActiveDriver      = errors.New("vehicle has no active driver assignment")
	ErrIdempotencyRequired = errors.New("idempotency_key is required")
	ErrInvalidAmount       = errors.New("amount_cents must be positive")
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so repo helpers can
// run either standalone or inside a caller-managed transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// EnsureSystemAccounts creates the funding_source and platform_fee accounts
// if they don't already exist. Safe to call on every startup.
func (r *Repo) EnsureSystemAccounts(ctx context.Context) error {
	if _, err := r.GetOrCreateAccount(ctx, r.pool, nil, AccountFundingSource); err != nil {
		return err
	}
	if _, err := r.GetOrCreateAccount(ctx, r.pool, nil, AccountPlatformFee); err != nil {
		return err
	}
	return nil
}

func (r *Repo) getAccount(ctx context.Context, q querier, ownerUserID *uuid.UUID, t AccountType) (Account, error) {
	var row pgx.Row
	if ownerUserID != nil {
		row = q.QueryRow(ctx,
			`SELECT id, owner_user_id, type, created_at FROM accounts WHERE owner_user_id = $1 AND type = $2`,
			*ownerUserID, t)
	} else {
		row = q.QueryRow(ctx,
			`SELECT id, owner_user_id, type, created_at FROM accounts WHERE owner_user_id IS NULL AND type = $1`,
			t)
	}

	var a Account
	if err := row.Scan(&a.ID, &a.OwnerUserID, &a.Type, &a.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrNotFound
		}
		return Account{}, err
	}
	return a, nil
}

func (r *Repo) insertAccount(ctx context.Context, q querier, ownerUserID *uuid.UUID, t AccountType) (Account, error) {
	var a Account
	err := q.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, type) VALUES ($1, $2)
		 RETURNING id, owner_user_id, type, created_at`,
		ownerUserID, t,
	).Scan(&a.ID, &a.OwnerUserID, &a.Type, &a.CreatedAt)
	return a, err
}

// GetOrCreateAccount fetches an account, creating it on first use. Account
// creation is lazy: commuter/driver/owner accounts come into existence the
// first time money needs to move in or out of them.
func (r *Repo) GetOrCreateAccount(ctx context.Context, q querier, ownerUserID *uuid.UUID, t AccountType) (Account, error) {
	acc, err := r.getAccount(ctx, q, ownerUserID, t)
	if err == nil {
		return acc, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Account{}, err
	}

	acc, err = r.insertAccount(ctx, q, ownerUserID, t)
	if err != nil {
		if isUniqueViolation(err) {
			return r.getAccount(ctx, q, ownerUserID, t)
		}
		return Account{}, err
	}
	return acc, nil
}

// lockAccount takes a row lock on the account, serializing concurrent
// operations against it (e.g. two simultaneous fare charges) until the
// holding transaction commits or rolls back.
func (r *Repo) lockAccount(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) error {
	var id uuid.UUID
	return tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&id)
}

// AccountBalance is always computed from postings — there is no stored
// balance column to drift out of sync.
func (r *Repo) AccountBalance(ctx context.Context, q querier, accountID uuid.UUID) (int64, error) {
	var total int64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM ledger_postings WHERE account_id = $1`,
		accountID,
	).Scan(&total)
	return total, err
}

func (r *Repo) insertTransaction(ctx context.Context, tx pgx.Tx, kind TransactionKind, idempotencyKey *string, metadata any) (LedgerTransaction, error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return LedgerTransaction{}, err
	}

	var t LedgerTransaction
	err = tx.QueryRow(ctx,
		`INSERT INTO ledger_transactions (kind, idempotency_key, metadata) VALUES ($1, $2, $3)
		 RETURNING id, kind, idempotency_key, metadata, created_at`,
		kind, idempotencyKey, metaJSON,
	).Scan(&t.ID, &t.Kind, &t.IdempotencyKey, &t.Metadata, &t.CreatedAt)
	return t, err
}

// tryInsertTransaction inserts a new transaction unless idempotencyKey
// already exists, in which case it inserts nothing (created=false).
func (r *Repo) tryInsertTransaction(ctx context.Context, tx pgx.Tx, kind TransactionKind, idempotencyKey string, metadata any) (t LedgerTransaction, created bool, err error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return LedgerTransaction{}, false, err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO ledger_transactions (kind, idempotency_key, metadata) VALUES ($1, $2, $3)
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING id, kind, idempotency_key, metadata, created_at`,
		kind, idempotencyKey, metaJSON,
	).Scan(&t.ID, &t.Kind, &t.IdempotencyKey, &t.Metadata, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LedgerTransaction{}, false, nil
		}
		return LedgerTransaction{}, false, err
	}
	return t, true, nil
}

func (r *Repo) getTransactionByIdempotencyKey(ctx context.Context, q querier, idempotencyKey string) (LedgerTransaction, error) {
	var t LedgerTransaction
	err := q.QueryRow(ctx,
		`SELECT id, kind, idempotency_key, metadata, created_at FROM ledger_transactions WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&t.ID, &t.Kind, &t.IdempotencyKey, &t.Metadata, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LedgerTransaction{}, ErrNotFound
		}
		return LedgerTransaction{}, err
	}
	return t, nil
}

func (r *Repo) insertPosting(ctx context.Context, tx pgx.Tx, transactionID, accountID uuid.UUID, amountCents int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO ledger_postings (transaction_id, account_id, amount_cents) VALUES ($1, $2, $3)`,
		transactionID, accountID, amountCents,
	)
	return err
}

// Topup is a SIMULATED top-up — there is no real payment gateway in the
// MVP. It moves amountCents from the funding_source system account into the
// commuter's wallet.
func (r *Repo) Topup(ctx context.Context, commuterUserID uuid.UUID, amountCents int64) (LedgerTransaction, int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return LedgerTransaction{}, 0, err
	}
	defer tx.Rollback(ctx)

	commuterAcc, err := r.GetOrCreateAccount(ctx, tx, &commuterUserID, AccountCommuterWallet)
	if err != nil {
		return LedgerTransaction{}, 0, err
	}
	fundingAcc, err := r.GetOrCreateAccount(ctx, tx, nil, AccountFundingSource)
	if err != nil {
		return LedgerTransaction{}, 0, err
	}

	txn, err := r.insertTransaction(ctx, tx, KindTopup, nil, map[string]any{
		"note": "simulated top-up — no real payment gateway in the MVP",
	})
	if err != nil {
		return LedgerTransaction{}, 0, err
	}

	if err := r.insertPosting(ctx, tx, txn.ID, fundingAcc.ID, -amountCents); err != nil {
		return LedgerTransaction{}, 0, err
	}
	if err := r.insertPosting(ctx, tx, txn.ID, commuterAcc.ID, amountCents); err != nil {
		return LedgerTransaction{}, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LedgerTransaction{}, 0, err
	}

	balance, err := r.AccountBalance(ctx, r.pool, commuterAcc.ID)
	if err != nil {
		return LedgerTransaction{}, 0, err
	}
	return txn, balance, nil
}

// InternalTransfer moves amountCents from one account type to another,
// both owned by the same user, as a single balanced ledger transaction.
// This is the generic primitive Stage 7's fuel withholding is built on:
// moving money from an owner's owner_revenue into their fuel_account is an
// internal transfer between that owner's own accounts, not new money and
// not a payout, so it reuses the exact same lock/read/post pattern
// ChargeFare uses rather than introducing a second way to move money.
func (r *Repo) InternalTransfer(ctx context.Context, ownerUserID uuid.UUID, fromType, toType AccountType, amountCents int64, kind TransactionKind, metadata map[string]any) (LedgerTransaction, error) {
	if amountCents <= 0 {
		return LedgerTransaction{}, ErrInvalidAmount
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return LedgerTransaction{}, err
	}
	defer tx.Rollback(ctx)

	fromAcc, err := r.GetOrCreateAccount(ctx, tx, &ownerUserID, fromType)
	if err != nil {
		return LedgerTransaction{}, err
	}

	// Lock the source account the same way ChargeFare locks the commuter's
	// wallet — this is what serializes two concurrent transfers out of the
	// same account.
	if err := r.lockAccount(ctx, tx, fromAcc.ID); err != nil {
		return LedgerTransaction{}, err
	}

	balance, err := r.AccountBalance(ctx, tx, fromAcc.ID)
	if err != nil {
		return LedgerTransaction{}, err
	}
	if balance < amountCents {
		return LedgerTransaction{}, ErrInsufficientFunds
	}

	toAcc, err := r.GetOrCreateAccount(ctx, tx, &ownerUserID, toType)
	if err != nil {
		return LedgerTransaction{}, err
	}

	txn, err := r.insertTransaction(ctx, tx, kind, nil, metadata)
	if err != nil {
		return LedgerTransaction{}, err
	}

	if err := r.insertPosting(ctx, tx, txn.ID, fromAcc.ID, -amountCents); err != nil {
		return LedgerTransaction{}, err
	}
	if err := r.insertPosting(ctx, tx, txn.ID, toAcc.ID, amountCents); err != nil {
		return LedgerTransaction{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LedgerTransaction{}, err
	}

	return txn, nil
}

// FareSplit is the resolved cent breakdown of a single fare charge.
type FareSplit struct {
	PlatformCents int64
	DriverCents   int64
	OwnerCents    int64
}

// splitFare divides fareCents by percentage, rounding platform and driver
// shares down and assigning the remainder to owner so nothing is lost or
// invented; platformPct+driverPct+ownerPct is expected to be 100.
func splitFare(fareCents int64, platformPct, driverPct int) FareSplit {
	platform := fareCents * int64(platformPct) / 100
	driver := fareCents * int64(driverPct) / 100
	owner := fareCents - platform - driver
	return FareSplit{PlatformCents: platform, DriverCents: driver, OwnerCents: owner}
}

// ChargeFare debits the commuter's wallet by fareCents and credits the
// driver/owner/platform accounts derived from vehicleID's active
// assignment, splitting the fare per platformPct/driverPct (owner gets the
// remainder). If idempotencyKey has already been used, no new postings are
// made and the original transaction is returned with replayed=true.
func (r *Repo) ChargeFare(ctx context.Context, commuterUserID, vehicleID uuid.UUID, fareCents int64, idempotencyKey string, platformPct, driverPct int) (txn LedgerTransaction, split FareSplit, replayed bool, err error) {
	if idempotencyKey == "" {
		return LedgerTransaction{}, FareSplit{}, false, ErrIdempotencyRequired
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	defer tx.Rollback(ctx)

	var ownerUserID, driverUserID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT v.owner_user_id, d.user_id
		 FROM vehicles v
		 JOIN vehicle_assignments va ON va.vehicle_id = v.id AND va.active
		 JOIN drivers d ON d.id = va.driver_id
		 WHERE v.id = $1`,
		vehicleID,
	).Scan(&ownerUserID, &driverUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LedgerTransaction{}, FareSplit{}, false, ErrNoActiveDriver
		}
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	split = splitFare(fareCents, platformPct, driverPct)

	txn, created, err := r.tryInsertTransaction(ctx, tx, KindFare, idempotencyKey, map[string]any{
		"vehicle_id":     vehicleID,
		"commuter_id":    commuterUserID,
		"driver_user_id": driverUserID,
		"owner_user_id":  ownerUserID,
		"fare_cents":     fareCents,
		"platform_cents": split.PlatformCents,
		"driver_cents":   split.DriverCents,
		"owner_cents":    split.OwnerCents,
	})
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	if !created {
		// Another transaction already used this idempotency key — return it
		// as-is and make no new postings. Nothing was changed by this call,
		// so the deferred rollback below is a no-op.
		existing, err := r.getTransactionByIdempotencyKey(ctx, r.pool, idempotencyKey)
		if err != nil {
			return LedgerTransaction{}, FareSplit{}, false, err
		}
		return existing, split, true, nil
	}

	commuterAcc, err := r.GetOrCreateAccount(ctx, tx, &commuterUserID, AccountCommuterWallet)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	// Lock the commuter's account row so a concurrent charge against the
	// same wallet blocks here until this transaction commits or rolls
	// back — this is what prevents two concurrent charges from both
	// reading a stale balance and double-spending it.
	if err := r.lockAccount(ctx, tx, commuterAcc.ID); err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	balance, err := r.AccountBalance(ctx, tx, commuterAcc.ID)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	if balance < fareCents {
		return LedgerTransaction{}, FareSplit{}, false, ErrInsufficientFunds
	}

	driverAcc, err := r.GetOrCreateAccount(ctx, tx, &driverUserID, AccountDriverEarnings)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	ownerAcc, err := r.GetOrCreateAccount(ctx, tx, &ownerUserID, AccountOwnerRevenue)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	platformAcc, err := r.GetOrCreateAccount(ctx, tx, nil, AccountPlatformFee)
	if err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	if err := r.insertPosting(ctx, tx, txn.ID, commuterAcc.ID, -fareCents); err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	if err := r.insertPosting(ctx, tx, txn.ID, driverAcc.ID, split.DriverCents); err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	if err := r.insertPosting(ctx, tx, txn.ID, ownerAcc.ID, split.OwnerCents); err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}
	if err := r.insertPosting(ctx, tx, txn.ID, platformAcc.ID, split.PlatformCents); err != nil {
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			// Lost the race on idempotency_key to a concurrent replay.
			existing, getErr := r.getTransactionByIdempotencyKey(ctx, r.pool, idempotencyKey)
			if getErr != nil {
				return LedgerTransaction{}, FareSplit{}, false, getErr
			}
			return existing, split, true, nil
		}
		return LedgerTransaction{}, FareSplit{}, false, err
	}

	return txn, split, false, nil
}

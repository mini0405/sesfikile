package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/fuel"
	"sesfikile/backend/internal/wallet"
)

// Repo answers owner-analytics reads. It holds no state of its own — every
// method is a query (or a small set of queries) over Stage 2's ledger
// tables, Stage 7's quota/authorization tables, or delegates to
// wallet.Repo/fuel.Repo for the pieces those packages already expose
// correctly (e.g. fuel.Repo.Balance for the CURRENT, non-range-bound
// fuel_account balance).
type Repo struct {
	pool       *pgxpool.Pool
	walletRepo *wallet.Repo
	fuelRepo   *fuel.Repo
}

func NewRepo(pool *pgxpool.Pool, walletRepo *wallet.Repo, fuelRepo *fuel.Repo) *Repo {
	return &Repo{pool: pool, walletRepo: walletRepo, fuelRepo: fuelRepo}
}

// fareTotals is the [trips, revenue_cents] pair produced by summing
// owner_revenue credits for kind='fare' postings — the single query every
// trip count and revenue figure in this package traces back to.
type fareTotals struct {
	Trips        int64
	RevenueCents int64
}

func (r *Repo) fareTotalsForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (fareTotals, error) {
	var t fareTotals
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT lt.id), COALESCE(SUM(lp.amount_cents), 0)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'
		   AND lt.created_at >= $2 AND lt.created_at < $3`,
		ownerUserID, from, to,
	).Scan(&t.Trips, &t.RevenueCents)
	return t, err
}

func (r *Repo) accountTypeTotalForOwnerFares(ctx context.Context, accountType wallet.AccountType, ownerUserID uuid.UUID, from, to time.Time) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(lp.amount_cents), 0)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = $1 AND lt.kind = 'fare' AND lt.metadata->>'owner_user_id' = $2
		   AND lt.created_at >= $3 AND lt.created_at < $4`,
		accountType, ownerUserID.String(), from, to,
	).Scan(&total)
	return total, err
}

func (r *Repo) fuelAllocatedForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(lp.amount_cents), 0)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'fuel_account' AND a.owner_user_id = $1 AND lt.kind = 'fuel_allocation'
		   AND lp.amount_cents > 0 AND lt.created_at >= $2 AND lt.created_at < $3`,
		ownerUserID, from, to,
	).Scan(&total)
	return total, err
}

func (r *Repo) fuelConsumedForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(fa.amount_cents), 0)
		 FROM fuel_authorizations fa
		 JOIN vehicles v ON v.id = fa.vehicle_id
		 WHERE v.owner_user_id = $1 AND fa.status = 'confirmed'
		   AND fa.confirmed_at >= $2 AND fa.confirmed_at < $3`,
		ownerUserID, from, to,
	).Scan(&total)
	return total, err
}

// Summary computes GET /owner/summary. Every cents figure except
// FuelBalanceCents (the CURRENT fuel_account balance, deliberately not
// range-bound — see the handler) is scoped to [from, to).
func (r *Repo) Summary(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (Summary, error) {
	fares, err := r.fareTotalsForOwner(ctx, ownerUserID, from, to)
	if err != nil {
		return Summary{}, err
	}
	platformFees, err := r.accountTypeTotalForOwnerFares(ctx, wallet.AccountPlatformFee, ownerUserID, from, to)
	if err != nil {
		return Summary{}, err
	}
	driverEarnings, err := r.accountTypeTotalForOwnerFares(ctx, wallet.AccountDriverEarnings, ownerUserID, from, to)
	if err != nil {
		return Summary{}, err
	}
	fuelAllocated, err := r.fuelAllocatedForOwner(ctx, ownerUserID, from, to)
	if err != nil {
		return Summary{}, err
	}
	fuelBalance, err := r.fuelRepo.Balance(ctx, ownerUserID)
	if err != nil {
		return Summary{}, err
	}

	return Summary{
		From:                from,
		To:                  to,
		RevenueCents:        fares.RevenueCents,
		Trips:               fares.Trips,
		PassengerVolume:     fares.Trips, // 1 fare = 1 boarding = 1 passenger in this MVP
		PlatformFeesCents:   platformFees,
		DriverEarningsCents: driverEarnings,
		FuelBalanceCents:    fuelBalance,
		FuelAllocatedCents:  fuelAllocated,
	}, nil
}

// vehicleFareStat is the per-vehicle trip/revenue pair.
type vehicleFareStat struct {
	Trips        int64
	RevenueCents int64
}

// VehicleStatsForOwner groups fare postings by the vehicle_id carried in
// each fare transaction's metadata (set once, at charge time, by
// wallet.Repo.ChargeFare — see CLAUDE.md/PROGRESS.md Stage 2), so a single
// grouped query answers every vehicle's trips/revenue instead of one query
// per vehicle.
func (r *Repo) VehicleStatsForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (map[uuid.UUID]vehicleFareStat, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT (lt.metadata->>'vehicle_id')::uuid AS vehicle_id, COUNT(*) AS trips, SUM(lp.amount_cents) AS revenue_cents
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'
		   AND lt.created_at >= $2 AND lt.created_at < $3
		 GROUP BY vehicle_id`,
		ownerUserID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]vehicleFareStat)
	for rows.Next() {
		var vehicleID uuid.UUID
		var stat vehicleFareStat
		if err := rows.Scan(&vehicleID, &stat.Trips, &stat.RevenueCents); err != nil {
			return nil, err
		}
		result[vehicleID] = stat
	}
	return result, rows.Err()
}

// driverFareStat is the per-driver trip/earnings pair.
type driverFareStat struct {
	Trips         int64
	EarningsCents int64
}

// DriverStatsForOwner groups driver_earnings postings by the driver_user_id
// carried in each fare transaction's metadata, scoped to this owner via
// metadata->>'owner_user_id' (driver_earnings accounts belong to the driver,
// not the owner, so they can't be scoped via account ownership the way
// owner_revenue/fuel_account are).
func (r *Repo) DriverStatsForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) (map[uuid.UUID]driverFareStat, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT (lt.metadata->>'driver_user_id')::uuid AS driver_user_id, COUNT(*) AS trips, SUM(lp.amount_cents) AS earnings_cents
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'driver_earnings' AND lt.kind = 'fare' AND lt.metadata->>'owner_user_id' = $1
		   AND lt.created_at >= $2 AND lt.created_at < $3
		 GROUP BY driver_user_id`,
		ownerUserID.String(), from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]driverFareStat)
	for rows.Next() {
		var driverUserID uuid.UUID
		var stat driverFareStat
		if err := rows.Scan(&driverUserID, &stat.Trips, &stat.EarningsCents); err != nil {
			return nil, err
		}
		result[driverUserID] = stat
	}
	return result, rows.Err()
}

// dailyBucket is one (day, amount) pair from a GROUP BY date_trunc query.
type dailyBucket struct {
	Day         time.Time
	AmountCents int64
}

func (r *Repo) revenueSeriesForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) ([]dailyBucket, error) {
	return r.dailySeries(ctx,
		`SELECT date_trunc('day', lt.created_at AT TIME ZONE 'Africa/Johannesburg') AS day, SUM(lp.amount_cents)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'
		   AND lt.created_at >= $2 AND lt.created_at < $3
		 GROUP BY day`,
		ownerUserID, from, to)
}

func (r *Repo) fuelAllocatedSeriesForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) ([]dailyBucket, error) {
	return r.dailySeries(ctx,
		`SELECT date_trunc('day', lt.created_at AT TIME ZONE 'Africa/Johannesburg') AS day, SUM(lp.amount_cents)
		 FROM ledger_postings lp
		 JOIN ledger_transactions lt ON lt.id = lp.transaction_id
		 JOIN accounts a ON a.id = lp.account_id
		 WHERE a.type = 'fuel_account' AND a.owner_user_id = $1 AND lt.kind = 'fuel_allocation' AND lp.amount_cents > 0
		   AND lt.created_at >= $2 AND lt.created_at < $3
		 GROUP BY day`,
		ownerUserID, from, to)
}

// fuelConsumedSeriesForOwner buckets CONFIRMED fuel_authorizations, not
// ledger postings — see the package doc comment on why fuel consumption is
// the one figure here that isn't ledger-derived.
func (r *Repo) fuelConsumedSeriesForOwner(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time) ([]dailyBucket, error) {
	return r.dailySeries(ctx,
		`SELECT date_trunc('day', fa.confirmed_at AT TIME ZONE 'Africa/Johannesburg') AS day, SUM(fa.amount_cents)
		 FROM fuel_authorizations fa
		 JOIN vehicles v ON v.id = fa.vehicle_id
		 WHERE v.owner_user_id = $1 AND fa.status = 'confirmed'
		   AND fa.confirmed_at >= $2 AND fa.confirmed_at < $3
		 GROUP BY day`,
		ownerUserID, from, to)
}

func (r *Repo) dailySeries(ctx context.Context, sql string, ownerUserID uuid.UUID, from, to time.Time) ([]dailyBucket, error) {
	rows, err := r.pool.Query(ctx, sql, ownerUserID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []dailyBucket
	for rows.Next() {
		var b dailyBucket
		if err := rows.Scan(&b.Day, &b.AmountCents); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// Ledger runs the union query behind GET /owner/ledger — see the handler
// for the shape it returns and models.go's LedgerEntry doc comment for what
// each entry_type means.
func (r *Repo) Ledger(ctx context.Context, ownerUserID uuid.UUID, from, to time.Time, limit, offset int) ([]LedgerEntry, int64, error) {
	const cte = `
		WITH fare_entries AS (
			SELECT lt.id AS id, 'fare'::text AS entry_type, lt.created_at AS occurred_at,
			       lp.amount_cents AS amount_cents,
			       NULLIF(lt.metadata->>'vehicle_id', '')::uuid AS vehicle_id,
			       lt.metadata AS detail
			FROM ledger_postings lp
			JOIN ledger_transactions lt ON lt.id = lp.transaction_id
			JOIN accounts a ON a.id = lp.account_id
			WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'
		),
		allocation_entries AS (
			SELECT lt.id AS id, 'fuel_allocation'::text AS entry_type, lt.created_at AS occurred_at,
			       lp.amount_cents AS amount_cents,
			       NULL::uuid AS vehicle_id,
			       lt.metadata AS detail
			FROM ledger_postings lp
			JOIN ledger_transactions lt ON lt.id = lp.transaction_id
			JOIN accounts a ON a.id = lp.account_id
			WHERE a.type = 'fuel_account' AND a.owner_user_id = $1 AND lt.kind = 'fuel_allocation' AND lp.amount_cents > 0
		),
		authorization_entries AS (
			SELECT fa.id AS id, 'fuel_authorization'::text AS entry_type,
			       COALESCE(fa.confirmed_at, fa.created_at) AS occurred_at,
			       fa.amount_cents AS amount_cents,
			       fa.vehicle_id AS vehicle_id,
			       jsonb_build_object('status', fa.status, 'litres', fa.litres) AS detail
			FROM fuel_authorizations fa
			JOIN vehicles v ON v.id = fa.vehicle_id
			WHERE v.owner_user_id = $1
		),
		combined AS (
			SELECT * FROM fare_entries
			UNION ALL SELECT * FROM allocation_entries
			UNION ALL SELECT * FROM authorization_entries
		)
		SELECT id, entry_type, occurred_at, amount_cents, vehicle_id, detail
		FROM combined
		WHERE occurred_at >= $2 AND occurred_at < $3
		ORDER BY occurred_at DESC
		LIMIT $4 OFFSET $5`

	rows, err := r.pool.Query(ctx, cte, ownerUserID, from, to, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.EntryType, &e.OccurredAt, &e.AmountCents, &e.VehicleID, &e.Detail); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	const countCTE = `
		WITH fare_entries AS (
			SELECT lt.id AS id, lt.created_at AS occurred_at
			FROM ledger_postings lp
			JOIN ledger_transactions lt ON lt.id = lp.transaction_id
			JOIN accounts a ON a.id = lp.account_id
			WHERE a.type = 'owner_revenue' AND a.owner_user_id = $1 AND lt.kind = 'fare'
		),
		allocation_entries AS (
			SELECT lt.id AS id, lt.created_at AS occurred_at
			FROM ledger_postings lp
			JOIN ledger_transactions lt ON lt.id = lp.transaction_id
			JOIN accounts a ON a.id = lp.account_id
			WHERE a.type = 'fuel_account' AND a.owner_user_id = $1 AND lt.kind = 'fuel_allocation' AND lp.amount_cents > 0
		),
		authorization_entries AS (
			SELECT fa.id AS id, COALESCE(fa.confirmed_at, fa.created_at) AS occurred_at
			FROM fuel_authorizations fa
			JOIN vehicles v ON v.id = fa.vehicle_id
			WHERE v.owner_user_id = $1
		),
		combined AS (
			SELECT * FROM fare_entries
			UNION ALL SELECT * FROM allocation_entries
			UNION ALL SELECT * FROM authorization_entries
		)
		SELECT COUNT(*) FROM combined WHERE occurred_at >= $2 AND occurred_at < $3`

	var total int64
	if err := r.pool.QueryRow(ctx, countCTE, ownerUserID, from, to).Scan(&total); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

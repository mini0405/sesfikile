// This file is a SIMULATION of the eFuel / FuelOmat / VIU (vehicle
// integration unit) hardware boundary. THERE IS NO REAL PUMP OR DEVICE
// BEHIND ANY OF THIS. In a real deployment, a physical VIU fitted to the
// vehicle would call an endpoint shaped like AuthorizePump before letting
// the pump dispense, and call something shaped like ConfirmPump once the
// nozzle actually delivered fuel — this file's job is to stand in for that
// device's half of the conversation so the rest of the system (quota
// accounting, HTTP handlers) can be built and tested against a realistic
// shape today. Swapping this file (and its two handlers in handlers.go)
// for a real hardware integration should not require touching
// repo.go/models.go — quota accounting doesn't know or care whether the
// request came from a simulator or a real VIU.
package fuel

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AuthResult is AuthorizePump's outcome — mirrors what a real FuelOmat/VIU
// device would need back from an authorize call: whether it may dispense,
// a reference to settle later, and the cap it was authorized for.
type AuthResult struct {
	Authorized     bool
	Reason         string
	AuthReference  uuid.UUID
	MaxAmountCents int64
}

// AuthorizePump is the MOCK VIU/pump requesting authorization to dispense
// litres worth amountCents. It checks the vehicle's fuel quota (real
// accounting, see repo.go) and, if there's enough available, RESERVES
// (does not yet finally debit) amountCents against it and hands back an
// auth_reference — exactly the "may I dispense, and how much" exchange a
// real VIU would need before opening the nozzle. If the quota can't cover
// it, it DENIES with a reason and the actual available amount, rather than
// reserving anything.
func (r *Repo) AuthorizePump(ctx context.Context, vehicleID uuid.UUID, litres float64, amountCents int64) (AuthResult, error) {
	if amountCents <= 0 {
		return AuthResult{}, ErrInvalidAmount
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, err
	}
	defer tx.Rollback(ctx)

	var quotaCents, reservedCents, usedCents int64
	err = tx.QueryRow(ctx,
		`SELECT quota_cents, reserved_cents, used_cents FROM vehicle_fuel_quotas WHERE vehicle_id = $1 FOR UPDATE`,
		vehicleID,
	).Scan(&quotaCents, &reservedCents, &usedCents)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthResult{Authorized: false, Reason: "no fuel quota allocated for this vehicle"}, nil
		}
		return AuthResult{}, err
	}

	available := quotaCents - reservedCents - usedCents
	if amountCents > available {
		return AuthResult{Authorized: false, Reason: "requested amount exceeds available fuel quota", MaxAmountCents: available}, nil
	}

	var authID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO fuel_authorizations (vehicle_id, litres, amount_cents, status)
		 VALUES ($1, $2, $3, 'reserved') RETURNING id`,
		vehicleID, litres, amountCents,
	).Scan(&authID); err != nil {
		return AuthResult{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE vehicle_fuel_quotas SET reserved_cents = reserved_cents + $2, updated_at = now() WHERE vehicle_id = $1`,
		vehicleID, amountCents,
	); err != nil {
		return AuthResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return AuthResult{}, err
	}

	return AuthResult{Authorized: true, AuthReference: authID, MaxAmountCents: amountCents}, nil
}

// ConfirmResult is ConfirmPump's outcome.
type ConfirmResult struct {
	AlreadyConfirmed bool
	VehicleID        uuid.UUID
	AmountCents      int64
}

// ConfirmPump is the MOCK VIU/pump confirming a dispense actually happened
// for authReference — the second half of the authorize-then-confirm
// pattern a real fuel-dispensing device would follow. This finalizes the
// quota debit: reserved_cents moves to used_cents, a genuine (if simulated)
// consumption of the vehicle's earmarked fuel money. A second confirm of
// the same reference is a no-op — it returns the original result with
// AlreadyConfirmed=true rather than debiting a second time, so a retried
// or duplicated device callback can never double-spend a quota.
//
// TODO (MVP scope limit, per the stage brief): a reservation that never
// gets confirmed (the pump session times out, the device loses power, the
// commuter drives off mid-fill) stays "reserved" forever here — a real
// system would need a background sweep that releases stale reservations
// back to available quota after some timeout. Not implemented in this MVP.
func (r *Repo) ConfirmPump(ctx context.Context, authReference uuid.UUID) (ConfirmResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ConfirmResult{}, err
	}
	defer tx.Rollback(ctx)

	var vehicleID uuid.UUID
	var amountCents int64
	var status AuthorizationStatus
	err = tx.QueryRow(ctx,
		`SELECT vehicle_id, amount_cents, status FROM fuel_authorizations WHERE id = $1 FOR UPDATE`,
		authReference,
	).Scan(&vehicleID, &amountCents, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ConfirmResult{}, ErrNotFound
		}
		return ConfirmResult{}, err
	}

	if status == StatusConfirmed {
		return ConfirmResult{AlreadyConfirmed: true, VehicleID: vehicleID, AmountCents: amountCents}, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE vehicle_fuel_quotas SET reserved_cents = reserved_cents - $2, used_cents = used_cents + $2, updated_at = now() WHERE vehicle_id = $1`,
		vehicleID, amountCents,
	); err != nil {
		return ConfirmResult{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE fuel_authorizations SET status = 'confirmed', confirmed_at = now() WHERE id = $1`,
		authReference,
	); err != nil {
		return ConfirmResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ConfirmResult{}, err
	}

	return ConfirmResult{AlreadyConfirmed: false, VehicleID: vehicleID, AmountCents: amountCents}, nil
}

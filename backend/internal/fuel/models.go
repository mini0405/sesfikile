// Package fuel implements Stage 7: fuel disbursement.
//
// SCOPE HONESTY (per CLAUDE.md): the eFuel / FuelOmat / VIU hardware this
// package's "VIU" endpoints imitate DOES NOT EXIST here. Everything in
// viu_mock.go is a simulation of the boundary a real pump/vehicle unit
// integration would sit behind — see that file's doc comment for exactly
// what a real integration would replace.
//
// The fuel WITHHOLDING path (Allocate) is the one part of this package that
// is REAL: it moves real money through Stage 2's double-entry ledger
// (wallet.Repo.InternalTransfer), from an owner's owner_revenue account
// into their fuel_account. Nothing here creates money — it only ever moves
// an owner's own money between their own accounts, and — see the anti-
// bypass note in repo.go — fuel_account/vehicle quota money can only ever
// flow toward a fuel authorization, never back out to a wallet or payout.
package fuel

import (
	"time"

	"github.com/google/uuid"
)

// AuthorizationStatus mirrors the fuel_authorization_status Postgres enum.
type AuthorizationStatus string

const (
	StatusReserved  AuthorizationStatus = "reserved"
	StatusConfirmed AuthorizationStatus = "confirmed"
)

// VehicleQuota is a vehicle's earmarked slice of its owner's fuel_account
// balance. Available-to-authorize is always QuotaCents - ReservedCents -
// UsedCents (also enforced by a DB CHECK constraint, not just this struct).
type VehicleQuota struct {
	VehicleID     uuid.UUID
	OwnerUserID   uuid.UUID
	QuotaCents    int64
	ReservedCents int64
	UsedCents     int64
	UpdatedAt     time.Time
}

func (q VehicleQuota) AvailableCents() int64 {
	return q.QuotaCents - q.ReservedCents - q.UsedCents
}

// Authorization is one MOCK VIU authorize-then-confirm pump session. Its ID
// is the auth_reference a real device would carry from authorize through to
// confirm.
type Authorization struct {
	ID          uuid.UUID
	VehicleID   uuid.UUID
	Litres      float64
	AmountCents int64
	Status      AuthorizationStatus
	CreatedAt   time.Time
	ConfirmedAt *time.Time
}

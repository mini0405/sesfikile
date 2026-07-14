package wallet

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AccountType string

const (
	AccountCommuterWallet AccountType = "commuter_wallet"
	AccountDriverEarnings AccountType = "driver_earnings"
	AccountOwnerRevenue   AccountType = "owner_revenue"
	AccountPlatformFee    AccountType = "platform_fee"
	AccountFundingSource  AccountType = "funding_source"
	// AccountFuelAccount holds money REAL-ledger-withheld from an owner's
	// revenue for fuel (Stage 7). See internal/fuel — the account type
	// itself is plain ledger accounting; only the pump/VIU on the far side
	// of it is mocked.
	AccountFuelAccount AccountType = "fuel_account"
)

type TransactionKind string

const (
	KindTopup TransactionKind = "topup"
	KindFare  TransactionKind = "fare"
	// KindFuelAllocation is an internal transfer from an owner's
	// owner_revenue account into their fuel_account (Stage 7) — no money is
	// created, it only moves between the owner's own accounts.
	KindFuelAllocation TransactionKind = "fuel_allocation"
)

// Account. OwnerUserID is nil for the system accounts (platform_fee,
// funding_source).
type Account struct {
	ID          uuid.UUID
	OwnerUserID *uuid.UUID
	Type        AccountType
	CreatedAt   time.Time
}

// LedgerTransaction. IdempotencyKey is nil for topups; fare charges always
// carry one for replay protection.
type LedgerTransaction struct {
	ID             uuid.UUID
	Kind           TransactionKind
	IdempotencyKey *string
	Metadata       json.RawMessage
	CreatedAt      time.Time
}

// LedgerPosting. AmountCents is signed: negative = debit, positive = credit.
// A transaction's postings must always sum to zero (enforced by a DB
// trigger — see migrations/000002).
type LedgerPosting struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	AccountID     uuid.UUID
	AmountCents   int64
	CreatedAt     time.Time
}

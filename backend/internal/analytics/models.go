// Package analytics implements Stage 8: read-side owner analytics.
//
// SCOPE HONESTY (per CLAUDE.md and the stage brief): this package adds no
// new money mechanics and no new persistence for money. Every monetary
// figure it reports is derived live from Stage 2's ledger_postings — SUM
// over postings, the same derivation every other stage already uses for a
// balance — never from a separate counter or cached tally. Trip/passenger
// counts come from counting real fare transactions on the ledger, not an
// incremented counter anywhere. There is exactly one source of truth: the
// ledger.
//
// The one deliberate exception is "fuel consumed": Stage 7 structurally
// keeps quota consumption OFF the ledger (funding a vehicle's quota,
// authorizing, and confirming a pump session post zero new ledger_postings
// rows — see internal/fuel's anti-bypass property). Consumption therefore
// cannot be read from ledger_postings by construction; it is read from
// fuel_authorizations (status='confirmed'), which is Stage 7's own real
// record of settled pump sessions. This is called out explicitly wherever
// it applies, and revenue/fuel-allocated figures nearby stay ledger-derived
// exactly like everything else.
//
// There are no persisted historical snapshots or GPS tracks yet: monetary/
// trip figures are computed live from timestamped ledger postings (accurate
// for any date range), while live fleet status reflects Stage 4's current
// in-memory telemetry state (right now), not a recorded timeline. A vehicle
// that was online yesterday but is offline right now shows offline — there
// is no historical position/online-status log to query instead.
package analytics

import (
	"time"

	"github.com/google/uuid"
)

// Summary is the response for GET /owner/summary.
type Summary struct {
	From                time.Time `json:"from"`
	To                  time.Time `json:"to"`
	RevenueCents        int64     `json:"revenue_cents"`
	Trips               int64     `json:"trips"`
	PassengerVolume     int64     `json:"passenger_volume"`
	PlatformFeesCents   int64     `json:"platform_fees_cents"`
	DriverEarningsCents int64     `json:"driver_earnings_cents"`
	FuelBalanceCents    int64     `json:"fuel_balance_cents"`
	FuelAllocatedCents  int64     `json:"fuel_allocated_cents"`
}

// VehicleStat is one owner-scoped vehicle's activity for a date range,
// derived from ledger_postings (trips/revenue) and Stage 4's live telemetry
// (status) and Stage 7's quota table (fuel).
type VehicleStat struct {
	VehicleID          uuid.UUID  `json:"vehicle_id"`
	Registration       string     `json:"registration"`
	SeatsTotal         int        `json:"seats_total"`
	AssignedDriverID   *uuid.UUID `json:"assigned_driver_id,omitempty"`
	AssignedDriver     *string    `json:"assigned_driver_name,omitempty"`
	Online             bool       `json:"online"`
	CurrentRouteID     *uuid.UUID `json:"current_route_id,omitempty"`
	CurrentRouteName   *string    `json:"current_route_name,omitempty"`
	SeatsAvailable     *int       `json:"seats_available,omitempty"`
	Trips              int64      `json:"trips"`
	RevenueCents       int64      `json:"revenue_cents"`
	FuelQuotaCents     int64      `json:"fuel_quota_cents"`
	FuelReservedCents  int64      `json:"fuel_reserved_cents"`
	FuelUsedCents      int64      `json:"fuel_used_cents"`
	FuelAvailableCents int64      `json:"fuel_available_cents"`
}

// DriverStat is one owner-scoped driver's activity for a date range.
type DriverStat struct {
	DriverID        uuid.UUID `json:"driver_id"`
	FullName        string    `json:"full_name"`
	AssignedVehicle *string   `json:"assigned_vehicle_registration,omitempty"`
	Online          bool      `json:"online"`
	Trips           int64     `json:"trips"`
	EarningsCents   int64     `json:"earnings_cents"`
}

// RevenueVsFuelDay is one day's bucket in the revenue-vs-fuel series.
type RevenueVsFuelDay struct {
	Date               string `json:"date"`
	RevenueCents       int64  `json:"revenue_cents"`
	FuelAllocatedCents int64  `json:"fuel_allocated_cents"`
	FuelConsumedCents  int64  `json:"fuel_consumed_cents"`
}

// RevenueVsFuel is the response for GET /owner/revenue-vs-fuel.
type RevenueVsFuel struct {
	From               time.Time          `json:"from"`
	To                 time.Time          `json:"to"`
	RevenueCents       int64              `json:"revenue_cents"`
	FuelAllocatedCents int64              `json:"fuel_allocated_cents"`
	FuelConsumedCents  int64              `json:"fuel_consumed_cents"`
	Series             []RevenueVsFuelDay `json:"series"`
}

// LedgerEntry is one readable, owner-scoped row in GET /owner/ledger. It
// merges three real sources on the owner's own money: fare transactions
// crediting owner_revenue, fuel_allocation transfers into fuel_account (both
// real ledger_postings), and fuel_authorizations (Stage 7's mock-VIU
// records, which are deliberately NOT ledger postings — see the package doc
// comment) — tagged by EntryType so the split stays visible to the owner.
type LedgerEntry struct {
	ID          uuid.UUID  `json:"id"`
	EntryType   string     `json:"entry_type"`
	OccurredAt  time.Time  `json:"occurred_at"`
	AmountCents int64      `json:"amount_cents"`
	VehicleID   *uuid.UUID `json:"vehicle_id,omitempty"`
	Detail      any        `json:"detail,omitempty"`
}

// LedgerPage is the paginated response for GET /owner/ledger.
type LedgerPage struct {
	Entries []LedgerEntry `json:"entries"`
	Total   int64         `json:"total"`
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
}

-- Stage 7: fuel disbursement.
--
-- SCOPE HONESTY (per CLAUDE.md): there is no real eFuel / FuelOmat / VIU
-- device anywhere in this schema or the internal/fuel module built on top
-- of it. The fuel WITHHOLDING below (owner_revenue -> fuel_account) is REAL
-- double-entry ledger accounting, reusing Stage 2's accounts/
-- ledger_transactions/ledger_postings tables and its zero-sum trigger. The
-- vehicle_fuel_quotas/fuel_authorizations tables model an authorize-then-
-- confirm pump session the way a real FuelOmat/VIU integration would need
-- to, but nothing on the other end of fuel_authorizations is a physical
-- device — see internal/fuel/viu_mock.go for exactly where a real
-- integration would attach.

-- New account type for the money real ledger accounting withholds toward
-- fuel. New transaction kind for the withholding transfer itself. Both are
-- additive (ALTER TYPE ... ADD VALUE) so Stage 2's existing account_type/
-- transaction_kind columns and trigger need no changes.
ALTER TYPE account_type ADD VALUE 'fuel_account';
ALTER TYPE transaction_kind ADD VALUE 'fuel_allocation';

-- Per-vehicle fuel quota: how much of the owner's fuel_account has been
-- earmarked to a specific vehicle for pump authorizations to draw against.
-- This is deliberately a plain table, not a second ledger account per
-- vehicle (the stage brief explicitly allows either) — the real money
-- movement already happened when it was withheld into fuel_account
-- (fuel_allocation, above); earmarking a slice of that balance to one
-- vehicle doesn't cross a ledger account boundary, it just tracks how much
-- of the owner's already-withheld fuel_account balance is committed to
-- which vehicle. quota_cents is the cumulative amount ever earmarked;
-- reserved_cents is currently held by in-flight VIU authorizations;
-- used_cents is confirmed/settled pump sessions. Available-to-authorize is
-- always quota_cents - reserved_cents - used_cents, enforced by the CHECK
-- below rather than trusted to application code.
CREATE TABLE vehicle_fuel_quotas (
    vehicle_id     UUID PRIMARY KEY REFERENCES vehicles(id),
    owner_user_id  UUID NOT NULL REFERENCES users(id),
    quota_cents    BIGINT NOT NULL DEFAULT 0 CHECK (quota_cents >= 0),
    reserved_cents BIGINT NOT NULL DEFAULT 0 CHECK (reserved_cents >= 0),
    used_cents     BIGINT NOT NULL DEFAULT 0 CHECK (used_cents >= 0),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (reserved_cents + used_cents <= quota_cents)
);

CREATE TYPE fuel_authorization_status AS ENUM ('reserved', 'confirmed');

-- MOCK VIU/pump authorization records. auth_reference (this row's id) is
-- what a real FuelOmat/VIU device would receive from /fuel/viu/authorize
-- and later present back to /fuel/viu/confirm to settle the pump session.
-- There is no physical device on the other end of this table in the MVP —
-- see internal/fuel/viu_mock.go.
CREATE TABLE fuel_authorizations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id   UUID NOT NULL REFERENCES vehicles(id),
    litres       NUMERIC(10,3) NOT NULL CHECK (litres > 0),
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    status       fuel_authorization_status NOT NULL DEFAULT 'reserved',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ
);

CREATE INDEX fuel_authorizations_vehicle_id_idx ON fuel_authorizations (vehicle_id);

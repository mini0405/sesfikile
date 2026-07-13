-- Stage 2: wallet + double-entry ledger.
--
-- Sign convention: within a ledger_posting, amount_cents is signed —
-- negative = debit (money leaving an account), positive = credit (money
-- entering an account). Every ledger_transaction's postings must sum to
-- exactly zero; this is enforced below with a deferred constraint trigger
-- so it is checked once per statement/transaction, not after every single
-- row insert.
CREATE TYPE account_type AS ENUM (
    'commuter_wallet',
    'driver_earnings',
    'owner_revenue',
    'platform_fee',
    'funding_source'
);

CREATE TYPE transaction_kind AS ENUM ('topup', 'fare');

-- owner_user_id is NULL for system accounts (platform_fee, funding_source);
-- every other account type belongs to exactly one user.
CREATE TABLE accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID REFERENCES users(id),
    type          account_type NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one account per (user, type), and at most one system account per type.
CREATE UNIQUE INDEX accounts_owner_type_idx ON accounts (owner_user_id, type) WHERE owner_user_id IS NOT NULL;
CREATE UNIQUE INDEX accounts_system_type_idx ON accounts (type) WHERE owner_user_id IS NULL;

-- idempotency_key is only populated for operations that need replay
-- protection (fare charges); topups leave it NULL.
CREATE TABLE ledger_transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            transaction_kind NOT NULL,
    idempotency_key TEXT UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- An account's balance is ALWAYS derived as SUM(amount_cents) over its
-- postings — there is deliberately no stored balance column that could drift.
CREATE TABLE ledger_postings (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES ledger_transactions(id),
    account_id     UUID NOT NULL REFERENCES accounts(id),
    amount_cents   BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ledger_postings_transaction_id_idx ON ledger_postings (transaction_id);
CREATE INDEX ledger_postings_account_id_idx ON ledger_postings (account_id);

-- Hard invariant: a transaction's postings must sum to zero. Deferred so it
-- is only checked at COMMIT, once all of a transaction's postings have been
-- inserted inside the same DB transaction.
CREATE OR REPLACE FUNCTION check_ledger_transaction_balanced() RETURNS TRIGGER AS $$
DECLARE
    txn_id UUID;
    total  BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        txn_id := OLD.transaction_id;
    ELSE
        txn_id := NEW.transaction_id;
    END IF;

    SELECT COALESCE(SUM(amount_cents), 0) INTO total
    FROM ledger_postings WHERE transaction_id = txn_id;

    IF total <> 0 THEN
        RAISE EXCEPTION 'ledger_transaction % postings do not sum to zero (got %)', txn_id, total;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_postings_balanced
    AFTER INSERT OR UPDATE OR DELETE ON ledger_postings
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_ledger_transaction_balanced();

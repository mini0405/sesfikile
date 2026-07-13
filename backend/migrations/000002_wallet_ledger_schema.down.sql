DROP TRIGGER IF EXISTS ledger_postings_balanced ON ledger_postings;
DROP FUNCTION IF EXISTS check_ledger_transaction_balanced();

DROP TABLE IF EXISTS ledger_postings;
DROP TABLE IF EXISTS ledger_transactions;
DROP TABLE IF EXISTS accounts;

DROP TYPE IF EXISTS transaction_kind;
DROP TYPE IF EXISTS account_type;

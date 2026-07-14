DROP INDEX IF EXISTS fuel_authorizations_vehicle_id_idx;
DROP TABLE IF EXISTS fuel_authorizations;
DROP TYPE IF EXISTS fuel_authorization_status;
DROP TABLE IF EXISTS vehicle_fuel_quotas;

-- Postgres cannot drop a single enum value (account_type.fuel_account,
-- transaction_kind.fuel_allocation) without recreating the whole type and
-- rewriting every dependent column. Not attempted here for an MVP down
-- migration — the values are simply left in place, unused, if this
-- migration is rolled back.

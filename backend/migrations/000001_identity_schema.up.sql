-- Stage 1: identity — users, drivers, vehicles, vehicle_assignments.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE user_role AS ENUM ('commuter', 'driver', 'owner');
CREATE TYPE kyc_status AS ENUM ('pending', 'verified', 'rejected');
CREATE TYPE compliance_status AS ENUM ('pending', 'verified');

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone         TEXT NOT NULL UNIQUE,
    email         TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    role          user_role NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- prdp_verified / kyc_status are stored fields only for the MVP — no real
-- verification workflow is wired up yet (see CLAUDE.md "SCOPE HONESTY").
CREATE TABLE drivers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id),
    full_name      TEXT NOT NULL,
    prdp_number    TEXT NOT NULL,
    prdp_verified  BOOLEAN NOT NULL DEFAULT false,
    id_number      TEXT NOT NULL,
    kyc_status     kyc_status NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

CREATE TABLE vehicles (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id      UUID NOT NULL REFERENCES users(id),
    registration       TEXT NOT NULL UNIQUE,
    capacity           INT NOT NULL,
    association_name   TEXT,
    compliance_status  compliance_status NOT NULL DEFAULT 'pending',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE vehicle_assignments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id  UUID NOT NULL REFERENCES vehicles(id),
    driver_id   UUID NOT NULL REFERENCES drivers(id),
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A vehicle or driver can only be part of one active assignment at a time.
CREATE UNIQUE INDEX vehicle_assignments_active_vehicle_idx ON vehicle_assignments (vehicle_id) WHERE active;
CREATE UNIQUE INDEX vehicle_assignments_active_driver_idx ON vehicle_assignments (driver_id) WHERE active;

CREATE INDEX drivers_user_id_idx ON drivers (user_id);
CREATE INDEX vehicles_owner_user_id_idx ON vehicles (owner_user_id);

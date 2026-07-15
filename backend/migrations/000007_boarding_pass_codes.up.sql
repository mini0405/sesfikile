-- Short boarding codes (airline-style): a small, auditable, restart-safe
-- table that lets POST /boarding/scan resolve an 8-character handle back to
-- the existing HMAC-signed pass token (Stage 5) it stands in for. The signed
-- token stored here remains the canonical, verified artifact — this table
-- only maps a short code to it, the way an airline PNR maps to a ticket.
--
-- Chosen over in-memory storage (unlike Stage 4/6's telemetry/stop-request
-- state) because a boarding pass, though short-lived (~3 min TTL), is a
-- financial artifact a demo or an auditor may want to trace after the fact,
-- and a server restart mid-demo should not silently invalidate a code a
-- commuter is currently holding up to a driver's camera.
CREATE TABLE boarding_pass_codes (
    code         TEXT PRIMARY KEY,
    pass_token   TEXT NOT NULL,
    nonce        TEXT NOT NULL,
    commuter_id  UUID NOT NULL REFERENCES users(id),
    route_id     UUID NOT NULL REFERENCES routes(id),
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sweep/lookup by expiry (see internal/boarding.Repo.CleanupExpired).
CREATE INDEX boarding_pass_codes_expires_at_idx ON boarding_pass_codes (expires_at);

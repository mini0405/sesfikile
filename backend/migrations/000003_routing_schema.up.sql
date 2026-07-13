-- Stage 3: fixed-route graph (stops, routes, route legs).
--
-- SCOPE HONESTY: the stop/route/fare data seeded by cmd/seed is a hand-seeded,
-- representative sample of Cape Town taxi corridors for demo purposes only —
-- it is NOT association-approved or authoritative. Real association routing
-- sign-off is an open dependency.
CREATE TABLE stops (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    latitude   DOUBLE PRECISION NOT NULL,
    longitude  DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE routes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    association_name TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A route is an ordered sequence of legs (sequence 1, 2, 3, ...) walked in
-- increasing order — travel direction along a route is fixed, matching a
-- real minibus taxi corridor. Interchanges are stops that appear on more
-- than one route, which is what enables multi-hop search.
CREATE TABLE route_legs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    route_id      UUID NOT NULL REFERENCES routes(id),
    from_stop_id  UUID NOT NULL REFERENCES stops(id),
    to_stop_id    UUID NOT NULL REFERENCES stops(id),
    sequence      INT NOT NULL,
    fare_cents    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (route_id, sequence)
);

CREATE INDEX route_legs_route_id_idx ON route_legs (route_id);
CREATE INDEX route_legs_from_stop_id_idx ON route_legs (from_stop_id);
CREATE INDEX route_legs_to_stop_id_idx ON route_legs (to_stop_id);

// Wire types, mirrored 1:1 from the backend's JSON responses (see
// backend/internal/*/handlers.go). Kept flat/plain rather than re-derived
// from OpenAPI or similar tooling, since the backend has none.

export type Role = "commuter" | "driver" | "owner";

export interface AuthResponse {
  token: string;
  user_id: string;
  role: Role;
}

// Source distinguishes cmd/seed's hand-seeded, live-vehicle-capable
// corridors ("seed") from cmd/importcatalogue's real-but-browse-only City of
// Cape Town rows ("catalogue") — see backend/internal/catalogue and
// docs/PROGRESS.md's "Real route catalogue import" entries. No vehicle will
// ever go online on a catalogue route; treat "catalogue" as ride-able-never,
// not ride-able-later.
export type RouteSource = "seed" | "catalogue";

export interface Route {
  id: string;
  name: string;
  association_name: string;
  source: RouteSource;
}

export interface RouteLeg {
  id: string;
  route_id: string;
  sequence: number;
  from_stop_id: string;
  from_stop_name: string;
  to_stop_id: string;
  to_stop_name: string;
  fare_cents: number;
  // True only for a catalogue-imported leg: fare_cents was derived from
  // distance (internal/catalogue.EstimateFareCents), not an actual
  // association tariff. Always false for a hand-seeded leg.
  fare_estimated: boolean;
}

export interface RouteDetail {
  route: Route;
  legs: RouteLeg[];
}

export interface Stop {
  id: string;
  name: string;
  // Null only for a genuinely coordinate-less stop (a defensive edge case).
  // A catalogue-imported stop normally HAS a coordinate since the GeoJSON
  // upgrade, but it's an APPROXIMATE median-derived rank centroid, not
  // surveyed — see source.
  latitude: number | null;
  longitude: number | null;
  coordinates_known: boolean;
  source: RouteSource;
}

export interface RouteSearchSegment {
  route_id: string;
  route_name: string;
  legs: RouteLeg[];
  fare_cents: number;
}

// One route's stored display polyline, decimated server-side for the bulk
// "network coverage" read (GET /routes/geometries) — see api.client's
// getRouteGeometries. Points are [lon, lat] pairs (GeoJSON order); swap to
// [lat, lon] for Leaflet.
export interface RouteGeometrySummary {
  route_id: string;
  original_point_count: number;
  points: [number, number][];
}

export interface RouteSearchResult {
  transfers: number;
  total_fare_cents: number;
  segments: RouteSearchSegment[];
}

export interface VehicleView {
  vehicle_id: string;
  route_id: string;
  driver_id: string;
  lat: number;
  lng: number;
  seats_total: number;
  seats_available: number;
  online: boolean;
  last_updated: string;
}

// Messages pushed over GET /ws/commuter?route_id=<id> — see
// telemetry.Handlers.CommuterWS.
export interface CommuterSnapshotMessage {
  type: "snapshot";
  vehicles: VehicleView[];
}

export interface CommuterUpdateMessage {
  type: "update";
  vehicle: VehicleView;
}

export interface CommuterOfflineMessage {
  type: "offline";
  vehicle_id: string;
}

export type CommuterWSMessage = CommuterSnapshotMessage | CommuterUpdateMessage | CommuterOfflineMessage;

export interface BalanceResponse {
  account_type: string;
  balance_cents: number;
}

export interface TopupResponse {
  transaction_id: string;
  balance_cents: number;
}

export interface IssuePassResponse {
  pass_token: string;
  short_code: string;
  expires_at: string;
  fare_cents: number;
}

export interface RequestStopResponse {
  request_id: string;
  status: string;
  driver_available: boolean;
  message?: string;
}

export interface ApiError {
  error: string;
}

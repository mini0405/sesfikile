// Wire types, mirrored 1:1 from the backend's JSON responses (see
// backend/internal/*/handlers.go). Kept flat/plain rather than re-derived
// from OpenAPI or similar tooling, since the backend has none.

export type Role = "commuter" | "driver" | "owner";

export interface AuthResponse {
  token: string;
  user_id: string;
  role: Role;
}

export interface Route {
  id: string;
  name: string;
  association_name: string;
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
}

export interface RouteDetail {
  route: Route;
  legs: RouteLeg[];
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

export interface SeatsResponse extends VehicleView {}

export interface BalanceResponse {
  account_type: string;
  balance_cents: number;
}

export interface ScanPassResponse {
  transaction_id: string;
  fare_cents: number;
  platform_cents: number;
  driver_cents: number;
  owner_cents: number;
  seats_remaining: number;
  replayed: boolean;
}

export interface AckResponse {
  request_id: string;
  status: string;
}

// Server-pushed message over /ws/driver — see telemetry.AlertMessage.
export interface StopRequestAlert {
  type: "stop_request";
  request_id: string;
  route_id: string;
  stop_id: string;
  stop_name: string;
  requested_at: string;
}

export interface ApiError {
  error: string;
}

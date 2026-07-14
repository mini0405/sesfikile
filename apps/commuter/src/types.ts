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

export interface Stop {
  id: string;
  name: string;
}

export interface RouteSearchSegment {
  route_id: string;
  route_name: string;
  legs: RouteLeg[];
  fare_cents: number;
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

export interface ApiError {
  error: string;
}

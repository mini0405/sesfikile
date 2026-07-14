// Wire types, mirrored 1:1 from the backend's JSON responses (see
// backend/internal/analytics/{models,handlers}.go and internal/identity).
// There's no OpenAPI/codegen in this repo, so these are kept by hand.

export type Role = "commuter" | "driver" | "owner";

export interface AuthResponse {
  token: string;
  user_id: string;
  role: Role;
}

// GET /owner/summary?from=&to=
export interface Summary {
  from: string;
  to: string;
  revenue_cents: number;
  trips: number;
  passenger_volume: number;
  platform_fees_cents: number;
  driver_earnings_cents: number;
  fuel_balance_cents: number;
  fuel_allocated_cents: number;
}

// One row of GET /owner/vehicles?from=&to=
export interface VehicleStat {
  vehicle_id: string;
  registration: string;
  seats_total: number;
  assigned_driver_id?: string;
  assigned_driver_name?: string;
  online: boolean;
  current_route_id?: string;
  current_route_name?: string;
  seats_available?: number;
  trips: number;
  revenue_cents: number;
  fuel_quota_cents: number;
  fuel_reserved_cents: number;
  fuel_used_cents: number;
  fuel_available_cents: number;
}

export interface VehiclesResponse {
  from: string;
  to: string;
  vehicles: VehicleStat[];
}

// One row of GET /owner/drivers?from=&to=
export interface DriverStat {
  driver_id: string;
  full_name: string;
  assigned_vehicle_registration?: string;
  online: boolean;
  trips: number;
  earnings_cents: number;
}

export interface DriversResponse {
  from: string;
  to: string;
  drivers: DriverStat[];
}

export interface RevenueVsFuelDay {
  date: string;
  revenue_cents: number;
  fuel_allocated_cents: number;
  fuel_consumed_cents: number;
}

// GET /owner/revenue-vs-fuel?from=&to=
export interface RevenueVsFuel {
  from: string;
  to: string;
  revenue_cents: number;
  fuel_allocated_cents: number;
  fuel_consumed_cents: number;
  series: RevenueVsFuelDay[];
}

// One row of GET /owner/ledger?from=&to=&limit=&offset=
export interface LedgerEntry {
  id: string;
  entry_type: string;
  occurred_at: string;
  amount_cents: number;
  vehicle_id?: string;
  detail?: Record<string, unknown>;
}

export interface LedgerPage {
  entries: LedgerEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface ApiError {
  error: string;
}

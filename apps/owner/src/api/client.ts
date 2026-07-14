import type {
  AuthResponse,
  DriversResponse,
  LedgerPage,
  RevenueVsFuel,
  Summary,
  VehiclesResponse,
} from "../types";

const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let authToken: string | null = null;

/** Called by AuthContext whenever the token changes (login/logout). */
export function setAuthToken(token: string | null): void {
  authToken = token;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  if (authToken) headers.set("Authorization", `Bearer ${authToken}`);

  let res: Response;
  try {
    res = await fetch(`${BASE_URL}${path}`, { ...init, headers });
  } catch {
    throw new ApiError(0, "Could not reach the server. Check your connection.");
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = await res.json();
      if (typeof body?.error === "string") message = body.error;
    } catch {
      // non-JSON error body, keep the generic message
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// Every /owner/* call below scopes to the caller's own JWT server-side
// (see internal/analytics/handlers.go's ownerFromContext) — this client
// never sends an owner id as a parameter, by design.
export interface DateRangeParams {
  from?: string;
  to?: string;
}

function rangeQuery(params?: DateRangeParams & { limit?: number; offset?: number }): string {
  if (!params) return "";
  const q = new URLSearchParams();
  if (params.from) q.set("from", params.from);
  if (params.to) q.set("to", params.to);
  if (params.limit !== undefined) q.set("limit", String(params.limit));
  if (params.offset !== undefined) q.set("offset", String(params.offset));
  const s = q.toString();
  return s ? `?${s}` : "";
}

export const api = {
  login: (phone: string, password: string) =>
    request<AuthResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ phone, password }),
    }),

  getSummary: (range?: DateRangeParams) => request<Summary>(`/owner/summary${rangeQuery(range)}`),

  getVehicles: (range?: DateRangeParams) => request<VehiclesResponse>(`/owner/vehicles${rangeQuery(range)}`),

  getDrivers: (range?: DateRangeParams) => request<DriversResponse>(`/owner/drivers${rangeQuery(range)}`),

  getRevenueVsFuel: (range?: DateRangeParams) =>
    request<RevenueVsFuel>(`/owner/revenue-vs-fuel${rangeQuery(range)}`),

  getLedger: (range?: DateRangeParams & { limit?: number; offset?: number }) =>
    request<LedgerPage>(`/owner/ledger${rangeQuery(range)}`),
};

import type { AuthResponse, Route, RouteDetail, RouteSearchResult } from "../types";

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

export function getAuthToken(): string | null {
  return authToken;
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

export const api = {
  login: (phone: string, password: string) =>
    request<AuthResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ phone, password }),
    }),

  listRoutes: () => request<Route[]>("/routes"),

  getRoute: (routeId: string) => request<RouteDetail>(`/routes/${routeId}`),

  searchRoutes: (from: string, to: string) =>
    request<RouteSearchResult>(`/routes/search?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
};

/** The WebSocket base URL, derived from the HTTP API base (ws(s):// swap). */
export function wsBaseUrl(): string {
  return BASE_URL.replace(/^http/, "ws");
}

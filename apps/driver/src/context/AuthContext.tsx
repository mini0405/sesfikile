import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { api, setAuthToken } from "../api/client";
import type { Role } from "../types";

export interface AuthState {
  token: string;
  userId: string;
  role: Role;
}

interface AuthContextValue {
  auth: AuthState | null;
  login: (phone: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

// Dev token storage: sessionStorage, not localStorage. This is still a
// plaintext-JS-readable store (fine for an MVP demo token with a 24h expiry,
// not a hardened choice), but sessionStorage at least clears when the tab
// closes rather than persisting indefinitely like localStorage would — the
// smaller of two imperfect options for a dev build with no refresh-token
// flow yet.
const STORAGE_KEY = "sesfikile.driver.auth";

function loadStoredAuth(): AuthState | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as AuthState;
  } catch {
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthState | null>(() => {
    const stored = loadStoredAuth();
    if (stored) setAuthToken(stored.token);
    return stored;
  });

  const login = useCallback(async (phone: string, password: string) => {
    const res = await api.login(phone, password);
    if (res.role !== "driver") {
      throw new Error("This account is not a driver account.");
    }
    const next: AuthState = { token: res.token, userId: res.user_id, role: res.role };
    setAuthToken(next.token);
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    setAuth(next);
  }, []);

  const logout = useCallback(() => {
    setAuthToken(null);
    sessionStorage.removeItem(STORAGE_KEY);
    setAuth(null);
  }, []);

  const value = useMemo(() => ({ auth, login, logout }), [auth, login, logout]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}

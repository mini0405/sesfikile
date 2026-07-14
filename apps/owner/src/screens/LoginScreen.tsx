import { useState, type FormEvent } from "react";
import { useAuth } from "../context/AuthContext";
import { ApiError } from "../api/client";

export function LoginScreen() {
  const { login } = useAuth();
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await login(phone.trim(), password);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Login failed. Try again.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-paper px-6">
      <div className="w-full max-w-sm">
        <div className="ledger-card mb-6 px-7 pb-7 pt-8">
          <p className="card-heading mb-1">Owner sign-in</p>
          <h1 className="mb-6 font-display text-2xl font-black uppercase leading-none tracking-tight text-ink">
            Ses&rsquo;fikile
          </h1>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label htmlFor="phone" className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">
                Phone number
              </label>
              <input
                id="phone"
                type="tel"
                inputMode="tel"
                autoComplete="tel"
                required
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
                placeholder="+27820000001"
                className="input-field w-full"
              />
            </div>

            <div>
              <label htmlFor="password" className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">
                Password
              </label>
              <input
                id="password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="input-field w-full"
              />
            </div>

            {error && (
              <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
                {error}
              </div>
            )}

            <button type="submit" disabled={loading} className="btn-brass w-full">
              {loading ? "Signing in…" : "Sign in"}
            </button>
          </form>
        </div>

        <p className="text-center text-xs uppercase tracking-widest text-ink/40">
          Seeded owner: +27820000001 / Owner123!
        </p>
      </div>
    </div>
  );
}

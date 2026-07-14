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
    <div className="flex min-h-screen flex-col items-center justify-center bg-tar bg-grain px-6">
      <div className="w-full max-w-sm">
        <div className="board relative mb-6 px-6 pb-6 pt-8">
          <span className="tape left-6" />
          <span className="tape right-6 rotate-[4deg] bg-taxi/70" />

          <p className="board-heading mb-1">Driver sign-in</p>
          <h1 className="mb-6 font-display text-3xl font-black uppercase leading-none tracking-tight">
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
                placeholder="+27820000002"
                className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-base text-ink placeholder-ink/40 outline-none focus:border-taxi"
              />
            </div>

            <div>
              <label
                htmlFor="password"
                className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60"
              >
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
                className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-base text-ink placeholder-ink/40 outline-none focus:border-taxi"
              />
            </div>

            {error && (
              <div className="rounded-sm border-2 border-brake bg-brake/10 px-3 py-2 text-sm font-medium text-brake">
                {error}
              </div>
            )}

            <button type="submit" disabled={loading} className="btn-rank">
              {loading ? "Signing in…" : "Sign in"}
            </button>
          </form>
        </div>

        <p className="text-center text-xs uppercase tracking-widest text-tar-400">On the road, cashless</p>
      </div>
    </div>
  );
}

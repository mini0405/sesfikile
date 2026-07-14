import { useState } from "react";
import { useAuth } from "./context/AuthContext";
import { Sidebar, type Tab } from "./components/Sidebar";
import { DateRangePicker } from "./components/DateRangePicker";
import type { DateRangeParams } from "./api/client";
import { OverviewScreen } from "./screens/OverviewScreen";
import { RevenueFuelScreen } from "./screens/RevenueFuelScreen";
import { FleetScreen } from "./screens/FleetScreen";
import { DriversScreen } from "./screens/DriversScreen";
import { LedgerScreen } from "./screens/LedgerScreen";

const TITLES: Record<Tab, string> = {
  overview: "Overview",
  "revenue-fuel": "Revenue vs Fuel",
  fleet: "Fleet",
  drivers: "Drivers",
  ledger: "Ledger",
};

export function OwnerApp() {
  const { logout } = useAuth();
  const [tab, setTab] = useState<Tab>("overview");
  // Lifted here, not per-screen, so every screen respects the same
  // date-range control (the brief's explicit requirement).
  const [range, setRange] = useState<DateRangeParams>({});

  return (
    <div className="flex min-h-screen bg-paper">
      <Sidebar active={tab} onChange={setTab} onLogout={logout} />

      <main className="flex-1 overflow-y-auto px-8 py-6">
        <div className="mx-auto max-w-6xl">
          <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
            <h1 className="font-display text-2xl font-black uppercase tracking-tight text-ink">{TITLES[tab]}</h1>
            <DateRangePicker onChange={setRange} />
          </div>

          {tab === "overview" && <OverviewScreen range={range} />}
          {tab === "revenue-fuel" && <RevenueFuelScreen range={range} />}
          {tab === "fleet" && <FleetScreen range={range} />}
          {tab === "drivers" && <DriversScreen range={range} />}
          {tab === "ledger" && <LedgerScreen range={range} />}
        </div>
      </main>
    </div>
  );
}

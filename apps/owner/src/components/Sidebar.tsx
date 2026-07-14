export type Tab = "overview" | "revenue-fuel" | "fleet" | "drivers" | "ledger";

const ITEMS: { id: Tab; label: string; icon: string }[] = [
  { id: "overview", label: "Overview", icon: "📋" },
  { id: "revenue-fuel", label: "Revenue vs Fuel", icon: "📈" },
  { id: "fleet", label: "Fleet", icon: "🚐" },
  { id: "drivers", label: "Drivers", icon: "🧑‍✈️" },
  { id: "ledger", label: "Ledger", icon: "📒" },
];

interface SidebarProps {
  active: Tab;
  onChange: (tab: Tab) => void;
  onLogout: () => void;
}

// Desktop-first vertical nav rail, not a phone bottom-tab bar — this app is
// read at a desk on a wide screen, so navigation lives down the left margin
// the way a back-office application's does, not in the thumb-reach zone.
export function Sidebar({ active, onChange, onLogout }: SidebarProps) {
  return (
    <aside className="flex h-screen w-60 shrink-0 flex-col border-r border-ink/15 bg-card px-3 py-5">
      <div className="mb-6 px-2">
        <p className="font-display text-lg font-black uppercase leading-none tracking-tight text-ink">
          Ses&rsquo;fikile
        </p>
        <p className="mt-1 text-[11px] font-bold uppercase tracking-[0.15em] text-ink/40">Owner dashboard</p>
      </div>

      <nav className="flex flex-1 flex-col gap-1">
        {ITEMS.map((item) => (
          <button
            key={item.id}
            onClick={() => onChange(item.id)}
            className={`nav-link ${active === item.id ? "nav-link-active" : ""}`}
          >
            <span className="text-base leading-none">{item.icon}</span>
            {item.label}
          </button>
        ))}
      </nav>

      <div className="border-t border-ink/10 px-2 pt-4">
        <span className="stamp-reconciled mb-3 block w-fit">✓ Ledger-reconciled</span>
        <button onClick={onLogout} className="text-xs font-bold uppercase tracking-wide text-ink/50 hover:text-ink">
          Log out
        </button>
      </div>
    </aside>
  );
}

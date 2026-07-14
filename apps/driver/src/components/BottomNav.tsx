export type Tab = "home" | "scan" | "seats" | "alerts";

interface BottomNavProps {
  active: Tab;
  onChange: (tab: Tab) => void;
  alertCount: number;
}

const ITEMS: { id: Tab; label: string; icon: string }[] = [
  { id: "home", label: "Board", icon: "🚐" },
  { id: "scan", label: "Scan", icon: "📷" },
  { id: "seats", label: "Seats", icon: "💺" },
  { id: "alerts", label: "Radio", icon: "📻" },
];

// Styled like a row of dashboard toggle switches rather than a soft
// rounded tab bar — the active tab lights up its top edge like an
// indicator strip.
export function BottomNav({ active, onChange, alertCount }: BottomNavProps) {
  return (
    <nav className="fixed inset-x-0 bottom-0 z-20 border-t-2 border-tar-600 bg-tar/95 backdrop-blur">
      <div className="mx-auto grid max-w-md grid-cols-4">
        {ITEMS.map((item) => {
          const isActive = active === item.id;
          return (
            <button
              key={item.id}
              onClick={() => onChange(item.id)}
              className={`relative flex flex-col items-center gap-1 border-t-2 py-3 text-[11px] font-bold uppercase tracking-wide transition ${
                isActive ? "border-rank text-rank" : "border-transparent text-tar-400"
              }`}
            >
              <span className={`text-lg leading-none transition ${isActive ? "" : "grayscale opacity-60"}`}>
                {item.icon}
              </span>
              {item.label}
              {item.id === "alerts" && alertCount > 0 && (
                <span className="absolute right-[calc(50%-24px)] top-1.5 flex h-4 min-w-4 items-center justify-center rounded-full border border-ink bg-brake px-1 font-display text-[10px] font-black text-board">
                  {alertCount}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </nav>
  );
}

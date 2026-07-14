export type Tab = "map" | "search" | "routes";

const ITEMS: { id: Tab; label: string; icon: string }[] = [
  { id: "map", label: "Live", icon: "🗺️" },
  { id: "search", label: "Search", icon: "🔎" },
  { id: "routes", label: "Routes", icon: "🚏" },
];

// Styled like the tab strip along the bottom of a paper timetable rather
// than a soft rounded app tab bar — the active tab's top edge marks it like
// a torn perforation.
export function BottomNav({ active, onChange }: { active: Tab; onChange: (tab: Tab) => void }) {
  return (
    <nav className="fixed inset-x-0 bottom-0 z-20 border-t-2 border-ink/20 bg-dawn/95 backdrop-blur">
      <div className="mx-auto grid max-w-md grid-cols-3">
        {ITEMS.map((item) => {
          const isActive = active === item.id;
          return (
            <button
              key={item.id}
              onClick={() => onChange(item.id)}
              className={`flex flex-col items-center gap-1 border-t-2 py-3 text-[11px] font-bold uppercase tracking-wide transition ${
                isActive ? "border-marigold text-ink" : "border-transparent text-dawn-400"
              }`}
            >
              <span className={`text-lg leading-none transition ${isActive ? "" : "grayscale opacity-60"}`}>
                {item.icon}
              </span>
              {item.label}
            </button>
          );
        })}
      </div>
    </nav>
  );
}

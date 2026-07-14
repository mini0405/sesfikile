/** @type {import('tailwindcss').Config} */
// Design language: the same taxi/destination-board lineage as the driver
// (Stage 9a) and commuter (Stage 9b) apps, but read from behind the counter
// at the taxi association's back office, not from the cab or the rank. The
// object here isn't a board or a boarding pass — it's the association
// clerk's ledger book: ruled paper, ink totals, a reconciliation stamp. See
// apps/owner/README.md "Design direction" for the full rationale.
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        // Ledger-paper backdrop — cooler and quieter than the commuter app's
        // warm midday `dawn`, because this is an indoor back-office screen,
        // not a rank at street level.
        paper: {
          DEFAULT: "#EEEAE2",
          800: "#E3DDD0",
          700: "#D2C9B6",
          600: "#B6AA8E",
          400: "#8A8071",
        },
        card: {
          DEFAULT: "#F8F6F1",
        },
        ink: "#1E1B16",
        // Brass fixture accent — a desaturated relative of the driver app's
        // curb-paint `rank`/commuter app's `marigold`, toned down for a
        // professional register rather than a street-level primary action.
        brass: {
          DEFAULT: "#8F6E2E",
          600: "#77591F",
          700: "#5E4718",
          100: "#E9DCBC",
        },
        // Live/positive status — a muted relative of the commuter app's
        // `transit` teal.
        signal: {
          DEFAULT: "#1B6E74",
          600: "#155459",
        },
        // Negative / attention — same family as the other apps' brake/flag
        // red, desaturated to match this app's calmer register.
        alert: {
          DEFAULT: "#A8402F",
          600: "#853225",
        },
      },
      fontFamily: {
        display: ["Arial Black", "Helvetica Neue", "Arial", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
      },
      boxShadow: {
        ledger: "0 1px 3px rgba(30,27,22,0.12)",
      },
      backgroundImage: {
        // Faint ruled-paper lines behind ledger-card content.
        ruled:
          "repeating-linear-gradient(to bottom, transparent, transparent 27px, rgba(30,27,22,0.05) 28px)",
      },
    },
  },
  plugins: [],
};

/** @type {import('tailwindcss').Config} */
// Design language: the driver's windscreen destination board and the
// minibus taxi rank — not a generic dark-mode SaaS palette. See
// apps/driver/README.md "Design direction" for the full token rationale.
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        // The asphalt/rank backdrop — warm near-black, not cool slate.
        tar: {
          DEFAULT: "#1C1A17",
          800: "#252220",
          700: "#332F2A",
          600: "#4A453D",
          400: "#8C8477",
        },
        // The destination board's cardstock.
        board: {
          DEFAULT: "#F3E9D6",
          dim: "#E4D5B7",
        },
        // Marker-pen lettering on the board.
        ink: "#201C16",
        // Rank-marking / curb-paint yellow — the "online" / primary accent.
        rank: {
          DEFAULT: "#FFC01E",
          600: "#E8A800",
          700: "#B9840A",
        },
        // Common livery blue — secondary / informational accent.
        taxi: {
          DEFAULT: "#1F4E8C",
          600: "#173C6E",
        },
        // Brake-light red — alerts and stop-requests only.
        brake: {
          DEFAULT: "#D8342A",
          600: "#B12820",
        },
      },
      fontFamily: {
        // Heavy grotesque for board/display lettering (system stack, no
        // webfont fetch — this is an offline-first dev tool).
        display: ["Arial Black", "Helvetica Neue", "Arial", "sans-serif"],
      },
      boxShadow: {
        board: "0 10px 30px -12px rgba(0,0,0,0.55)",
        tape: "0 1px 2px rgba(0,0,0,0.25)",
        stamp: "0 0 0 3px rgba(216,52,42,0.15)",
      },
      backgroundImage: {
        grain:
          "radial-gradient(circle at 1px 1px, rgba(0,0,0,0.05) 1px, transparent 0)",
      },
      backgroundSize: {
        grain: "4px 4px",
      },
    },
  },
  plugins: [],
};

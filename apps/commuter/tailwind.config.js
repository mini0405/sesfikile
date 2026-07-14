/** @type {import('tailwindcss').Config} */
// Design language: same destination-board object the driver app tapes to a
// windscreen (the commuter is reading the *same physical board*, just from
// the rank rather than the cab) — but this app lives in the rank at street
// level, in daylight, waiting, not on shift in a dark cab. See
// apps/commuter/README.md "Design direction" for the full rationale.
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        // Daylight parchment backdrop — the rank at midday, not the tar/night
        // dashboard of the driver app.
        dawn: {
          DEFAULT: "#F6EEDD",
          800: "#EDE0C4",
          700: "#DFCBA0",
          600: "#C9AF7C",
          400: "#8C7C57",
        },
        // The destination board itself — shared identity with the driver app.
        board: {
          DEFAULT: "#F3E9D6",
          dim: "#E4D5B7",
        },
        ink: "#201C16",
        // Rank-wall paint — the primary commuter accent. A relative of the
        // driver app's curb-paint yellow, but the wall behind you rather
        // than the paint at your feet.
        marigold: {
          DEFAULT: "#E2963A",
          600: "#C97C22",
          700: "#A6631A",
        },
        // Live-transit teal — vehicles in motion on the map. Deliberately not
        // the driver app's livery blue, so a commuter never confuses "a taxi
        // livery colour" with "this dot is moving right now".
        transit: {
          DEFAULT: "#1C7A82",
          600: "#145F65",
        },
        // No-route / disconnected states.
        flag: {
          DEFAULT: "#C13B30",
          600: "#9C2E25",
        },
      },
      fontFamily: {
        display: ["Arial Black", "Helvetica Neue", "Arial", "sans-serif"],
      },
      boxShadow: {
        board: "0 10px 30px -12px rgba(32,28,22,0.35)",
        tape: "0 1px 2px rgba(32,28,22,0.2)",
      },
      backgroundImage: {
        grain:
          "radial-gradient(circle at 1px 1px, rgba(32,28,22,0.04) 1px, transparent 0)",
      },
      backgroundSize: {
        grain: "4px 4px",
      },
    },
  },
  plugins: [],
};

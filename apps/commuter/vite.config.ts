import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 5174 is the driver app's dev port (Stage 9a) — the backend owns 8080.
    port: 5175,
  },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 5174 = driver app, 5175 = commuter app (Stages 9a/9b) — the backend
    // owns 8080. This is the owner dashboard's dev port.
    port: 5176,
  },
});

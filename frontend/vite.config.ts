import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// VITE_API_TARGET allows overriding the backend URL for local dev.
// In Docker: defaults to "http://sentinel:8099" (container name).
// Local dev: run with VITE_API_TARGET=http://localhost:8099
const apiTarget = process.env.VITE_API_TARGET || "http://sentinel:8099";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      // Proxies all /api/* requests to the backend, including WebSocket upgrades
      // for live stream proxying (Phase 3: /api/v1/streams/:name/ws).
      "/api": {
        target: apiTarget,
        changeOrigin: true,
        ws: true,
      },
    },
  },
});

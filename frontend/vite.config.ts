/// <reference types="vitest" />
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The dev proxy target and an optional injected session cookie are env-driven so a
// local preview can point at a remote appliance without hardcoding its address.
const apiTarget = process.env.OC_API_TARGET ?? "http://localhost:8080";
const devCookie = process.env.OC_SESSION;

function withCookie(target: string) {
  return {
    target,
    changeOrigin: true,
    configure: (proxy: { on: (e: string, cb: (req: unknown) => void) => void }) => {
      if (!devCookie) return;
      proxy.on("proxyReq", (proxyReq: unknown) => {
        (proxyReq as { setHeader: (k: string, v: string) => void }).setHeader(
          "cookie",
          `opencuttles_session=${devCookie}`,
        );
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": withCookie(apiTarget),
      "/agents": withCookie(apiTarget),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    globals: true,
  },
});

import react from "@vitejs/plugin-react";
import { loadEnv } from "vite";
import { defineConfig } from "vitest/config";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "VITE_");
  const target = env.VITE_API_TARGET || "http://127.0.0.1:8080";

  return {
    plugins: [react()],
    build: {
      outDir: "../internal/web/dist",
      emptyOutDir: true,
    },
    server: {
      proxy: {
        "/api": target,
        "/ws": { target: target.replace(/^http/, "ws"), ws: true },
      },
    },
    test: {
      environment: "jsdom",
      include: ["src/**/*.test.{ts,tsx}"],
    },
  };
});

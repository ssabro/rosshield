/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

// E10 Stage D — Vitest 설정.
// jsdom 환경 + RTL setup. Vite alias `@`를 그대로 상속해 src/ import 호환.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    // Vitest는 src/만 — playwright/는 별도 runner (C4).
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    exclude: ["node_modules", "dist", "playwright/**", "../internal/web/dist/**"],
  },
});

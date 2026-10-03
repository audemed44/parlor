import { readFileSync } from "node:fs";
import preact from "@preact/preset-vite";
import { defineConfig } from "vitest/config";

const core = JSON.parse(
  readFileSync("node_modules/@thenick775/mgba-wasm/package.json", "utf8"),
).version;

export default defineConfig({
  plugins: [preact()],
  define: { __CORE_VERSION__: JSON.stringify(core) },
  build: { outDir: "../web/dist", emptyOutDir: true, assetsInlineLimit: 0 },
  server: {
    // The emulator's threads need cross-origin isolation, as in production.
    headers: {
      "Cross-Origin-Opener-Policy": "same-origin",
      "Cross-Origin-Embedder-Policy": "require-corp",
    },
    // `npm run dev` proxies the API to a local `parlor` binary (or PARLOR_URL).
    // Keep the Host header: Parlor refuses writes whose Origin doesn't match it.
    proxy: {
      "/api": { target: process.env.PARLOR_URL ?? "http://localhost:8080", changeOrigin: false },
    },
  },
  test: { environment: "jsdom" },
});

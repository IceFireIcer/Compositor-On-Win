/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Wails dev server proxies to this port; keep it fixed (wails.json expects it).
export default defineConfig({
  plugins: [svelte()],
  clearScreen: false,
  server: {
    port: 1421,
    strictPort: true,
    watch: { ignored: ["**/reference/**"] },
  },
  test: {
    environment: "node",
  },
});

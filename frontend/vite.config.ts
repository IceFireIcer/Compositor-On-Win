/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Wails dev server proxies to this port; keep it fixed (wails.json expects it).
export default defineConfig({
  plugins: [
    svelte(),
    {
      name: "render-routes-use-wails-handler",
      configureServer(server) {
        // Wails dev only invokes its Go asset handler after Vite returns 404.
        // Keep SPA fallback for the application, but make the render asset
        // plane fall through to the Go handler instead of returning index.html.
        server.middlewares.use((request, response, next) => {
          if (request.url?.startsWith("/render/") || request.url?.startsWith("/pixel/")) {
            response.statusCode = 404;
            response.end();
            return;
          }
          next();
        });
      },
    },
  ],
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

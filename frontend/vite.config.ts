import { fileURLToPath, URL } from "node:url";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";

const apiTarget = process.env.NOTEBOOK_API_TARGET ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [svelte()],
  // Kit UI ships Svelte/TypeScript source; compile it with the app.
  optimizeDeps: { exclude: ["@kenn-io/kit-ui"] },
  base: "/",
  resolve: {
    alias: { $lib: fileURLToPath(new URL("./src/lib", import.meta.url)) },
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      // Preserve the browser-facing Host so the API can validate its Origin.
      "/api": { target: apiTarget, changeOrigin: false },
    },
  },
});

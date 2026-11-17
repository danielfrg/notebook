import { fileURLToPath, URL } from "node:url";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { preferencesScript } from "@ui-sdk/core";
import { defineConfig } from "vite";
import { colorScheme } from "./src/lib/color-scheme.ts";

const apiTarget = process.env.NOTEBOOK_API_TARGET ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [
    svelte(),
    {
      // Applies the saved color scheme before the app loads, so dark never flashes light.
      name: "preferences-script",
      transformIndexHtml: () => [
        { tag: "script", children: preferencesScript([colorScheme]), injectTo: "head-prepend" },
      ],
    },
  ],
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

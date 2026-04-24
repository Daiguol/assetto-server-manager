import { defineConfig } from "vite";
import inject from "@rollup/plugin-inject";
import { resolve } from "node:path";

// Bundles src/main.ts into a single IIFE at ../static/js/bundle.js so the
// existing <script src="/static/js/bundle.js"> template tag keeps working
// without needing type="module". SCSS is compiled separately via the sass
// CLI (see package.json scripts) because Rollup doesn't support IIFE with
// multiple entries.
//
// The @rollup/plugin-inject pass replaces the browserify-shim we used under
// the old gulp pipeline — it rewrites bare `$` / `jQuery` references in any
// bundled module into explicit imports from the jquery package.
export default defineConfig({
  // Some CJS jQuery plugins inside node_modules do `require("jQuery")` (mixed
  // case). Alias to the canonical lowercase package so Rollup's commonjs layer
  // can resolve it instead of treating it as an external.
  resolve: {
    alias: {
      jQuery: "jquery",
    },
  },
  build: {
    outDir: resolve(__dirname, "../static/js"),
    emptyOutDir: false,
    sourcemap: true,
    rollupOptions: {
      input: resolve(__dirname, "src/main.ts"),
      plugins: [inject({ $: "jquery", jQuery: "jquery" })],
      output: {
        format: "iife",
        name: "ServerManager",
        entryFileNames: "bundle.js",
        assetFileNames: "bundle-[name][extname]",
        inlineDynamicImports: true,
      },
    },
  },
});

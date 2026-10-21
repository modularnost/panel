import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "/",
  plugins: [
    react(),
    {
      // Vite empties dist, and the Go embed needs the folder to hold a file.
      name: "keep-dist",
      generateBundle() {
        this.emitFile({ type: "asset", fileName: ".gitkeep", source: "" });
      },
    },
  ],
  test: {
    // The package imports without file extensions, which only a bundler resolves.
    server: { deps: { inline: ["@material/material-color-utilities"] } },
  },
  server: {
    // Everything the Go server answers itself; the rest is the SPA's routes.
    proxy: Object.fromEntries(
      ["/api", "/healthz"].map((p) => [p, "http://localhost:8080"]),
    ),
  },
});

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  base: "/app/",
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
  server: { proxy: { "/api": "http://localhost:8080" } },
});

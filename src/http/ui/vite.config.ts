import { defineConfig } from "vite";
import dotenv from "dotenv";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";
import tailwindcss from "@tailwindcss/vite";

dotenv.config();
const REMOTE_BACKEND = process.env.B4_BACKEND_URL || "http://192.168.1.1:7000";
const APP_VERSION = process.env.VITE_APP_VERSION || "dev";

console.log("Using backend:", REMOTE_BACKEND);
console.log("Building version:", APP_VERSION);

export default defineConfig({
  plugins: [tailwindcss(), tsconfigPaths(), react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  define: {
    "import.meta.env.VITE_APP_VERSION": JSON.stringify(APP_VERSION),
  },

  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: REMOTE_BACKEND,
        changeOrigin: true,
        ws: true,
        secure: false,
      },
    },
  },
});

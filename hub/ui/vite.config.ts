import { defineConfig, Plugin } from "vite";
import { closeSync, openSync } from "node:fs";
import { join } from "node:path";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";

const HUB_BACKEND = process.env.B4HUB_BACKEND_URL || "http://127.0.0.1:7100";
const APP_VERSION = process.env.VITE_APP_VERSION || "dev";

const keepDist = (): Plugin => ({
  name: "b4hub-keep-dist",
  closeBundle() {
    closeSync(openSync(join(__dirname, "dist", ".gitkeep"), "w"));
  },
});

export default defineConfig({
  base: "/admin/",
  plugins: [tsconfigPaths(), react(), keepDist()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    cssCodeSplit: true,
    minify: "terser",
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
      },
    },
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes("node_modules")) {
            if (id.includes("@mui/icons-material")) {
              return "mui-icons";
            }
            if (id.includes("@mui")) {
              return "mui";
            }
            return "vendor";
          }
        },
      },
    },
    chunkSizeWarningLimit: 800,
  },
  define: {
    "import.meta.env.VITE_APP_VERSION": JSON.stringify(APP_VERSION),
  },
  server: {
    port: 5174,
    proxy: {
      "/admin/api": {
        target: HUB_BACKEND,
        changeOrigin: true,
        secure: false,
      },
    },
  },
});

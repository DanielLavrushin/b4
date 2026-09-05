import { defineConfig } from "vite";
import dotenv from "dotenv";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";

dotenv.config();
const REMOTE_BACKEND = process.env.B4_BACKEND_URL || "http://192.168.1.1:7000";
const APP_VERSION = process.env.VITE_APP_VERSION || "dev";

console.log("Using backend:", REMOTE_BACKEND);
console.log("Building version:", APP_VERSION);

export default defineConfig({
  plugins: [tsconfigPaths(), react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Optimize chunk size
    cssCodeSplit: true,
    minify: "terser",
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
        pure_funcs: ["console.log", "console.debug", "console.trace"],
      },
    },
    rollupOptions: {
      output: {
        // Manual chunk splitting for better caching
        manualChunks: (id) => {
          if (id.includes("node_modules")) {
            // Icons in separate chunk (they're huge!)
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
    port: 5173,
    open: true,
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

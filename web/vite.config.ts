import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

const operatorTarget = process.env.VITE_OPERATOR_PROXY_TARGET || "http://127.0.0.1:4002";

export default defineConfig({
  plugins: [vue()],
  build: {
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) {
            return undefined;
          }
          if (id.includes("ant-design-vue") || id.includes("@ant-design")) {
            return "antdv";
          }
          if (id.includes("/vue/") || id.includes("@vue")) {
            return "vue";
          }
          return "vendor";
        },
      },
    },
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api/v1": {
        target: operatorTarget,
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: "./src/test/setup.ts",
  },
});

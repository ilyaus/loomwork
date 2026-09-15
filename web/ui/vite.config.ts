import {defineConfig} from "vite";
import react from "@vitejs/plugin-react";

const backend = "http://127.0.0.1:8787";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../dist",
    emptyOutDir: true,
    // swagger-ui-react ships a single ~830kB vendor bundle that is already
    // isolated into its own lazy-loaded chunk (see OpenAPIViewer.tsx) and
    // can't be split further, so raise the warning threshold past it.
    chunkSizeWarningLimit: 900,
    rolldownOptions: {
      output: {
        manualChunks(id) {
          if (id.indexOf("node_modules") === -1) return;
          if (id.indexOf("/swagger-ui-es-bundle-core.js") !== -1) return "swagger-core";
          if (
            id.indexOf("/swagger-client/") !== -1 ||
            id.indexOf("/core-js-pure/") !== -1 ||
            id.indexOf("/@swagger-api/") !== -1 ||
            id.indexOf("/fast-json-patch/") !== -1 ||
            id.indexOf("/deepmerge/") !== -1 ||
            id.indexOf("/ramda/") !== -1 ||
            id.indexOf("/ramda-adjunct/") !== -1 ||
            id.indexOf("/minim/") !== -1 ||
            id.indexOf("/short-unique-id/") !== -1 ||
            id.indexOf("/neotraverse/") !== -1 ||
            id.indexOf("/@swaggerexpert/") !== -1 ||
            id.indexOf("/apg-lite/") !== -1 ||
            id.indexOf("/ts-mixer/") !== -1 ||
            id.indexOf("/openapi-server-url-templating/") !== -1 ||
            id.indexOf("/openapi-path-templating/") !== -1 ||
            id.indexOf("/@babel/runtime-corejs3/") !== -1
          ) {
            return "swagger-client";
          }
          if (
            id.indexOf("/react-syntax-highlighter/") !== -1 ||
            id.indexOf("/refractor/") !== -1 ||
            id.indexOf("/prismjs/") !== -1 ||
            id.indexOf("/highlight.js/") !== -1 ||
            id.indexOf("/lowlight/") !== -1
          ) {
            return "swagger-syntax";
          }
          if (id.indexOf("/immutable/") !== -1) return "swagger-immutable";
          if (id.indexOf("/lodash") !== -1) return "swagger-lodash";
          if (
            id.indexOf("/redux/") !== -1 ||
            id.indexOf("/react-redux/") !== -1 ||
            id.indexOf("/use-sync-external-store/") !== -1
          ) {
            return "swagger-redux";
          }
          if (id.indexOf("/js-yaml/") !== -1) return "swagger-yaml";
        },
      },
    },
  },
  server: {
    proxy: {
      "/api": {
        target: backend,
        changeOrigin: true,
      },
    },
  },
});

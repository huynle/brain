import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";
import { fileURLToPath, URL } from "node:url";

// Paths that must always hit the Go server, never the SPA / service worker.
const API_PREFIXES = [
  "/api",
  "/mcp",
  "/token",
  "/authorize",
  "/register",
  "/health",
  "/.well-known",
];

// In dev, proxy backend routes to the running brain-api server (default :3333).
const BACKEND = process.env.BRAIN_API_URL || "http://localhost:3333";

const proxy = Object.fromEntries(
  API_PREFIXES.map((p) => [
    p,
    {
      target: BACKEND,
      changeOrigin: true,
      // SSE / streaming endpoints must not be buffered.
      ws: false,
    },
  ]),
);

export default defineConfig({
  // Built assets are embedded into the Go binary at internal/webui/dist and
  // served from the site root.
  base: "/",
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  plugins: [
    react(),
    VitePWA({
      // "prompt" holds a new build in the SW "waiting" state and fires
      // onNeedRefresh, so we can show an "Update available — Reload" banner
      // instead of silently swapping the build under the open tab.
      registerType: "prompt",
      // We register the SW ourselves in main.tsx so we can poll for updates
      // on long-open tabs (otherwise an open tab keeps serving the cached
      // build until the user manually reloads).
      injectRegister: false,
      // The OAuth flow does full-page redirects to server-rendered routes; the
      // SW must let these through to the network rather than serving the shell.
      workbox: {
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [
          /^\/api/,
          /^\/mcp/,
          /^\/token/,
          /^\/authorize/,
          /^\/register/,
          /^\/health/,
          /^\/\.well-known/,
        ],
        // updateSW() sends SKIP_WAITING, but the open PWA tab will not reload
        // until the activated worker controls it. Claim clients immediately so
        // Workbox's controlling event fires after the user clicks Reload.
        clientsClaim: true,
        // Don't precache source maps; cache the app shell + assets.
        globPatterns: ["**/*.{js,css,html,svg,png,ico,woff2}"],
        // API responses are real-time; never serve them from the SW cache.
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.pathname.startsWith("/api"),
            handler: "NetworkOnly",
          },
        ],
        cleanupOutdatedCaches: true,
      },
      includeAssets: ["favicon.svg", "icons/apple-touch-icon.png"],
      manifest: {
        name: "Brain",
        short_name: "Brain",
        description: "AI agent memory, tasks, and automation dashboard.",
        theme_color: "#f7f3ea",
        background_color: "#f7f3ea",
        display: "standalone",
        orientation: "any",
        scope: "/",
        start_url: "/",
        categories: ["productivity", "developer"],
        icons: [
          {
            src: "icons/pwa-192x192.png",
            sizes: "192x192",
            type: "image/png",
          },
          {
            src: "icons/pwa-512x512.png",
            sizes: "512x512",
            type: "image/png",
          },
          {
            src: "icons/maskable-512x512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "maskable",
          },
        ],
      },
      devOptions: {
        enabled: false,
      },
    }),
  ],
  server: {
    port: 5179,
    proxy,
  },
  build: {
    outDir: fileURLToPath(
      new URL("../internal/webui/dist", import.meta.url),
    ),
    // Kept false so the committed .gitkeep/.gitignore that go:embed relies on
    // survive a build. The `just web-build` recipe clears stale assets first.
    emptyOutDir: false,
    sourcemap: false,
    chunkSizeWarningLimit: 1500,
  },
});

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// Vite outputs the built SPA into the Go embed directory so the api binary
// picks up the new bundle on next build. The dev server proxies /api/* to the
// api process on :8081 (not the proxy process on :8080).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8081',
      '/healthz': 'http://127.0.0.1:8081',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
});

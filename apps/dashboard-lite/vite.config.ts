import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev requests to /api are proxied to the Lite control plane so the
// dashboard is same-origin in both dev and prod (no CORS dependence).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
});

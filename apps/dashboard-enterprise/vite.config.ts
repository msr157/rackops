import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev requests to /api are proxied to the Enterprise control plane (:8081).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5174,
    proxy: {
      '/api': 'http://localhost:8081',
    },
  },
});

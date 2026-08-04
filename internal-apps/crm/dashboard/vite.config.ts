import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // Emit straight into the Go embed directory so `go build` picks it up.
    outDir: '../backend/internal/server/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8091',
        changeOrigin: true,
      },
      '/ingest': {
        target: 'http://localhost:8091',
        changeOrigin: true,
      },
    },
  },
})

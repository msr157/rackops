import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

// The Go control plane serves this dashboard from its binary:
// internal/server/embed.go declares `//go:embed all:dist`, which is a compile
// error when the directory is missing. A committed .gitkeep keeps a clean
// clone buildable — but emptyOutDir wipes it on every build, which would stage
// a deletion and break `go build ./...` for everyone else. Put it back once
// the bundle is written.
const EMBED_DIR = '../backend/internal/server/dist'

function keepEmbedDir(): Plugin {
  return {
    name: 'rackops-keep-embed-dir',
    apply: 'build',
    closeBundle() {
      writeFileSync(resolve(import.meta.dirname, EMBED_DIR, '.gitkeep'), '')
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), keepEmbedDir()],
  build: {
    // Emit straight into the Go embed directory so `go build` picks it up.
    outDir: EMBED_DIR,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8090',
        changeOrigin: true,
      },
    },
  },
})

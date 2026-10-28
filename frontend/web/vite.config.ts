import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:55555',
      '/healthz': 'http://127.0.0.1:55555',
      // The browser must connect from the dev-server origin so the realtime
      // service sees an Origin it can allowlist. ws: true keeps the upgrade.
      '/ws': {
        target: 'ws://127.0.0.1:8090',
        ws: true,
      },
    },
  },
})

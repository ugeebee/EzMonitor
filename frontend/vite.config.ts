import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
// In development, /api is proxied to the Go backend so the app can call it
// on the same origin, exactly as it does behind Caddy in Docker.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: process.env.VITE_PROXY_TARGET ?? 'http://localhost:8080', ws: true },
    },
  },
})

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Go server embeds web/dist and serves it, so the build lands there.
// In dev, `npm run dev` proxies the API to the Go process on :8080.
export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8080' },
  },
})

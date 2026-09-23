import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': 'http://localhost:8080',
      '/admin': 'http://localhost:8080',
      '/anthropic': 'http://localhost:8080',
    }
  },
  build: {
    outDir: '../static',
    emptyOutDir: true,
  }
})

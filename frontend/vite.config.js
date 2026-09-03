import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 开发时用 5173 提供页面，/api 反向代理到后端 54321。
// 生产由 Go 后端在 54321 同源提供静态资源，无需代理。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:54321',
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1200,
  },
})

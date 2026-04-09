import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8085',
        changeOrigin: true,
      },
      '/market': {
        target: 'http://localhost:8081',
        changeOrigin: true,
      },
      '/stocks': {
        target: 'http://localhost:8085',
        changeOrigin: true,
      },
      '/backtest': {
        target: 'http://localhost:8085',
        changeOrigin: true,
      },
      '/ohlcv': {
        target: 'http://localhost:8085',
        changeOrigin: true,
      },
    },
  },
}))

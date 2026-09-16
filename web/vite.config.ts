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
    // ODR-062 (S-E): the SPA reaches L0 data only through the analysis
    // gateway (:8085) — the dead /market → 8081 (L3 → L0 direct connect),
    // /stocks and /ohlcv dev proxies were removed (zero consumers; ODR-062
    // 取证 f/g). All SPA API paths carry the /api prefix.
    proxy: {
      '/api': {
        target: 'http://localhost:8085',
        changeOrigin: true,
      },
    },
  },
}))

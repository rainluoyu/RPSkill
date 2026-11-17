import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'
import { loadEnv } from 'vite'
import { devProxy } from './config/dev-proxy.ts'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'RPS_')
  const { guard, proxy } = devProxy(process.env.RPS_API_TARGET || env.RPS_API_TARGET || 'http://127.0.0.1:8717')
  return {
    plugins: [guard, vue()],
    base: './',
    server: { host: '127.0.0.1', cors: false, proxy: { '/api': proxy } },
    build: { outDir: 'dist', emptyOutDir: true },
    test: { environment: 'jsdom', include: ['tests/**/*.test.ts'], clearMocks: true, restoreMocks: true },
  }
})

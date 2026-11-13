import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite-plus'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The MeetHub SPA.
// - vp build → ../web/dist (embedded in the Go binary through go:embed)
// - vp dev   → proxies /api to the Go server (MEETHUB_API_URL, default http://127.0.0.1:8080)
const apiTarget = process.env.MEETHUB_API_URL ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    // go:embed needs at least one file in web/dist even before a build: recreate the versioned placeholder
    {
      name: 'gitkeep',
      closeBundle: () => writeFileSync(fileURLToPath(new URL('../web/dist/.gitkeep', import.meta.url)), ''),
    },
  ],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: { outDir: '../web/dist', emptyOutDir: true },
  server: { host: '0.0.0.0', port: 5173, proxy: { '/api': { target: apiTarget, changeOrigin: false, ws: true } } },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts', 'tests/unit/**/*.test.ts'],
    exclude: ['tests/e2e/**', 'node_modules/**'],
  },
  fmt: { singleQuote: true, semi: false, printWidth: 120 },
  // themed toasts, alerts and dialogs only: the browser's native ones are banned
  lint: {
    rules: {
      'no-alert': 'error',
      'no-restricted-globals': ['error', 'alert', 'confirm', 'prompt'],
      'no-restricted-properties': [
        'error',
        { object: 'window', property: 'alert' },
        { object: 'window', property: 'confirm' },
        { object: 'window', property: 'prompt' },
      ],
    },
  },
})

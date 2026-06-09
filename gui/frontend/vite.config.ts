import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

function manualChunks(id: string): string | undefined {
  const normalized = id.replace(/\\/g, '/')

  if (normalized.includes('/node_modules/')) {
    if (normalized.includes('/katex/')) return 'katex'
    if (normalized.includes('/cytoscape/')) return 'cytoscape'
  }

  if (normalized.includes('/wailsjs/')) return 'wails'

  return undefined
}

// https://vitejs.dev/config/
export default defineConfig({
  base: './',
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
})

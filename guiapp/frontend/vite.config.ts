import {writeFileSync, readFileSync, existsSync} from 'node:fs'
import {resolve} from 'node:path'
import {defineConfig, type Plugin} from 'vite'
import react from '@vitejs/plugin-react'
import {rewriteWebviewFirstPaintHtml} from './webview-first-paint'
import {buildBootThemePalette, renderBootThemeScript} from './src/components/ai/themeBootPalette'

const BOOT_THEME_PLACEHOLDER = '<!-- @maclaw-boot-theme -->'

function bootThemePlugin(): Plugin {
  return {
    name: 'maclaw-boot-theme',
    transformIndexHtml: {
      order: 'pre',
      handler(html) {
        if (!html.includes(BOOT_THEME_PLACEHOLDER)) return html
        const script = renderBootThemeScript(buildBootThemePalette())
        return html.replace(BOOT_THEME_PLACEHOLDER, `<script>\n      ${script}\n    </script>`)
      },
    },
  }
}

function webviewFirstPaintPlugin(): Plugin {
  const rewriteIndex = () => {
    const indexPath = resolve(__dirname, 'dist/index.html')
    if (!existsSync(indexPath)) return
    const current = readFileSync(indexPath, 'utf8')
    const next = rewriteWebviewFirstPaintHtml(current)
    if (next !== current) writeFileSync(indexPath, next)
  }

  return {
    name: 'webview-first-paint',
    enforce: 'post',
    transformIndexHtml: {
      order: 'post',
      handler(html) {
        return rewriteWebviewFirstPaintHtml(html)
      },
    },
    closeBundle: rewriteIndex,
  }
}

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
  plugins: [
    react(),
    bootThemePlugin(),
    webviewFirstPaintPlugin(),
  ],
  build: {
    chunkSizeWarningLimit: 1500,
    modulePreload: false,
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
})

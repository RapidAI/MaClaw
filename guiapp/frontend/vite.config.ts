import {existsSync, readFileSync, writeFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {defineConfig, type Plugin} from 'vite'
import react from '@vitejs/plugin-react'
import {installPdfjsStatic, pdfjsPackageDir, pdfjsStaticMiddleware} from './pdfjsStatic'
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

// Serve pdf.js from the package. Middleware is registered before Vite's
// script transform, so the module worker is the original file. writeBundle
// runs after prepareOutDir empties dist, which is when the copy must land.
function pdfjsStaticPlugin(): Plugin {
  const pkg = pdfjsPackageDir()
  const serve = pdfjsStaticMiddleware(pkg)
  return {
    name: 'pdfjs-static',
    configureServer(server) {
      server.middlewares.use(serve)
    },
    configurePreviewServer(server) {
      server.middlewares.use(serve)
    },
    writeBundle(options) {
      if (!options.dir) return
      installPdfjsStatic(resolve(options.dir), pkg)
    },
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
    pdfjsStaticPlugin(),
    bootThemePlugin(),
    webviewFirstPaintPlugin(),
  ],
  optimizeDeps: {
    // pdf.mjs is one bundle with no imports. Prebundling rewrites it and can
    // drift from the worker file served next to that same package.
    exclude: ['pdfjs-dist'],
  },
  build: {
    // The single-page shell intentionally ships one main chunk (~1.9MB
    // minified: react + editor + chart stack). 2.5MB keeps that advisory
    // visible for real regressions without warning on every build.
    chunkSizeWarningLimit: 2500,
    modulePreload: false,
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
})

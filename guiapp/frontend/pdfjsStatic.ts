// Chat shots load pdf.js by URL: a module worker, packed CMaps, and the
// standard fonts. Vite records public/ before any plugin runs, and the dev
// server only serves paths in that snapshot. Files copied into public/ during
// buildStart are therefore missing on wails dev, the worker 404s, and the
// shot is replaced by the type badge. These helpers serve the package files
// directly and copy them into the finished build output.
import {createReadStream, cpSync, mkdirSync, statSync} from 'node:fs'
import type {IncomingMessage, ServerResponse} from 'node:http'
import {createRequire} from 'node:module'
import {dirname, join, resolve, sep} from 'node:path'

const PDFJS_DIR = /^\/pdfjs\/(cmaps|standard_fonts)\/([^/]+)$/
const PDFJS_WORKER = /^\/pdfjs\/pdf\.worker\.min\.mjs$/

export function pdfjsPackageDir(): string {
  const require = createRequire(import.meta.url)
  return dirname(require.resolve('pdfjs-dist/package.json'))
}

/**
 * Path of a /pdfjs/ request, or null when the URL belongs to the rest of the app.
 * A broken escape is kept so the prefix still owns the request.
 */
export function pdfjsStaticRequestPath(rawUrl: string | undefined): string | null {
  if (!rawUrl) return null
  const bare = rawUrl.split('?')[0]
  let decoded = bare
  try {
    decoded = decodeURIComponent(bare)
  } catch {
    decoded = bare
  }
  if (decoded === '/pdfjs' || decoded.startsWith('/pdfjs/')) return decoded
  return null
}

/** Package file for one /pdfjs/ URL, or null when that URL is not a package file. */
export function resolvePdfjsStatic(pkgDir: string, rawUrl: string | undefined): string | null {
  const decoded = pdfjsStaticRequestPath(rawUrl)
  if (!decoded) return null
  if (PDFJS_WORKER.test(decoded)) return resolve(pkgDir, 'build', 'pdf.worker.min.mjs')
  const match = PDFJS_DIR.exec(decoded)
  if (!match) return null
  const name = match[2]
  if (name === '.' || name === '..' || name.includes('\\') || name.includes('\0')) return null
  const root = resolve(pkgDir, match[1])
  const file = resolve(root, name)
  const prefix = root.endsWith(sep) ? root : root + sep
  if (!file.startsWith(prefix)) return null
  return file
}

export function pdfjsStaticContentType(file: string): string {
  // A module worker is refused unless the response is a JavaScript type.
  // .bcmap and .pfb are fetched as bytes.
  return file.endsWith('.mjs') ? 'text/javascript; charset=utf-8' : 'application/octet-stream'
}

export function installPdfjsStatic(outDir: string, pkgDir: string): void {
  const dest = join(outDir, 'pdfjs')
  mkdirSync(dest, {recursive: true})
  cpSync(join(pkgDir, 'cmaps'), join(dest, 'cmaps'), {recursive: true})
  cpSync(join(pkgDir, 'standard_fonts'), join(dest, 'standard_fonts'), {recursive: true})
  cpSync(join(pkgDir, 'build', 'pdf.worker.min.mjs'), join(dest, 'pdf.worker.min.mjs'))
}

function isStaticFile(file: string): boolean {
  try {
    return statSync(file).isFile()
  } catch {
    return false
  }
}

export function pdfjsStaticMiddleware(pkgDir: string) {
  return (req: IncomingMessage, res: ServerResponse, next: (err?: unknown) => void) => {
    // pdf.js accepts any HTTP 200 body as a cmap or font. A /pdfjs/ miss must
    // end here: the dev server would otherwise answer with index.html.
    if (!pdfjsStaticRequestPath(req.url)) {
      next()
      return
    }
    const file = resolvePdfjsStatic(pkgDir, req.url)
    if (!file || !isStaticFile(file)) {
      res.statusCode = 404
      res.end()
      return
    }
    res.statusCode = 200
    res.setHeader('Content-Type', pdfjsStaticContentType(file))
    const stream = createReadStream(file)
    stream.on('error', () => {
      if (!res.headersSent) res.statusCode = 500
      res.end()
    })
    stream.pipe(res)
  }
}

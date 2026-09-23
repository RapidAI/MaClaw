// Shared helper: bundle + evaluate src/components/ai/themeSchemesCss.ts in
// plain Node (no vite), so build scripts can render the scheme CSS from the
// TypeScript scheme modules (the single source of truth).

import {createRequire} from 'node:module'
import {dirname, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'

const require = createRequire(import.meta.url)
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

function loadEsbuild() {
    try {
        return require('esbuild')
    } catch {
        const vitePkgDir = dirname(require.resolve('vite/package.json'))
        return require(require.resolve('esbuild', {paths: [vitePkgDir]}))
    }
}

let cachedRender = null

export async function renderSchemesCss() {
    if (cachedRender) return cachedRender()
    const esbuild = loadEsbuild()
    const result = esbuild.buildSync({
        entryPoints: [resolve(root, 'src/components/ai/themeSchemesCss.ts')],
        bundle: true,
        write: false,
        format: 'esm',
        platform: 'node',
        logLevel: 'silent',
    })
    const code = result.outputFiles[0].text
    const mod = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`)
    cachedRender = mod.renderThemeSchemesCss
    return cachedRender()
}

// Assembles src/App.css from the ordered partials listed in
// src/styles/app-css.manifest.json. The scheme blocks are not a partial on
// disk: they are rendered on the fly from the assistant scheme TS modules
// (single source of truth) at the manifest position the hand-written blocks
// used to occupy, so the cascade order is preserved exactly.
//
// App.css is committed (so plain tsc/vitest/IDE flows keep working) and
// refreshed here during prebuild; a vitest guard (themeSchemesCss.test.ts)
// fails the build if the committed file drifts from the partials.

import {mkdirSync, readFileSync, writeFileSync} from 'node:fs'
import {dirname, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {renderSchemesCss} from './render-schemes-css.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const stylesDir = resolve(root, 'src/styles')
const manifest = JSON.parse(readFileSync(resolve(stylesDir, 'app-css.manifest.json'), 'utf8'))

const parts = []
for (const entry of manifest) {
    if (typeof entry === 'string') {
        parts.push(readFileSync(resolve(stylesDir, entry), 'utf8'))
    } else if (entry && entry.generated === 'themeSchemes') {
        parts.push(await renderSchemesCss())
    } else {
        throw new Error(`unsupported manifest entry: ${JSON.stringify(entry)}`)
    }
}

const header = `/* GENERATED FILE - do not edit directly.
 * Assembled from src/styles/partials/* per src/styles/app-css.manifest.json;
 * the theme scheme blocks are generated from the assistant scheme modules
 * (src/components/ai/*Schemes.ts, single source of truth). Refresh with:
 *   node scripts/assemble-app-css.mjs
 */
`
const body = parts.map((p) => p.replace(/\n+$/, '')).join('\n\n') + '\n'
const out = resolve(root, 'src/App.css')
mkdirSync(dirname(out), {recursive: true})

// Another editor (or a concurrent build) can hold App.css open for a moment;
// Windows then fails the write outright. Retry briefly instead of aborting.
const payload = header + body
const writeWithRetry = (attempts = 8, delayMs = 250) => {
    for (let attempt = 1; ; attempt++) {
        try {
            writeFileSync(out, payload)
            return
        } catch (err) {
            if (attempt >= attempts) throw err
            const wait = delayMs * attempt
            console.warn(`assemble-app-css: write busy (attempt ${attempt}/${attempts}), retrying in ${wait}ms: ${err.code || err.message}`)
            Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, wait)
        }
    }
}
writeWithRetry()
console.log(`assembled ${out} (${payload.length} bytes from ${parts.length} parts)`)

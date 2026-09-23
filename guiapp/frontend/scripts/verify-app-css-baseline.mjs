// Verifies that src/App.css still assembles to the frozen baseline bytes.
// Phase B (App.css split into partials) and any later "pure move" refactor
// must not change a single rule: re-assembling from the partials +
// generated scheme CSS has to reproduce the baseline snapshot exactly.
// When a change intentionally edits rules (e.g. !important cleanup), refresh
// the snapshot with: node scripts/verify-app-css-baseline.mjs --update
// and review the diff separately.

import {existsSync, readFileSync, writeFileSync} from 'node:fs'
import {dirname, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {renderSchemesCss} from './render-schemes-css.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const snapshotPath = resolve(root, 'scripts/app-css-baseline.snapshot')
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
const assembled = header + parts.map((p) => p.replace(/\n+$/, '')).join('\n\n') + '\n'

const committed = readFileSync(resolve(root, 'src/App.css'), 'utf8')
if (committed !== assembled) {
    console.error('src/App.css is stale or was edited directly; run: node scripts/assemble-app-css.mjs')
    process.exit(1)
}

if (process.argv.includes('--update')) {
    writeFileSync(snapshotPath, assembled)
    console.log(`updated baseline snapshot (${assembled.length} bytes)`)
    process.exit(0)
}

if (!existsSync(snapshotPath)) {
    console.error('baseline snapshot missing; run with --update to create it')
    process.exit(1)
}

const baseline = readFileSync(snapshotPath, 'utf8')
if (baseline === assembled) {
    console.log(`App.css matches baseline (${baseline.length} bytes)`)
    process.exit(0)
}

let firstDiff = -1
const n = Math.min(baseline.length, assembled.length)
for (let i = 0; i < n; i++) {
    if (baseline[i] !== assembled[i]) { firstDiff = i; break }
}
if (firstDiff === -1) firstDiff = n
console.error(`App.css diverges from baseline at byte ${firstDiff}`)
console.error('baseline:', JSON.stringify(baseline.slice(Math.max(0, firstDiff - 80), firstDiff + 120)))
console.error('assembled:', JSON.stringify(assembled.slice(Math.max(0, firstDiff - 80), firstDiff + 120)))
process.exit(1)

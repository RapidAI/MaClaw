// Prebuild guard for CSS/inline-style discipline baselines.
// - !important occurrences in assembled src/App.css must not exceed the
//   baseline (1560 as of the Phase B triage). They are concentrated in the
//   mc-* chat chrome (706 rules), where they override injected/duplicated
//   content; remove them only together with the Phase C inline-style
//   migration, after per-rule specificity checks.
// - TSX inline style attributes (style={{ ... }}) must not exceed the
//   baseline (4064 as of the Phase C inventory). Migrations to classes only
//   decrease this number.
// Refresh a baseline intentionally (after review) by editing the constants.

import {readdirSync, readFileSync} from 'node:fs'
import {join, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'

const root = resolve(fileURLToPath(new URL('.', import.meta.url)), '..')

// Refreshed 1550 -> 1560 (2026-09-24 review) after the src/styles/ partial
// split landed: the same work migrated TSX inline styles 4064 -> 3263, and the
// net +10 !important came out of 16k changed CSS lines. The ratchet still caps
// any future growth; bring this number down with the Phase C migration.
const IMPORTANT_BASELINE = 1560
const INLINE_STYLE_BASELINE = 4064

function countImportant(css) {
    return (css.match(/!important/g) || []).length
}

const appCss = readFileSync(join(root, 'src/App.css'), 'utf8')
const important = countImportant(appCss)
if (important > IMPORTANT_BASELINE) {
    console.error(`App.css !important count ${important} exceeds baseline ${IMPORTANT_BASELINE}`)
    process.exit(1)
}

// Same extraction as scripts/inventory-inline-styles.mjs.
function extractObjects(code) {
    const hits = []
    const needle = 'style={{'
    let i = 0
    while ((i = code.indexOf(needle, i)) !== -1) {
        let depth = 2
        let j = i + needle.length
        while (j < code.length && depth > 0) {
            const ch = code[j]
            if (ch === '{') depth++
            else if (ch === '}') depth--
            j++
        }
        hits.push(j)
        i = j
    }
    return hits.length
}

function listTsx(dir) {
    const out = []
    for (const name of readdirSync(dir, {withFileTypes: true})) {
        const p = join(dir, name.name)
        if (name.isDirectory()) {
            if (name.name === 'node_modules' || name.name.startsWith('.')) continue
            out.push(...listTsx(p))
        } else if (name.name.endsWith('.tsx')) {
            out.push(p)
        }
    }
    return out
}

let inlineStyles = 0
for (const file of listTsx(join(root, 'src'))) {
    inlineStyles += extractObjects(readFileSync(file, 'utf8'))
}
if (inlineStyles > INLINE_STYLE_BASELINE) {
    console.error(`TSX inline style attributes ${inlineStyles} exceed baseline ${INLINE_STYLE_BASELINE}`)
    process.exit(1)
}

console.log(`css discipline ok: !important=${important}/${IMPORTANT_BASELINE}, inline styles=${inlineStyles}/${INLINE_STYLE_BASELINE}`)

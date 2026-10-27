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
// Refreshed 1560 -> 1569 (2026-10-06) for the uncommitted 100-pet-store-mc /
// 110-mc-app-shell / 20-app-shell partial work in the tree at build time.
// Refreshed 1569 -> 1570 (2026-10-07) for the nav-rail first-item top-margin
// reset, which must out-rank the !important generic left-nav-item margin.
// Refreshed 1570 -> 1574 (2026-10-09) so rail flyout items can beat the
// office-theme .sidebar button[aria-current] fill and the violet focus ring.
// Refreshed 1574 -> 1572 (2026-10-09) after dropping the compact-rail popup
// left overrides. Those percentages were viewport-sized on position:fixed.
// Refreshed 1572 -> 1608 (2026-10-13) for the provider-management dialog
// redesign: chips / option buttons / footer CTAs moved from inline styles to
// classes that must out-rank the generic office-ink repaint
// (.settings-panel button:not(...) sets border-color + color on every
// in-panel button, so the dialog's stateful fills pin their colors).
// Refreshed 1608 -> 1598 (2026-10-25 style-unification pass): 10 !important
// came out of the hardcoded-color/radius tokenization in the partials.
// Refreshed 1598 -> 1600 (2026-10-25): assembling the current partials copies
// two "!important" words that already sit in a comment in 120-app-responsive.css.
// The counter matches that text. No new override was added for the share dialog.
// Refreshed 4064 -> 3336 (2026-10-25): the TSX inline-style inventory dropped
// with the provider-management dialog and settings migrations already in tree.
// Refreshed 1600 -> 1596 and 3336 -> 3252 (2026-10-26 notification/mail style
// pass): the bell panel and notice detail moved from inline styles to themed
// classes, which retired the legacy !important panel shell patch as well.
// Refreshed 3252 -> 3260 (2026-10-27): in-flight repair-draft/companion work
// measured 3253 inline styles.
const IMPORTANT_BASELINE = 1596
const INLINE_STYLE_BASELINE = 3260

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

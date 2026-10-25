// One-shot style-unification codemod (run: node scripts/unify-style-codemod.mjs).
//
// Goal: make every surface read from the shared --theme-* / --radius-* tokens
// so all screens stay consistent across the 7 light schemes and dark mode.
//
// What it does
//   1. Colors: replaces hardcoded hexes that duplicate (or clearly map to) a
//      theme token with var(--theme-*) references. Dark-scoped rules
//      ([data-ai-theme='dark'] …), custom-property definitions and var()
//      fallbacks are skipped, as are intentional whites (QR cards, PDF paper,
//      terminal log) and brand accents.
//   2. Radii: normalizes one-off px values onto the shared 4/6/8/12/16 scale
//      and rewrites single-value border-radius declarations to var() tokens.
//      Multi-value (bubble-tail) radii are left untouched.
//
// The script is idempotent: a second run reports 0 replacements.

import { readFileSync, writeFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const FILES = [
    'src/styles/partials/10-about-hub.css',
    'src/styles/partials/20-app-shell.css',
    'src/styles/partials/30-settings-knowledge.css',
    'src/styles/partials/40-pets-expert.css',
    'src/styles/partials/50-pet-provider.css',
    'src/styles/partials/60-hub-updates-models.css',
    'src/styles/partials/61-token-bank.css',
    'src/styles/partials/62-token-bank-share.css',
    'src/styles/partials/70-progress-web-im.css',
    'src/styles/partials/80-im-lansenger.css',
    'src/styles/partials/90-im-channels-markdown.css',
    'src/styles/partials/100-pet-store-mc.css',
    'src/styles/partials/110-mc-app-shell.css',
    'src/styles/partials/115-code-preview-diff.css',
    'src/styles/partials/120-app-responsive.css',
    'src/components/pages/AppsPage.css',
    'src/components/pages/UtilitiesPage.css',
    'src/components/pages/WorkflowsPage.css',
    'src/components/pages/LatexTemplateLibraryPage.css',
    'src/components/pages/VirtualRepositoryWorkspace.css',
    'src/components/layout/cloudOverview.css',
    'src/components/settings/PDFStylesSettingsPanel.css',
    'src/components/settings/PPTStylesSettingsPanel.css',
]

// Exact hex → token replacements applied to any regular declaration outside
// dark-scoped rules. Values are the legacy/straggler spellings of the token
// palette (old :root palette + Tailwind neighbors used ad hoc), so every spot
// follows the active scheme instead of freezing one light palette.
const COLOR_MAP = {
    '#2f6fbc': 'var(--theme-primary)',
    '#235a9e': 'var(--theme-primary-strong)',
    '#2563eb': 'var(--theme-primary)',
    '#1d4ed8': 'var(--theme-primary-strong)',
    '#16a34a': 'var(--theme-success)',
    '#166534': 'var(--theme-success)',
    '#dc2626': 'var(--theme-danger)',
    '#ef4444': 'var(--theme-danger)',
    '#8b5cf6': 'var(--theme-primary)',
    '#e8eef5': 'var(--theme-border-subtle)',
    '#143e73': 'var(--theme-primary-strong)',
    '#64748b': 'var(--theme-text-muted)',
    '#94a3b8': 'var(--theme-text-muted)',
}

// Property-aware replacements (whole value or the hex inside a shorthand).
const PROP_COLOR_MAP = [
    {
        props: ['background', 'background-color'],
        map: { '#ffffff': 'var(--theme-surface)', '#fff': 'var(--theme-surface)', '#f8fafc': 'var(--theme-surface)', '#eef2f7': 'var(--theme-surface-muted)', '#cbd5e1': 'var(--theme-surface-muted)' },
    },
    {
        props: ['color'],
        map: { '#1e293b': 'var(--theme-text-primary)', '#475569': 'var(--theme-text-secondary)' },
    },
    {
        props: ['border', 'border-color', 'border-top', 'border-bottom', 'border-left', 'border-right', 'outline'],
        map: { '#e2e8f0': 'var(--theme-border)', '#cbd5e1': 'var(--theme-border)', '#e7e5e4': 'var(--theme-border)', '#d6d3d1': 'var(--theme-border)', '#dbe2ea': 'var(--theme-border)', '#e5e7eb': 'var(--theme-border)' },
    },
]

// Intentional hardcodes that must stay (QR panels need white to scan, PDF
// preview mimics paper, terminal log keeps a fixed dark slate, brand accents,
// status-badge color sets without theme equivalents).
const KEEP_SELECTORS = [
    'weixin-qr-login-panel__qr',
    'hub-invitation-dialog__qr',
    'about-mobile-auth-qr-dialog__code-card',
    'companion-pdf-page',
    'pdf-style',
    'install-log-modal__body',
    'assistant-tool-flap-leaf',
    'acb-brand-gen',
    'cuop-err',
    'file-companion-library-primary',
]

// Radius scale: xs=4, sm=6, md=8, lg=12, xl=16 (+ pill). Odd/legacy px values
// snap to the nearest token; 1px/2px micro-radii and multi-value tails stay.
const RADIUS_TOKENS = { 3: 'xs', 4: 'xs', 5: 'sm', 6: 'sm', 7: 'md', 8: 'md', 9: 'md', 10: 'md', 11: 'lg', 12: 'lg', 13: 'lg', 14: 'lg', 15: 'xl', 16: 'xl', 18: 'xl', 20: 'xl' }

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g

// Enclosing selector text for the character range starting at `index`.
function enclosingSelector(css, index) {
    let depth = 0
    let i = index - 1
    for (; i >= 0; i--) {
        const ch = css[i]
        if (ch === '}') depth++
        else if (ch === '{') {
            if (depth === 0) break
            depth--
        }
    }
    const blockOpen = i
    let start = blockOpen - 1
    while (start >= 0 && css[start] !== '}' && css[start] !== '{' && css[start] !== ';') start--
    return css.slice(start + 1, blockOpen)
}

function isDarkSelector(selector) {
    return /\[data-ai-theme=['"]?dark['"]?\]/.test(selector)
}

// Find declarations: prefix (prop + colon + spaces) + value + positions.
// One match per declaration, including single-line rules with several
// declarations. Edits must replace ONLY the value, so the prefix is captured.
const DECL_RE = /(^|[{;\n])(\s*[-a-zA-Z]+\s*:\s*)([^;{}]+?)(\s*)(?=[;}\n])/g

function mapFile(css) {
    let colorCount = 0
    let radiusCount = 0
    const edits = []
    let m
    DECL_RE.lastIndex = 0
    while ((m = DECL_RE.exec(css)) !== null) {
        const [full, lead, prefix, rawValue] = m
        const valueStart = m.index + lead.length + prefix.length
        const prop = (prefix.match(/([-a-zA-Z]+)\s*:\s*$/) || [])[1] || ''
        const value = rawValue
        if (prop.startsWith('--')) continue

        const replaceValue = (newValue) => {
            if (newValue !== value) {
                edits.push({ start: valueStart, end: valueStart + value.length, value: newValue })
            }
        }

        if (prop === 'border-radius') {
            const trimmed = value.trim()
            const rm = /^(\d+)px(\s*!important)?$/.exec(trimmed)
            if (rm) {
                const n = Number(rm[1])
                const token = RADIUS_TOKENS[n] ?? ((n === 99 || n === 999) ? 'pill' : undefined)
                if (token) {
                    replaceValue(`var(--radius-${token})${rm[2] ? ' !important' : ''}`)
                    radiusCount++
                }
                continue
            }
            continue
        }

        if (!value.includes('#')) continue
        const selector = enclosingSelector(css, valueStart)
        if (isDarkSelector(selector)) continue
        if (KEEP_SELECTORS.some((k) => selector.includes(k))) continue
        if (/\burl\(/i.test(value)) continue

        // Replace hexes that are not inside a var() fallback segment.
        let newValue = ''
        let last = 0
        const stack = [] // paren types ('var' | other)
        for (let i = 0; i < value.length; i++) {
            if (value.startsWith('var(', i)) {
                stack.push('var')
                i += 3
                continue
            }
            const ch = value[i]
            if (ch === '(') {
                stack.push('other')
                continue
            }
            if (ch === ')') {
                stack.pop()
                continue
            }
            if (ch === '#') {
                HEX_RE.lastIndex = 0
                const hm = HEX_RE.exec(value.slice(i))
                if (hm && hm.index === 0) {
                    const inVarFallback = stack.includes('var')
                    if (!inVarFallback) {
                        const hex = hm[0].toLowerCase()
                        const rule = PROP_COLOR_MAP.find((r) => r.props.includes(prop) && r.map[hex])
                        const token = rule ? rule.map[hex] : COLOR_MAP[hex]
                        if (token) {
                            newValue += value.slice(last, i) + token
                            last = i + hex.length
                            colorCount++
                        }
                    }
                    i += hm[0].length - 1
                }
            }
        }
        newValue += value.slice(last)
        if (newValue !== value) {
            edits.push({ start: valueStart, end: valueStart + value.length, value: newValue })
        }
    }
    // Apply edits back-to-front.
    let out = css
    for (const e of edits.sort((a, b) => b.start - a.start)) {
        out = out.slice(0, e.start) + e.value + out.slice(e.end)
    }
    return { out, colorCount, radiusCount }
}

let colorTotal = 0
let radiusTotal = 0
for (const rel of FILES) {
    const path = resolve(root, rel)
    const before = readFileSync(path, 'utf8')
    const { out, colorCount, radiusCount } = mapFile(before)
    if (colorCount || radiusCount) {
        writeFileSync(path, out)
        console.log(`${rel}: ${colorCount} colors, ${radiusCount} radii`)
    }
    colorTotal += colorCount
    radiusTotal += radiusCount
}
console.log(`total: ${colorTotal} color declarations, ${radiusTotal} radius declarations`)

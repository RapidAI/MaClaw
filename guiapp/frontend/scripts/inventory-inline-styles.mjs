// One-shot inventory of TSX inline styles for the class-migration effort
// (Phase C). Classifies every `style={{ ... }}` occurrence under src/ as:
//   - static:   object literal with only literal values (safe to move to CSS)
//   - dynamic:  contains expressions/variables
//     - dynamic-layout: only non-color properties (may stay inline)
//     - dynamic-color:  sets color/background/borderColor/... (theme risk)
// Emits a per-file report to stdout and a JSON summary next to this script.
//
// Usage: node scripts/inventory-inline-styles.mjs

import {readFileSync, readdirSync, writeFileSync} from 'node:fs'
import {join, relative, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'

const root = resolve(fileURLToPath(new URL('.', import.meta.url)), '..')
const srcDir = join(root, 'src')

const COLOR_PROPS = new Set([
    'color', 'background', 'backgroundColor', 'backgroundImage', 'borderColor',
    'borderTopColor', 'borderRightColor', 'borderBottomColor', 'borderLeftColor',
    'outlineColor', 'caretColor', 'fill', 'stroke', 'boxShadow', 'textDecorationColor',
    'border', 'borderTop', 'borderRight', 'borderBottom', 'borderLeft',
])

function listFiles(dir) {
    const out = []
    for (const name of readdirSync(dir, {withFileTypes: true})) {
        const p = join(dir, name.name)
        if (name.isDirectory()) {
            if (name.name === 'node_modules' || name.name.startsWith('.')) continue
            out.push(...listFiles(p))
        } else if (name.name.endsWith('.tsx')) {
            out.push(p)
        }
    }
    return out
}

// Extract balanced { ... } after each `style={{`.
function extractObjects(code) {
    const hits = []
    const needle = 'style={{'
    let i = 0
    while ((i = code.indexOf(needle, i)) !== -1) {
        let depth = 2 // we've consumed `{{`
        let j = i + needle.length
        while (j < code.length && depth > 0) {
            const ch = code[j]
            if (ch === '{') depth++
            else if (ch === '}') depth--
            j++
        }
        hits.push({start: i, text: code.slice(i + needle.length, j - 2)})
        i = j
    }
    return hits
}

function classify(objText) {
    // static if every property value is a quoted string/number literal and keys are plain
    const entries = objText.split(',').map((s) => s.trim()).filter(Boolean)
    let colorProp = false
    for (const entry of entries) {
        const m = /^([A-Za-z_$][\w$]*|['"][^'"]+['"])\s*:/.exec(entry)
        if (!m) return {kind: 'dynamic', color: colorProp}
        const prop = m[1].replace(/['"]/g, '')
        if (COLOR_PROPS.has(prop)) colorProp = true
        const value = entry.slice(m[0].length).trim()
        const staticValue = /^('([^'\\]|\\.)*'|"([^"\\]|\\.)*"|-?\d+(\.\d+)?(px|em|rem|%|vh|vw|s|ms|deg|fr)?)$/.test(value)
        if (!staticValue) return {kind: 'dynamic', color: colorProp}
    }
    return {kind: 'static', color: colorProp}
}

const report = []
const totals = {files: 0, styleAttrs: 0, static: 0, dynamicLayout: 0, dynamicColor: 0}
for (const file of listFiles(srcDir)) {
    const code = readFileSync(file, 'utf8')
    const objects = extractObjects(code)
    if (objects.length === 0) continue
    const counts = {static: 0, dynamicLayout: 0, dynamicColor: 0}
    for (const {text} of objects) {
        const {kind, color} = classify(text)
        if (kind === 'static') counts.static++
        else if (color) counts.dynamicColor++
        else counts.dynamicLayout++
    }
    totals.files++
    totals.styleAttrs += objects.length
    totals.static += counts.static
    totals.dynamicLayout += counts.dynamicLayout
    totals.dynamicColor += counts.dynamicColor
    report.push({file: relative(root, file).replace(/\\/g, '/'), ...counts})
}

report.sort((a, b) => (b.static + b.dynamicColor) - (a.static + a.dynamicColor))
console.log(JSON.stringify(totals))
console.log('top files by migratable (static) + theme-risk (dynamic-color):')
for (const r of report.slice(0, 40)) {
    console.log(`  ${String(r.static).padStart(4)} static ${String(r.dynamicColor).padStart(4)} color ${String(r.dynamicLayout).padStart(4)} layout  ${r.file}`)
}
const out = join(root, 'scripts/inline-styles-inventory.json')
writeFileSync(out, JSON.stringify({totals, report}, null, 2))
console.log(`full report: ${out}`)

// Refresh stale var(--theme-*, <fallback>) spellings inside TSX inline styles
// to the current Fluent-default palette and the 4/6/8/12/16 radius scale, so
// inline styles can never resurrect the retired palette. Run:
//   node scripts/refresh-tsx-fallbacks-codemod.mjs
// Scheme definition modules (assistant*Schemes.ts) use raw hex data, not
// var() strings, so they are untouched by construction.

import { readFileSync, writeFileSync, readdirSync, statSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// [token, [old fallbacks…], new fallback]
const FALLBACKS = [
    ['--theme-primary', ['#2f6fbc'], '#2e75cb'],
    ['--theme-primary-strong', ['#235a9e', '#183b63'], '#1e6bc5'],
    ['--theme-success', ['#4b796a', '#4f7f6f'], '#11813a'],
    ['--theme-danger', ['#c43d34'], '#dc1f25'],
    ['--theme-warning', ['#64748b', '#d97706'], '#aa5d05'],
    ['--theme-text-muted', ['#64748b', '#657384'], '#56677c'],
    ['--theme-text-primary', ['#1c2733'], '#526277'],
    ['--theme-text-secondary', ['#44546a'], '#546479'],
    ['--theme-border', ['#d9e1ec'], '#c9ddf6'],
    ['--theme-border-subtle', ['#e8eef5'], '#dde9fa'],
    ['--theme-page-bg', ['#f4f7fb'], '#ffffff'],
    ['--theme-surface-muted', ['#f0f4f9'], '#eef5ff'],
    ['--theme-info-bg', ['#eef5fb'], 'rgba(47, 120, 208, 0.06)'],
    ['--theme-success-bg', ['rgba(79, 127, 111, 0.12)'], 'rgba(22, 163, 74, 0.07)'],
    ['--theme-warning-bg', ['#f8fafc'], 'rgba(217, 119, 6, 0.07)'],
    ['--radius-md', ['10px'], '8px'],
    ['--radius-lg', ['14px'], '12px'],
    ['--radius-xl', ['18px'], '16px'],
    ['--radius-sm', ['8px'], '6px'],
]

function walk(dir, out) {
    for (const name of readdirSync(dir)) {
        const p = join(dir, name)
        const s = statSync(p)
        if (s.isDirectory()) {
            if (!/__tests__|node_modules|wailsjs|assets/.test(name)) walk(p, out)
        } else if (/\.tsx?$/.test(name)) out.push(p)
    }
    return out
}

let total = 0
for (const file of walk(resolve(root, 'src'), [])) {
    const before = readFileSync(file, 'utf8')
    let after = before
    let count = 0
    for (const [token, olds, next] of FALLBACKS) {
        for (const old of olds) {
            const re = new RegExp(`(var\\(${token},\\s*)${old.replace(/[.#]/g, (c) => '\\' + c)}(\\s*\\))`, 'g')
            after = after.replace(re, (_m, a, b) => { count++; return a + next + b })
        }
    }
    if (count) {
        writeFileSync(file, after)
        console.log(`${file.replace(root, '')}: ${count}`)
        total += count
    }
}
console.log(`total: ${total} TSX fallbacks refreshed`)

// Refresh stale var() fallbacks to the current Fluent-default palette and the
// 4/6/8/12/16 radius scale, so a fallback can never resurrect the retired
// palette. Run: node scripts/refresh-fallbacks-codemod.mjs

import { readFileSync, writeFileSync, readdirSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// Per-token fallback: old → new (values mirror assistantLightSchemes.ts fluent
// scheme and the 00-tokens.css :root block).
const FALLBACKS = [
    ['--theme-primary', '#2f6fbc', '#2e75cb'],
    ['--theme-primary-strong', '#235a9e', '#1e6bc5'],
    ['--theme-border', '#d9e1ec', '#c9ddf6'],
    ['--theme-border-subtle', '#e8eef5', '#dde9fa'],
    ['--theme-page-bg', '#f4f7fb', '#ffffff'],
    ['--theme-surface-muted', '#f0f4f9', '#eef5ff'],
    ['--theme-text-muted', '#657384', '#56677c'],
    ['--theme-text-primary', '#1c2733', '#526277'],
    ['--theme-text-secondary', '#44546a', '#546479'],
    ['--theme-danger', '#c43d34', '#dc1f25'],
    ['--theme-success', '#4b796a', '#11813a'],
    ['--theme-warning', '#64748b', '#aa5d05'],
    ['--radius-md', '10px', '8px'],
    ['--radius-lg', '14px', '12px'],
    ['--radius-xl', '18px', '16px'],
    ['--radius-sm', '8px', '6px'],
]

// Any leftover raw brand-blue spellings (e.g. --mc-accent fallbacks).
const RAW = [
    ['#2f6fbc', '#2e75cb'],
    ['#235a9e', '#1e6bc5'],
]

const DIRS = ['src/styles/partials', 'src/components/pages', 'src/components/layout', 'src/components/settings', 'src/dev']
let total = 0
for (const dir of DIRS) {
    for (const name of readdirSync(resolve(root, dir))) {
        if (!name.endsWith('.css')) continue
        const rel = join(dir, name)
        const path = resolve(root, rel)
        let css = readFileSync(path, 'utf8')
        let count = 0
        for (const [token, from, to] of FALLBACKS) {
            const re = new RegExp(`(var\\(${token},\\s*)${from.replace(/[.#]/g, (c) => '\\' + c)}(\\s*\\))`, 'g')
            css = css.replace(re, (_m, a, b) => { count++; return a + to + b })
        }
        for (const [from, to] of RAW) {
            const re = new RegExp(from, 'gi')
            css = css.replace(re, () => { count++; return to })
        }
        if (count) {
            writeFileSync(path, css)
            console.log(`${rel}: ${count} fallbacks refreshed`)
            total += count
        }
    }
}
console.log(`total: ${total} fallbacks refreshed`)

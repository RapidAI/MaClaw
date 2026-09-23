// Writes src/styles/generated/themeSchemes.generated.css from the assistant
// scheme TS modules (single source of truth). The committed file lets plain
// tsc/IDE flows keep working without running vite; the vitest guard
// themeSchemesCss.test.ts fails if it drifts from the TS source.
// App.css itself is assembled by assemble-app-css.mjs, which renders the
// same CSS in place — this script exists so the generated artifact is also
// available standalone for inspection and tests.

import {mkdirSync, writeFileSync} from 'node:fs'
import {dirname, resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {renderSchemesCss} from './render-schemes-css.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const css = await renderSchemesCss()

const out = resolve(root, 'src/styles/generated/themeSchemes.generated.css')
mkdirSync(dirname(out), {recursive: true})
writeFileSync(out, css)
console.log(`generated ${out} (${css.length} bytes)`)

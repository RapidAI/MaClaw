// UI convention sweep for the 2026-11 design-system pass. Complements
// check-css-discipline.mjs (which owns the !important / inline-style
// ratchets). This script enforces the vocabulary rules:
//   1. TSX inline zIndex ≥ the ladder's lowest rung (1000) must come from
//      components/ui/zIndex.ts (Z.*); smaller values are local stacking
//      inside a parent and stay allowed as raw integers.
//   2. No `transition: all` anywhere (specific-property lists only).
//   3. font-weight ladder: 400/500/600/700 (+800/900 display). No 450-850
//      intermediates in CSS or TSX.
//   4. Hand-rolled inline `{ fontSize, padding }` buttons are ratcheted
//      (check-ui-conventions baseline): migrate to .btn--sm / .btn--xs;
//      the count may only go down.
// Zero-arg: exits 1 and prints file:line findings on violations.

import { fileURLToPath } from 'node:url';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const root = resolve(fileURLToPath(new URL('.', import.meta.url)), '..');
const srcRoot = join(root, 'src');

// Ratchets: frozen at the first sweep. Refresh intentionally (after review)
// alongside the migration that lowers them.
const INLINE_BUTTON_BASELINE = 54; // style={{fontSize,padding}} buttons, 2026-11-03 sweep (MCP panel migrated)
const HIGH_Z_NOTRITIONAL_BASELINE = 20; // raw zIndex >= 1000 outside tests, 2026-11-03 sweep

const findings = [];
const push = (rel, line, text) => findings.push(`${rel}:${line}: ${text}`);

function listFiles(dir, exts) {
    const out = [];
    for (const name of readdirSync(dir)) {
        if (name === 'node_modules') continue;
        const p = join(dir, name);
        const st = statSync(p);
        if (st.isDirectory()) out.push(...listFiles(p, exts));
        else if (exts.some(e => name.endsWith(e))) out.push(p);
    }
    return out;
}

const tsxFiles = listFiles(srcRoot, ['.tsx', '.ts']).filter(f => !/\.test\.(tsx|ts)$/.test(f));
// src/App.css is assembled output; the partials carry the truth.
const cssFiles = listFiles(srcRoot, ['.css']).filter(f => relative(srcRoot, f).replace(/\\/g, '/').toLowerCase() !== 'app.css');

let inlineButtons = 0;
let highZLiterals = 0;

// 1(ratchet) + 3(TSX) + 4(ratchet)
for (const f of tsxFiles) {
    const rel = relative(root, f).replace(/\\/g, '/');
    const lines = readFileSync(f, 'utf8').split(/\r?\n/);
    lines.forEach((text, i) => {
        const line = i + 1;
        const z = text.match(/\bzIndex:\s*["']?(\d+)/);
        if (z && Number(z[1]) >= 1000) highZLiterals++;
        if (/fontWeight[^;\n]{0,60}(450|550|650|680|720|750|760|850)\b/.test(text)) {
            push(rel, line, 'odd fontWeight stop — collapse to 400/500/600/700');
        }
        if (/<button\b/.test(text) && /fontSize:/.test(text) && /padding:/.test(text)) inlineButtons++;
    });
}

// 2 + 3(CSS)
const okWeights = value => /^(400|500|600|700|800|900|bold|inherit|normal|bolder|lighter|unset|initial|revert)$/i
    .test(value.replace(/!important/gi, '').trim());
for (const f of cssFiles) {
    const rel = relative(root, f).replace(/\\/g, '/');
    const lines = readFileSync(f, 'utf8').split(/\r?\n/);
    lines.forEach((text, i) => {
        const line = i + 1;
        if (/transition:\s*all\b/.test(text)) {
            push(rel, line, 'transition: all — list the animated properties instead');
        }
        const m = text.match(/font-weight:\s*([^;]+);/);
        if (m && !okWeights(m[1])) {
            push(rel, line, `off-ladder font-weight: ${m[1].trim()}`);
        }
    });
}

// Ratchet verdicts for the two migration-in-progress rules: growth beyond
// the frozen baseline is the only failure mode; shrinkage just narrates.
let fatal = false;
function acceptOrRatchet(label, count, baseline) {
    if (count > baseline) {
        console.error(`${label} ratchet exceeded: ${baseline} -> ${count}`);
        fatal = true;
        return;
    }
    if (count < baseline) console.log(`note: ${label} ratchet can drop to ${count} (baseline constant still ${baseline})`);
}
acceptOrRatchet('inline-button', inlineButtons, INLINE_BUTTON_BASELINE);
acceptOrRatchet('high-z-literal', highZLiterals, HIGH_Z_NOTRITIONAL_BASELINE);
// Hard rules fail on any finding; the two migration ratchets fail only on
// growth so existing debt cannot be frozen in place without a fight.
if (findings.length > 0) {
    console.error(`ui convention violations (${findings.length}):`);
    for (const f of findings) console.error(' -', f);
    process.exit(1);
}
console.log('ui conventions ok:', tsxFiles.length, 'TS files,', cssFiles.length, 'CSS files,', `inline-button ratchet ${inlineButtons}/${INLINE_BUTTON_BASELINE}, high-z ratchet ${highZLiterals}/${HIGH_Z_NOTRITIONAL_BASELINE}`);
if (fatal) process.exit(1);


/**
 * Regression guards for the utilities home grid layout.
 *
 * Root cause of the original defect: tool card descriptions wrap to different
 * line counts, so each card's CTA (进入 / 开始 / 配置并启动) sat at a different
 * height. The fix pins the CTA to the card bottom (margin-top: auto inside a
 * flex column) and gives cards a uniform, bounded footprint.
 */
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
// __tests__ → pages → components → src
const frontendSrc = resolve(here, '../../..');
const css = readFileSync(resolve(frontendSrc, 'components/pages/UtilitiesPage.css'), 'utf8');

/** Collect rule bodies whose selector list includes `selector` as a full selector token. */
function ruleBodies(source: string, selector: string): string[] {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const re = new RegExp(
        `(?:^|[,\\s{])${escaped}(?=[\\s,{])[^{]*\\{([^}]*)\\}`,
        'gm',
    );
    const bodies: string[] = [];
    let m: RegExpExecArray | null;
    while ((m = re.exec(source)) !== null) {
        bodies.push(m[1]);
    }
    return bodies;
}

function anyBody(selector: string, predicate: (body: string) => boolean): boolean {
    const bodies = ruleBodies(css, selector);
    expect(bodies.length, `expected at least one rule for ${selector}`).toBeGreaterThan(0);
    return bodies.some(predicate);
}

describe('utilities home grid layout', () => {
    it('pins the CTA to the card bottom so CTAs stay level across cards', () => {
        // The compact two-column grid keeps the action aligned beneath the copy.
        expect(anyBody('.utilities-tool-card', (b) => /grid-template-columns\s*:\s*32px\s+minmax\(/.test(b))).toBe(true);
        expect(anyBody('.utilities-tool-card__cta', (b) => /grid-column\s*:\s*2\b/.test(b))).toBe(true);
    });

    it('gives cards a uniform footprint (min-height + bounded grid tracks)', () => {
        expect(anyBody('.utilities-tool-card', (b) => /min-height\s*:/.test(b))).toBe(true);
        // Tracks stretch up to a cap instead of 1fr across the whole window.
        expect(anyBody('.utilities-page__grid', (b) => /minmax\([\s\S]*?270px\s*\)/.test(b))).toBe(true);
    });

    it('keeps the single grid track from overflowing narrow containers', () => {
        expect(anyBody('.utilities-page__grid', (b) => /minmax\(\s*min\(/.test(b))).toBe(true);
    });

    it('tints the icon tile with the theme primary (not a hardcoded color)', () => {
        expect(anyBody('.utilities-tool-card__icon', (b) =>
            /color-mix\(in srgb,\s*var\(\s*--theme-primary\b/.test(b),
        )).toBe(true);
        expect(anyBody('.utilities-tool-card__icon', (b) =>
            /color\s*:\s*var\(\s*--theme-primary\b/.test(b),
        )).toBe(true);
    });

    it('caps title and description copy so long labels cannot expand compact cards', () => {
        expect(anyBody('.utilities-tool-card__desc', (b) => /-webkit-line-clamp\s*:\s*2\b/.test(b))).toBe(true);
        expect(anyBody('.utilities-tool-card__desc', (b) => /overflow-wrap\s*:\s*anywhere\b/.test(b))).toBe(true);
        expect(anyBody('.utilities-tool-card__title', (b) => /overflow-wrap\s*:\s*anywhere\b/.test(b))).toBe(true);
        expect(anyBody('.utilities-tool-card__title', (b) => /-webkit-line-clamp\s*:\s*2\b/.test(b))).toBe(true);
    });

    it('distinguishes unavailable cards from cards that are actively starting', () => {
        expect(anyBody('.utilities-tool-card:disabled:not(.is-starting)', (b) => /cursor\s*:\s*not-allowed\b/.test(b))).toBe(true);
        expect(anyBody('.utilities-tool-card.is-starting', (b) => /cursor\s*:\s*wait\b/.test(b))).toBe(true);
    });

    it('disables hover motion under prefers-reduced-motion', () => {
        const blocks = css.match(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{[\s\S]*?\n\}/g) || [];
        const joined = blocks.join('\n');
        expect(joined).toContain('.utilities-tool-card');
        expect(joined).toMatch(/transform\s*:\s*none/);
    });
});

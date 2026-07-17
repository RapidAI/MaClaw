/**
 * Regression guards for dark-mode button/card text contrast.
 *
 * Root cause: <button> does not inherit page color (UA ButtonText is often black),
 * so surface-colored cards need an explicit theme text color on the control itself.
 */
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
// __tests__ → pages → components → src
const frontendSrc = resolve(here, '../../..');

const cssCache = new Map<string, string>();

function readCss(relFromSrc: string): string {
    let css = cssCache.get(relFromSrc);
    if (css === undefined) {
        css = readFileSync(resolve(frontendSrc, relFromSrc), 'utf8');
        cssCache.set(relFromSrc, css);
    }
    return css;
}

/**
 * Collect rule bodies whose selector list includes `selector` as a full selector
 * token (not a substring of a longer class name). Supports multi-selector rules.
 */
function ruleBodies(css: string, selector: string): string[] {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    // Match "...selector..." before `{`, allowing commas / newlines in the selector list.
    const re = new RegExp(
        `(?:^|[,\\s{])${escaped}(?=[\\s,{])[^{]*\\{([^}]*)\\}`,
        'gm',
    );
    const bodies: string[] = [];
    let m: RegExpExecArray | null;
    while ((m = re.exec(css)) !== null) {
        bodies.push(m[1]);
    }
    return bodies;
}

function anyBody(css: string, selector: string, predicate: (body: string) => boolean): boolean {
    const bodies = ruleBodies(css, selector);
    expect(bodies.length, `expected at least one rule for ${selector}`).toBeGreaterThan(0);
    return bodies.some(predicate);
}

function hasThemeTextPrimary(body: string): boolean {
    return /color\s*:\s*var\(\s*--theme-text-primary\b/.test(body);
}

function hasColorInherit(body: string): boolean {
    return /color\s*:\s*inherit\b/.test(body);
}

function hasFontInherit(body: string): boolean {
    return /font\s*:\s*inherit\b/.test(body);
}

function hasColorScheme(body: string, scheme: 'dark' | 'light'): boolean {
    return new RegExp(`color-scheme\\s*:\\s*${scheme}\\b`).test(body);
}

describe('dark-mode button/card text contrast', () => {
    it('utilities tool cards set theme text on the button (not only children)', () => {
        const css = readCss('components/pages/UtilitiesPage.css');
        expect(anyBody(css, '.utilities-tool-card', hasThemeTextPrimary)).toBe(true);
        expect(anyBody(css, '.utilities-tool-card', hasFontInherit)).toBe(true);
        expect(anyBody(css, '.utilities-tool-card__desc', (b) =>
            /color\s*:\s*var\(\s*--theme-text-secondary\b/.test(b),
        )).toBe(true);
        expect(anyBody(css, '.utilities-tool-card__cta', (b) =>
            /color\s*:\s*var\(\s*--theme-primary\b/.test(b),
        )).toBe(true);
    });

    it('utilities watch list items and member chips set theme text on the control', () => {
        const css = readCss('components/pages/UtilitiesPage.css');
        expect(anyBody(css, '.utilities-watch-item', hasThemeTextPrimary)).toBe(true);
        expect(anyBody(css, '.utilities-member', hasThemeTextPrimary)).toBe(true);

        // Hover/active wash should use theme primary (not a hardcoded light-mode blue).
        const hoverBodies = ruleBodies(css, '.utilities-watch-item.is-active')
            .concat(ruleBodies(css, '.utilities-watch-item:hover'));
        const hoverCss = hoverBodies.join('\n');
        expect(hoverCss.length).toBeGreaterThan(0);
        expect(hoverCss).toMatch(/color-mix|var\(\s*--theme-primary\b/);
        expect(hoverCss).not.toMatch(/rgba\(\s*59\s*,\s*130\s*,\s*246/);
    });

    it('workflows tiles set theme text on the button', () => {
        const css = readCss('components/pages/WorkflowsPage.css');
        expect(anyBody(css, '.workflows-page__tile', hasThemeTextPrimary)).toBe(true);
        expect(anyBody(css, '.workflows-page__tile', hasFontInherit)).toBe(true);
    });

    it('shared surface button cards set theme text', () => {
        const css = readCss('App.css');
        for (const sel of [
            '.provider-selector-card',
            '.install-skill-list__item',
            '.knowledge-import-button',
            '.prog-tools__kb-item-title--link',
        ]) {
            expect(anyBody(css, sel, hasThemeTextPrimary), sel).toBe(true);
        }
        // Dual-class sidebar control: inherits from themed parent text color.
        expect(anyBody(css, '.sidebar-system-status__provider--button', hasColorInherit)).toBe(true);
        expect(anyBody(css, '.sidebar-system-status__provider-dropdown-item', hasThemeTextPrimary)).toBe(true);
    });

    it('apps approval/install rows set theme text on interactive rows', () => {
        const css = readCss('components/pages/AppsPage.css');
        expect(anyBody(css, '.apps-datasrv-approval-summary__item', hasThemeTextPrimary)).toBe(true);
        expect(anyBody(css, '.apps-datasrv-approval-summary__row-main', hasThemeTextPrimary)).toBe(true);
        // Background mix must use defined tokens only.
        const rowMain = ruleBodies(css, '.apps-datasrv-approval-summary__row-main').join('\n');
        expect(rowMain).toMatch(/--theme-page-bg\b/);
        expect(rowMain).not.toMatch(/--theme-bg(?!-)/);
        expect(anyBody(css, '.apps-install-preview__row', hasThemeTextPrimary)).toBe(true);
    });

    it('install skill progress uses defined theme tokens', () => {
        const css = readCss('App.css');
        const body = ruleBodies(css, '.install-skill-progress').join('\n');
        expect(body).toMatch(/background\s*:\s*var\(\s*--theme-surface-muted\b/);
        expect(body).toMatch(/color\s*:\s*var\(\s*--theme-text-primary\b/);
        expect(body).not.toMatch(/--theme-bg-secondary\b/);
        expect(body).not.toMatch(/var\(\s*--theme-text\s*[,)]/);
    });

    it('dark theme roots declare color-scheme: dark', () => {
        const css = readCss('App.css');
        for (const sel of [
            '#App[data-ai-theme=\'dark\']',
            '.app-loading-shell[data-ai-theme=\'dark\']',
            '.sidebar[data-ai-theme=\'dark\']',
            '.modal-backdrop[data-ai-theme=\'dark\']',
        ]) {
            expect(anyBody(css, sel, (b) => hasColorScheme(b, 'dark')), sel).toBe(true);
        }
        expect(anyBody(css, ':root', (b) => hasColorScheme(b, 'light'))).toBe(true);
    });
});

import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
const frontendSrc = resolve(here, '../../..');
const css = readFileSync(resolve(frontendSrc, 'components/pages/UtilitiesPage.css'), 'utf8');

function ruleBody(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = new RegExp(`${escaped}\\s*\\{([^}]*)\\}`, 'm').exec(css);
    expect(match, `expected a CSS rule for ${selector}`).toBeTruthy();
    return match?.[1] || '';
}

describe('expert editor dialog layout', () => {
    it('keeps the dialog outer box inside the viewport height budget', () => {
        const overlay = ruleBody('.expert-editor-overlay');
        const dialog = ruleBody('.expert-editor');
        expect(overlay).toMatch(/box-sizing\s*:\s*border-box/);
        expect(overlay).toMatch(/overflow\s*:\s*auto/);
        expect(dialog).toMatch(/max-height\s*:\s*100%/);
        expect(dialog).toMatch(/box-sizing\s*:\s*border-box/);
        expect(dialog).toMatch(/overflow\s*:\s*hidden/);
    });

    it('scrolls only the editor content while header and actions remain available', () => {
        const body = ruleBody('.expert-editor__body');
        expect(body).toMatch(/min-height\s*:\s*0/);
        expect(body).toMatch(/overflow-y\s*:\s*auto/);
        expect(ruleBody('.expert-editor__actions')).toMatch(/flex\s*:\s*0\s+0\s+auto/);
    });

    it('uses a modal stacking level above the app chrome', () => {
        expect(ruleBody('.expert-editor-overlay')).toMatch(/z-index\s*:\s*1500/);
    });
});

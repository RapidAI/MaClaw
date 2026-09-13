/**
 * Guards the programming-tools knowledge action buttons against the 30x30
 * icon-button layout that stacked CJK labels like 清空 vertically.
 */
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
const appCss = readFileSync(resolve(here, '../../../App.css'), 'utf8');

function ruleBodies(css: string, selector: string): string[] {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const re = new RegExp(`(?:^|[,\\s{])${escaped}(?=[\\s,{])[^{]*\\{([^}]*)\\}`, 'gm');
    const bodies: string[] = [];
    let match: RegExpExecArray | null;
    while ((match = re.exec(css)) !== null) {
        bodies.push(match[1]);
    }
    return bodies;
}

describe('coding knowledge action button layout', () => {
    it('keeps knowledge action buttons as a horizontal row', () => {
        const body = ruleBodies(appCss, '.prog-tools__kb-actions').join('\n');
        expect(body).toMatch(/display\s*:\s*flex/);
        expect(body).toMatch(/flex-wrap\s*:\s*wrap/);
        expect(body).not.toMatch(/flex-direction\s*:\s*column/);
    });

    it('keeps knowledge and reset buttons as auto-width nowrap chips', () => {
        for (const selector of ['.prog-tools__kb-btn', '.prog-tools__btn-reset']) {
            const body = ruleBodies(appCss, selector).join('\n');
            expect(body, selector).toMatch(/white-space\s*:\s*nowrap/);
            expect(body, selector).toMatch(/display\s*:\s*inline-flex/);
            expect(body, selector).toMatch(/width\s*:\s*auto/);
            expect(body, selector).not.toMatch(/width\s*:\s*30px/);
            expect(body, selector).not.toMatch(/height\s*:\s*30px/);
        }
    });
});

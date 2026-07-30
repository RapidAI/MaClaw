import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
const frontendSrc = resolve(here, '..');

function readSource(relativePath: string): string {
    return readFileSync(resolve(frontendSrc, relativePath), 'utf8');
}

describe('retained assistant host navigation', () => {
    it('hides the retained assistant whenever a non-AI page is active', () => {
        const app = readSource('App.tsx');
        const css = readSource('App.css');

        expect(app).toContain('className="ai-main-panel-host" hidden={navTab !== \'ai\'}');
        expect(css).toMatch(/\.ai-main-panel-host\[hidden\]\s*\{\s*display:\s*none;\s*\}/s);
    });
});

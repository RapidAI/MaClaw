import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
const frontendSrc = resolve(here, '..');

function readSource(relativePath: string): string {
    return readFileSync(resolve(frontendSrc, relativePath), 'utf8');
}

describe('frameless window shell regression guards', () => {
    it('removes CSS window decoration from opaque Windows/macOS shells', () => {
        const css = readSource('App.css');
        expect(css).toMatch(
            /#App\[data-css-window-corners="false"\]\s*\{[^}]*border-radius:\s*0;[^}]*border:\s*none;[^}]*box-shadow:\s*none;/s,
        );
        expect(css).toMatch(
            /\.app-loading-shell\[data-css-window-corners="false"\]\s*\{[^}]*border-radius:\s*0;[^}]*border:\s*none;/s,
        );
    });

    it('keeps root-level loading and startup-error views inside the WebView client area', () => {
        const css = readSource('App.css');
        const main = readSource('main.tsx');

        expect(css).toMatch(/\.app-loading-shell\s*\{[^}]*height:\s*100%;[^}]*min-height:\s*100%;/s);
        expect(css).toMatch(/\.app-config-loading\s*\{[^}]*min-height:\s*100%;/s);
        expect(main).toContain("minHeight: '100%'");
    });
});

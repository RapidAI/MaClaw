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

    it('keeps the DWM top safety inset when Windows owns rounded corners', () => {
        const css = readSource('App.css');
        const nativeRoundedRule = css.match(/#App\[data-native-rounded="true"\]\s*\{([^}]*)\}/s)?.[1] || '';

        expect(nativeRoundedRule).not.toMatch(/padding-top\s*:/);
    });

    it('keeps Windows 10 content inside the opaque WebView2 edge', () => {
        const css = readSource('App.css');
        const app = readSource('App.tsx');

        expect(css).toMatch(
            /#App\[data-windows-legacy-frameless="true"\]\s*\{[^}]*padding-right:\s*var\(--window-edge-safety-inset\);[^}]*padding-bottom:\s*var\(--window-edge-safety-inset\);[^}]*padding-left:\s*var\(--window-edge-safety-inset\);/s,
        );
        expect(app).toContain('data-windows-legacy-frameless={isLegacyWindowsFrameless ? "true" : undefined}');
        expect(app).toContain('const isLegacyWindowsFrameless = isWindowsHost && nativeRoundedResolved && !nativeRounded;');
        expect(app).toContain('const disableAutoUIScaleTransform = isWindowsHost;');
    });

    it('keeps root-level loading and startup-error views inside the WebView client area', () => {
        const css = readSource('App.css');
        const main = readSource('main.tsx');

        expect(css).toMatch(/\.app-loading-shell\s*\{[^}]*height:\s*100%;[^}]*min-height:\s*100%;/s);
        expect(css).toMatch(/\.app-config-loading\s*\{[^}]*min-height:\s*100%;/s);
        expect(css).toMatch(/\.app-config-loading\s*\{[^}]*padding:\s*calc\(24px \+ var\(--dwm-top-offset, 0px\)\) 24px 24px;/s);
        expect(css).toMatch(
            /\.app-config-loading\[data-windows-legacy-frameless="true"\]\s*\{[^}]*padding-right:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);[^}]*padding-bottom:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);[^}]*padding-left:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);/s,
        );
        expect(main).toContain("minHeight: '100%'");
        expect(readSource('App.tsx')).toContain('data-windows-legacy-frameless={isLegacyWindowsFrameless ? "true" : undefined}');
    });

    it('applies the DWM top-safe inset to the startup shell too', () => {
        const css = readSource('App.css');

        expect(css).toMatch(
            /\.app-loading-shell\s*\{[^}]*padding:\s*calc\(16px \+ var\(--dwm-top-offset, 0px\)\) 20px 16px;/s,
        );
    });

    it('rechecks the native inset after the first compositor frame', () => {
        const app = readSource('App.tsx');

        expect(app).toContain('retryTimer = setTimeout(refreshFramelessTopInset, 240);');
        expect(app).toContain('if (disposed) return;');
    });

    it('keeps the environment-check resize fallback inside a scaled display', () => {
        const app = readSource('App.tsx');

        expect(app).toContain('function getSafeFallbackWindowSize()');
        expect(app).toContain('Math.floor(screenWidth * 0.9)');
        expect(app).toContain('Math.floor(screenHeight * 0.9)');
        expect(app).toContain('ResizeWindow(fallbackSize.width, fallbackSize.height)');
    });
});

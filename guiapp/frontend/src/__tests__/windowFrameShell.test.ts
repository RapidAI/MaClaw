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

        expect(css).toMatch(/:root\s*\{[^}]*--theme-page-bg:\s*#/s);
        expect(css).toMatch(/\.app-loading-shell\s*\{[^}]*position:\s*fixed;[^}]*inset:\s*0;/s);
        expect(css).toMatch(/\.app-loading-shell\s*\{[^}]*min-height:\s*100%;/s);
        expect(css).toMatch(/\.app-config-loading\s*\{[^}]*min-height:\s*100%;/s);
        expect(css).toMatch(/\.app-config-loading\s*\{[^}]*padding:\s*calc\(24px \+ var\(--dwm-top-offset, 0px\)\) 24px 24px;/s);
        expect(css).toMatch(
            /\.app-config-loading\[data-windows-legacy-frameless="true"\]\s*\{[^}]*padding-right:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);[^}]*padding-bottom:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);[^}]*padding-left:\s*calc\(24px \+ var\(--window-edge-safety-inset\)\);/s,
        );
        expect(main).toContain("minHeight: '100%'");
        expect(readSource('App.tsx')).toContain('data-windows-legacy-frameless={isLegacyWindowsFrameless ? "true" : undefined}');
    });

    it('keeps the env-check mascot proportional instead of stretching it', () => {
        const css = readSource('App.css');
        expect(css).toMatch(/\.app-loading-mascot\s*\{[^}]*aspect-ratio:\s*192\s*\/\s*220;/s);
        expect(css).toMatch(/\.app-loading-mascot__mark\s*\{[^}]*object-fit:\s*contain;/s);
    });

    it('fills the environment-check shell instead of floating a small card', () => {
        const css = readSource('App.css');
        expect(css).toMatch(/\.app-loading-card\s*\{[^}]*width:\s*min\(1200px,\s*100%\);[^}]*height:\s*min\(750px,\s*100%\);/s);
        expect(css).toMatch(/\.app-loading-card--quit\s*\{[^}]*z-index:\s*1000;/s);
        expect(css).toMatch(/\.app-loading-runtimes\s*\{[^}]*grid-template-columns:\s*1fr 1fr 1fr;/s);
        expect(css).toMatch(/\.app-loading-runtime__icon\s*\{[^}]*aspect-ratio:\s*1;/s);
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

    it('loads production CSS/JS without blocking the boot splash first paint', () => {
        const vite = readSource('../vite.config.ts');
        const rewrite = readSource('../webview-first-paint.ts');
        expect(vite).toContain('rewriteWebviewFirstPaintHtml');
        expect(rewrite).toContain("crossorigin");
        expect(vite).toContain("order: 'post'");
        expect(vite).toContain('transformIndexHtml');
        expect(vite).toContain('modulePreload: false');
    });

    it('keeps the environment-check shell viewport-fixed so a 0-height root cannot collapse it', () => {
        const css = readSource('App.css');
        expect(css).toMatch(/\.app-loading-shell\s*\{[^}]*position:\s*fixed;/s);
    });

    it('paints a static env-check splash before React so the compact window is never native-black', () => {
        const indexHtml = readFileSync(resolve(frontendSrc, '../index.html'), 'utf8');
        const main = readSource('main.tsx');
        const splash = readSource('components/startup/EnvCheckSplash.tsx');

        expect(indexHtml).toContain('id="maclaw-boot-splash"');
        expect(indexHtml).toContain("document.documentElement.style.backgroundColor = c");
        expect(indexHtml).toContain('<script>');
        expect(indexHtml.indexOf('<script>')).toBeLessThan(indexHtml.indexOf('<body>'));
        expect(indexHtml).toContain('环境准备中');
        expect(indexHtml).toContain('window.go.main = window.go.guiapp');
        expect(indexHtml).toContain('Preparing Environment');
        expect(indexHtml).toContain('boot-runtimes');
        expect(indexHtml).toContain('boot-runtime--node');
        expect(indexHtml).toContain('boot-runtime--git');
        expect(indexHtml).toContain('boot-runtime--python');
        expect(main).toContain('hideBootSplash()');
        expect(main).toContain('scheduleBootSplashHandoff');
        expect(main).toContain('requestAnimationFrame');
        expect(indexHtml).not.toContain("addEventListener('maclaw-env-check-done'");
        expect(splash).not.toContain('hideBootSplash()');
    });

    it('keeps the environment-check resize fallback inside a scaled display', () => {
        const app = readSource('App.tsx');

        expect(app).toContain('function getSafeFallbackWindowSize()');
        expect(app).toContain('Math.floor(screenWidth * 0.9)');
        expect(app).toContain('Math.floor(screenHeight * 0.9)');
        expect(app).toContain('ResizeWindow(fallbackSize.width, fallbackSize.height)');
    });

    it('restores the pre-maximize window size after work-area clamp', () => {
        const app = readSource('App.tsx');

        expect(app).toContain('createWindowMaximizeRestoreSession');
        expect(app).toContain('windowMaximizeRestore.rememberNormal(op)');
        expect(app).toContain('windowMaximizeRestore.restoreNormal()');
        expect(app).toContain('windowMaximizeRestore.shouldSkipClamp()');
        expect(app).toContain('if (!windowMaximizeRestore.isCurrent(op)) return;');
        expect(app).toContain('WindowUnmaximise()');
    });

    it('hands window drag to the execution task header after the MaClaw title bar is hidden', () => {
        const css = readSource('App.css');
        const panel = readSource('components/ai/AIAssistantPanel.tsx');
        const drag = readSource('utils/windowDrag.ts');

        expect(css).toMatch(/\[data-testid='ai-panel-root'\]\[data-ai-view='execution'\] > \.mc-ai-titlebar \{ display: none !important; \}/);
        expect(css).toMatch(/\.mc-task-execution-header\[data-window-drag\]\s*\{[^}]*--wails-draggable:\s*drag;/);
        expect(panel).toContain('data-testid="task-execution-header"');
        expect(panel).toContain('windowDragHandleProps(!!inline');
        expect(panel).toContain('windowNoDragRegionProps()');
        expect(drag).toContain('isWindowDragArmTarget');
        expect(drag).toContain('[data-window-no-drag]');
        expect(readSource('components/ai/assistantTaskExecutionChrome.ts')).toContain('pointerEvents: "none"');
    });
});

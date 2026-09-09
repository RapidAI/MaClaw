import { describe, expect, it } from 'vitest';
import { rewriteWebviewFirstPaintHtml } from '../../webview-first-paint';

const viteProductionHead = `<!DOCTYPE html>
<html lang="en">
<head>
    <title>MaClaw</title>
    <style>#maclaw-boot-splash { background: #f3f5f7; }</style>
  <script type="module" crossorigin src="./assets/index-CI7zT0BZ.js"></script>
  <link rel="modulepreload" crossorigin href="./assets/wails-hPdfwblJ.js">
  <link rel="stylesheet" crossorigin href="./assets/index-BsHTQaMl.css">
</head>
<body>
<div id="maclaw-boot-splash"><h1>正在准备环境</h1></div>
</body>
</html>`;

describe('rewriteWebviewFirstPaintHtml', () => {
    it('strips crossorigin but leaves Vite CSS/JS parser-owned for Wails bindings', () => {
        const out = rewriteWebviewFirstPaintHtml(viteProductionHead);

        expect(out).toContain('id="maclaw-boot-splash"');
        expect(out).toContain('正在准备环境');
        expect(out).toContain('<script type="module" src="./assets/index-CI7zT0BZ.js"></script>');
        expect(out).toContain('<link rel="stylesheet" href="./assets/index-BsHTQaMl.css">');
        expect(out).not.toMatch(/\scrossorigin/);
        expect(out).not.toContain('media="not all"');
        expect(out).not.toContain('maclaw-enable-css');
    });

    it('is idempotent so closeBundle can re-run after transformIndexHtml', () => {
        const once = rewriteWebviewFirstPaintHtml(viteProductionHead);
        expect(rewriteWebviewFirstPaintHtml(once)).toBe(once);
    });
});

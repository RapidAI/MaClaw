/**
 * The Utilities page stays mounted after the first visit and uses the hidden
 * attribute. A Skills layout rule that sets display on every .app-main-content
 * overrides that attribute, so AI experts paint under Skills settings.
 */
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const src = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const partial = readFileSync(resolve(src, 'styles/partials/120-app-responsive.css'), 'utf8');
const appCss = readFileSync(resolve(src, 'App.css'), 'utf8');

const skillsLayout = "#App[data-nav-tab='skills'] .app-main-content:not([hidden])";
const keptHidden = "#App[data-nav-tab='skills'] .app-main-content[hidden]";

function hidesKeptHost(css: string) {
    const source = css.replace(/\r\n/g, '\n');
    const layoutAt = source.indexOf(`${skillsLayout} {`);
    const hiddenAt = source.indexOf(`${keptHidden} {\n  display: none;\n}`);
    expect(layoutAt, 'skills layout must not target the hidden kept host').toBeGreaterThan(-1);
    expect(source.includes("#App[data-nav-tab='skills'] .app-main-content {")).toBe(false);
    expect(hiddenAt, 'hidden host must force display:none after the layout rule').toBeGreaterThan(layoutAt);
}

describe('kept utilities host under Skills', () => {
    it('keeps the partial and assembled stylesheet in agreement', () => {
        hidesKeptHost(partial);
        hidesKeptHost(appCss);
    });
});

import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const srcRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const providerCss = readFileSync(resolve(srcRoot, 'styles/partials/50-pet-provider.css'), 'utf8');
const settingsCss = readFileSync(resolve(srcRoot, 'styles/partials/30-settings-knowledge.css'), 'utf8');
const shellCss = readFileSync(resolve(srcRoot, 'styles/partials/20-app-shell.css'), 'utf8');
const hubCss = readFileSync(resolve(srcRoot, 'styles/partials/60-hub-updates-models.css'), 'utf8');
const progressCss = readFileSync(resolve(srcRoot, 'styles/partials/70-progress-web-im.css'), 'utf8');
const imCss = readFileSync(resolve(srcRoot, 'styles/partials/90-im-channels-markdown.css'), 'utf8');
const petStoreCss = readFileSync(resolve(srcRoot, 'styles/partials/100-pet-store-mc.css'), 'utf8');
const titleBar = readFileSync(resolve(srcRoot, 'components/ai/AssistantTitleBar.tsx'), 'utf8');

function ruleBody(source: string, selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = source.match(new RegExp(escaped + '\\s*\\{([^}]*)\\}'));
    expect(match, selector).toBeTruthy();
    return match![1];
}

function zIndexOf(source: string, selector: string): number {
    const value = Number(ruleBody(source, selector).match(/z-index:\s*(\d+)/)?.[1]);
    expect(Number.isFinite(value), selector).toBe(true);
    return value;
}

function titleBarZIndex(): number {
    const rootStyle = titleBar.slice(titleBar.indexOf('const rootStyle'), titleBar.indexOf('return ('));
    return Number(rootStyle.match(/zIndex:\s*(\d+)/)?.[1]);
}

describe('modal overlay layering', () => {
    it('keeps the dialog scrim above the inline assistant title bar', () => {
        const titleBarZ = titleBarZIndex();
        expect(Number.isFinite(titleBarZ)).toBe(true);
        expect(zIndexOf(providerCss, '.modal-overlay')).toBeGreaterThan(titleBarZ);
    });

    it('keeps the other in-shell dialog backdrops above the title bar', () => {
        const titleBarZ = titleBarZIndex();
        expect(zIndexOf(shellCss, '.feb-overlay')).toBeGreaterThan(titleBarZ);
        expect(zIndexOf(hubCss, '.confirm-dialog-overlay')).toBeGreaterThan(titleBarZ);
        expect(zIndexOf(imCss, '.vead-overlay')).toBeGreaterThan(titleBarZ);
        expect(zIndexOf(petStoreCss, '.vet-dialog-overlay')).toBeGreaterThan(titleBarZ);
    });

    it('keeps every full-screen dialog scrim on the same layer', () => {
        const scrims: Array<[string, string]> = [
            [providerCss, '.modal-overlay'],
            [settingsCss, '.knowledge-import-overlay'],
            [settingsCss, '.knowledge-share-modal-overlay'],
            [shellCss, '.feb-overlay'],
            [hubCss, '.confirm-dialog-overlay'],
            [hubCss, '.llm-config-dialog-overlay'],
            [progressCss, '.prog-tools__kb-editor-overlay'],
            [imCss, '.modal-backdrop'],
            [imCss, '.vead-overlay'],
            [petStoreCss, '.vet-dialog-overlay'],
        ];
        for (const [source, selector] of scrims) {
            expect(zIndexOf(source, selector), selector).toBeGreaterThanOrEqual(40000);
        }
    });

    it('hides the body-portalled update card while any full-screen dialog is open', () => {
        const start = settingsCss.indexOf('body:has(');
        expect(start).toBeGreaterThan(-1);
        const end = settingsCss.indexOf('}', start);
        expect(end).toBeGreaterThan(start);
        const rule = settingsCss.slice(start, end + 1);

        expect(rule).toMatch(/display:\s*none/);
        const scrimClasses = [
            '.modal-overlay',
            '.modal-backdrop',
            '.knowledge-import-overlay',
            '.knowledge-share-modal-overlay',
            '.llm-config-dialog-overlay',
            '.prog-tools__kb-editor-overlay',
            '.confirm-dialog-overlay',
            '.feb-overlay',
            '.vead-overlay',
            '.vet-dialog-overlay',
            '.im-audit-overlay',
            '.mailbox-manage-overlay',
            '.im-groups-modal-overlay',
            '.cwshare-backdrop',
        ];
        for (const selector of scrimClasses) {
            expect(rule).toContain(selector);
        }
    });
});

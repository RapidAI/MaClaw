import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const css = readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), '../styles/partials/90-im-channels-markdown.css'),
    'utf8',
);

function dialogChrome(): string {
    const start = css.indexOf('.custom-dialog-backdrop');
    const end = css.indexOf('/* Terminal-style output panel */');
    expect(start).toBeGreaterThan(-1);
    expect(end).toBeGreaterThan(start);
    return css.slice(start, end);
}

function ruleBody(source: string, selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = source.match(new RegExp(escaped + '\\s*\\{([^}]+)\\}'));
    expect(match, selector).toBeTruthy();
    return match![1];
}

describe('custom dialog danger chrome', () => {
    it('follows the active scheme instead of a fixed red or light-theme blue', () => {
        const dialog = dialogChrome();
        expect(dialog).not.toMatch(/#c43d34|#a8332b/);
        expect(dialog).not.toMatch(/custom-dialog--danger \.custom-dialog__subject/);

        const mark = ruleBody(dialog, '.custom-dialog__mark--danger');
        expect(mark).toMatch(/background:\s*var\(--theme-primary-soft\)/);
        expect(mark).toMatch(/color:\s*var\(--theme-primary-strong\)/);

        const notice = ruleBody(dialog, '.custom-dialog__notice');
        expect(notice).toMatch(/color:\s*var\(--theme-text-secondary\)/);
        expect(notice).not.toMatch(/theme-danger/);

        const subject = ruleBody(dialog, '.custom-dialog__subject-name');
        expect(subject).toMatch(/font-weight:\s*400/);
        expect(subject).toMatch(/text-align:\s*justify/);
        expect(subject).toMatch(/text-align-last:\s*left/);
        expect(subject).toMatch(/text-justify:\s*auto/);
        expect(subject).not.toMatch(/text-justify:\s*inter-character/);

        const resetAt = dialog.indexOf(".modal-backdrop[data-ai-theme='dark'] .custom-dialog__footer .btn-secondary {");
        const dangerAt = dialog.indexOf('.modal-backdrop.custom-dialog-backdrop .custom-dialog__footer .btn-secondary.btn-danger');
        expect(resetAt).toBeGreaterThan(-1);
        expect(dangerAt).toBeGreaterThan(resetAt);

        const action = dialog.match(
            /\.modal-backdrop\.custom-dialog-backdrop \.custom-dialog__footer \.btn-secondary\.btn-danger,\s*\.modal-backdrop\.custom-dialog-backdrop \.custom-dialog__footer \.btn-secondary\.btn-danger:hover\s*\{([^}]+)\}/,
        )?.[1];
        expect(action, 'grouped danger button rule').toBeTruthy();
        expect(action).toMatch(/background:\s*var\(--theme-primary\)/);
        expect(action).toMatch(/color:\s*var\(--theme-on-primary\)/);
        expect(action).not.toMatch(/#2f6fbc|#fff/);

        const hover = dialog.match(
            /\}\s*\.modal-backdrop\.custom-dialog-backdrop \.custom-dialog__footer \.btn-secondary\.btn-danger:hover\s*\{([^}]+)\}/,
        )?.[1];
        expect(hover, 'danger hover rule').toBeTruthy();
        expect(hover).toMatch(/background:\s*var\(--theme-primary-strong\)/);
    });
});

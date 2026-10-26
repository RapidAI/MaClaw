import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const srcRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const notificationCss = readFileSync(resolve(srcRoot, 'styles/partials/90-im-channels-markdown.css'), 'utf8');

function ruleBody(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = notificationCss.match(new RegExp(escaped + '\\s*\\{([^}]*)\\}'));
    expect(match, selector).toBeTruthy();
    return match![1];
}

describe('notification category colors', () => {
    it('leaves category ink to the modifier classes', () => {
        // The badge and detail pill are both assembled before the category
        // modifiers. A base-class default would tie on specificity and win by
        // order once assembled, repainting every category the same color.
        expect(ruleBody('.notification-cat-badge')).not.toMatch(/--notif-cat\s*:/);
        expect(ruleBody('.ndet-cat')).not.toMatch(/--notif-cat\s*:/);
        expect(notificationCss).toContain(
            ':where(.notification-cat-badge, .ndet-cat) { --notif-cat: var(--theme-primary); }',
        );
    });

    it.each([
        ['system_announcement', 'var(--theme-primary)'],
        ['feature_update', 'var(--theme-success)'],
        ['security_alert', 'var(--theme-danger)'],
        ['maintenance', 'var(--theme-warning)'],
        ['custom', 'var(--theme-text-secondary)'],
    ])('maps %s to %s', (category, token) => {
        expect(ruleBody(`.notification-cat--${category}`)).toContain(`--notif-cat: ${token}`);
    });
});

describe('notification panel shell', () => {
    it('does not rely on the retired legacy !important patch', () => {
        const shellCss = readFileSync(resolve(srcRoot, 'styles/partials/110-mc-app-shell.css'), 'utf8');
        expect(shellCss).not.toContain("[data-testid='notification-panel'] {");
        expect(ruleBody('.notification-panel')).toContain('background: var(--theme-surface)');
        expect(ruleBody('.notification-panel')).toContain('box-shadow: var(--shadow-lg)');
    });
});

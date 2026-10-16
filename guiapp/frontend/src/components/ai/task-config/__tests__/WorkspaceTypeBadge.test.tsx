import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { WorkspaceTypeBadge } from '../WorkspaceTypeBadge';

const css = readFileSync(join(process.cwd(), 'src/components/ai/task-config/WorkspaceTypeBadge.css'), 'utf8');

function ruleBody(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
    return match?.[1] ?? '';
}

afterEach(() => {
    cleanup();
});

describe('WorkspaceTypeBadge', () => {
    it('keeps the light accent fills and does not paint them inline', () => {
        const { getByTestId } = render(<WorkspaceTypeBadge kind="local" label="本地" />);
        const el = getByTestId('workspace-badge-local');
        expect(el.className).toContain('mc-workspace-kind--local');
        expect(el.getAttribute('style') || '').not.toMatch(/background|color/i);
        expect(ruleBody('.mc-workspace-kind--local')).toMatch(/var\(\s*--theme-primary-soft\)/);
        expect(ruleBody('.mc-workspace-kind--local')).toMatch(/var\(\s*--theme-primary\)/);
        expect(ruleBody('.mc-workspace-kind--cloud')).toMatch(/var\(\s*--theme-success-bg\)/);
        expect(ruleBody('.mc-workspace-kind--remote')).toMatch(/#f3eefe/);
    });

    it('uses a translucent wash in dark mode instead of the near-white fills', () => {
        const local = ruleBody("[data-ai-theme='dark'] .mc-workspace-kind--local");
        const cloud = ruleBody("[data-ai-theme='dark'] .mc-workspace-kind--cloud");
        const remote = ruleBody("[data-ai-theme='dark'] .mc-workspace-kind--remote");
        expect(local.length).toBeGreaterThan(0);
        expect(cloud.length).toBeGreaterThan(0);
        expect(remote.length).toBeGreaterThan(0);
        const dark = `${local}\n${cloud}\n${remote}`;
        expect(dark).not.toMatch(/#eef4fd|#ecfdf3|#f3eefe|#fff\b|#ffffff/i);
        expect(local).toMatch(/var\(\s*--theme-primary\b/);
        expect(local).toMatch(/color-mix/);
        expect(cloud).toMatch(/var\(\s*--theme-success\b/);
        expect(remote).toMatch(/rgba\(\s*124\s*,\s*58\s*,\s*237\s*,\s*0?\.18\s*\)/);
    });
});

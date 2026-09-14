/**
 * Document minimap (thumbnail + page locator) for CodePreviewPanel.
 */
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { CodePreviewPanel, lightCodePreviewTheme, CODE_PREVIEW_VIEW_PREFS_KEY, loadCodePreviewViewPrefs, saveCodePreviewViewPrefs } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

function makeFiles(overrides?: Partial<CodeFile>): Map<string, CodeFile> {
    const lines = Array.from({ length: 80 }, (_, i) => `const n${i} = ${i};`);
    const file: CodeFile = {
        filePath: '/src/main.ts',
        fileName: 'main.ts',
        content: lines.join('\n'),
        opType: 'read',
        language: 'typescript',
        updatedAt: 1,
        ...overrides,
    };
    return new Map([[file.filePath, file]]);
}

function stubScrollMetrics(scroll: HTMLElement, opts: { clientHeight: number; scrollHeight: number; scrollTop?: number }) {
    let scrollTop = opts.scrollTop ?? 0;
    Object.defineProperty(scroll, 'clientHeight', { configurable: true, get: () => opts.clientHeight });
    Object.defineProperty(scroll, 'scrollHeight', { configurable: true, get: () => opts.scrollHeight });
    Object.defineProperty(scroll, 'scrollTop', {
        configurable: true,
        get: () => scrollTop,
        set: (v: number) => { scrollTop = v; },
    });
    return () => scrollTop;
}

describe('CodePreviewPanel minimap', () => {
    it('renders the minimap by default on a source file', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        const minimap = screen.getByTestId('code-preview-minimap');
        expect(minimap).toBeTruthy();
        expect(screen.getByTestId('code-preview-minimap-toggle').getAttribute('data-active')).toBe('true');
        expect(minimap.getAttribute('aria-label')).toMatch(/minimap/i);
        const controls = minimap.getAttribute('aria-controls');
        expect(controls).toBeTruthy();
        expect(document.getElementById(controls!)).toBeTruthy();
    });

    it('uses a Chinese locator label', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.getByTestId('code-preview-minimap').getAttribute('aria-label')).toMatch(/缩略图/);
        expect(screen.getByTestId('code-preview-minimap-toggle').textContent).toBe('缩略');
    });

    it('toggles the minimap from the toolbar and persists the pref', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        fireEvent.click(screen.getByTestId('code-preview-minimap-toggle'));
        expect(screen.queryByTestId('code-preview-minimap')).toBeNull();
        expect(screen.getByTestId('code-preview-minimap-toggle').getAttribute('data-active')).toBe('false');
        expect(loadCodePreviewViewPrefs().minimap).toBe(false);
    });

    it('restores a turned-off minimap from localStorage', () => {
        saveCodePreviewViewPrefs({ wordWrap: false, fontSize: 13, minimap: false });
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        expect(screen.queryByTestId('code-preview-minimap')).toBeNull();
        expect(screen.getByTestId('code-preview-minimap-toggle').getAttribute('data-active')).toBe('false');
    });

    it('clicking the minimap jumps the preview to that page', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        const minimap = screen.getByTestId('code-preview-minimap');
        const scroll = document.getElementById(minimap.getAttribute('aria-controls')!) as HTMLElement;
        const readTop = stubScrollMetrics(scroll, { clientHeight: 200, scrollHeight: 1000, scrollTop: 0 });
        vi.spyOn(minimap, 'getBoundingClientRect').mockReturnValue({
            x: 0, y: 0, top: 0, left: 0, bottom: 200, right: 64, width: 64, height: 200, toJSON: () => ({}),
        } as DOMRect);

        fireEvent.pointerDown(minimap, { clientY: 150, button: 0, buttons: 1, pointerId: 1 });
        expect(readTop()).toBeGreaterThan(300);
    });

    it('dragging the minimap follows the pointer and stops when buttons are released', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        const minimap = screen.getByTestId('code-preview-minimap');
        const scroll = document.getElementById(minimap.getAttribute('aria-controls')!) as HTMLElement;
        const readTop = stubScrollMetrics(scroll, { clientHeight: 200, scrollHeight: 1000, scrollTop: 0 });
        vi.spyOn(minimap, 'getBoundingClientRect').mockReturnValue({
            x: 0, y: 0, top: 0, left: 0, bottom: 200, right: 64, width: 64, height: 200, toJSON: () => ({}),
        } as DOMRect);

        fireEvent.pointerDown(minimap, { clientY: 40, button: 0, buttons: 1, pointerId: 1 });
        const afterDown = readTop();
        fireEvent.pointerMove(minimap, { clientY: 140, pointerId: 1, buttons: 1 });
        const afterDrag = readTop();
        expect(afterDrag).toBeGreaterThan(afterDown);
        fireEvent.pointerMove(minimap, { clientY: 140, pointerId: 1, buttons: 0 });
        const afterRelease = readTop();
        fireEvent.pointerMove(minimap, { clientY: 190, pointerId: 1, buttons: 0 });
        expect(readTop()).toBe(afterRelease);
    });

    it('PageDown / PageUp on the minimap move the preview by a page', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        const minimap = screen.getByTestId('code-preview-minimap');
        const scroll = document.getElementById(minimap.getAttribute('aria-controls')!) as HTMLElement;
        const readTop = stubScrollMetrics(scroll, { clientHeight: 180, scrollHeight: 900, scrollTop: 0 });

        fireEvent.keyDown(minimap, { key: 'PageDown' });
        expect(readTop()).toBe(180);
        fireEvent.keyDown(minimap, { key: 'PageUp' });
        expect(readTop()).toBe(0);
        fireEvent.keyDown(minimap, { key: 'End' });
        expect(readTop()).toBe(720);
        fireEvent.keyDown(minimap, { key: 'Home' });
        expect(readTop()).toBe(0);
        fireEvent.keyDown(minimap, { key: ' ' });
        expect(readTop()).toBe(180);
        fireEvent.keyDown(minimap, { key: ' ', shiftKey: true });
        expect(readTop()).toBe(0);
    });
});

import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CODE_PREVIEW_VIEW_PREFS_KEY, CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

// 9 lines with the edit in the middle, so hunk folding leaves a collapsed gap.
const LINES = [
    'const a = 1;',
    'const b = 2;',
    'const c = 3;',
    'const d = 4;',
    'const e = 5;',
    'const f = 6;',
    'const g = 7;',
    'const h = 8;',
    'const i = 9;',
];

function renderPanel(file: CodeFile) {
    return render(
        <CodePreviewPanel
            files={new Map([[file.filePath, file]])}
            activeFilePath={file.filePath}
            onSelectFile={vi.fn()}
            onClose={vi.fn()}
            theme={lightCodePreviewTheme}
            lang="zh-Hans"
        />,
    );
}

function rowsWithKind(kind: string): number {
    return document.querySelectorAll(`[data-diff-kind="${kind}"]`).length;
}

/** Pretend the preview body is `width` px wide (jsdom has no layout engine). */
function mockPaneWidth(width: number): () => void {
    const g = globalThis as Record<string, unknown>;
    const original = g.ResizeObserver;
    class FakeResizeObserver {
        constructor(private cb: (entries: unknown[]) => void) {}
        observe() {
            this.cb([{ contentRect: { width } }]);
        }
        disconnect() {}
        unobserve() {}
    }
    g.ResizeObserver = FakeResizeObserver;
    return () => {
        g.ResizeObserver = original;
    };
}

function changedFile(): CodeFile {
    const content = [...LINES];
    content[4] = 'const e = 55;';
    return {
        sessionID: 's1',
        filePath: 'src/app.ts',
        fileName: 'app.ts',
        content: content.join('\n'),
        original: LINES.join('\n'),
        opType: 'modify',
        language: 'typescript',
        updatedAt: 1,
    };
}

function unchangedFile(): CodeFile {
    return {
        sessionID: 's1',
        filePath: 'src/same.ts',
        fileName: 'same.ts',
        content: LINES.join('\n'),
        original: LINES.join('\n'),
        opType: 'modify',
        language: 'typescript',
        updatedAt: 1,
    };
}

describe('CodePreviewPanel — change-focused diff', () => {
    beforeEach(() => {
        localStorage.clear();
        Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
            configurable: true,
            value: vi.fn(),
        });
    });

    it('summarizes the change and pairs the rewrite into one modify row', () => {
        renderPanel(changedFile());
        expect(screen.getByTestId('code-preview-diff-summary').textContent).toContain('+1');
        expect(rowsWithKind('modify')).toBe(1);
        const marks = Array.from(document.querySelectorAll('.cp-diff-mark-add')).map((el) => el.textContent);
        expect(marks.join('')).toBe('55');
    });

    it('defaults to the side-by-side layout and switches to inline', () => {
        renderPanel(changedFile());
        expect(screen.getByTestId('code-preview-diff-view').getAttribute('data-diff-mode')).toBe('split');
        fireEvent.click(screen.getByTestId('code-preview-diff-mode-inline'));
        expect(screen.getByTestId('code-preview-diff-view').getAttribute('data-diff-mode')).toBe('inline');
        // Inline renders the rewrite as a '-' row followed by a '+' row.
        expect(rowsWithKind('modify')).toBe(2);
    });

    it('falls back to inline in a narrow pane until the user picks a layout', () => {
        const restore = mockPaneWidth(420);
        try {
            renderPanel(changedFile());
            expect(screen.getByTestId('code-preview-diff-view').getAttribute('data-diff-mode')).toBe('inline');
            // An explicit choice wins over the automatic fallback.
            fireEvent.click(screen.getByTestId('code-preview-diff-mode-split'));
            expect(screen.getByTestId('code-preview-diff-view').getAttribute('data-diff-mode')).toBe('split');
        } finally {
            restore();
        }
    });

    it('drops unchanged context when 只看变更 is on', () => {
        renderPanel(changedFile());
        expect(rowsWithKind('unchanged')).toBeGreaterThan(0);
        fireEvent.click(screen.getByTestId('code-preview-diff-only-changes'));
        expect(rowsWithKind('unchanged')).toBe(0);
        expect(rowsWithKind('modify')).toBe(1);
    });

    it('falls back to the full file when 只看变更 would leave nothing', () => {
        localStorage.setItem(CODE_PREVIEW_VIEW_PREFS_KEY, JSON.stringify({ diffOnlyChanges: true }));
        renderPanel(unchangedFile());
        const view = screen.getByTestId('code-preview-diff-view');
        expect(view.getAttribute('data-only-changes')).toBe('true');
        expect(rowsWithKind('unchanged')).toBe(LINES.length);
        expect(screen.queryByTestId('code-preview-diff-summary')).toBeNull();
    });

    it('walks to the next change and reports the position', () => {
        renderPanel(changedFile());
        expect(screen.getByTestId('code-preview-diff-cursor').textContent).toContain('1 / 1');
        fireEvent.click(screen.getByTestId('code-preview-diff-next'));
        expect(screen.getByTitle(/下一处变更/)).toBeTruthy();
        expect(screen.getByTestId('code-preview-diff-cursor').textContent).toContain('1 / 1');
    });

    it('steps through changes with Alt+ArrowDown / Alt+ArrowUp', () => {
        renderPanel(changedFile());
        const panel = screen.getByTestId('code-preview-panel');
        fireEvent.keyDown(panel, { key: 'ArrowDown', altKey: true });
        expect(screen.getByTestId('code-preview-diff-cursor').textContent).toContain('1 / 1');
        fireEvent.keyDown(panel, { key: 'ArrowUp', altKey: true });
        expect(screen.getByTestId('code-preview-diff-cursor').textContent).toContain('1 / 1');
        expect(screen.getByTestId('code-preview-wrap-toggle').getAttribute('data-active')).toBe('false');
        fireEvent.keyDown(panel, { key: 'z', altKey: true });
        expect(screen.getByTestId('code-preview-wrap-toggle').getAttribute('data-active')).toBe('true');
    });

    it('expands a collapsed context gap between hunks', () => {
        renderPanel(changedFile());
        const gaps = screen.getAllByTestId('code-preview-diff-gap');
        expect(gaps.length).toBe(2);
        expect(gaps[0].textContent).toContain('未变更');
        fireEvent.click(gaps[0]);
        expect(screen.getAllByTestId('code-preview-diff-gap').length).toBe(1);
    });
});
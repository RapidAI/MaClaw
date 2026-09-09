/**
 * In-file find (Ctrl+F) for CodePreviewPanel.
 */
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import {
    CodePreviewPanel,
    lightCodePreviewTheme,
    findMatchLineIndexes,
    compileFindMatcher,
    cycleMatchIndex,
    parseGoToLineInput,
    clampCodePreviewFontSize,
    CODE_PREVIEW_FONT_DEFAULT,
    CODE_PREVIEW_FONT_MAX,
    CODE_PREVIEW_FONT_MIN,
    CODE_PREVIEW_VIEW_PREFS_KEY,
    loadCodePreviewViewPrefs,
    saveCodePreviewViewPrefs,
    formatCodeLanguageLabel,
} from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

function makeFiles(overrides?: Partial<CodeFile>): Map<string, CodeFile> {
    const file: CodeFile = {
        filePath: '/src/main.ts',
        fileName: 'main.ts',
        content: 'const alpha = 1;\nconst beta = 2;\nconst alphaAgain = 3;\n',
        opType: 'read',
        language: 'typescript',
        updatedAt: 1,
        ...overrides,
    };
    return new Map([[file.filePath, file]]);
}

describe('findMatchLineIndexes / cycleMatchIndex / parseGoToLineInput', () => {
    it('finds matching lines case-insensitively', () => {
        const content = 'Hello\nworld\nHELLO again';
        expect(findMatchLineIndexes(content, 'hello')).toEqual([0, 2]);
        expect(findMatchLineIndexes(content, 'WORLD')).toEqual([1]);
        expect(findMatchLineIndexes(content, '')).toEqual([]);
        expect(findMatchLineIndexes(content, '  ')).toEqual([]);
    });

    it('supports match case, whole word, and regex find options', () => {
        const content = 'cat\ncategory\nCat\nfoo_cat_bar\nca.';
        expect(findMatchLineIndexes(content, 'cat', { caseSensitive: true })).toEqual([0, 1, 3]);
        expect(findMatchLineIndexes(content, 'Cat', { caseSensitive: true })).toEqual([2]);
        expect(findMatchLineIndexes(content, 'cat', { wholeWord: true })).toEqual([0, 2]);
        expect(findMatchLineIndexes(content, 'ca.', { useRegex: true })).toEqual([0, 1, 2, 3, 4]);
        expect(findMatchLineIndexes(content, 'ca\\.', { useRegex: true })).toEqual([4]);

        const bad = compileFindMatcher('(', { useRegex: true });
        expect(bad.ok).toBe(false);
        if (!bad.ok) expect(bad.error.length).toBeGreaterThan(0);
        expect(findMatchLineIndexes(content, '(', { useRegex: true })).toEqual([]);
    });

    it('cycles match indexes with wrap-around', () => {
        expect(cycleMatchIndex(0, 0, 1)).toBe(-1);
        expect(cycleMatchIndex(3, 0, 1)).toBe(1);
        expect(cycleMatchIndex(3, 2, 1)).toBe(0);
        expect(cycleMatchIndex(3, 0, -1)).toBe(2);
        expect(cycleMatchIndex(3, -1, 1)).toBe(0);
    });

    it('parses go-to-line input with clamping', () => {
        expect(parseGoToLineInput('12', 100)).toBe(12);
        expect(parseGoToLineInput('12:5', 100)).toBe(12);
        expect(parseGoToLineInput('999', 10)).toBe(10);
        expect(parseGoToLineInput('0', 10)).toBeNull();
        expect(parseGoToLineInput('abc', 10)).toBeNull();
        expect(parseGoToLineInput('', 10)).toBeNull();
    });

    it('clamps font size to supported range', () => {
        expect(clampCodePreviewFontSize(CODE_PREVIEW_FONT_DEFAULT)).toBe(CODE_PREVIEW_FONT_DEFAULT);
        expect(clampCodePreviewFontSize(1)).toBe(CODE_PREVIEW_FONT_MIN);
        expect(clampCodePreviewFontSize(99)).toBe(CODE_PREVIEW_FONT_MAX);
        expect(clampCodePreviewFontSize(Number.NaN)).toBe(CODE_PREVIEW_FONT_DEFAULT);
    });

    it('loads and saves view prefs in localStorage', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        expect(loadCodePreviewViewPrefs()).toEqual({
            wordWrap: false,
            fontSize: CODE_PREVIEW_FONT_DEFAULT,
        });

        saveCodePreviewViewPrefs({ wordWrap: true, fontSize: 18 });
        expect(loadCodePreviewViewPrefs()).toEqual({ wordWrap: true, fontSize: 18 });

        // Invalid / out-of-range font is clamped.
        localStorage.setItem(CODE_PREVIEW_VIEW_PREFS_KEY, JSON.stringify({ wordWrap: 1, fontSize: 99 }));
        expect(loadCodePreviewViewPrefs()).toEqual({ wordWrap: false, fontSize: CODE_PREVIEW_FONT_MAX });
    });

    it('formats language labels for the path bar', () => {
        expect(formatCodeLanguageLabel('typescript')).toBe('TypeScript');
        expect(formatCodeLanguageLabel('plaintext')).toBe('Plain Text');
        expect(formatCodeLanguageLabel('go')).toBe('Go');
        expect(formatCodeLanguageLabel('')).toBe('Plain Text');
        expect(formatCodeLanguageLabel('objective-c')).toBe('Objective-C');
    });

    it('shows one line for empty file content (go-to-line bounds)', () => {
        render(
            <CodePreviewPanel
                files={makeFiles({ content: '' })}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        expect(screen.getByTestId('code-preview-line-count').textContent).toMatch(/1/);
    });

    it('handles Ctrl+Tab / Ctrl+W from panel focus (not only tab bar)', () => {
        const files = new Map<string, CodeFile>([
            ['/src/a.ts', {
                filePath: '/src/a.ts',
                fileName: 'a.ts',
                content: 'a',
                opType: 'read',
                language: 'typescript',
                updatedAt: 1,
            }],
            ['/src/b.ts', {
                filePath: '/src/b.ts',
                fileName: 'b.ts',
                content: 'b',
                opType: 'read',
                language: 'typescript',
                updatedAt: 2,
            }],
        ]);
        const onSelect = vi.fn();
        const onCloseFile = vi.fn();
        render(
            <CodePreviewPanel
                files={files}
                activeFilePath="/src/a.ts"
                mruOrder={['/src/a.ts', '/src/b.ts']}
                onSelectFile={onSelect}
                onCloseFile={onCloseFile}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        const panel = screen.getByTestId('code-preview-panel');
        fireEvent.keyDown(panel, { key: 'Tab', ctrlKey: true });
        expect(onSelect).toHaveBeenCalledWith('/src/b.ts');

        fireEvent.keyDown(panel, { key: 'w', ctrlKey: true });
        expect(onCloseFile).toHaveBeenCalledWith('/src/a.ts');
    });

    it('leaves the working directory when an existing file tab is selected', () => {
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="D:/projects/demo"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
        fireEvent.click(screen.getByTestId('file-tab'));
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
        expect(screen.getByTestId('code-preview-plain-view')).toBeTruthy();
    });

    it('resets find match index when switching files (no stale active match)', () => {
        const files = new Map<string, CodeFile>([
            ['/src/a.ts', {
                filePath: '/src/a.ts',
                fileName: 'a.ts',
                content: 'alpha one\nalpha two\nalpha three\n',
                opType: 'read',
                language: 'typescript',
                updatedAt: 1,
            }],
            ['/src/b.ts', {
                filePath: '/src/b.ts',
                fileName: 'b.ts',
                content: 'alpha only\n',
                opType: 'read',
                language: 'typescript',
                updatedAt: 2,
            }],
        ]);

        const { rerender } = render(
            <CodePreviewPanel
                files={files}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'f', ctrlKey: true });
        fireEvent.change(screen.getByTestId('code-preview-find-input'), { target: { value: 'alpha' } });
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*3/);
        fireEvent.click(screen.getByTestId('code-preview-find-next'));
        fireEvent.click(screen.getByTestId('code-preview-find-next'));
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/3\s*\/\s*3/);

        rerender(
            <CodePreviewPanel
                files={files}
                activeFilePath="/src/b.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        // Find stays open with same query; index resets to first match of the new file.
        expect(screen.getByTestId('code-preview-find-bar')).toBeTruthy();
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*1/);
        expect(document.querySelector('[data-find-active="true"]')?.getAttribute('data-line')).toBe('1');
    });
});

describe('CodePreviewPanel find bar', () => {
    it('opens with Ctrl+F, filters lines, and navigates matches', () => {
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

        const panel = screen.getByTestId('code-preview-panel');
        fireEvent.keyDown(panel, { key: 'f', ctrlKey: true });

        expect(screen.getByTestId('code-preview-find-bar')).toBeTruthy();
        const input = screen.getByTestId('code-preview-find-input');
        fireEvent.change(input, { target: { value: 'alpha' } });

        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*2/);
        expect(document.querySelectorAll('[data-find-match="true"]').length).toBe(2);
        expect(document.querySelector('[data-find-active="true"]')?.getAttribute('data-line')).toBe('1');

        fireEvent.click(screen.getByTestId('code-preview-find-next'));
        expect(document.querySelector('[data-find-active="true"]')?.getAttribute('data-line')).toBe('3');

        fireEvent.click(screen.getByTestId('code-preview-find-close'));
        expect(screen.queryByTestId('code-preview-find-bar')).toBeNull();
    });

    it('highlights find matches in diff view on new-file lines', () => {
        render(
            <CodePreviewPanel
                files={makeFiles({
                    opType: 'modify',
                    original: 'const alpha = 0;\nconst beta = 2;\n',
                    content: 'const alpha = 1;\nconst beta = 2;\nconst alphaAgain = 3;\n',
                })}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'f', ctrlKey: true });
        fireEvent.change(screen.getByTestId('code-preview-find-input'), { target: { value: 'alpha' } });

        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*2/);
        // Diff rows that carry new-line numbers should be marked.
        expect(document.querySelectorAll('[data-find-match="true"]').length).toBeGreaterThanOrEqual(1);
        expect(document.querySelector('[data-find-active="true"]')).toBeTruthy();
    });

    it('opens go-to-line with Ctrl+G and scrolls to the target line', async () => {
        const scrollIntoView = vi.fn();
        Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
            configurable: true,
            value: scrollIntoView,
        });

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

        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'g', ctrlKey: true });
        expect(screen.getByTestId('code-preview-goto-bar')).toBeTruthy();

        fireEvent.change(screen.getByTestId('code-preview-goto-input'), { target: { value: '2' } });
        fireEvent.click(screen.getByTestId('code-preview-goto-go'));

        // Jump is scheduled on the next animation frame after the bar closes.
        await new Promise<void>((resolve) => {
            requestAnimationFrame(() => resolve());
        });

        expect(scrollIntoView).toHaveBeenCalled();
        expect(screen.queryByTestId('code-preview-goto-bar')).toBeNull();
    });

    it('toggles word wrap and zooms font size from toolbar / shortcuts', () => {
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

        const panel = screen.getByTestId('code-preview-panel');
        const view = () => screen.getByTestId('code-preview-plain-view');

        expect(view().getAttribute('data-word-wrap')).toBe('false');
        expect(view().getAttribute('data-font-size')).toBe(String(CODE_PREVIEW_FONT_DEFAULT));

        fireEvent.click(screen.getByTestId('code-preview-wrap-toggle'));
        expect(view().getAttribute('data-word-wrap')).toBe('true');
        expect(loadCodePreviewViewPrefs().wordWrap).toBe(true);

        fireEvent.keyDown(panel, { key: 'z', altKey: true });
        expect(view().getAttribute('data-word-wrap')).toBe('false');

        fireEvent.click(screen.getByTestId('code-preview-zoom-in'));
        expect(view().getAttribute('data-font-size')).toBe(String(CODE_PREVIEW_FONT_DEFAULT + 1));
        expect(loadCodePreviewViewPrefs().fontSize).toBe(CODE_PREVIEW_FONT_DEFAULT + 1);

        fireEvent.keyDown(panel, { key: '=', ctrlKey: true });
        expect(view().getAttribute('data-font-size')).toBe(String(CODE_PREVIEW_FONT_DEFAULT + 2));

        fireEvent.keyDown(panel, { key: '0', ctrlKey: true });
        expect(view().getAttribute('data-font-size')).toBe(String(CODE_PREVIEW_FONT_DEFAULT));

        fireEvent.keyDown(panel, { key: '-', ctrlKey: true });
        expect(view().getAttribute('data-font-size')).toBe(String(CODE_PREVIEW_FONT_DEFAULT - 1));
    });

    it('shows language, line count, and read-only badges on the path bar', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);

        render(
            <CodePreviewPanel
                files={makeFiles({ language: 'typescript', opType: 'read' })}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        expect(screen.getByTestId('code-preview-lang-badge').textContent).toBe('TypeScript');
        expect(screen.getByTestId('code-preview-readonly-badge').textContent).toMatch(/read-only/i);
        expect(screen.getByTestId('code-preview-line-count').textContent).toMatch(/4\s*lines/);
    });

    it('restores wrap/font prefs from localStorage on mount', () => {
        saveCodePreviewViewPrefs({ wordWrap: true, fontSize: 16 });

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

        const view = screen.getByTestId('code-preview-plain-view');
        expect(view.getAttribute('data-word-wrap')).toBe('true');
        expect(view.getAttribute('data-font-size')).toBe('16');
        expect(screen.getByTestId('code-preview-wrap-toggle').getAttribute('data-active')).toBe('true');
    });

    it('toggles case / word / regex options in the find bar', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);

        render(
            <CodePreviewPanel
                files={makeFiles({
                    content: 'cat\ncategory\nCat\n',
                    opType: 'read',
                })}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'f', ctrlKey: true });
        fireEvent.change(screen.getByTestId('code-preview-find-input'), { target: { value: 'cat' } });
        // Default case-insensitive substring: cat, category, Cat
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*3/);

        fireEvent.click(screen.getByTestId('code-preview-find-case'));
        expect(screen.getByTestId('code-preview-find-case').getAttribute('data-active')).toBe('true');
        // case-sensitive "cat": cat + category
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*2/);

        fireEvent.click(screen.getByTestId('code-preview-find-word'));
        expect(screen.getByTestId('code-preview-find-word').getAttribute('data-active')).toBe('true');
        // whole word + case-sensitive: only "cat"
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*1/);

        fireEvent.click(screen.getByTestId('code-preview-find-regex'));
        fireEvent.change(screen.getByTestId('code-preview-find-input'), { target: { value: '(' } });
        expect(screen.getByTestId('code-preview-find-regex-error')).toBeTruthy();
        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/Invalid regex/i);
    });

    it('highlights find matches in markdown preview blocks', () => {
        localStorage.removeItem(CODE_PREVIEW_VIEW_PREFS_KEY);

        render(
            <CodePreviewPanel
                files={makeFiles({
                    filePath: '/docs/readme.md',
                    fileName: 'readme.md',
                    language: 'markdown',
                    content: '# Title\n\nHello alpha world\n\n## Section\nalpha again\n',
                    opType: 'read',
                })}
                activeFilePath="/docs/readme.md"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        expect(screen.getByTestId('code-preview-markdown-view')).toBeTruthy();

        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'f', ctrlKey: true });
        fireEvent.change(screen.getByTestId('code-preview-find-input'), { target: { value: 'alpha' } });

        expect(screen.getByTestId('code-preview-find-count').textContent).toMatch(/1\s*\/\s*2/);
        const matches = document.querySelectorAll('[data-testid="code-preview-md-block"][data-find-match="true"]');
        expect(matches.length).toBeGreaterThanOrEqual(1);
        expect(document.querySelector('[data-testid="code-preview-md-block"][data-find-active="true"]')).toBeTruthy();

        fireEvent.click(screen.getByTestId('code-preview-find-next'));
        // Second match should become active
        const active = document.querySelector('[data-testid="code-preview-md-block"][data-find-active="true"]');
        expect(active).toBeTruthy();
        expect(active?.getAttribute('data-line-start')).toBeTruthy();
    });
});

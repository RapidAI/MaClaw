/**
 * FileTabBar overflow / VS Code-style multi-file tab management.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import {
    FileTabBar,
    computeMaxVisibleTabs,
    countFittingTabs,
    reconcileVisibleCount,
    nextVisibleCountFromLayout,
    formatOverflowControlLabel,
    resizeObserverEntryWidth,
    MIN_TAB_WIDTH,
} from '../FileTabBar';
import type { CodePreviewTheme } from '../FileTabBar';
import type { CodeFile } from '../useCodePreviewState';

const lightTheme: CodePreviewTheme = {
    bg: '#ffffff',
    text: '#1f2937',
    textMuted: '#64748b',
    border: '#d8dee8',
    lineNumBg: '#f8fafc',
    lineNumText: '#94a3b8',
    tabBg: '#f8fafc',
    tabActiveBg: '#ffffff',
    tabActiveText: '#111827',
    tabHoverBg: '#eef2f7',
    diffAddBg: 'rgba(79, 127, 111, 0.12)',
    diffAddText: '#4f7f6f',
    diffDeleteBg: 'rgba(196, 61, 52, 0.10)',
    diffDeleteText: '#c43d34',
    syntaxKeyword: '#2f5f98',
    syntaxString: '#4f7f6f',
    syntaxComment: '#64748b',
    syntaxNumber: '#2f5f98',
    syntaxFunction: '#334155',
    syntaxType: '#2f5f98',
    syntaxOperator: '#334155',
};

function makeFile(path: string, opType: CodeFile['opType'] = 'modify', updatedAt = 1): CodeFile {
    return {
        filePath: path,
        fileName: path.split(/[/\\]/).pop() || path,
        content: `// ${path}`,
        opType,
        language: 'typescript',
        updatedAt,
        absPath: path,
    };
}

function makeFiles(paths: string[]): Map<string, CodeFile> {
    const map = new Map<string, CodeFile>();
    paths.forEach((p, i) => map.set(p, makeFile(p, 'modify', i + 1)));
    return map;
}

function mockResizeObserver(width: number) {
    class MockResizeObserver {
        private cb: ResizeObserverCallback;
        constructor(cb: ResizeObserverCallback) {
            this.cb = cb;
        }
        observe() {
            this.cb(
                [{ contentRect: { width } } as ResizeObserverEntry],
                this as unknown as ResizeObserver,
            );
        }
        unobserve() {}
        disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', MockResizeObserver);
}

describe('overflow capacity helpers', () => {
    it('returns null for non-positive width so callers can keep prior capacity', () => {
        expect(computeMaxVisibleTabs(0, 5)).toBeNull();
        expect(computeMaxVisibleTabs(-10, 5)).toBeNull();
        expect(computeMaxVisibleTabs(Number.NaN, 5)).toBeNull();
    });

    it('returns 0 when there are no files', () => {
        expect(computeMaxVisibleTabs(400, 0)).toBe(0);
        expect(countFittingTabs([], 200)).toBe(0);
        expect(reconcileVisibleCount(3, 5, 0, false)).toBe(0);
        expect(nextVisibleCountFromLayout(3, 0, 5, false, null, null).count).toBe(0);
    });

    it('caps at fileCount and floors by min tab width', () => {
        expect(computeMaxVisibleTabs(MIN_TAB_WIDTH * 2 + 10, 6)).toBe(2);
        expect(computeMaxVisibleTabs(MIN_TAB_WIDTH * 10, 3)).toBe(3);
        expect(computeMaxVisibleTabs(1, 4)).toBe(1);
    });

    it('countFittingTabs keeps first tab and packs the rest', () => {
        expect(countFittingTabs([200], 100)).toBe(1); // first always kept (clips)
        expect(countFittingTabs([100, 100, 100], 250)).toBe(2);
        expect(countFittingTabs([80, 80, 80, 80], 400)).toBe(4);
        expect(countFittingTabs([120, 120, 120], 0)).toBe(1);
    });

    it('reconcileVisibleCount grows only when strip is not overflowing', () => {
        expect(reconcileVisibleCount(2, 5, 8, true)).toBe(2); // overflowing → keep fitted
        expect(reconcileVisibleCount(2, 5, 8, false)).toBe(5); // room → grow
        expect(reconcileVisibleCount(5, 3, 8, false)).toBe(3); // width shrank
        expect(reconcileVisibleCount(4, null, 8, true)).toBe(4); // null estimate → clamp prev
        // maxFitted ceiling blocks thrash grow above last measured fit
        expect(reconcileVisibleCount(2, 5, 8, false, 2)).toBe(2);
        expect(reconcileVisibleCount(1, 5, 8, false, 3)).toBe(3);
    });

    it('nextVisibleCountFromLayout shrinks on overflow and respects maxFitted when growing', () => {
        // Overflowing → snap to measured fit and record ceiling
        expect(nextVisibleCountFromLayout(5, 8, 5, true, 2, null)).toEqual({
            count: 2,
            maxFitted: 2,
        });
        // Fitting with ceiling: do not re-expand above maxFitted (prevents loops)
        expect(nextVisibleCountFromLayout(2, 8, 5, false, null, 2)).toEqual({
            count: 2,
            maxFitted: 2,
        });
        // Fitting with no ceiling: grow to estimate
        expect(nextVisibleCountFromLayout(2, 8, 5, false, null, null)).toEqual({
            count: 5,
            maxFitted: null,
        });
        // Estimate lower than prev (width shrank)
        expect(nextVisibleCountFromLayout(5, 8, 3, false, null, null)).toEqual({
            count: 3,
            maxFitted: null,
        });
    });

    it('prefers borderBoxSize.inlineSize when present', () => {
        const entry = {
            borderBoxSize: [{ inlineSize: 320, blockSize: 36 }],
            contentRect: { width: 100 },
        } as unknown as ResizeObserverEntry;
        expect(resizeObserverEntryWidth(entry)).toBe(320);
    });

    it('falls back to contentRect.width', () => {
        const entry = {
            contentRect: { width: 280 },
        } as unknown as ResizeObserverEntry;
        expect(resizeObserverEntryWidth(entry)).toBe(280);
    });

    it('formatOverflowControlLabel stays compact for the fixed-width control', () => {
        expect(formatOverflowControlLabel(0, false)).toBe('\u22EF');
        expect(formatOverflowControlLabel(3, true)).toBe('+3');
        expect(formatOverflowControlLabel(99, true)).toBe('+99');
        expect(formatOverflowControlLabel(100, true)).toBe('99+');
    });
});

describe('FileTabBar overflow management', () => {
    beforeEach(() => {
        // Default: narrow strip so only a few tabs fit.
        mockResizeObserver(280);
    });

    afterEach(() => {
        cleanup();
        vi.unstubAllGlobals();
    });

    it('shows sticky +N control and clips strip instead of rendering every tab when narrow', () => {
        const paths = [
            '/src/a.ts',
            '/src/b.ts',
            '/src/c.ts',
            '/src/d.ts',
            '/src/e.ts',
            '/src/f.ts',
        ];
        const onSelect = vi.fn();
        const onClose = vi.fn();

        render(
            <FileTabBar
                files={makeFiles(paths)}
                activeFilePath="/src/e.ts"
                onSelectFile={onSelect}
                onCloseFile={onClose}
                theme={lightTheme}
                lang="en"
            />,
        );

        expect(screen.getByTestId('file-tab-bar').style.overflowX).toBe('visible');
        expect(screen.getByTestId('file-tab-strip').style.overflow).toBe('hidden');
        const overflowBtn = screen.getByTestId('file-tab-overflow-btn');
        expect(overflowBtn).toBeTruthy();
        // Compact stable label: +N for hidden count.
        expect(overflowBtn.textContent).toMatch(/^\+\d+$/);
        expect(overflowBtn.getAttribute('aria-expanded')).toBe('false');

        // Active file must remain among visible tabs.
        const visibleTabs = screen.getAllByTestId('file-tab');
        const active = visibleTabs.find((el) => el.getAttribute('data-file-path') === '/src/e.ts');
        expect(active).toBeTruthy();
        expect(active?.getAttribute('data-active')).toBe('true');

        // Not all files are shown as direct tabs.
        expect(visibleTabs.length).toBeLessThan(paths.length);
        // Expected capacity for 280px strip: floor(280/150)=1.
        expect(visibleTabs.length).toBe(1);
        expect(visibleTabs[0].getAttribute('data-file-path')).toBe('/src/e.ts');
    });

    it('opens overflow dropdown and activates a hidden file', () => {
        const paths = ['/src/a.ts', '/src/b.ts', '/src/c.ts', '/src/d.ts', '/src/e.ts'];
        const onSelect = vi.fn();

        render(
            <FileTabBar
                files={makeFiles(paths)}
                activeFilePath="/src/a.ts"
                onSelectFile={onSelect}
                theme={lightTheme}
                lang="zh-Hans"
            />,
        );

        fireEvent.click(screen.getByTestId('file-tab-overflow-btn'));
        expect(screen.getByTestId('file-tab-overflow-dropdown')).toBeTruthy();
        expect(screen.getByTestId('file-tab-open-editors-filter')).toBeTruthy();

        const items = screen.getAllByTestId('file-tab-overflow-item');
        // Open Editors lists all open files, not only overflow.
        expect(items.length).toBe(paths.length);

        fireEvent.click(items[0]);
        expect(onSelect).toHaveBeenCalled();
        expect(screen.queryByTestId('file-tab-overflow-dropdown')).toBeNull();
    });

    it('filters open editors by query and activates via keyboard', () => {
        mockResizeObserver(800);

        const onSelect = vi.fn();
        render(
            <FileTabBar
                files={makeFiles(['/src/alpha.ts', '/src/beta.ts', '/src/gamma.ts'])}
                activeFilePath="/src/alpha.ts"
                mruOrder={['/src/alpha.ts', '/src/beta.ts', '/src/gamma.ts']}
                onSelectFile={onSelect}
                theme={lightTheme}
                lang="en"
            />,
        );

        // Even without overflow, open-editors button is available for 2+ files.
        fireEvent.click(screen.getByTestId('file-tab-overflow-btn'));
        const filter = screen.getByTestId('file-tab-open-editors-filter');
        fireEvent.change(filter, { target: { value: 'beta' } });

        const items = screen.getAllByTestId('file-tab-overflow-item');
        expect(items).toHaveLength(1);
        expect(items[0].getAttribute('data-file-path')).toBe('/src/beta.ts');

        fireEvent.keyDown(screen.getByTestId('file-tab-overflow-dropdown'), { key: 'Enter' });
        expect(onSelect).toHaveBeenCalledWith('/src/beta.ts');
    });

    it('closes a file from the tab close button', () => {
        const paths = ['/src/a.ts', '/src/b.ts'];
        const onClose = vi.fn();

        // Wide enough for both tabs: no overflow needed.
        mockResizeObserver(800);

        render(
            <FileTabBar
                files={makeFiles(paths)}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                onCloseFile={onClose}
                theme={lightTheme}
            />,
        );

        const closeButtons = screen.getAllByTestId('file-tab-close');
        fireEvent.click(closeButtons[0]);
        expect(onClose).toHaveBeenCalledWith('/src/a.ts');
    });

    it('middle-click closes a tab', () => {
        mockResizeObserver(800);

        const onClose = vi.fn();
        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts'])}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                onCloseFile={onClose}
                theme={lightTheme}
            />,
        );

        const tab = screen.getAllByTestId('file-tab')[1];
        fireEvent.mouseDown(tab, { button: 1 });
        expect(onClose).toHaveBeenCalledWith('/src/b.ts');
    });

    it('keyboard cycles files and closes active with Ctrl+W', () => {
        mockResizeObserver(800);

        const onSelect = vi.fn();
        const onClose = vi.fn();
        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts', '/src/c.ts'])}
                activeFilePath="/src/a.ts"
                // MRU: a most recent, then c, then b — Ctrl+Tab goes to c not b
                mruOrder={['/src/a.ts', '/src/c.ts', '/src/b.ts']}
                onSelectFile={onSelect}
                onCloseFile={onClose}
                theme={lightTheme}
            />,
        );

        const bar = screen.getByTestId('file-tab-bar');
        fireEvent.keyDown(bar, { key: 'ArrowRight' });
        expect(onSelect).toHaveBeenCalledWith('/src/b.ts');

        fireEvent.keyDown(bar, { key: 'Tab', ctrlKey: true });
        expect(onSelect).toHaveBeenCalledWith('/src/c.ts');

        fireEvent.keyDown(bar, { key: 'Tab', ctrlKey: true, shiftKey: true });
        // Reverse MRU from a → b (last in MRU list wraps)
        expect(onSelect).toHaveBeenCalledWith('/src/b.ts');

        fireEvent.keyDown(bar, { key: 'w', ctrlKey: true });
        expect(onClose).toHaveBeenCalledWith('/src/a.ts');
    });

    it('context menu copies path / relative path / file name', async () => {
        mockResizeObserver(800);

        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.assign(navigator, { clipboard: { writeText } });

        render(
            <FileTabBar
                files={makeFiles(['/src/app.ts', '/src/b.ts'])}
                activeFilePath="/src/app.ts"
                onSelectFile={vi.fn()}
                theme={lightTheme}
                lang="en"
            />,
        );

        fireEvent.contextMenu(screen.getAllByTestId('file-tab')[0]);
        fireEvent.click(screen.getByTestId('file-tab-ctx-copy-path'));
        expect(writeText).toHaveBeenCalledWith('/src/app.ts');

        fireEvent.contextMenu(screen.getAllByTestId('file-tab')[0]);
        fireEvent.click(screen.getByTestId('file-tab-ctx-copy-relative'));
        expect(writeText).toHaveBeenCalledWith('/src/app.ts');

        fireEvent.contextMenu(screen.getAllByTestId('file-tab')[0]);
        fireEvent.click(screen.getByTestId('file-tab-ctx-copy-name'));
        expect(writeText).toHaveBeenCalledWith('app.ts');
    });

    it('shows dirty marker for modified/new files but not pure reads', () => {
        mockResizeObserver(800);

        const files = new Map<string, CodeFile>([
            ['/src/a.ts', { ...makeFile('/src/a.ts', 'modify'), original: 'old', content: 'new' }],
            ['/src/b.ts', makeFile('/src/b.ts', 'read')],
            ['/src/c.ts', makeFile('/src/c.ts', 'create')],
        ]);

        render(
            <FileTabBar
                files={files}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                theme={lightTheme}
            />,
        );

        const tabs = screen.getAllByTestId('file-tab');
        const byPath = (p: string) => tabs.find((t) => t.getAttribute('data-file-path') === p)!;
        expect(byPath('/src/a.ts').getAttribute('data-dirty')).toBe('true');
        expect(byPath('/src/b.ts').getAttribute('data-dirty')).toBe('false');
        expect(byPath('/src/c.ts').getAttribute('data-dirty')).toBe('true');
        expect(screen.getAllByTestId('file-tab-dirty').length).toBe(2);
        expect(byPath('/src/a.ts').textContent).toContain('+1');
        expect(byPath('/src/a.ts').textContent).toContain('-1');
        expect(byPath('/src/a.ts').textContent).not.toContain('MOD');
        expect(byPath('/src/b.ts').textContent).toContain('READ');
    });

    it('shows pin marker and pin context action', () => {
        mockResizeObserver(800);

        const onTogglePin = vi.fn();
        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts'])}
                activeFilePath="/src/a.ts"
                pinnedPaths={['/src/b.ts']}
                onSelectFile={vi.fn()}
                onTogglePinFile={onTogglePin}
                theme={lightTheme}
                lang="en"
            />,
        );

        // Pinned tab sorts left
        const tabs = screen.getAllByTestId('file-tab');
        expect(tabs[0].getAttribute('data-file-path')).toBe('/src/b.ts');
        expect(tabs[0].getAttribute('data-pinned')).toBe('true');
        expect(screen.getByTestId('file-tab-pin-marker')).toBeTruthy();

        fireEvent.contextMenu(tabs[1]);
        fireEvent.click(screen.getByTestId('file-tab-ctx-pin'));
        expect(onTogglePin).toHaveBeenCalledWith('/src/a.ts');
    });

    it('drag-and-drop reorders via onMoveFile', () => {
        mockResizeObserver(800);

        const onMove = vi.fn();
        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts', '/src/c.ts'])}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                onMoveFile={onMove}
                theme={lightTheme}
            />,
        );

        const tabs = screen.getAllByTestId('file-tab');
        const source = tabs[0];
        const target = tabs[2];

        const dataTransfer = {
            effectAllowed: 'all',
            dropEffect: 'move',
            setData: vi.fn(),
            getData: (type: string) => (type.includes('file-tab') || type === 'text/plain' ? '/src/a.ts' : ''),
            types: ['text/plain'],
        };

        fireEvent.dragStart(source, { dataTransfer });
        // Drop on the right half of target → place after
        const rect = { left: 100, width: 100, top: 0, height: 30, right: 200, bottom: 30, x: 100, y: 0, toJSON: () => ({}) };
        vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(rect as DOMRect);
        fireEvent.dragOver(target, { dataTransfer, clientX: 180 });
        fireEvent.drop(target, { dataTransfer, clientX: 180 });

        expect(onMove).toHaveBeenCalled();
        const [fromPath, toIndex] = onMove.mock.calls[0];
        expect(fromPath).toBe('/src/a.ts');
        expect(typeof toIndex).toBe('number');
    });

    it('context menu exposes close others / to the right / all', () => {
        mockResizeObserver(800);

        const onClose = vi.fn();
        const onCloseOthers = vi.fn();
        const onCloseRight = vi.fn();
        const onCloseAll = vi.fn();

        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts', '/src/c.ts'])}
                activeFilePath="/src/b.ts"
                onSelectFile={vi.fn()}
                onCloseFile={onClose}
                onCloseOtherFiles={onCloseOthers}
                onCloseFilesToTheRight={onCloseRight}
                onCloseAllFiles={onCloseAll}
                theme={lightTheme}
                lang="en"
            />,
        );

        const tabs = screen.getAllByTestId('file-tab');
        fireEvent.contextMenu(tabs[1]); // /src/b.ts

        expect(screen.getByTestId('file-tab-context-menu')).toBeTruthy();
        fireEvent.click(screen.getByTestId('file-tab-ctx-close-others'));
        expect(onCloseOthers).toHaveBeenCalledWith('/src/b.ts');

        fireEvent.contextMenu(tabs[0]); // /src/a.ts
        fireEvent.click(screen.getByTestId('file-tab-ctx-close-right'));
        expect(onCloseRight).toHaveBeenCalledWith('/src/a.ts');

        fireEvent.contextMenu(tabs[0]);
        fireEvent.click(screen.getByTestId('file-tab-ctx-close-all'));
        expect(onCloseAll).toHaveBeenCalled();
    });

    it.each([
        ['zh-Hans', '在资源管理器中显示', '使用默认应用打开', '复制路径', '关闭其他'],
        ['zh-Hant', '在檔案總管中顯示', '使用預設應用程式開啟', '複製路徑', '關閉其他'],
    ])('localizes context-menu actions for %s', (lang, reveal, open, copyPath, closeOthers) => {
        mockResizeObserver(800);

        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts'])}
                activeFilePath="/src/a.ts"
                onSelectFile={vi.fn()}
                onCloseFile={vi.fn()}
                onCloseOtherFiles={vi.fn()}
                theme={lightTheme}
                lang={lang}
            />,
        );

        fireEvent.contextMenu(screen.getAllByTestId('file-tab')[0]);

        expect(screen.getByText(reveal)).toBeTruthy();
        expect(screen.getByText(open)).toBeTruthy();
        expect(screen.getByTestId('file-tab-ctx-copy-path').textContent).toBe(copyPath);
        expect(screen.getByTestId('file-tab-ctx-close-others').textContent).toBe(closeOthers);
    });

    it('disables close others/right/all when only pinned tabs would remain', () => {
        mockResizeObserver(800);

        render(
            <FileTabBar
                files={makeFiles(['/src/a.ts', '/src/b.ts', '/src/c.ts'])}
                activeFilePath="/src/a.ts"
                pinnedPaths={['/src/a.ts', '/src/b.ts', '/src/c.ts']}
                onSelectFile={vi.fn()}
                onCloseFile={vi.fn()}
                onCloseOtherFiles={vi.fn()}
                onCloseFilesToTheRight={vi.fn()}
                onCloseAllFiles={vi.fn()}
                theme={lightTheme}
                lang="en"
            />,
        );

        const tabs = screen.getAllByTestId('file-tab');
        fireEvent.contextMenu(tabs[0]);
        expect((screen.getByTestId('file-tab-ctx-close-others') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('file-tab-ctx-close-right') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('file-tab-ctx-close-all') as HTMLButtonElement).disabled).toBe(true);
    });
});

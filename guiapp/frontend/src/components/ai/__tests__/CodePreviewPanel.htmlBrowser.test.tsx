import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenWebPreview: vi.fn(async () => {
        throw new Error('preview server unavailable');
    }),
    CloseWebPreview: vi.fn(async () => undefined),
    PreviewTaskResultFile: vi.fn(async () => ({ kind: 'text', content: '' })),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
}));

function htmlFile(): CodeFile {
    return {
        filePath: 'site/index.html',
        fileName: 'index.html',
        absPath: 'D:/site/index.html',
        content: 'HELLO_MARKER',
        language: 'html',
        opType: 'read',
        updatedAt: 1,
    };
}

function renderFile(file: CodeFile) {
    return render(
        <CodePreviewPanel
            files={new Map([[file.filePath, file]])}
            activeFilePath={file.filePath}
            onSelectFile={vi.fn()}
            onClose={vi.fn()}
            theme={lightCodePreviewTheme}
            lang="zh"
        />,
    );
}

describe('assistant web preview', () => {
    it('opens HTML as a browser and can switch back to source', () => {
        renderFile(htmlFile());
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
        expect(screen.getByTestId('code-preview-html-browser-toggle').textContent).toContain('源码');
        expect(screen.queryByText('HELLO_MARKER')).toBeNull();

        fireEvent.click(screen.getByTestId('code-preview-html-browser-toggle'));
        expect(screen.queryByTestId('web-preview-browser')).toBeNull();
        expect(screen.getByText('HELLO_MARKER')).toBeTruthy();
        expect(screen.getByTestId('code-preview-html-browser-toggle').textContent).toContain('预览');

        fireEvent.click(screen.getByTestId('code-preview-html-browser-toggle'));
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
    });

    it('opens an SVG in the web preview instead of source', () => {
        renderFile({
            filePath: 'icon.svg',
            fileName: 'icon.svg',
            absPath: 'D:/site/icon.svg',
            content: '<svg>SVG_MARKER</svg>',
            language: 'svg',
            opType: 'read',
            updatedAt: 1,
        });
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
        expect(screen.queryByText('SVG_MARKER')).toBeNull();
    });

    it('opens an http(s) URL directly', () => {
        renderFile({
            filePath: 'https://example.com/docs',
            fileName: 'docs',
            content: '',
            language: 'text',
            opType: 'read',
            updatedAt: 1,
        });
        expect(screen.getByTestId('html-preview-panel').getAttribute('src')).toBe('https://example.com/docs');
    });

    it('reopens the rendered page when the assistant rewrites HTML', () => {
        const file = { ...htmlFile(), opType: 'create' as const, updatedAt: 1 };
        const { rerender } = renderFile(file);
        fireEvent.click(screen.getByTestId('code-preview-html-browser-toggle'));
        expect(screen.getByText('HELLO_MARKER')).toBeTruthy();

        const rewritten = { ...file, content: 'NEXT_PAGE', updatedAt: 2, opType: 'modify' as const };
        rerender(
            <CodePreviewPanel
                files={new Map([[rewritten.filePath, rewritten]])}
                activeFilePath={rewritten.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
        expect(screen.queryByText('NEXT_PAGE')).toBeNull();
    });

    it('keeps source mode when a later HTML event is only a read', () => {
        const file = htmlFile();
        const { rerender } = renderFile(file);
        fireEvent.click(screen.getByTestId('code-preview-html-browser-toggle'));

        const reread = { ...file, updatedAt: 2 };
        rerender(
            <CodePreviewPanel
                files={new Map([[reread.filePath, reread]])}
                activeFilePath={reread.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.queryByTestId('web-preview-browser')).toBeNull();
        expect(screen.getByText('HELLO_MARKER')).toBeTruthy();
    });

    it('leaves the directory tree to show a page the assistant just wrote', () => {
        const { rerender } = render(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                projectPath="D:/proj"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.queryByTestId('web-preview-browser')).toBeNull();

        const page = {
            filePath: 'site/index.html',
            fileName: 'index.html',
            absPath: 'D:/proj/site/index.html',
            content: '<h1>PAGE</h1>',
            language: 'html',
            opType: 'create' as const,
            updatedAt: 3,
        };
        rerender(
            <CodePreviewPanel
                files={new Map([[page.filePath, page]])}
                activeFilePath={page.filePath}
                projectPath="D:/proj"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
        expect(screen.queryByText('PAGE')).toBeNull();
    });

    it('shows a generated page when the directory focus nonce is newer, then returns to the tree', () => {
        const page = {
            filePath: 'site/index.html',
            fileName: 'index.html',
            absPath: 'D:/proj/site/index.html',
            content: '<h1>PAGE</h1>',
            language: 'html',
            opType: 'read' as const,
            updatedAt: 1,
        };
        const { rerender } = render(
            <CodePreviewPanel
                files={new Map([[page.filePath, page]])}
                activeFilePath={page.filePath}
                projectPath="D:/proj"
                fileFocusNonce={2}
                treeFocusNonce={1}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        fireEvent.click(screen.getByTestId('code-preview-html-browser-toggle'));
        expect(screen.getByText('PAGE')).toBeTruthy();

        const written = { ...page, content: '<h1>FRESH</h1>', opType: 'modify' as const, updatedAt: 2 };
        rerender(
            <CodePreviewPanel
                files={new Map([[written.filePath, written]])}
                activeFilePath={written.filePath}
                projectPath="D:/proj"
                fileFocusNonce={2}
                treeFocusNonce={5}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.getByTestId('web-preview-browser')).toBeTruthy();
        expect(screen.queryByText('FRESH')).toBeNull();

        rerender(
            <CodePreviewPanel
                files={new Map([[written.filePath, written]])}
                activeFilePath={written.filePath}
                projectPath="D:/proj"
                fileFocusNonce={2}
                treeFocusNonce={6}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.queryByTestId('web-preview-browser')).toBeNull();
        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
    });

    it('does not use web preview for a raster image', () => {
        renderFile({
            filePath: 'photo.png',
            fileName: 'photo.png',
            absPath: 'D:/photo.png',
            content: 'PNG_BYTES',
            language: 'image',
            opType: 'read',
            updatedAt: 1,
        });
        expect(screen.queryByTestId('web-preview-browser')).toBeNull();
    });
});

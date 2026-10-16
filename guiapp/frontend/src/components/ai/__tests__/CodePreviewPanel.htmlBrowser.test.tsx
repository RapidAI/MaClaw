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

describe('assistant HTML browser preview', () => {
    it('opens HTML as a browser and can switch back to source', () => {
        render(
            <CodePreviewPanel
                files={new Map([['site/index.html', htmlFile()]])}
                activeFilePath="site/index.html"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
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
});

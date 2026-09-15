import { render, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../../ai/CodePreviewPanel';
import { FilePreviewView, filePreviewUsesSpecialRenderer, isAssistantSourcePreview } from '../FilePreviewView';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    PreviewTaskResultFile: vi.fn(async (path: string) => {
        if (String(path).endsWith('.pdf')) {
            return { kind: 'pdf', preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' };
        }
        if (String(path).endsWith('.png')) {
            return { kind: 'image', preview_url: '/maclaw-preview/v1/file?t=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' };
        }
        if (String(path).endsWith('.docx')) {
            return { kind: 'text', language: 'markdown', content: '# Heading\n\nBody' };
        }
        if (String(path).includes('missing.html')) {
            throw new Error('文件不存在');
        }
        if (String(path).endsWith('.html')) {
            return { kind: 'text', language: 'html', content: '<html><body>Hi</body></html>' };
        }
        return { kind: 'text', content: 'plain' };
    }),
    AIAssistantAttachmentFullDataURL: vi.fn(async () => ''),
    PptxPreviewEnsure: vi.fn(async () => ({
        images: ['D:/decks/slide_001.png'],
    })),
    PptxSlideThumbnailDataURL: vi.fn(async () => 'data:image/png;base64,thumb'),
}));

describe('filePreviewUsesSpecialRenderer', () => {
    it('covers visual and formatted documents, not source', () => {
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.pptx', absPath: 'D:/a.pptx' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.pptx' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.docx' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.md' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.html' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'icon.svg' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.go' })).toBe(false);
        expect(isAssistantSourcePreview({ fileName: 'icon.svg' })).toBe(true);
        expect(isAssistantSourcePreview({ fileName: 'photo.png' })).toBe(false);
    });
});

describe('FilePreviewView', () => {
    it('renders formatted markdown for Word instead of a plain dump', async () => {
        const { getByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'brief.docx', absPath: 'D:/docs/brief.docx' }}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        await waitFor(() => expect(getByTestId('office-preview-panel')).toBeTruthy());
        expect(getByTestId('code-preview-markdown-view')).toBeTruthy();
        expect(getByTestId('code-preview-markdown-view').textContent).toContain('Heading');
    });

    it('renders a PDF reader', async () => {
        const { getByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'report.pdf', absPath: 'D:/docs/report.pdf', language: 'pdf' }}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        await waitFor(() => expect(getByTestId('pdf-preview-panel')).toBeTruthy());
    });

    it('keeps raster images as images even if a parent passes children', async () => {
        const { queryByText, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'photo.png', absPath: 'D:/docs/photo.png' }}
                theme={lightCodePreviewTheme}
                lang="en"
            >
                <div>diff-child</div>
            </FilePreviewView>,
        );
        expect(queryByText('diff-child')).toBeNull();
        await waitFor(() => expect(
            queryByTestId('image-preview-loading')
            || queryByTestId('image-preview-panel')
            || queryByTestId('image-preview-error'),
        ).toBeTruthy());
    });

    it('lets a parent source view replace an SVG image', () => {
        const { getByText, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'icon.svg', absPath: 'D:/icon.svg' }}
                theme={lightCodePreviewTheme}
                lang="en"
            >
                <div>svg-source</div>
            </FilePreviewView>,
        );
        expect(getByText('svg-source')).toBeTruthy();
        expect(queryByTestId('image-preview-panel')).toBeNull();
    });

    it('lets a parent source view replace the HTML iframe', () => {
        const { getByText, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'page.html', content: '<html></html>' }}
                theme={lightCodePreviewTheme}
                lang="en"
            >
                <div>html-source</div>
            </FilePreviewView>,
        );
        expect(getByText('html-source')).toBeTruthy();
        expect(queryByTestId('html-preview-panel')).toBeNull();
    });

    it('lets a parent diff replace rendered markdown', () => {
        const { getByText, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'README.md', language: 'markdown', content: '# New' }}
                theme={lightCodePreviewTheme}
                lang="en"
            >
                <div>diff-child</div>
            </FilePreviewView>,
        );
        expect(getByText('diff-child')).toBeTruthy();
        expect(queryByTestId('code-preview-markdown-view')).toBeNull();
    });

    it('falls through to children for source files', () => {
        const { getByText } = render(
            <FilePreviewView
                file={{ fileName: 'main.go', content: 'package main', language: 'go' }}
                theme={lightCodePreviewTheme}
                lang="en"
            >
                <div>source-child</div>
            </FilePreviewView>,
        );
        expect(getByText('source-child')).toBeTruthy();
    });

    it('rejects protocol-relative image URLs', async () => {
        const { getByTestId, queryByRole } = render(
            <FilePreviewView
                file={{ fileName: 'shot.png', dataUrl: '//evil.example/x.png' }}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        await waitFor(() => expect(getByTestId('image-preview-error')).toBeTruthy());
        expect(queryByRole('img')).toBeNull();
    });

    it('classifies a deck by absPath when the display name has no extension', async () => {
        const { queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: '布偶小猫5岁生日', absPath: 'D:/docs/cat.pptx' }}
                theme={lightCodePreviewTheme}
                lang="zh"
            >
                <div>fallback-plain</div>
            </FilePreviewView>,
        );
        await waitFor(() => expect(queryByTestId('pptx-preview-panel') || queryByTestId('pptx-preview-loading')).toBeTruthy());
    });

    it('falls back to the extract when the HTML original cannot be read', async () => {
        const { getByTestId, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'page.html', absPath: 'D:/docs/missing.html', content: '# extract only' }}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        await waitFor(() => expect(getByTestId('html-preview-panel')).toBeTruthy());
        expect(queryByTestId('html-preview-error')).toBeNull();
    });

    it('loads original HTML when the extract is not a document', async () => {
        const { getByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'page.html', absPath: 'D:/docs/page.html', content: '# not html' }}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        await waitFor(() => expect(getByTestId('html-preview-panel')).toBeTruthy());
    });
});

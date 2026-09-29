import { fireEvent, render, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../../ai/CodePreviewPanel';
import { fitDocxToPane } from '../../ai/DocxPreviewPanel';
import { FilePreviewView, filePreviewUsesSpecialRenderer, isAssistantSourcePreview } from '../FilePreviewView';
import { SaveCodingWorkbenchTextFile } from '../../../../wailsjs/go/main/App';

vi.mock('docx-preview', () => ({
    renderAsync: vi.fn(async (_data: ArrayBuffer, el: HTMLElement) => {
        const page = document.createElement('section');
        page.textContent = 'rendered-docx';
        el.appendChild(page);
    }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    PreviewTaskResultFile: vi.fn(async (path: string) => {
        if (String(path).endsWith('.pdf')) {
            return { kind: 'pdf', preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' };
        }
        if (String(path).endsWith('.png')) {
            return { kind: 'image', preview_url: '/maclaw-preview/v1/file?t=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' };
        }
        if (String(path).endsWith('.docx')) {
            return { kind: 'docx', preview_url: '/maclaw-preview/v1/file?t=cccccccccccccccccccccccccccccccc' };
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
    SaveCodingWorkbenchTextFile: vi.fn(async () => undefined),
    CompileLatexWorkbenchFile: vi.fn(async () => ({})),
    GetCodingWorkbenchFilePreview: vi.fn(async () => ({ content: '' })),
}));

describe('filePreviewUsesSpecialRenderer', () => {
    it('covers visual and formatted documents, not source', () => {
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.pptx', absPath: 'D:/a.pptx' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.pptx' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.docx', absPath: 'D:/a.docx' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.docx' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.md' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.html' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'icon.svg' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'a.go' })).toBe(false);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'main.tex', absPath: 'D:/paper/main.tex' })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'main.tex', latexWorkbench: true })).toBe(true);
        expect(filePreviewUsesSpecialRenderer({ fileName: 'main.tex' })).toBe(false);
        expect(isAssistantSourcePreview({ fileName: 'icon.svg' })).toBe(true);
        expect(isAssistantSourcePreview({ fileName: 'photo.png' })).toBe(false);
    });
});

describe('FilePreviewView', () => {
    it('renders a Word document in its own layout instead of a text extract', async () => {
        const fetchMock = vi.fn(async () => ({
            ok: true,
            arrayBuffer: async () => new ArrayBuffer(8),
        }));
        vi.stubGlobal('fetch', fetchMock);
        const { getByTestId, queryByTestId } = render(
            <FilePreviewView
                file={{ fileName: 'brief.docx', absPath: 'D:/docs/brief.docx' }}
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        await waitFor(() => expect(getByTestId('docx-preview-panel').textContent).toContain('rendered-docx'));
        expect(queryByTestId('office-preview-panel')).toBeNull();
        expect(fetchMock).toHaveBeenCalled();
        vi.unstubAllGlobals();
    });

    it('scales a wide Word page down to the preview pane and keeps the left edge', () => {
        const scroll = document.createElement('div');
        const body = document.createElement('div');
        const wrapper = document.createElement('div');
        wrapper.className = 'maclaw-docx-wrapper';
        wrapper.style.alignItems = 'center';
        const page = document.createElement('section');
        wrapper.appendChild(page);
        body.appendChild(wrapper);
        Object.defineProperty(wrapper, 'scrollWidth', { configurable: true, value: 800 });
        Object.defineProperty(scroll, 'clientWidth', { configurable: true, value: 408 });
        fitDocxToPane(scroll, body);
        expect(wrapper.style.alignItems).toBe('flex-start');
        expect(body.style.getPropertyValue('zoom')).toBe('0.5');
        expect(scroll.scrollLeft).toBe(0);
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

    it('opens a cloud tex file in the editor instead of compiling a cache path', () => {
        const { getByTestId, queryByTestId } = render(
            <FilePreviewView
                file={{
                    fileName: 'main.tex',
                    filePath: 'paper/main.tex',
                    content: '\\documentclass{article}',
                    language: 'latex',
                    latexWorkbench: true,
                }}
                projectPath="cloud-task"
                theme={lightCodePreviewTheme}
                lang="zh"
            >
                <div>source-child</div>
            </FilePreviewView>,
        );
        expect(getByTestId('latex-workbench-editor')).toBeTruthy();
        expect((getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('\\documentclass{article}');
        expect(queryByTestId('latex-preview-status')).toBeNull();
    });

    it('saves a LaTeX document into its own workspace', async () => {
        const { getByTestId } = render(
            <FilePreviewView
                file={{
                    fileName: 'main.tex',
                    filePath: 'main.tex',
                    projectPath: 'latex-task',
                    content: '\\documentclass{article}',
                    language: 'latex',
                    latexWorkbench: true,
                }}
                projectPath="other-task"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        fireEvent.change(getByTestId('latex-workbench-source'), { target: { value: '\\documentclass{article}\n% edited' } });
        fireEvent.click(getByTestId('latex-workbench-save'));
        await waitFor(() => expect(SaveCodingWorkbenchTextFile).toHaveBeenCalledWith(
            'latex-task',
            'main.tex',
            '\\documentclass{article}\n% edited',
        ));
    });

    it('keeps an unsaved draft when the open paper gains an absolute path', () => {
        vi.mocked(SaveCodingWorkbenchTextFile).mockClear();
        const file = {
            fileName: 'main.tex',
            filePath: 'main.tex',
            projectPath: 'latex-task',
            content: 'old-source',
            language: 'latex',
            latexWorkbench: true,
            updatedAt: 1,
        };
        const view = render(
            <FilePreviewView file={file} projectPath="latex-task" theme={lightCodePreviewTheme} lang="zh" />,
        );
        fireEvent.change(view.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        view.rerender(
            <FilePreviewView
                file={{ ...file, absPath: 'D:/tasks/latex/workspace/main.tex', content: 'from-disk', updatedAt: 2 }}
                projectPath="latex-task"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect((view.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('draft');
        expect(SaveCodingWorkbenchTextFile).not.toHaveBeenCalled();
    });

    it('opens the next paper instead of keeping the previous main.tex', async () => {
        const file = {
            fileName: 'main.tex',
            filePath: 'main.tex',
            content: 'alpha',
            language: 'latex',
            latexWorkbench: true,
            updatedAt: 1,
        };
        const view = render(
            <FilePreviewView
                file={{ ...file, projectPath: 'task-a' }}
                projectPath="task-a"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        fireEvent.change(view.getByTestId('latex-workbench-source'), { target: { value: 'alpha-edited' } });
        view.rerender(
            <FilePreviewView
                file={{ ...file, projectPath: 'task-b', content: 'beta' }}
                projectPath="task-b"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect((view.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('beta');
        await waitFor(() => expect(SaveCodingWorkbenchTextFile).toHaveBeenCalledWith('task-a', 'main.tex', 'alpha-edited'));
        expect(SaveCodingWorkbenchTextFile).not.toHaveBeenCalledWith('task-b', 'main.tex', 'alpha-edited');
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

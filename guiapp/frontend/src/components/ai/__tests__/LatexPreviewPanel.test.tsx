import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../CodePreviewPanel';
import { LatexPreviewPanel, clearLatexEditorStateForTests, latexProgressMatches } from '../LatexPreviewPanel';

const mocks = vi.hoisted(() => ({
    compile: vi.fn(),
    compileWorkbench: vi.fn(),
    save: vi.fn(),
    reload: vi.fn(),
    preview: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CompileLatexPreview: mocks.compile,
    CompileLatexWorkbenchFile: mocks.compileWorkbench,
    SaveCodingWorkbenchTextFile: mocks.save,
    GetCodingWorkbenchFilePreview: mocks.reload,
    PreviewTaskResultFile: mocks.preview,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

describe('LatexPreviewPanel', () => {
    afterEach(async () => {
        cleanup();
        await act(async () => { await Promise.resolve(); });
        clearLatexEditorStateForTests();
        vi.clearAllMocks();
    });

    it('compiles on open and shows the PDF', async () => {
        mocks.compile.mockResolvedValue({
            ok: true,
            pdf_path: 'D:/paper/main.pdf',
            repaired: true,
        });
        mocks.preview.mockResolvedValue({
            kind: 'pdf',
            preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        });
        render(<LatexPreviewPanel absPath="D:/paper/main.tex" theme={lightCodePreviewTheme} lang="zh" />);
        expect(await screen.findByTestId('latex-preview-ready')).toBeTruthy();
        expect(screen.getByText(/自动修改源文件/)).toBeTruthy();
        await waitFor(() => expect(mocks.compile).toHaveBeenCalledWith('D:/paper/main.tex'));
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
    });

    it('shows the compile log when TinyTeX is not ready', async () => {
        mocks.compile.mockResolvedValue({
            ok: false,
            error: 'TinyTeX 尚未就绪，正在后台安装 scheme-small',
        });
        render(<LatexPreviewPanel absPath="D:/paper/main.tex" theme={lightCodePreviewTheme} lang="zh" />);
        expect(await screen.findByText(/scheme-small/)).toBeTruthy();
        expect(screen.getByRole('button', { name: '重试编译' })).toBeTruthy();
    });

    it('edits a cloud tex file and compiles from the workspace path', async () => {
        mocks.save.mockResolvedValue(undefined);
        mocks.compileWorkbench.mockResolvedValue({
            ok: true,
            preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        });
        render(
            <LatexPreviewPanel
                projectPath="cloud-task"
                relativePath="paper/main.tex"
                initialContent="\\documentclass{article}"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(mocks.compile).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'edited' } });
        fireEvent.click(screen.getByTestId('latex-workbench-preview'));
        await waitFor(() => expect(mocks.save).toHaveBeenCalledWith('cloud-task', 'paper/main.tex', 'edited'));
        await waitFor(() => expect(mocks.compileWorkbench).toHaveBeenCalledWith('cloud-task', 'paper/main.tex'));
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        expect(mocks.preview).not.toHaveBeenCalled();
    });

    it('shows a new listing when the editor has no local edits', () => {
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            updatedAt: 1,
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const view = render(<LatexPreviewPanel {...props} />);
        view.rerender(<LatexPreviewPanel {...props} initialContent="from-disk" updatedAt={2} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('from-disk');
        expect(mocks.save).not.toHaveBeenCalled();
    });

    it('shows a listing that arrived during an edit after the draft is reverted', () => {
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            updatedAt: 1,
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const view = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        view.rerender(<LatexPreviewPanel {...props} initialContent="from-disk" updatedAt={2} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('draft');
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'old-source' } });
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('from-disk');
        expect(mocks.save).not.toHaveBeenCalled();
    });

    it('keeps a saved draft when a listing arrived during the edit', async () => {
        mocks.save.mockResolvedValue(undefined);
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            updatedAt: 1,
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const view = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        view.rerender(<LatexPreviewPanel {...props} initialContent="from-disk" updatedAt={2} />);
        fireEvent.click(screen.getByTestId('latex-workbench-save'));
        await waitFor(() => expect(mocks.save).toHaveBeenCalledWith('cloud-task', 'paper/main.tex', 'draft'));
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('draft');
    });

    it('keeps unsaved text when a newer listing arrives', () => {
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            updatedAt: 1,
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const view = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        view.rerender(<LatexPreviewPanel {...props} initialContent="from-disk" updatedAt={2} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('draft');
        expect(mocks.save).not.toHaveBeenCalled();
    });

    it('restores unsaved text when the cloud editor is opened again', () => {
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const first = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        first.unmount();
        render(<LatexPreviewPanel {...props} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('draft');
        expect((screen.getByTestId('latex-workbench-save') as HTMLButtonElement).disabled).toBe(false);
    });

    it('reports a saved cloud edit under StrictMode', async () => {
        mocks.save.mockResolvedValue(undefined);
        const onSourceChange = vi.fn();
        render(
            <StrictMode>
                <LatexPreviewPanel
                    projectPath="cloud-task"
                    relativePath="paper/main.tex"
                    initialContent="old-source"
                    theme={lightCodePreviewTheme}
                    lang="zh"
                    onSourceChange={onSourceChange}
                />
            </StrictMode>,
        );
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'edited' } });
        fireEvent.click(screen.getByTestId('latex-workbench-save'));
        await waitFor(() => expect(onSourceChange).toHaveBeenCalledWith('edited'));
        expect((screen.getByTestId('latex-workbench-save') as HTMLButtonElement).disabled).toBe(true);
    });

    it('uses a newly opened file instead of an older saved copy', async () => {
        mocks.save.mockResolvedValue(undefined);
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            updatedAt: 1,
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const first = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'draft' } });
        first.unmount();
        await waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(1));
        await act(async () => { await Promise.resolve(); });
        render(<LatexPreviewPanel {...props} initialContent="from-disk" updatedAt={2} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('from-disk');
    });

    it('reopens onto the repaired source when the editor closes during compile', async () => {
        let releaseReload: (value: { content?: string }) => void = () => {};
        mocks.save.mockResolvedValue(undefined);
        mocks.compileWorkbench.mockResolvedValue({ ok: false, repaired: true, error: '编译失败' });
        mocks.reload.mockImplementation(() => new Promise((resolve) => { releaseReload = resolve; }));
        const props = {
            projectPath: 'cloud-task',
            relativePath: 'paper/main.tex',
            initialContent: 'old-source',
            theme: lightCodePreviewTheme,
            lang: 'zh',
        };
        const first = render(<LatexPreviewPanel {...props} />);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'edited-source' } });
        fireEvent.click(screen.getByTestId('latex-workbench-preview'));
        await waitFor(() => expect(mocks.reload).toHaveBeenCalled());
        first.unmount();
        releaseReload({ content: 'repaired-source' });
        await act(async () => { await Promise.resolve(); });
        render(<LatexPreviewPanel {...props} />);
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('repaired-source');
        expect(mocks.save.mock.calls).toEqual([['cloud-task', 'paper/main.tex', 'edited-source']]);
    });

    it('starts one compile when preview is clicked twice', async () => {
        mocks.compileWorkbench.mockImplementation(() => new Promise(() => {}));
        render(
            <LatexPreviewPanel
                projectPath="cloud-task"
                relativePath="paper/main.tex"
                initialContent="\\documentclass{article}"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        const preview = screen.getByTestId('latex-workbench-preview');
        fireEvent.click(preview);
        fireEvent.click(preview);
        await waitFor(() => expect(mocks.compileWorkbench).toHaveBeenCalledTimes(1));
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).readOnly).toBe(true);
    });

    it('does not save when the source could not be read', () => {
        render(
            <LatexPreviewPanel
                projectPath="cloud-task"
                relativePath="paper/main.tex"
                initialContent=""
                readError="无法读取论文源文件"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(screen.getByTestId('latex-workbench-read-error').textContent).toBe('无法读取论文源文件');
        expect((screen.getByTestId('latex-workbench-save') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('latex-workbench-preview') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(screen.getByTestId('latex-workbench-source'), { target: { value: 'overwrite' } });
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('');
        expect(mocks.save).not.toHaveBeenCalled();
    });

    it('matches compile progress for a cloud cache path without showing it', () => {
        expect(latexProgressMatches(
            'C:/Users/me/.maclaw/data/cloud-workspaces/t/ws/paper/main.tex',
            '',
            'paper/main.tex',
        )).toBe(true);
        expect(latexProgressMatches('main.tex', '', 'paper/main.tex')).toBe(true);
        expect(latexProgressMatches(
            'C:/Users/me/.maclaw/data/cloud-workspaces/t/ws/other.tex',
            '',
            'paper/main.tex',
        )).toBe(false);
        expect(latexProgressMatches('notmain.tex', '', 'main.tex')).toBe(false);
        expect(latexProgressMatches('paper/main.tex', '', 'main.tex')).toBe(false);
        expect(latexProgressMatches('notes/main.tex', '', 'paper/main.tex')).toBe(false);
    });

    it('reloads a repaired source even when the rebuild still fails', async () => {
        mocks.compileWorkbench.mockResolvedValue({
            ok: false,
            repaired: true,
            error: '编译失败',
        });
        mocks.reload.mockResolvedValue({ content: 'repaired-source' });
        render(
            <LatexPreviewPanel
                projectPath="cloud-task"
                relativePath="paper/main.tex"
                initialContent="old-source"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        fireEvent.click(screen.getByTestId('latex-workbench-preview'));
        expect(await screen.findByText('编译失败')).toBeTruthy();
        expect((screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value).toBe('repaired-source');
    });

    it('does not replace the editor with a truncated repaired source', async () => {
        mocks.compileWorkbench.mockResolvedValue({
            ok: true,
            repaired: true,
            preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        });
        mocks.reload.mockResolvedValue({ content: 'truncated', truncated: true });
        render(
            <LatexPreviewPanel
                projectPath="cloud-task"
                relativePath="paper/main.tex"
                initialContent="\\documentclass{article}"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        fireEvent.click(screen.getByTestId('latex-workbench-preview'));
        expect(await screen.findByText(/没有重新加载/)).toBeTruthy();
        const source = (screen.getByTestId('latex-workbench-source') as HTMLTextAreaElement).value;
        expect(source).toContain('documentclass');
        expect(source).not.toBe('truncated');
        expect((screen.getByTestId('latex-workbench-save') as HTMLButtonElement).disabled).toBe(true);
    });
});

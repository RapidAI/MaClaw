import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { dispatchOpenLatexDocument, resetLatexDocumentRequestForTests, takePendingLatexDocument } from '../latexDocumentOpen';
import { useAssistantPreviewOpenEvents } from '../useAssistantPreviewOpenEvents';
import { getWailsAppModule } from '../../../utils/wailsAppModule';

vi.mock('../../../utils/wailsAppModule', () => ({
    getWailsAppModule: vi.fn(),
}));

describe('useAssistantPreviewOpenEvents latex document', () => {
    beforeEach(() => {
        resetLatexDocumentRequestForTests();
        vi.mocked(getWailsAppModule).mockReset();
    });

    it('reads the template from disk when the open request has no source', async () => {
        const preview = vi.fn().mockResolvedValue({
            content: '\\documentclass{article}\n\\begin{document}Hi\\end{document}\n',
            truncated: false,
            abs_path: 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/ws/main.tex',
        });
        vi.mocked(getWailsAppModule).mockResolvedValue({ GetCodingWorkbenchFilePreview: preview } as never);
        const openWorkspaceFile = vi.fn();
        const openPreviewPane = vi.fn();
        const generationRef = { current: 0 };
        renderHook(() => useAssistantPreviewOpenEvents({
            allowed: true,
            lang: 'zh-Hans',
            openWorkspaceFile,
            focusOpenedFile: () => {},
            generationRef,
            openPreviewPane,
        }));

        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'main.tex' });

        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(preview).toHaveBeenCalledWith('D:/tasks/latex', 'main.tex');
        expect(openPreviewPane).toHaveBeenCalledWith(true);
        const file = openWorkspaceFile.mock.calls[0][0];
        expect(file.content).toContain('\\documentclass{article}');
        expect(file.latexWorkbench).toBe(true);
        expect(file.projectPath).toBe('D:/tasks/latex');
        expect(file.absPath).toBeUndefined();
        expect(String(file.content)).not.toContain('cloud-workspaces');
    });

    it('keeps preloaded source and does not read the file again', async () => {
        const preview = vi.fn();
        vi.mocked(getWailsAppModule).mockResolvedValue({ GetCodingWorkbenchFilePreview: preview } as never);
        const openWorkspaceFile = vi.fn();
        renderHook(() => useAssistantPreviewOpenEvents({
            allowed: true,
            lang: 'zh-Hans',
            openWorkspaceFile,
            focusOpenedFile: () => {},
            generationRef: { current: 0 },
            openPreviewPane: () => {},
        }));

        dispatchOpenLatexDocument({
            projectPath: 'D:/tasks/latex',
            relativePath: 'main.tex',
            content: '\\documentclass{article}',
        });

        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(preview).not.toHaveBeenCalled();
        expect(openWorkspaceFile.mock.calls[0][0].content).toBe('\\documentclass{article}');
    });

    it('locks the editor when the source cannot be read', async () => {
        vi.mocked(getWailsAppModule).mockResolvedValue({
            GetCodingWorkbenchFilePreview: vi.fn().mockRejectedValue(new Error('open C:/Users/me/.maclaw/data/cloud-workspaces/tenant/ws/main.tex')),
        } as never);
        const openWorkspaceFile = vi.fn();
        renderHook(() => useAssistantPreviewOpenEvents({
            allowed: true,
            lang: 'zh-Hans',
            openWorkspaceFile,
            focusOpenedFile: () => {},
            generationRef: { current: 0 },
            openPreviewPane: () => {},
        }));

        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'main.tex' });

        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        const file = openWorkspaceFile.mock.calls[0][0];
        expect(file.content).toBe('');
        expect(file.latexReadError).toBe('无法读取论文源文件');
        expect(file.latexReadError).not.toContain('cloud-workspaces');
        expect(file.absPath).toBeUndefined();
    });

    it('clears the parked copy once the live listener has handled it', async () => {
        // A dispatch is parked as well as broadcast. If the parked copy survived,
        // a panel that mounts later would apply the same request a second time
        // and reset the editor buffer.
        resetLatexDocumentRequestForTests();
        const preview = vi.fn().mockResolvedValue({ content: '\\documentclass{article}\n' });
        vi.mocked(getWailsAppModule).mockResolvedValue({ GetCodingWorkbenchFilePreview: preview } as never);
        const openWorkspaceFile = vi.fn();
        renderHook(() => useAssistantPreviewOpenEvents({
            allowed: true,
            sessionKey: 'tab-a',
            lang: 'zh-Hans',
            openWorkspaceFile,
            focusOpenedFile: () => {},
            generationRef: { current: 0 },
            openPreviewPane: () => {},
        }));

        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'main.tex' });

        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(preview).toHaveBeenCalledTimes(1);
        // Nothing is left parked, so no second panel can re-apply it.
        expect(takePendingLatexDocument()).toBeNull();
    });

    it('reads the source again after the assistant tab changes', async () => {
        const resolvers: Array<(value: { content: string }) => void> = [];
        const preview = vi.fn().mockImplementation(() => new Promise((resolve) => { resolvers.push(resolve); }));
        vi.mocked(getWailsAppModule).mockResolvedValue({ GetCodingWorkbenchFilePreview: preview } as never);
        const openWorkspaceFile = vi.fn();
        const generationRef = { current: 0 };
        const view = renderHook(
            ({ sessionKey }) => useAssistantPreviewOpenEvents({
                allowed: true,
                lang: 'zh-Hans',
                openWorkspaceFile,
                focusOpenedFile: () => {},
                generationRef,
                openPreviewPane: () => {},
                sessionKey,
            }),
            { initialProps: { sessionKey: 'tab-a' } },
        );

        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'main.tex' });
        await waitFor(() => expect(preview).toHaveBeenCalledTimes(1));
        generationRef.current += 1;
        view.rerender({ sessionKey: 'tab-b' });
        await waitFor(() => expect(preview).toHaveBeenCalledTimes(2));
        resolvers[0]({ content: 'stale-template' });
        resolvers[1]({ content: '\\documentclass{article}\n' });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(openWorkspaceFile.mock.calls[0][0].content).toContain('\\documentclass{article}');
        expect(openWorkspaceFile.mock.calls[0][0].content).not.toContain('stale-template');
    });
});

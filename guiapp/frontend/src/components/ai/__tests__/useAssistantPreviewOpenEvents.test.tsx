import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { dispatchOpenLatexDocument, resetLatexDocumentRequestForTests, takePendingLatexDocument } from '../latexDocumentOpen';
import { dispatchPreviewTaskResult } from '../taskResultPreview';
import { latexPreviewSessionAction, useAssistantPreviewOpenEvents } from '../useAssistantPreviewOpenEvents';
import { getWailsAppModule } from '../../../utils/wailsAppModule';

vi.mock('../../../utils/wailsAppModule', () => ({
    getWailsAppModule: vi.fn(),
}));

describe('latexPreviewSessionAction', () => {
    const paper = {
        parked: false,
        hasDocument: true,
        ownerSession: 'tab-latex',
        openedForSession: 'tab-latex',
    };

    it('restores only when the owning session is active again', () => {
        expect(latexPreviewSessionAction({
            ...paper,
            previousSession: 'tab-weather',
            sessionKey: 'tab-latex',
            openedForSession: undefined,
        })).toBe('restore');
        expect(latexPreviewSessionAction({
            ...paper,
            previousSession: 'tab-latex',
            sessionKey: 'tab-weather',
        })).toBe('cancel-read');
        expect(latexPreviewSessionAction({
            ...paper,
            previousSession: 'tab-weather',
            sessionKey: 'tab-other',
            openedForSession: undefined,
        })).toBe('keep');
    });

    it('does not open a parked paper twice in the same session', () => {
        expect(latexPreviewSessionAction({
            ...paper,
            parked: true,
            previousSession: 'tab-latex',
            sessionKey: 'tab-latex',
        })).toBe('open-parked');
        expect(latexPreviewSessionAction({
            ...paper,
            previousSession: 'tab-latex',
            sessionKey: 'tab-latex',
        })).toBe('keep');
    });
});

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

    it('does not carry the previous LaTeX document into another task', async () => {
        const resolvers: Array<(value: { content: string }) => void> = [];
        const preview = vi.fn().mockImplementation(() => new Promise((resolve) => { resolvers.push(resolve); }));
        vi.mocked(getWailsAppModule).mockResolvedValue({ GetCodingWorkbenchFilePreview: preview } as never);
        const openWorkspaceFile = vi.fn();
        const openPreviewPane = vi.fn();
        const generationRef = { current: 0 };
        const view = renderHook(
            ({ sessionKey }) => useAssistantPreviewOpenEvents({
                allowed: true,
                lang: 'zh-Hans',
                openWorkspaceFile,
                focusOpenedFile: () => {},
                generationRef,
                openPreviewPane,
                sessionKey,
            }),
            { initialProps: { sessionKey: 'tab-latex' } },
        );

        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'elsarticle/elsarticle-template-num.tex' });
        await waitFor(() => expect(preview).toHaveBeenCalledTimes(1));
        const paneOpensBeforeSwitch = openPreviewPane.mock.calls.length;
        view.rerender({ sessionKey: 'tab-weather' });
        resolvers[0]({ content: '\\documentclass{elsarticle}\n' });
        await Promise.resolve();
        expect(preview).toHaveBeenCalledTimes(1);
        expect(openWorkspaceFile).not.toHaveBeenCalled();
        expect(openPreviewPane).toHaveBeenCalledTimes(paneOpensBeforeSwitch);
    });

    it('restores the LaTeX document when returning to the task that owns it', async () => {
        vi.mocked(getWailsAppModule).mockResolvedValue({} as never);
        const openWorkspaceFile = vi.fn();
        const openPreviewPane = vi.fn();
        const view = renderHook(
            ({ sessionKey }) => useAssistantPreviewOpenEvents({
                allowed: true,
                lang: 'zh-Hans',
                openWorkspaceFile,
                focusOpenedFile: () => {},
                generationRef: { current: 0 },
                openPreviewPane,
                sessionKey,
            }),
            { initialProps: { sessionKey: 'tab-latex' } },
        );

        dispatchOpenLatexDocument({
            projectPath: 'D:/tasks/latex',
            relativePath: 'elsarticle/elsarticle-template-num.tex',
            content: '\\documentclass{elsarticle}',
        });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));

        view.rerender({ sessionKey: 'tab-weather' });
        expect(openWorkspaceFile).toHaveBeenCalledTimes(1);

        view.rerender({ sessionKey: 'tab-latex' });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(2));
        expect(openWorkspaceFile.mock.calls[1][0].filePath).toBe('elsarticle/elsarticle-template-num.tex');
        expect(openWorkspaceFile.mock.calls[1][0].projectPath).toBe('D:/tasks/latex');
        expect(openPreviewPane).toHaveBeenLastCalledWith(true);
    });

    it('closes the pane when the current task refuses a result file', async () => {
        const openWorkspaceFile = vi.fn().mockReturnValue(false);
        const openPreviewPane = vi.fn();
        renderHook(() => useAssistantPreviewOpenEvents({
            allowed: true,
            sessionKey: 'tab-weather',
            lang: 'zh-Hans',
            openWorkspaceFile,
            focusOpenedFile: () => {},
            generationRef: { current: 0 },
            openPreviewPane,
        }));

        dispatchPreviewTaskResult('D:/tasks/latex/paper.pdf');
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(openPreviewPane).toHaveBeenLastCalledWith(false);
    });

    it('does not adopt a paper the current task refused', async () => {
        vi.mocked(getWailsAppModule).mockResolvedValue({} as never);
        const openWorkspaceFile = vi.fn().mockReturnValue(false);
        const openPreviewPane = vi.fn();
        const view = renderHook(
            ({ sessionKey }) => useAssistantPreviewOpenEvents({
                allowed: true,
                lang: 'zh-Hans',
                openWorkspaceFile,
                focusOpenedFile: () => {},
                generationRef: { current: 0 },
                openPreviewPane,
                sessionKey,
            }),
            { initialProps: { sessionKey: 'tab-weather' } },
        );

        dispatchOpenLatexDocument({
            projectPath: 'D:/tasks/latex',
            relativePath: 'main.tex',
            content: '\\documentclass{article}',
        });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        expect(openPreviewPane).toHaveBeenLastCalledWith(false);

        view.rerender({ sessionKey: 'tab-other' });
        view.rerender({ sessionKey: 'tab-weather' });
        expect(openWorkspaceFile).toHaveBeenCalledTimes(1);
        expect(openPreviewPane).toHaveBeenLastCalledWith(false);
    });

    it('does not cancel a later task switch that does not own the paper', async () => {
        vi.mocked(getWailsAppModule).mockResolvedValue({} as never);
        const generationRef = { current: 0 };
        const view = renderHook(
            ({ sessionKey }) => useAssistantPreviewOpenEvents({
                allowed: true,
                lang: 'zh-Hans',
                openWorkspaceFile: () => {},
                focusOpenedFile: () => {},
                generationRef,
                openPreviewPane: () => {},
                sessionKey,
            }),
            { initialProps: { sessionKey: 'tab-latex' } },
        );

        dispatchOpenLatexDocument({
            projectPath: 'D:/tasks/latex',
            relativePath: 'main.tex',
            content: '\\documentclass{article}',
        });
        await waitFor(() => expect(generationRef.current).toBe(1));
        view.rerender({ sessionKey: 'tab-weather' });
        expect(generationRef.current).toBe(2);
        view.rerender({ sessionKey: 'tab-other' });
        expect(generationRef.current).toBe(2);
    });

    it('restores the paper after a stop on a tab that cannot preview', async () => {
        vi.mocked(getWailsAppModule).mockResolvedValue({} as never);
        const openWorkspaceFile = vi.fn();
        const view = renderHook(
            ({ allowed, sessionKey }) => useAssistantPreviewOpenEvents({
                allowed,
                lang: 'zh-Hans',
                openWorkspaceFile,
                focusOpenedFile: () => {},
                generationRef: { current: 0 },
                openPreviewPane: () => {},
                sessionKey,
            }),
            { initialProps: { allowed: true, sessionKey: 'tab-latex' } },
        );

        dispatchOpenLatexDocument({
            projectPath: 'D:/tasks/latex',
            relativePath: 'main.tex',
            content: '\\documentclass{article}',
        });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(1));
        view.rerender({ allowed: false, sessionKey: 'tab-group' });
        expect(openWorkspaceFile).toHaveBeenCalledTimes(1);
        view.rerender({ allowed: true, sessionKey: 'tab-latex' });
        await waitFor(() => expect(openWorkspaceFile).toHaveBeenCalledTimes(2));
        expect(openWorkspaceFile.mock.calls[1][0].projectPath).toBe('D:/tasks/latex');
    });
});

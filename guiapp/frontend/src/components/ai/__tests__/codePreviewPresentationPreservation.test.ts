import { describe, expect, it } from 'vitest';
import { acceptTaskResultFileEvent, acceptWorkspacePreviewFile, applyFileUpdate, applyOpenWorkspaceFile, filterCodePreviewStateForProject, initialState, latexWorkbenchFromAgentFile, prepareLatexResultFile, withTaskResultMark, type CodePreviewUIState } from '../useCodePreviewState';
import { codePreviewHasTaskResult, shouldReopenTaskResultPreview } from '../assistantPreviewState';
import type { CodeFile } from '../useCodePreviewState';

/**
 * A `code:file_update` event describes content only. It must not be able to
 * change how an already-open document is presented.
 */
function openLatexWorkbench(state: CodePreviewUIState): CodePreviewUIState {
    return applyFileUpdate(state, {
        filePath: 'main.tex',
        fileName: 'main.tex',
        projectPath: 'D:/tasks/latex',
        content: '\\documentclass{article}\n\\begin{document}x\\end{document}',
        language: 'latex',
        opType: 'read',
        updatedAt: 1,
        latexWorkbench: true,
    });
}

/** What the backend emits when the LaTeX expert writes the file. */
function agentEdit(content: string): CodeFile {
    return {
        filePath: 'main.tex',
        fileName: 'main.tex',
        content,
        language: 'latex',
        opType: 'modify',
        updatedAt: 2,
    };
}

describe('applyFileUpdate presentation preservation', () => {
    it('keeps the LaTeX workbench after an agent edits the source', () => {
        const opened = openLatexWorkbench(initialState());
        const updated = applyFileUpdate(opened, agentEdit('\\documentclass{article}\n\\begin{document}edited\\end{document}'));
        const file = updated.files.get('main.tex');
        // Without this the editor and the compile button disappear on the agent's
        // first write, which is the normal interaction of the whole feature.
        expect(file?.latexWorkbench).toBe(true);
        expect(file?.projectPath).toBe('D:/tasks/latex');
        expect(file?.content).toContain('edited');
    });

    it('survives a burst of agent edits', () => {
        let state = openLatexWorkbench(initialState());
        for (let index = 0; index < 5; index += 1) {
            state = applyFileUpdate(state, agentEdit(`draft ${index}`));
            expect(state.files.get('main.tex')?.latexWorkbench).toBe(true);
        }
        expect(state.files.get('main.tex')?.content).toBe('draft 4');
    });

    it('opens the result pane for a new or rewritten task file', () => {
        const sessionID = 'local-tools:desktop-user:expert:builtin-paper-polish';
        for (const sample of [
            { opType: 'create' as const, filePath: 'reports/brief.md', fileName: 'brief.md' },
            { opType: 'modify' as const, filePath: 'reports/deck.pptx', fileName: 'deck.pptx' },
        ]) {
            expect(acceptTaskResultFileEvent({
                opType: sample.opType,
                forceOpen: true,
                eventProjectPath: 'F:/work',
                tabProjectPath: 'D:/tasks/office',
                sessionID,
                expertId: 'builtin-paper-polish',
                expertTab: true,
            })).toBe(true);
            const opened = applyFileUpdate(initialState(), {
                sessionID,
                filePath: sample.filePath,
                fileName: sample.fileName,
                absPath: `F:/work/${sample.filePath}`,
                projectPath: 'F:/work',
                content: 'result',
                language: 'plaintext',
                opType: sample.opType,
                updatedAt: 1,
                forceOpen: true,
            });
            expect(opened.active).toBe(true);
            const kept = filterCodePreviewStateForProject(opened, 'D:/tasks/office', undefined, false, false, true, 'builtin-paper-polish');
            expect(kept.files.has(sample.filePath)).toBe(true);
        }
        expect(acceptTaskResultFileEvent({
            opType: 'modify',
            forceOpen: true,
            eventProjectPath: 'F:/other-project',
            tabProjectPath: 'D:/tasks/office',
            sessionID: 'local-tools:desktop-user:project',
            expertId: 'builtin-paper-polish',
            expertTab: true,
        })).toBe(false);
        expect(acceptTaskResultFileEvent({
            opType: 'modify',
            forceOpen: true,
            eventProjectPath: 'F:/work',
            sessionID: 'local-tools:desktop-user:expert:builtin-paper-polish-extra',
            expertId: 'builtin-paper-polish',
            expertTab: true,
        })).toBe(false);
    });

    it('keeps a task result marked after a later read', () => {
        const written = withTaskResultMark({
            filePath: 'reports/brief.md',
            fileName: 'brief.md',
            content: 'result',
            language: 'markdown',
            opType: 'modify' as const,
            updatedAt: 2,
            forceOpen: true,
        }, true);
        expect((written as { taskResult?: boolean }).taskResult).toBe(true);
        expect((withTaskResultMark({ opType: 'read' as const }, true) as { taskResult?: boolean }).taskResult).toBeUndefined();
        expect((withTaskResultMark({ opType: 'modify' as const }, false) as { taskResult?: boolean }).taskResult).toBeUndefined();
        const opened = applyFileUpdate(initialState(), written);
        const reread = applyFileUpdate(opened, {
            filePath: 'reports/brief.md',
            fileName: 'brief.md',
            content: 'result',
            language: 'markdown',
            opType: 'read',
            updatedAt: 3,
        });
        expect(reread.files.get('reports/brief.md')?.taskResult).toBe(true);
        expect(codePreviewHasTaskResult(reread)).toBe(true);
        expect(codePreviewHasTaskResult(initialState())).toBe(false);
        expect(shouldReopenTaskResultPreview({ ...reread, active: false })).toBe(true);
        expect(shouldReopenTaskResultPreview(reread)).toBe(false);
    });

    it('opens the result pane for both a new tex and a rewritten tex', () => {
        for (const opType of ['create', 'modify'] as const) {
            const file = prepareLatexResultFile({
                filePath: 'elsarticle/paper.tex',
                fileName: 'paper.tex',
                absPath: 'F:/latex-test3/elsarticle/paper.tex',
                projectPath: 'D:/tasks/latex',
                content: '\\begin{document}paper\\end{document}',
                language: 'plaintext',
                opType,
                updatedAt: 1,
            }, 'D:/tasks/latex');
            expect(file.latexWorkbench).toBe(true);
            expect(file.forceOpen).toBe(true);
            expect(file.opType).toBe(opType);
            const read = prepareLatexResultFile({ ...file, opType: 'read', forceOpen: false }, 'D:/tasks/latex');
            expect(read.latexWorkbench).toBe(true);
            expect(read.forceOpen).toBe(false);
            expect(acceptWorkspacePreviewFile(file, 'D:/tasks/latex')).toBeNull();
            const adopted = applyOpenWorkspaceFile(initialState(), file);
            expect(adopted.active).toBe(true);
            expect(adopted.files.get('elsarticle/paper.tex')?.latexWorkbench).toBe(true);
            expect(file.projectPath).toBe('F:/latex-test3');
            const opened = applyFileUpdate(initialState(), file);
            expect(opened.active).toBe(true);
            expect(opened.files.get('elsarticle/paper.tex')?.latexWorkbench).toBe(true);
            const kept = filterCodePreviewStateForProject(opened, 'D:/tasks/latex', undefined, false, true);
            expect(kept.files.has('elsarticle/paper.tex')).toBe(true);
            expect(kept.active).toBe(true);
        }
    });

    it('adopts a rewrite of an exported template as the paper preview', () => {
        const rewrite = latexWorkbenchFromAgentFile({
            filePath: 'elsarticle/elsarticle-template-num.tex',
            fileName: 'elsarticle-template-num.tex',
            projectPath: 'D:/tasks/latex',
            content: '\\documentclass{elsarticle}\n\\begin{document}paper\\end{document}',
            language: 'latex',
            opType: 'modify',
            updatedAt: 2,
        }, '');
        expect(rewrite?.latexWorkbench).toBe(true);
        expect(rewrite?.projectPath).toBe('D:/tasks/latex');
        expect(latexWorkbenchFromAgentFile(rewrite!, '')).toBeNull();
        expect(latexWorkbenchFromAgentFile({
            filePath: 'notes.md',
            fileName: 'notes.md',
            content: 'hello',
            language: 'markdown',
            opType: 'modify',
            updatedAt: 2,
        })).toBeNull();
    });

    it('does not add a workbench flag to an ordinary file', () => {
        const opened = applyFileUpdate(initialState(), {
            filePath: 'notes.md',
            fileName: 'notes.md',
            content: 'hello',
            language: 'markdown',
            opType: 'read',
            updatedAt: 1,
        });
        const updated = applyFileUpdate(opened, {
            filePath: 'notes.md',
            fileName: 'notes.md',
            content: 'hello world',
            language: 'markdown',
            opType: 'modify',
            updatedAt: 2,
        });
        expect(updated.files.get('notes.md')?.latexWorkbench).toBeUndefined();
    });

    it('lets a successful agent write clear a previous read error', () => {
        // The lock exists to stop an unreadable buffer being saved. Once the
        // agent has actually written the file the content is known-good, so the
        // lock must not persist and freeze the editor.
        const locked = applyFileUpdate(openLatexWorkbench(initialState()), {
            filePath: 'main.tex',
            fileName: 'main.tex',
            projectPath: 'D:/tasks/latex',
            content: '',
            language: 'latex',
            opType: 'read',
            updatedAt: 2,
            latexWorkbench: true,
            latexReadError: '无法读取论文源文件',
        });
        expect(locked.files.get('main.tex')?.latexReadError).toBeTruthy();
        const written = applyFileUpdate(locked, agentEdit('recovered content'));
        expect(written.files.get('main.tex')?.latexReadError).toBeUndefined();
        expect(written.files.get('main.tex')?.latexWorkbench).toBe(true);
    });

    it('applies an absolute workspace write to the open paper', () => {
        const opened = openLatexWorkbench(initialState());
        const updated = applyFileUpdate(opened, {
            ...agentEdit('\\documentclass{article}\n\\begin{document}from disk\\end{document}'),
            filePath: 'D:/tasks/latex/workspace/main.tex',
            fileName: 'main.tex',
        });
        expect(updated.files.size).toBe(1);
        expect(updated.files.get('main.tex')?.content).toContain('from disk');
        expect(updated.files.get('main.tex')?.latexWorkbench).toBe(true);
        expect(updated.activeFilePath).toBe('main.tex');
        expect(updated.files.get('main.tex')?.projectPath).toBe('D:/tasks/latex');
    });

    it('keeps the task directory when the write is routed from the workspace folder', () => {
        const opened = openLatexWorkbench(initialState());
        const updated = applyFileUpdate(opened, {
            ...agentEdit('\\documentclass{article}\n\\begin{document}chapter\\end{document}'),
            filePath: 'D:/tasks/latex/workspace/main.tex',
            projectPath: 'D:/tasks/latex/workspace',
        });
        expect(updated.files.get('main.tex')?.projectPath).toBe('D:/tasks/latex');
        expect(updated.files.get('main.tex')?.absPath).toBeUndefined();
        expect(updated.files.get('main.tex')?.content).toContain('chapter');
    });

    it('keeps the paper editor when a tool write starts a new session', () => {
        const opened = applyFileUpdate(openLatexWorkbench(initialState()), {
            filePath: 'main.tex',
            fileName: 'main.tex',
            projectPath: 'D:/tasks/latex',
            content: '\\documentclass{article}',
            language: 'latex',
            opType: 'read',
            updatedAt: 2,
            latexWorkbench: true,
        });
        const lingering = { ...opened, sessionID: 'older-session', sessionActive: false };
        const updated = applyFileUpdate(lingering, {
            ...agentEdit('\\documentclass{article}\n\\begin{document}written\\end{document}'),
            filePath: 'D:/tasks/latex/workspace/main.tex',
            projectPath: 'D:/tasks/latex/workspace',
            absPath: 'D:/tasks/latex/workspace/main.tex',
            sessionID: 'local-tool:expert',
            forceOpen: true,
        });
        expect(updated.sessionID).toBe('local-tool:expert');
        expect(updated.files.get('main.tex')?.latexWorkbench).toBe(true);
        expect(updated.files.get('main.tex')?.absPath).toBeUndefined();
        expect(updated.files.get('main.tex')?.projectPath).toBe('D:/tasks/latex');
        expect(updated.files.get('main.tex')?.content).toContain('written');
    });

    it('keeps an expert write when the editor open finishes later', () => {
        const written = applyFileUpdate(initialState(), {
            ...agentEdit('\\documentclass{article}\n\\begin{document}written\\end{document}'),
            projectPath: 'D:/tasks/latex/workspace',
            absPath: 'D:/tasks/latex/workspace/main.tex',
            updatedAt: 50,
            forceOpen: true,
        });
        const opened = applyOpenWorkspaceFile(written, {
            filePath: 'main.tex',
            fileName: 'main.tex',
            projectPath: 'D:/tasks/latex',
            content: '\\documentclass{article}',
            language: 'latex',
            opType: 'read',
            updatedAt: 10,
            latexWorkbench: true,
        });
        const file = opened.files.get('main.tex');
        expect(file?.content).toContain('written');
        expect(file?.latexWorkbench).toBe(true);
        expect(file?.projectPath).toBe('D:/tasks/latex');
        expect(file?.absPath).toBeUndefined();
        expect(opened.active).toBe(true);
    });

    it('still opens a disk read that started after the expert write', () => {
        const written = applyFileUpdate(initialState(), {
            ...agentEdit('stale-write'),
            updatedAt: 10,
        });
        const opened = applyOpenWorkspaceFile(written, {
            filePath: 'main.tex',
            fileName: 'main.tex',
            projectPath: 'D:/tasks/latex',
            content: 'from-disk-later',
            language: 'latex',
            opType: 'read',
            updatedAt: 50,
            latexWorkbench: true,
        });
        expect(opened.files.get('main.tex')?.content).toBe('from-disk-later');
        expect(opened.files.get('main.tex')?.latexWorkbench).toBe(true);
    });

    it('drops a disk read that finished after the expert write', () => {
        const opened = openLatexWorkbench(initialState());
        const written = applyFileUpdate(opened, { ...agentEdit('written by the expert'), updatedAt: 50 });
        const stale = applyFileUpdate(written, {
            filePath: 'main.tex',
            fileName: 'main.tex',
            content: '\\documentclass{article}',
            language: 'latex',
            opType: 'read',
            updatedAt: 10,
            latexWorkbench: true,
        });
        expect(stale.files.get('main.tex')?.content).toBe('written by the expert');
    });
});

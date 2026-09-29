import { describe, expect, it } from 'vitest';
import { applyFileUpdate, applyOpenWorkspaceFile, initialState, type CodePreviewUIState } from '../useCodePreviewState';
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

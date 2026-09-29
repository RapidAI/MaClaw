import { describe, expect, it, vi } from 'vitest';
import {
    OPEN_LATEX_DOCUMENT_EVENT,
    dispatchOpenLatexDocument,
    openLatexDocumentFromEvent,
    resetLatexDocumentRequestForTests,
    takePendingLatexDocument,
} from '../latexDocumentOpen';

function captureNext() {
    const seen: unknown[] = [];
    const listener = (event: Event) => { seen.push((event as CustomEvent).detail); };
    window.addEventListener(OPEN_LATEX_DOCUMENT_EVENT, listener, { once: true });
    return seen;
}

describe('dispatchOpenLatexDocument', () => {
    it('fills in the fields the code-preview store needs', () => {
        const seen = captureNext();
        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'paper/main.tex' });
        expect(seen).toHaveLength(1);
        expect(seen[0]).toEqual({
            projectPath: 'D:/tasks/latex',
            relativePath: 'paper/main.tex',
            content: '',
            fileName: 'main.tex',
        });
    });

    it('keeps a supplied file name and preloaded content', () => {
        const seen = captureNext();
        dispatchOpenLatexDocument({ projectPath: 'D:/t', relativePath: 'a.tex', fileName: 'a.tex', content: '\\documentclass{article}' });
        expect(seen[0]).toMatchObject({ fileName: 'a.tex', content: '\\documentclass{article}' });
    });

    it('ignores incomplete or non-LaTeX requests', () => {
        // A non-LaTeX path would land in the plain code viewer with no editor and
        // no compile button, so it is refused at the boundary.
        const seen = captureNext();
        dispatchOpenLatexDocument({ projectPath: '', relativePath: 'main.tex' });
        dispatchOpenLatexDocument({ projectPath: 'D:/t', relativePath: '' });
        dispatchOpenLatexDocument({ projectPath: 'D:/t', relativePath: 'notes.md' });
        expect(seen).toHaveLength(0);
    });
});

describe('openLatexDocumentFromEvent', () => {
    it('returns null for an unrelated event', () => {
        const listener = vi.fn();
        window.addEventListener('maclaw:preview-task-result', listener, { once: true });
        window.dispatchEvent(new CustomEvent('maclaw:preview-task-result', { detail: { path: 'D:/x.pdf' } }));
        expect(openLatexDocumentFromEvent(new CustomEvent('maclaw:preview-task-result', { detail: { path: 'D:/x.pdf' } }))).toBeNull();
    });
});

describe('the parked request', () => {
    it('survives a dispatch that no listener has consumed yet', () => {
        // The launcher cannot know when the expert tab finished mounting, so the
        // request has to outlive the absence of a listener.
        resetLatexDocumentRequestForTests();
        captureNext();
        dispatchOpenLatexDocument({ projectPath: 'D:/tasks/latex', relativePath: 'main.tex' });
        expect(takePendingLatexDocument()).toEqual({
            projectPath: 'D:/tasks/latex',
            relativePath: 'main.tex',
            content: '',
            fileName: 'main.tex',
        });
    });

    it('is handed out once so two panels cannot both claim it', () => {
        resetLatexDocumentRequestForTests();
        captureNext();
        dispatchOpenLatexDocument({ projectPath: 'D:/t', relativePath: 'main.tex' });
        expect(takePendingLatexDocument()).not.toBeNull();
        expect(takePendingLatexDocument()).toBeNull();
    });

    it('is not parked for a rejected request', () => {
        resetLatexDocumentRequestForTests();
        captureNext();
        dispatchOpenLatexDocument({ projectPath: 'D:/t', relativePath: 'notes.md' });
        expect(takePendingLatexDocument()).toBeNull();
    });
});

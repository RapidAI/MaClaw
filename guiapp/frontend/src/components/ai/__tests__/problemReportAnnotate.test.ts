import { describe, expect, it } from 'vitest';
import { appendProblemReportScreenshotPaths, problemReportAnnotatedExports, problemReportPasteExtension, problemReportPastedImages, problemReportShotAdded, problemReportShotMounts, removeProblemReportScreenshotPath } from '../problemReportAnnotate';

describe('problemReportAnnotatedExports', () => {
    it('keeps an unmarked picture on the original file without clearing another mark', () => {
        const got = problemReportAnnotatedExports(['a.png', 'b.png'], {
            'a.png': { annotated: 'data:image/png;base64,aaa', failed: false },
            'b.png': { annotated: '', failed: false },
        });
        expect(got).toEqual({ annotated: ['data:image/png;base64,aaa', ''] });
    });

    it('uses the original file when that picture has no editor yet', () => {
        const got = problemReportAnnotatedExports(['a.png', 'b.png'], {
            'a.png': { annotated: 'data:image/png;base64,aaa', failed: false },
        });
        expect(got).toEqual({ annotated: ['data:image/png;base64,aaa', ''] });
    });

    it('stops on the picture that could not be flattened', () => {
        expect(problemReportAnnotatedExports(['a.png', 'b.png'], {
            'a.png': { annotated: '', failed: true },
            'b.png': { annotated: 'data:image/png;base64,bbb', failed: false },
        })).toEqual({ error: 'export', path: 'a.png' });
    });
});

describe('appendProblemReportScreenshotPaths', () => {
    it('selects the picture just added and ignores a duplicate', () => {
        const next = appendProblemReportScreenshotPaths(['a.png'], ['b.png', 'a.png', '']);
        expect(next).toEqual(['a.png', 'b.png']);
        expect(problemReportShotAdded(['a.png'], next)).toBe('b.png');
        expect(problemReportShotAdded(['a.png'], appendProblemReportScreenshotPaths(['a.png'], ['a.png']))).toBe('');
    });

    it('stops at 12 pictures', () => {
        const previous = Array.from({ length: 12 }, (_, index) => `p${index}.png`);
        expect(appendProblemReportScreenshotPaths(previous, ['extra.png'])).toEqual(previous);
    });
});

describe('removeProblemReportScreenshotPath', () => {
    it('removes the named picture after an earlier remove has shifted the indexes', () => {
        const afterFirst = removeProblemReportScreenshotPath(['a.png', 'b.png', 'c.png'], 'a.png');
        expect(afterFirst).toEqual(['b.png', 'c.png']);
        expect(removeProblemReportScreenshotPath(afterFirst, 'b.png')).toEqual(['c.png']);
    });
});

describe('problemReportPastedImages', () => {
    it('reads a file-list-only screenshot and keeps one copy when both lists have it', () => {
        const shot = new File(['png'], 'shot.png', { type: 'image/png' });
        expect(problemReportPastedImages({ items: null, files: [shot] })).toEqual([shot]);
        const fromItem = new File(['png'], 'image.png', { type: 'image/png' });
        const fromList = new File(['png'], 'blob', { type: 'image/png' });
        expect(problemReportPastedImages({
            items: [{ kind: 'file', getAsFile: () => fromItem }],
            files: [fromList],
        })).toEqual([fromItem]);
    });

    it('ignores text and still accepts an image whose item type is empty', () => {
        expect(problemReportPastedImages({
            items: [{ kind: 'string', getAsFile: () => null }],
            files: [],
        })).toEqual([]);
        expect(problemReportPastedImages({
            items: null,
            files: [new File(['x'], 'notes.pdf', { type: 'application/pdf' })],
        })).toEqual([]);
        const file = new File(['png'], 'from-explorer.png', { type: '' });
        expect(problemReportPastedImages({
            items: [{ kind: 'file', getAsFile: () => file }],
            files: [],
        })).toEqual([file]);
        const jpeg = new File(['jpeg'], 'photo.jpeg', { type: '' });
        expect(problemReportPastedImages({ items: null, files: [jpeg] })).toEqual([jpeg]);
        expect(problemReportPastedImages({
            items: null,
            files: [
                new File(['gif'], 'anim.gif', { type: '' }),
                new File(['gif'], 'anim.gif', { type: 'image/gif' }),
            ],
        })).toEqual([]);
    });
});

describe('problemReportPasteExtension', () => {
    it('uses a known MIME type, and the filename when the type is empty', () => {
        expect(problemReportPasteExtension({ type: 'image/jpeg', name: 'photo.png' })).toBe('jpg');
        expect(problemReportPasteExtension({ type: 'image/png', name: 'photo.jpg' })).toBe('png');
        expect(problemReportPasteExtension({ type: '', name: 'photo.jpeg' })).toBe('jpg');
        expect(problemReportPasteExtension({ type: '', name: 'from-explorer.png' })).toBe('png');
        expect(problemReportPasteExtension({ type: 'image/gif', name: 'anim.gif' })).toBe('png');
    });
});

describe('problemReportShotMounts', () => {
    it('keeps a picture mounted after the user switches away and back', () => {
        const first = problemReportShotMounts(['a.png', 'b.png'], 'a.png', []);
        expect(first.mounts).toEqual(['a.png']);
        const second = problemReportShotMounts(['a.png', 'b.png'], 'b.png', first.seen);
        expect(second.mounts).toEqual(['a.png', 'b.png']);
        const back = problemReportShotMounts(['a.png', 'b.png'], 'a.png', second.seen);
        expect(back.mounts).toEqual(['a.png', 'b.png']);
    });

    it('drops a picture that was removed', () => {
        const next = problemReportShotMounts(['b.png'], 'b.png', ['a.png', 'b.png']);
        expect(next.mounts).toEqual(['b.png']);
        expect(next.seen).toEqual(['b.png']);
    });
});

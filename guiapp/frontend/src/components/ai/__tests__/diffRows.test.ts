import { describe, expect, it } from 'vitest';
import {
    buildDiffHunks,
    buildDiffRows,
    computeDiff,
    computeDiffRowStats,
    computeWordSegments,
    lineSimilarity,
} from '../diffCompute';
import { minimapRowFromDiffRow } from '../CodePreviewDiffView';

function rowsOf(original: string, modified: string) {
    const lines = computeDiff(original, modified);
    expect(lines).not.toBeNull();
    return buildDiffRows(lines!);
}

describe('diffCompute — change-focused rows', () => {
    it('pairs a rewritten line into one modify row', () => {
        const rows = rowsOf('const alpha = 0;\nconst beta = 2;', 'const alpha = 1;\nconst beta = 2;');
        const kinds = rows.map((r) => r.kind);
        expect(kinds).toEqual(['modify', 'unchanged']);
        const mod = rows[0];
        expect(mod.oldLineNum).toBe(1);
        expect(mod.newLineNum).toBe(1);
        expect(mod.oldText).toBe('const alpha = 0;');
        expect(mod.newText).toBe('const alpha = 1;');
    });

    it('marks only the rewritten token inside a modify row', () => {
        const marks = computeWordSegments('const alpha = 0;', 'const alpha = 1;');
        const changedOld = marks.old.filter((s) => s.changed).map((s) => s.text);
        const changedNew = marks.new.filter((s) => s.changed).map((s) => s.text);
        expect(changedOld.join('')).toBe('0');
        expect(changedNew.join('')).toBe('1');
    });

    it('keeps pure additions and deletions unpaired', () => {
        const rows = rowsOf('a\nb', 'a\nb\nc\nd');
        expect(rows.map((r) => r.kind)).toEqual(['unchanged', 'unchanged', 'add', 'add']);
    });

    it('does not pair unrelated lines', () => {
        const rows = rowsOf('aaaaaaaaaaaa', 'zzzzzzzzzzzz');
        expect(rows.map((r) => r.kind)).toEqual(['delete', 'add']);
        expect(lineSimilarity('aaaaaaaaaaaa', 'aaaaaaaaaaab')).toBeGreaterThan(0.5);
    });

    it('counts added / removed / modified', () => {
        const rows = rowsOf('x = 0\ny = 1\nz = 2', 'x = 9\ny = 1');
        expect(computeDiffRowStats(rows)).toEqual({ added: 1, removed: 2, modified: 1 });
    });

    it('groups changes into hunks with context', () => {
        const original = Array.from({ length: 20 }, (_, i) => `line ${i}`).join('\n');
        const modified = original.replace('line 10', 'line TEN');
        const rows = rowsOf(original, modified);
        const hunks = buildDiffHunks(rows, 3);
        expect(hunks).toHaveLength(1);
        expect(hunks[0].start).toBe(7);
        expect(hunks[0].end).toBe(14);
        expect(hunks[0].oldStart).toBe(8);
        expect(hunks[0].newStart).toBe(8);
        expect(hunks[0].oldCount).toBe(7);
        expect(hunks[0].newCount).toBe(7);
    });

    it('returns no hunks for identical content', () => {
        const rows = rowsOf('a\nb', 'a\nb');
        expect(buildDiffHunks(rows, 3)).toHaveLength(0);
    });

    it('anchors a pure-insert hunk to the surrounding old position', () => {
        const rows = rowsOf('a\nb', 'a\nb\nc');
        expect(buildDiffHunks(rows, 0)[0]).toMatchObject({
            oldStart: 2,
            oldCount: 0,
            newStart: 3,
            newCount: 1,
        });
    });

    it('anchors a pure-delete hunk to the surrounding new position', () => {
        const rows = rowsOf('a\nb\nc', 'a\nb');
        expect(buildDiffHunks(rows, 0)[0]).toMatchObject({
            oldStart: 3,
            oldCount: 1,
            newStart: 2,
            newCount: 0,
        });
    });

    it('ignores a gained or lost trailing newline (no phantom blank row)', () => {
        expect(rowsOf('const a = 1;', 'const a = 1;\n').map((r) => r.kind)).toEqual(['unchanged']);
        expect(rowsOf('const a = 1;\n', 'const a = 1;').map((r) => r.kind)).toEqual(['unchanged']);
    });

    it('still reports a real blank line added at the end', () => {
        // 'a\nb' -> 'a\nb\n\n': one blank line is real, the extra newline is not.
        const rows = rowsOf('const a = 1;\nconst b = 2;', 'const a = 1;\nconst b = 2;\n\n');
        expect(rows.map((r) => r.kind)).toEqual(['unchanged', 'unchanged', 'add']);
    });

    it('maps rendered rows onto the minimap row model', () => {
        const rows = rowsOf('x = 0\ny = 1\nz = 2', 'x = 9\ny = 1');
        expect(minimapRowFromDiffRow(rows[0])).toEqual({ type: 'add', content: 'x = 9', newLineNum: 1 });
        expect(minimapRowFromDiffRow(rows[1])).toEqual({ type: 'unchanged', content: 'y = 1', oldLineNum: 2, newLineNum: 2 });
        expect(minimapRowFromDiffRow(rows[2])).toEqual({ type: 'delete', content: 'z = 2', oldLineNum: 3 });
    });

    it('diffs a brand-new large file (trivial edit script skips the size guard)', () => {
        const big: string[] = [];
        for (let i = 0; i < 4000; i++) big.push(`export const v${i} = ${i};`);
        const lines = computeDiff('', big.join('\n'));
        expect(lines).not.toBeNull();
        expect(lines).toHaveLength(4000);
        expect(computeDiffRowStats(buildDiffRows(lines!))).toEqual({ added: 4000, removed: 0, modified: 0 });
    });

    it('diffs a large emptied file without the size guard', () => {
        const big: string[] = [];
        for (let i = 0; i < 4000; i++) big.push(`export const v${i} = ${i};`);
        const lines = computeDiff(big.join('\n'), '');
        expect(lines).not.toBeNull();
        expect(computeDiffRowStats(buildDiffRows(lines!))).toEqual({ added: 0, removed: 4000, modified: 0 });
    });

    it('still refuses a real two-sided diff above the size guard', () => {
        const big: string[] = [];
        for (let i = 0; i < 3000; i++) big.push(`export const v${i} = ${i};`);
        const text = big.join('\n');
        expect(computeDiff(text, text.replace('v0 = 0', 'v0 = 9'))).toBeNull();
    });
});

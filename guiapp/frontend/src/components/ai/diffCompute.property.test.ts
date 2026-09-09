/**
 * Property-based tests for diffCompute module.
 *
 * Uses fast-check with minimum 100 iterations per property.
 *
 * Properties tested:
 *   Property 7: Diff computation round-trip correctness
 *   Property 9: Diff dual line number correctness
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { computeDiff, type DiffLine } from './diffCompute';

// ── Generators ──

/**
 * Generate a single line of text (no newline characters).
 * Uses a restricted character set to avoid generating \n within lines.
 */
const arbLine = fc
    .array(
        fc.constantFrom(
            'a', 'b', 'c', 'd', 'e', 'f', 'g', ' ', '1', '2', '3',
            'x', 'y', 'z', '{', '}', '(', ')', '=', ';', '.', '!',
        ),
        { minLength: 0, maxLength: 30 },
    )
    .map(chars => chars.join(''));

/**
 * Generate a text string as lines joined by '\n'.
 * Each individual line is guaranteed to not contain '\n'.
 * An empty array produces '' (empty text).
 */
const arbTextLines = fc
    .array(arbLine, { minLength: 0, maxLength: 20 })
    .map(lines => lines.join('\n'));

// ── Helper functions ──

/** Reconstruct text from diff lines by filtering specific types. */
function reconstructFromDiff(
    diff: DiffLine[],
    includeTypes: Set<DiffLine['type']>,
): string {
    const lines = diff
        .filter(d => includeTypes.has(d.type))
        .map(d => d.content);
    return lines.join('\n');
}

// ── Property Tests ──

describe('diffCompute — Property Tests', () => {

    /**
     * **Validates: Requirements 4.1**
     *
     * Property 7: Diff computation round-trip correctness
     *
     * For any pair of text strings (original, modified), applying the diff:
     * - Taking all `unchanged` and `add` lines in order SHALL reconstruct
     *   the modified text
     * - Taking all `unchanged` and `delete` lines in order SHALL reconstruct
     *   the original text
     */
    it('Property 7: Diff round-trip correctness — modified reconstruction', () => {
        fc.assert(
            fc.property(
                arbTextLines,
                arbTextLines,
                (original, modified) => {
                    const diff = computeDiff(original, modified);
                    if (diff === null) return; // input too large — skip

                    // Reconstruct modified from unchanged + add lines
                    const reconstructed = reconstructFromDiff(
                        diff,
                        new Set(['unchanged', 'add']),
                    );

                    expect(reconstructed).toBe(modified);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 7: Diff round-trip correctness — original reconstruction', () => {
        fc.assert(
            fc.property(
                arbTextLines,
                arbTextLines,
                (original, modified) => {
                    const diff = computeDiff(original, modified);
                    if (diff === null) return; // input too large — skip

                    // Reconstruct original from unchanged + delete lines
                    const reconstructed = reconstructFromDiff(
                        diff,
                        new Set(['unchanged', 'delete']),
                    );

                    expect(reconstructed).toBe(original);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 4.7**
     *
     * Property 9: Diff dual line number correctness
     *
     * - `oldLineNum` values on `unchanged` and `delete` lines SHALL form
     *   a monotonically increasing sequence starting from 1
     * - `newLineNum` values on `unchanged` and `add` lines SHALL form
     *   a monotonically increasing sequence starting from 1
     */
    it('Property 9: Diff dual line number correctness — oldLineNum monotonicity', () => {
        fc.assert(
            fc.property(
                arbTextLines,
                arbTextLines,
                (original, modified) => {
                    const diff = computeDiff(original, modified);
                    if (diff === null) return; // input too large — skip

                    // Collect oldLineNum from unchanged + delete lines
                    const oldNums = diff
                        .filter(d => d.type === 'unchanged' || d.type === 'delete')
                        .map(d => d.oldLineNum!);

                    if (oldNums.length === 0) return;

                    // Must start from 1
                    expect(oldNums[0]).toBe(1);

                    // Must be strictly monotonically increasing
                    for (let i = 1; i < oldNums.length; i++) {
                        expect(oldNums[i]).toBe(oldNums[i - 1] + 1);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 9: Diff dual line number correctness — newLineNum monotonicity', () => {
        fc.assert(
            fc.property(
                arbTextLines,
                arbTextLines,
                (original, modified) => {
                    const diff = computeDiff(original, modified);
                    if (diff === null) return; // input too large — skip

                    // Collect newLineNum from unchanged + add lines
                    const newNums = diff
                        .filter(d => d.type === 'unchanged' || d.type === 'add')
                        .map(d => d.newLineNum!);

                    if (newNums.length === 0) return;

                    // Must start from 1
                    expect(newNums[0]).toBe(1);

                    // Must be strictly monotonically increasing
                    for (let i = 1; i < newNums.length; i++) {
                        expect(newNums[i]).toBe(newNums[i - 1] + 1);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });

    // ── Edge case unit tests ──

    it('empty original → all adds', () => {
        const diff = computeDiff('', 'line1\nline2\nline3');
        expect(diff).not.toBeNull();
        expect(diff!).toHaveLength(3);
        expect(diff!.every(d => d.type === 'add')).toBe(true);
        expect(diff!.map(d => d.newLineNum)).toEqual([1, 2, 3]);
        expect(diff!.every(d => d.oldLineNum === undefined)).toBe(true);
        expect(diff!.map(d => d.content)).toEqual(['line1', 'line2', 'line3']);
    });

    it('empty modified → all deletes', () => {
        const diff = computeDiff('line1\nline2\nline3', '');
        expect(diff).not.toBeNull();
        expect(diff!).toHaveLength(3);
        expect(diff!.every(d => d.type === 'delete')).toBe(true);
        expect(diff!.map(d => d.oldLineNum)).toEqual([1, 2, 3]);
        expect(diff!.every(d => d.newLineNum === undefined)).toBe(true);
        expect(diff!.map(d => d.content)).toEqual(['line1', 'line2', 'line3']);
    });

    it('identical content → all unchanged', () => {
        const text = 'line1\nline2\nline3';
        const diff = computeDiff(text, text);
        expect(diff).not.toBeNull();
        expect(diff!).toHaveLength(3);
        expect(diff!.every(d => d.type === 'unchanged')).toBe(true);
        expect(diff!.map(d => d.oldLineNum)).toEqual([1, 2, 3]);
        expect(diff!.map(d => d.newLineNum)).toEqual([1, 2, 3]);
    });

    it('both empty → empty diff', () => {
        const diff = computeDiff('', '');
        expect(diff).toHaveLength(0);
    });
});

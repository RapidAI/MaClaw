/**
 * Property-based tests for FileTabBar pure helper functions.
 *
 * Tests the exported pure functions directly (no React rendering needed).
 * Uses fast-check with minimum 100 iterations per property.
 *
 * Properties tested:
 *   Property 8: Tab indicator matches operation type
 *   Property 4: File name extraction from path
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { getOpTypeIndicator, extractFileName } from './FileTabBar';

// ── Generators ──

/** Generate a valid path segment (no separators). */
const arbPathSegment = fc
    .array(
        fc.constantFrom(
            'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
            'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
            'A', 'B', 'C', 'D', 'E', 'F', 'G',
            '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
            '_', '-', '.',
        ),
        { minLength: 1, maxLength: 15 },
    )
    .map(chars => chars.join(''));

/** Generate a Unix-style file path (e.g. /src/components/App.tsx). */
const arbUnixPath = fc
    .tuple(
        fc.array(arbPathSegment, { minLength: 1, maxLength: 5 }),
        arbPathSegment,
    )
    .map(([dirs, file]) => '/' + [...dirs, file].join('/'));

/** Generate a Windows-style file path (e.g. C:\Users\src\App.tsx). */
const arbWindowsPath = fc
    .tuple(
        fc.constantFrom('C', 'D', 'E'),
        fc.array(arbPathSegment, { minLength: 1, maxLength: 5 }),
        arbPathSegment,
    )
    .map(([drive, dirs, file]) => `${drive}:\\${[...dirs, file].join('\\')}`);

/** Generate a bare file name (no separators). */
const arbBareFileName = arbPathSegment;

/** Generate any file path: Unix, Windows, or bare name. */
const arbFilePath = fc.oneof(arbUnixPath, arbWindowsPath, arbBareFileName);

/** Generate an opType value. */
const arbOpType = fc.constantFrom('create' as const, 'modify' as const, 'read' as const);

// ── Property Tests ──

describe('FileTabBar — Property Tests', () => {

    /**
     * **Validates: Requirements 4.6**
     *
     * Property 8: Tab indicator matches operation type
     *
     * For any file in the files map, the tab visual indicator SHALL reflect
     * the file's opType: 'modify' -> MOD, 'create' -> NEW, 'read' -> READ.
     * Pure function of opType.
     */
    it('Property 8: Tab indicator matches operation type', () => {
        fc.assert(
            fc.property(
                arbOpType,
                (opType) => {
                    const indicator = getOpTypeIndicator(opType);

                    if (opType === 'modify') {
                        expect(indicator).toBe('MOD');
                    } else if (opType === 'read') {
                        expect(indicator).toBe('READ');
                    } else {
                        expect(indicator).toBe('NEW');
                    }

                    // Indicator is deterministic — same opType always gives same result
                    expect(getOpTypeIndicator(opType)).toBe(indicator);
                },
            ),
            { numRuns: 100 },
        );
    });

    /**
     * **Validates: Requirements 2.5**
     *
     * Property 4: File name extraction from path
     *
     * For any file path string (Unix or Windows format), the extracted file
     * name SHALL equal the last path segment after the final `/` or `\`
     * separator. For paths with no separator, the entire string is the file name.
     */
    it('Property 4: File name extraction from path (Unix paths)', () => {
        fc.assert(
            fc.property(
                arbUnixPath,
                (filePath) => {
                    const result = extractFileName(filePath);
                    const segments = filePath.split('/');
                    const expected = segments[segments.length - 1];

                    expect(result).toBe(expected);
                    // Result should not contain any separators
                    expect(result).not.toContain('/');
                    expect(result).not.toContain('\\');
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 4: File name extraction from path (Windows paths)', () => {
        fc.assert(
            fc.property(
                arbWindowsPath,
                (filePath) => {
                    const result = extractFileName(filePath);
                    const segments = filePath.split('\\');
                    const expected = segments[segments.length - 1];

                    expect(result).toBe(expected);
                    // Result should not contain any separators
                    expect(result).not.toContain('/');
                    expect(result).not.toContain('\\');
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 4: File name extraction from path (bare file names)', () => {
        fc.assert(
            fc.property(
                arbBareFileName,
                (fileName) => {
                    const result = extractFileName(fileName);
                    // Bare file name (no separators) should return the entire string
                    expect(result).toBe(fileName);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 4: File name extraction from path (mixed separators)', () => {
        fc.assert(
            fc.property(
                arbFilePath,
                (filePath) => {
                    const result = extractFileName(filePath);

                    // The result should be the last segment after the final separator
                    const lastSlash = Math.max(
                        filePath.lastIndexOf('/'),
                        filePath.lastIndexOf('\\'),
                    );
                    if (lastSlash === -1) {
                        expect(result).toBe(filePath);
                    } else {
                        expect(result).toBe(filePath.substring(lastSlash + 1));
                    }
                },
            ),
            { numRuns: 100 },
        );
    });
});

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
import { getOpTypeIndicator, extractFileName, computeVisibleFilePaths, cycleFilePath, computeDropIndex, filterOpenFilePaths } from './FileTabBar';

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

    /**
     * Overflow visibility: active file stays visible; visible set size ≤ maxVisible;
     * relative order matches the original list.
     */
    it('computeVisibleFilePaths keeps active file and original order', () => {
        fc.assert(
            fc.property(
                fc.array(arbFilePath, { minLength: 1, maxLength: 20 }),
                fc.integer({ min: 1, max: 20 }),
                (rawPaths, maxVisible) => {
                    // Deduplicate while preserving first-seen order.
                    const filePaths = Array.from(new Set(rawPaths));
                    if (filePaths.length === 0) return true;

                    const activeIndex = Math.min(maxVisible, filePaths.length) - 1;
                    const activeFilePath = filePaths[Math.max(0, activeIndex % filePaths.length)];
                    const visible = computeVisibleFilePaths(filePaths, activeFilePath, maxVisible);

                    expect(visible.length).toBeLessThanOrEqual(Math.min(maxVisible, filePaths.length));
                    expect(visible.length).toBeGreaterThan(0);
                    expect(visible).toContain(activeFilePath);

                    // Relative order preserved.
                    let lastIndex = -1;
                    for (const path of visible) {
                        const idx = filePaths.indexOf(path);
                        expect(idx).toBeGreaterThan(lastIndex);
                        lastIndex = idx;
                    }
                    return true;
                },
            ),
            { numRuns: 100 },
        );
    });

    it('computeVisibleFilePaths returns full list when capacity is enough', () => {
        const paths = ['/a.ts', '/b.ts', '/c.ts'];
        expect(computeVisibleFilePaths(paths, '/b.ts', 10)).toEqual(paths);
        expect(computeVisibleFilePaths(paths, '/missing.ts', 2)).toEqual(['/a.ts', '/b.ts']);
    });

    it('cycleFilePath wraps around and handles empty lists', () => {
        expect(cycleFilePath([], '', 1)).toBeNull();
        expect(cycleFilePath(['/a.ts'], '/a.ts', 1)).toBe('/a.ts');
        expect(cycleFilePath(['/a.ts', '/b.ts', '/c.ts'], '/a.ts', 1)).toBe('/b.ts');
        expect(cycleFilePath(['/a.ts', '/b.ts', '/c.ts'], '/c.ts', 1)).toBe('/a.ts');
        expect(cycleFilePath(['/a.ts', '/b.ts', '/c.ts'], '/a.ts', -1)).toBe('/c.ts');
        expect(cycleFilePath(['/a.ts', '/b.ts', '/c.ts'], '/missing.ts', 1)).toBe('/b.ts');
    });

    it('computeDropIndex places the dragged tab before/after the target', () => {
        const paths = ['/a.ts', '/b.ts', '/c.ts', '/d.ts'];
        // Move a after c → index of c after removal is 1, after → 2 → [b,c,a,d]
        expect(computeDropIndex(paths, '/a.ts', '/c.ts', true)).toBe(2);
        // Move d before a → 0
        expect(computeDropIndex(paths, '/d.ts', '/a.ts', false)).toBe(0);
        // No-op same path
        expect(computeDropIndex(paths, '/b.ts', '/b.ts', true)).toBe(1);
    });

    it('filterOpenFilePaths matches file name and full path, empty query keeps all', () => {
        const paths = ['/src/components/App.tsx', '/src/utils/helpers.ts', 'D:\\work\\main.go'];
        expect(filterOpenFilePaths(paths, '')).toEqual(paths);
        expect(filterOpenFilePaths(paths, '  ')).toEqual(paths);
        expect(filterOpenFilePaths(paths, 'app')).toEqual(['/src/components/App.tsx']);
        expect(filterOpenFilePaths(paths, 'helpers')).toEqual(['/src/utils/helpers.ts']);
        expect(filterOpenFilePaths(paths, 'work')).toEqual(['D:\\work\\main.go']);
        expect(filterOpenFilePaths(paths, 'nope')).toEqual([]);
    });
});

/**
 * Property-based tests for syntaxHighlight module.
 *
 * Uses fast-check with minimum 100 iterations per property.
 *
 * Properties tested:
 *   Property 6: Language detection from file extension
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { detectLanguage } from './syntaxHighlight';

// ── Known extension → language mapping (ground truth) ──

const knownExtensions: Array<[string, string]> = [
    ['.go', 'go'],
    ['.ts', 'typescript'],
    ['.tsx', 'typescript'],
    ['.js', 'javascript'],
    ['.jsx', 'javascript'],
    ['.py', 'python'],
    ['.rs', 'rust'],
    ['.java', 'java'],
    ['.c', 'c'],
    ['.h', 'c'],
    ['.cpp', 'cpp'],
    ['.cc', 'cpp'],
    ['.hpp', 'cpp'],
    ['.html', 'html'],
    ['.htm', 'html'],
    ['.css', 'css'],
    ['.json', 'json'],
    ['.yaml', 'yaml'],
    ['.yml', 'yaml'],
    ['.md', 'markdown'],
    ['.sh', 'shell'],
    ['.bash', 'shell'],
];

// ── Generators ──

/** Generate a valid file basename (no path separators, no dots except extension). */
const arbBasename = fc
    .array(
        fc.constantFrom(
            'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k',
            'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v',
            'w', 'x', 'y', 'z', 'A', 'B', 'C', 'D', 'E', 'F', 'G',
            '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '_', '-',
        ),
        { minLength: 1, maxLength: 20 },
    )
    .map(chars => chars.join(''));

/** Generate a known extension (from the mapping table). */
const arbKnownExt = fc.constantFrom(...knownExtensions.map(([ext]) => ext));

/** Generate a known extension with its expected language. */
const arbKnownExtWithLang = fc.constantFrom(...knownExtensions);

/** Generate an unknown extension (not in the mapping table). */
const knownExtSet = new Set(knownExtensions.map(([ext]) => ext));
const arbUnknownExt = fc
    .array(fc.constantFrom('a', 'b', 'c', 'x', 'z', '1', '2', '3'), { minLength: 1, maxLength: 6 })
    .map(chars => `.${chars.join('')}`)
    .filter(ext => !knownExtSet.has(ext) && !knownExtSet.has(ext.toLowerCase()));

/** Generate a Unix-style directory prefix. */
const arbUnixDir = fc
    .array(arbBasename, { minLength: 0, maxLength: 3 })
    .map(parts => (parts.length > 0 ? parts.join('/') + '/' : ''));

/** Generate a Windows-style directory prefix. */
const arbWindowsDir = fc
    .array(arbBasename, { minLength: 0, maxLength: 3 })
    .map(parts => (parts.length > 0 ? parts.join('\\') + '\\' : ''));

// ── Property Tests ──

describe('syntaxHighlight — Property Tests', () => {

    /**
     * **Validates: Requirements 3.2**
     *
     * Property 6: Language detection from file extension
     *
     * For any file name with a known extension, detectLanguage() SHALL return
     * the correct language identifier. For unknown extensions, it SHALL return
     * "plaintext".
     */
    it('Property 6: Known extension → correct language (bare filename)', () => {
        fc.assert(
            fc.property(
                arbBasename,
                arbKnownExtWithLang,
                (basename, [ext, expectedLang]) => {
                    const fileName = basename + ext;
                    expect(detectLanguage(fileName)).toBe(expectedLang);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 6: Known extension → correct language (Unix path)', () => {
        fc.assert(
            fc.property(
                arbUnixDir,
                arbBasename,
                arbKnownExtWithLang,
                (dir, basename, [ext, expectedLang]) => {
                    const filePath = dir + basename + ext;
                    expect(detectLanguage(filePath)).toBe(expectedLang);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 6: Known extension → correct language (Windows path)', () => {
        fc.assert(
            fc.property(
                arbWindowsDir,
                arbBasename,
                arbKnownExtWithLang,
                (dir, basename, [ext, expectedLang]) => {
                    const filePath = dir + basename + ext;
                    expect(detectLanguage(filePath)).toBe(expectedLang);
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 6: Unknown extension → plaintext', () => {
        fc.assert(
            fc.property(
                arbBasename,
                arbUnknownExt,
                (basename, ext) => {
                    const fileName = basename + ext;
                    expect(detectLanguage(fileName)).toBe('plaintext');
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 6: No extension → plaintext', () => {
        fc.assert(
            fc.property(
                arbBasename,
                (basename) => {
                    // Basename has no dots (generator only produces alphanumeric + _ -)
                    expect(detectLanguage(basename)).toBe('plaintext');
                },
            ),
            { numRuns: 100 },
        );
    });

    it('Property 6: Empty filename → plaintext', () => {
        expect(detectLanguage('')).toBe('plaintext');
    });
});

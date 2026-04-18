/**
 * Unit tests for dark/light CodePreviewTheme constants.
 *
 * Verifies that:
 *   1. Every color property in the dark theme differs from the corresponding light theme property
 *   2. Both themes have all required properties defined (non-empty strings)
 *   3. Specific critical color differences (bg, text, diffAddBg, diffDeleteBg, etc.)
 */
import { describe, it, expect } from 'vitest';
import {
    darkCodePreviewTheme,
    lightCodePreviewTheme,
    type CodePreviewTheme,
} from './CodePreviewPanel';

// ── All theme property keys ──

const themeKeys: (keyof CodePreviewTheme)[] = [
    'bg',
    'text',
    'textMuted',
    'border',
    'lineNumBg',
    'lineNumText',
    'tabBg',
    'tabActiveBg',
    'tabActiveText',
    'tabHoverBg',
    'diffAddBg',
    'diffAddText',
    'diffDeleteBg',
    'diffDeleteText',
    'syntaxKeyword',
    'syntaxString',
    'syntaxComment',
    'syntaxNumber',
    'syntaxFunction',
    'syntaxType',
    'syntaxOperator',
];

// ── All properties defined (non-empty strings) ──

describe('Theme completeness', () => {
    it('darkCodePreviewTheme has all required properties as non-empty strings', () => {
        for (const key of themeKeys) {
            expect(darkCodePreviewTheme[key], `dark theme missing or empty: ${key}`).toBeTruthy();
            expect(typeof darkCodePreviewTheme[key], `dark theme ${key} should be string`).toBe('string');
            expect(darkCodePreviewTheme[key].length, `dark theme ${key} should be non-empty`).toBeGreaterThan(0);
        }
    });

    it('lightCodePreviewTheme has all required properties as non-empty strings', () => {
        for (const key of themeKeys) {
            expect(lightCodePreviewTheme[key], `light theme missing or empty: ${key}`).toBeTruthy();
            expect(typeof lightCodePreviewTheme[key], `light theme ${key} should be string`).toBe('string');
            expect(lightCodePreviewTheme[key].length, `light theme ${key} should be non-empty`).toBeGreaterThan(0);
        }
    });
});

// ── Every color property differs between dark and light ──

describe('Dark vs Light theme distinctness', () => {
    it('every color property in dark theme differs from the corresponding light theme property', () => {
        for (const key of themeKeys) {
            expect(
                darkCodePreviewTheme[key],
                `${key} should differ between dark and light themes`,
            ).not.toBe(lightCodePreviewTheme[key]);
        }
    });
});

// ── Critical color differences ──

describe('Critical color differences', () => {
    it('background colors are distinct', () => {
        expect(darkCodePreviewTheme.bg).not.toBe(lightCodePreviewTheme.bg);
    });

    it('text colors are distinct', () => {
        expect(darkCodePreviewTheme.text).not.toBe(lightCodePreviewTheme.text);
    });

    it('diff add background colors are distinct', () => {
        expect(darkCodePreviewTheme.diffAddBg).not.toBe(lightCodePreviewTheme.diffAddBg);
    });

    it('diff add text colors are distinct', () => {
        expect(darkCodePreviewTheme.diffAddText).not.toBe(lightCodePreviewTheme.diffAddText);
    });

    it('diff delete background colors are distinct', () => {
        expect(darkCodePreviewTheme.diffDeleteBg).not.toBe(lightCodePreviewTheme.diffDeleteBg);
    });

    it('diff delete text colors are distinct', () => {
        expect(darkCodePreviewTheme.diffDeleteText).not.toBe(lightCodePreviewTheme.diffDeleteText);
    });

    it('line number text colors are distinct', () => {
        expect(darkCodePreviewTheme.lineNumText).not.toBe(lightCodePreviewTheme.lineNumText);
    });

    it('tab bar background colors are distinct', () => {
        expect(darkCodePreviewTheme.tabBg).not.toBe(lightCodePreviewTheme.tabBg);
    });

    it('syntax keyword colors are distinct', () => {
        expect(darkCodePreviewTheme.syntaxKeyword).not.toBe(lightCodePreviewTheme.syntaxKeyword);
    });

    it('syntax string colors are distinct', () => {
        expect(darkCodePreviewTheme.syntaxString).not.toBe(lightCodePreviewTheme.syntaxString);
    });

    it('syntax comment colors are distinct', () => {
        expect(darkCodePreviewTheme.syntaxComment).not.toBe(lightCodePreviewTheme.syntaxComment);
    });

    it('syntax function colors are distinct', () => {
        expect(darkCodePreviewTheme.syntaxFunction).not.toBe(lightCodePreviewTheme.syntaxFunction);
    });
});

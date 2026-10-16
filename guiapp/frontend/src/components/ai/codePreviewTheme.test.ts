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
    createCodePreviewTheme,
    darkCodePreviewTheme,
    LIGHT_EDITOR_CHROME_BG,
    lightCodePreviewTheme,
    maximumContrastInkOnFill,
    type CodePreviewTheme,
} from './CodePreviewPanel';
import { darkTheme, lightTheme } from './aiAssistantPanelTheme';

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

describe('Active assistant scheme derivation', () => {
    it('inherits the active assistant surfaces and semantic colors', () => {
        const preview = createCodePreviewTheme({ ...darkTheme, isDark: true });

        expect(preview.bg).toBe(darkTheme.bg);
        expect(preview.border).toBe(darkTheme.divider);
        expect(preview.tabBg).toBe(darkTheme.bg);
        expect(preview.tabActiveBg).toBe(darkTheme.fieldBg);
        expect(preview.diffDeleteBg).toBe(darkTheme.errorBg);
        expect(preview.diffDeleteText).toBe(darkTheme.errorText);
    });

    it('keeps scheme ink while light editor chrome stays neutral', () => {
        const alternateLight = {
            ...lightTheme,
            bg: '#f3f8ff',
            titleBarBg: '#e5f0ff',
            fieldBg: '#ffffff',
            divider: '#9bb8d6',
            linkColor: '#0f4c81',
            pathColor: '#0b5b76',
        };

        const preview = createCodePreviewTheme(alternateLight);

        expect(preview.syntaxKeyword).toBe(alternateLight.linkColor);
        expect(preview.syntaxNumber).toBe(alternateLight.pathColor);
        expect(preview.tabActiveText).toBe(alternateLight.headingColor);
        expect(preview.bg).toBe(lightCodePreviewTheme.bg);
        expect(preview.border).toBe(lightCodePreviewTheme.border);
    });

    it('uses explicit dark-state semantics when deriving success tones', () => {
        const preview = createCodePreviewTheme({
            ...lightTheme,
            isDark: true,
        });

        expect(preview.diffAddText).toBe('#7aa89a');
        expect(preview.diffAddBg).toContain('#7aa89a');
    });

    it('uses scheme body text for default ink instead of inline-code color', () => {
        const preview = createCodePreviewTheme(lightTheme);
        expect(preview.text).toBe(lightTheme.text);
        expect(preview.text).not.toBe(lightTheme.codeText);
        expect(preview.syntaxFunction).toBe(lightTheme.codeText || lightTheme.text);
    });

    it('uses the maximum-contrast ink for the muted dark-mode success fill', () => {
        const successFill = '#7aa89a';
        expect(maximumContrastInkOnFill(successFill)).toBe('#111111');
    });

    it('uses a neutral light editor instead of scheme blue washes', () => {
        const preview = createCodePreviewTheme(lightTheme);
        expect(preview.bg).toBe(lightCodePreviewTheme.bg);
        expect(preview.lineNumBg).toBe(LIGHT_EDITOR_CHROME_BG);
        expect(preview.border).toBe(lightCodePreviewTheme.border);
        expect(preview.tabBg).toBe(LIGHT_EDITOR_CHROME_BG);
        expect(preview.tabActiveBg).toBe(lightCodePreviewTheme.tabActiveBg);
        expect(preview.tabHoverBg).toBe(lightCodePreviewTheme.tabHoverBg);
        expect(preview.lineNumBg).not.toBe(lightTheme.codeBg);
        expect(preview.border).not.toBe(lightTheme.divider);
        expect(preview.tabActiveBg).not.toBe(lightTheme.fieldBg);
        expect(lightCodePreviewTheme.lineNumBg).toBe(LIGHT_EDITOR_CHROME_BG);
        expect(lightCodePreviewTheme.tabBg).toBe(LIGHT_EDITOR_CHROME_BG);
    });

    it('keeps the light editor neutral when the scheme canvas is blue-tinted', () => {
        const preview = createCodePreviewTheme({
            ...lightTheme,
            bg: '#f3f8ff',
            codeBg: '#d6e8ff',
            divider: '#9bb8d6',
            fieldBg: '#e5f0ff',
        });
        expect(preview.bg).toBe(lightCodePreviewTheme.bg);
        expect(preview.bg).not.toBe('#f3f8ff');
        expect(preview.lineNumBg).toBe(LIGHT_EDITOR_CHROME_BG);
        expect(preview.lineNumBg).not.toBe('#d6e8ff');
        expect(preview.border).toBe(lightCodePreviewTheme.border);
        expect(preview.tabActiveBg).toBe(lightCodePreviewTheme.tabActiveBg);
    });

    it('keeps a dark gutter on a dark canvas even when isDark is omitted', () => {
        const preview = createCodePreviewTheme(darkTheme);
        expect(preview.lineNumBg).toBe(darkTheme.codeBg);
        expect(preview.lineNumBg).not.toBe(LIGHT_EDITOR_CHROME_BG);
    });
});

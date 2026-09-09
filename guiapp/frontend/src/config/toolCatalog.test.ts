import { describe, expect, it } from 'vitest';
import { getAllToolOptions, getToolLabel, getVisibleToolOptions, normalizeToolTab } from './toolCatalog';

describe('toolCatalog', () => {
    it('returns stable display names for coding tools', () => {
        expect(getToolLabel('claude')).toBe('Claude Code');
        expect(getToolLabel('codex')).toBe('OpenAI Codex');
        expect(getToolLabel('codebuddy')).toBe('CodeBuddy');
        expect(getToolLabel('kilo')).toBe('Kilo Code');
    });

    it('filters hidden tools but keeps visible tools in catalog order', () => {
        expect(getVisibleToolOptions({ show_kilo: false }).map((tool) => tool.id)).toEqual([
            'claude',
            'codex',
            'opencode',
            'codebuddy',
            'iflow',
        ]);
    });

    it('falls back to all coding tools when stale config hides every optional tool', () => {
        expect(getVisibleToolOptions({
            show_codex: false,
            show_opencode: false,
            show_codebuddy: false,
            show_iflow: false,
            show_kilo: false,
        }).map((tool) => tool.id)).toEqual([
            'claude',
            'codex',
            'opencode',
            'codebuddy',
            'iflow',
            'kilo',
        ]);
    });

    it('returns all coding tools for the top switcher regardless of visibility flags', () => {
        expect(getAllToolOptions().map((tool) => tool.id)).toEqual([
            'claude',
            'codex',
            'opencode',
            'codebuddy',
            'iflow',
            'kilo',
        ]);
    });

    it('normalizes removed tools to Claude Code', () => {
        expect(normalizeToolTab('cursor')).toBe('claude');
        expect(normalizeToolTab('gemini')).toBe('claude');
        expect(normalizeToolTab(' CoDeX ')).toBe('codex');
        expect(normalizeToolTab('codex')).toBe('codex');
    });
});

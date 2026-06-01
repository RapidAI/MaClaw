import { describe, expect, it } from 'vitest';
import { getToolLabel, getVisibleToolOptions } from './toolCatalog';

describe('toolCatalog', () => {
    it('returns stable display names for coding tools', () => {
        expect(getToolLabel('claude')).toBe('Claude Code');
        expect(getToolLabel('gemini')).toBe('Gemini CLI');
        expect(getToolLabel('codex')).toBe('OpenAI Codex');
        expect(getToolLabel('codebuddy')).toBe('CodeBuddy');
        expect(getToolLabel('kilo')).toBe('Kilo Code');
    });

    it('filters hidden tools but keeps visible tools in catalog order', () => {
        expect(getVisibleToolOptions({ show_gemini: false, show_kilo: false }).map((tool) => tool.id)).toEqual([
            'claude',
            'codex',
            'opencode',
            'codebuddy',
            'cursor',
            'iflow',
        ]);
    });
});

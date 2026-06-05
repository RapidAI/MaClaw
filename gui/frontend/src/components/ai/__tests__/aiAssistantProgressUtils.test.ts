import { describe, expect, it } from 'vitest';
import { formatToolProgressStatus, isToolProgressMessage } from '../aiAssistantProgressUtils';

describe('aiAssistantProgressUtils', () => {
    it('formats Skill progress into one compact Chinese status', () => {
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query \u{1f324}\u300d...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u6267\u884c Weather Query \u{1f324}');
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u542f\u52a8 Skill\u300cWeather Query \u{1f324}\u300d...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u542f\u52a8 Weather Query \u{1f324}');
    });

    it('formats shell-style tool paths into the user-facing tool name', () => {
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u6267\u884c Shell /Weather Query \u{1f324} / ...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u6267\u884c Weather Query \u{1f324}');
        expect(formatToolProgressStatus('\u{1f680} executing Shell /Weather Query \u{1f324} / ...', 'en'))
            .toBe('Running Weather Query \u{1f324}');
        expect(formatToolProgressStatus('\u{1f680} starting Shell /Weather Query \u{1f324} / ...', 'en'))
            .toBe('Starting Weather Query \u{1f324}');
    });

    it('only treats progress rows with known tool prefixes as tool progress', () => {
        expect(isToolProgressMessage({ id: 'p1', role: 'progress', content: '\u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query\u300d...', timestamp: 1 })).toBe(true);
        expect(isToolProgressMessage({ id: 'p1-space', role: 'progress', content: '  \u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query\u300d...', timestamp: 1 })).toBe(true);
        expect(isToolProgressMessage({ id: 'p1-generic', role: 'progress', content: '\u{1f680} working', timestamp: 1 })).toBe(false);
        expect(isToolProgressMessage({ id: 'p2', role: 'progress', content: 'working', timestamp: 1 })).toBe(false);
        expect(isToolProgressMessage({ id: 'a1', role: 'assistant', content: '\u{1f680} working', timestamp: 1 })).toBe(false);
    });
});

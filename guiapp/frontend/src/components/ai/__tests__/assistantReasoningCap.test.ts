import { describe, expect, it } from 'vitest';
import { capReasoningForDisplay, REASONING_DISPLAY_MAX_CHARS } from '../assistantReasoningBody';

// A real transcript was found carrying a 1,041,748-character reasoning trail.
// Every render sanitized it, every render concatenated it into a cache key, and
// every save re-serialized it — together a multi-second main-thread block when
// the assistant panel restored. The cap bounds all three.
describe('capReasoningForDisplay', () => {
    it('leaves short trails untouched', () => {
        const short = 'thinking about the answer';
        expect(capReasoningForDisplay(short)).toBe(short);
    });

    it('leaves a trail exactly at the limit untouched', () => {
        const exact = 'x'.repeat(REASONING_DISPLAY_MAX_CHARS);
        expect(capReasoningForDisplay(exact)).toBe(exact);
        expect(capReasoningForDisplay(exact).length).toBe(REASONING_DISPLAY_MAX_CHARS);
    });

    it('keeps the tail, because thinking models put the conclusion last', () => {
        const head = 'A'.repeat(REASONING_DISPLAY_MAX_CHARS);
        const tail = 'FINAL_ANSWER_TAIL';
        const capped = capReasoningForDisplay(head + tail);

        expect(capped.length).toBeLessThan(REASONING_DISPLAY_MAX_CHARS + 16);
        expect(capped.endsWith(tail)).toBe(true);
        // The head is what gets dropped.
        expect(capped.startsWith('…')).toBe(true);
    });

    it('bounds a megabyte-sized trail by three orders of magnitude', () => {
        const huge = 'reasoning '.repeat(100_000); // ~1 MB, the observed size
        expect(huge.length).toBeGreaterThan(REASONING_DISPLAY_MAX_CHARS * 50);
        expect(capReasoningForDisplay(huge).length).toBeLessThan(REASONING_DISPLAY_MAX_CHARS + 16);
    });

    it('handles empty and undefined input', () => {
        expect(capReasoningForDisplay('')).toBe('');
        // The function is typed string but callers pass undefined in practice.
        expect(capReasoningForDisplay(undefined as unknown as string)).toBe(undefined);
    });
});

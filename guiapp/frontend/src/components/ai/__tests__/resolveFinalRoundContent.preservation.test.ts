import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { resolveFinalRoundContent, type ChatMessage } from '../useAIAssistant';

/**
 * Preservation Property Tests — Baseline Behavior on UNFIXED Code
 *
 * These tests capture the CURRENT (unfixed) behavior of resolveFinalRoundContent
 * for non-buggy input domains. They must PASS on unfixed code, confirming the
 * baseline behavior that the fix must preserve.
 *
 * After the fix is applied, these tests must STILL PASS — any failure would
 * indicate a regression in preserved behavior.
 */

function makeMessage(content: string): ChatMessage {
    return {
        id: 'msg-preservation-1',
        role: 'assistant',
        content,
        timestamp: Date.now(),
    };
}

/**
 * Arbitrary: non-empty string of printable characters (including CJK).
 * Avoids empty strings which would change control flow.
 */
const nonEmptyText = fc.string({ minLength: 1, maxLength: 120 }).filter(s => s.trim().length > 0);

/**
 * Arbitrary: generates a (streamedContent, finalText) pair where
 * streamedContent.endsWith(finalText) AND streamedContent.length > finalText.length.
 */
const endsWithPair = nonEmptyText.chain(suffix =>
    nonEmptyText.map(prefix => ({
        streamedContent: prefix + suffix,
        finalText: suffix,
    })),
).filter(({ streamedContent, finalText }) =>
    streamedContent.length > finalText.length && streamedContent.endsWith(finalText),
);

const specialSources: readonly string[] = ['ask_user', 'cancel', 'file_delivery', 'screenshot'];

describe('resolveFinalRoundContent — Preservation Properties', () => {

    /**
     * Property 2a: Special source paths behavior on UNFIXED code.
     *
     * **Validates: Requirements 3.1, 3.2, 3.3**
     *
     * On unfixed code, `response_source` is completely ignored. The function
     * uses the same endsWith + fallback logic regardless of source. So:
     * - If streamedContent.endsWith(finalText) && streamedContent.length > finalText.length → returns streamedContent
     * - Otherwise if finalText is non-empty → returns finalText
     *
     * For preservation: we test the case where endsWith does NOT match
     * (or streamedContent is empty), so the function returns finalText.
     * This is the behavior we want to preserve for special sources.
     */
    describe('Property 2a: Special source paths return finalText when endsWith does not match', () => {
        it('special source with empty streamedContent always returns finalText', () => {
            fc.assert(
                fc.property(
                    fc.constantFrom(...specialSources),
                    nonEmptyText,
                    (source, finalText) => {
                        const message = makeMessage('');
                        const response = { text: finalText, response_source: source };
                        const result = resolveFinalRoundContent(message, response);
                        expect(result).toBe(finalText);
                    },
                ),
                { numRuns: 200 },
            );
        });

        it('special source with non-suffix streamedContent returns finalText', () => {
            fc.assert(
                fc.property(
                    fc.constantFrom(...specialSources),
                    nonEmptyText,
                    nonEmptyText.filter(s => s.length >= 2),
                    (source, finalText, streamedContent) => {
                        // Ensure streamedContent does NOT end with finalText
                        // by appending a distinguishing suffix
                        const distinctStreamed = streamedContent + '___DISTINCT___';
                        const message = makeMessage(distinctStreamed);
                        const response = { text: finalText, response_source: source };
                        const result = resolveFinalRoundContent(message, response);
                        // On unfixed code: response_source is ignored, endsWith fails,
                        // falls through to `if (finalText) return finalText`
                        expect(result).toBe(finalText);
                    },
                ),
                { numRuns: 200 },
            );
        });
    });

    /**
     * Property 2b: Non-streaming responses use finalText.
     *
     * **Validates: Requirements 3.4, 3.6**
     *
     * When streamedContent is empty and finalText is non-empty,
     * the function returns finalText. This is straightforward on
     * both unfixed and fixed code.
     */
    describe('Property 2b: Empty streamedContent with non-empty finalText returns finalText', () => {
        it('empty message content always yields finalText', () => {
            fc.assert(
                fc.property(
                    nonEmptyText,
                    fc.option(fc.constantFrom('agent_loop', 'ask_user', 'cancel', undefined), { nil: undefined }),
                    (finalText, source) => {
                        const message = makeMessage('');
                        const response: any = { text: finalText };
                        if (source !== undefined) {
                            response.response_source = source;
                        }
                        const result = resolveFinalRoundContent(message, response);
                        expect(result).toBe(finalText);
                    },
                ),
                { numRuns: 200 },
            );
        });
    });

    /**
     * Property 2c: EndsWith fallback preserves streamedContent.
     *
     * **Validates: Requirements 3.5**
     *
     * When streamedContent.endsWith(finalText) and streamedContent.length > finalText.length,
     * the function returns streamedContent. This is the original improvement #19 behavior.
     */
    describe('Property 2c: endsWith match with longer streamedContent returns streamedContent', () => {
        it('streamedContent preserved when it ends with finalText and is longer', () => {
            fc.assert(
                fc.property(
                    endsWithPair,
                    fc.option(fc.constantFrom('agent_loop', undefined), { nil: undefined }),
                    ({ streamedContent, finalText }, source) => {
                        const message = makeMessage(streamedContent);
                        const response: any = { text: finalText };
                        if (source !== undefined) {
                            response.response_source = source;
                        }
                        const result = resolveFinalRoundContent(message, response);
                        expect(result).toBe(streamedContent);
                    },
                ),
                { numRuns: 200 },
            );
        });
    });

    /**
     * Property 2d: Missing/empty response_source degrades gracefully.
     *
     * **Validates: Requirements 3.7**
     *
     * When response_source is undefined or empty, the function does not throw
     * and produces a consistent result via the length + endsWith degraded strategy.
     * On unfixed code, this is the only strategy (response_source is always ignored).
     */
    describe('Property 2d: Missing response_source degrades gracefully without errors', () => {
        it('undefined response_source never throws and returns a string', () => {
            fc.assert(
                fc.property(
                    fc.string({ maxLength: 200 }),
                    fc.string({ maxLength: 200 }),
                    fc.constantFrom(undefined, '', null),
                    (streamedContent, finalText, source) => {
                        const message = makeMessage(streamedContent);
                        const response: any = { text: finalText };
                        if (source !== undefined && source !== null) {
                            response.response_source = source;
                        }
                        // Must not throw
                        const result = resolveFinalRoundContent(message, response);
                        expect(typeof result).toBe('string');
                    },
                ),
                { numRuns: 200 },
            );
        });

        it('undefined response_source produces same result as explicit behavior check', () => {
            fc.assert(
                fc.property(
                    nonEmptyText,
                    nonEmptyText,
                    (streamedContent, finalText) => {
                        const message = makeMessage(streamedContent);
                        // No response_source at all
                        const response = { text: finalText };
                        const result = resolveFinalRoundContent(message, response);

                        // Replicate the fixed code's three-layer logic manually:
                        let expected: string;
                        const finalTextLen = finalText.trim().length;
                        // Layer 2: length comparison (no response_source → eligible)
                        if (streamedContent && finalText && finalTextLen > 0 && streamedContent.length >= finalTextLen * 2) {
                            expected = streamedContent;
                        // Layer 3: endsWith fallback
                        } else if (streamedContent && finalText && streamedContent.length > finalText.length) {
                            if (streamedContent.endsWith(finalText)) {
                                expected = streamedContent;
                            } else {
                                expected = finalText;
                            }
                        } else {
                            expected = finalText;
                        }

                        expect(result).toBe(expected);
                    },
                ),
                { numRuns: 200 },
            );
        });
    });
});

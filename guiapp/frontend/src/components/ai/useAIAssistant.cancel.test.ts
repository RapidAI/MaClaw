import { describe, expect, it } from 'vitest';
import { CANCELED_BY_USER_LINE, markRoundCancelled, type ChatMessage } from './useAIAssistant';

function assistantMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
    return {
        id: 'assistant-1',
        role: 'assistant',
        content: 'partial generated content',
        requestId: 'request-1',
        timestamp: 1,
        ...overrides,
    };
}

describe('AI assistant cancellation state transition', () => {
    it('preserves generated assistant content and appends the cancellation line', () => {
        const messages = [
            assistantMessage({ content: 'line one\nline two\n\n' }),
        ];

        const next = markRoundCancelled(messages, 'assistant-1', 'request-1');

        expect(next).toHaveLength(1);
        expect(next[0].content).toBe(`line one\nline two\n${CANCELED_BY_USER_LINE}`);
    });

    it('marks an empty assistant placeholder instead of deleting it', () => {
        const messages = [
            assistantMessage({ content: '' }),
        ];

        const next = markRoundCancelled(messages, 'assistant-1', 'request-1');

        expect(next).toHaveLength(1);
        expect(next[0].content).toBe(CANCELED_BY_USER_LINE);
    });

    it('is idempotent for repeated cancellation signals', () => {
        const messages = [
            assistantMessage({ content: `partial generated content\n${CANCELED_BY_USER_LINE}` }),
        ];

        const next = markRoundCancelled(messages, 'assistant-1', 'request-1');

        expect(next[0].content).toBe(`partial generated content\n${CANCELED_BY_USER_LINE}`);
    });
});


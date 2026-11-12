import { describe, expect, it } from 'vitest';
import { continueSessionForSend, desktopThreadOf, lastSettledConversation, DESKTOP_RECENT_THREAD_MS } from './desktopContinueSession';

describe('continueSessionForSend', () => {
    const bot = 'bot_p';
    const thread = 'bot_p#t-k-b';

    it('routes a follow-up to the last finished thread', () => {
        const messages = [
            { role: 'user', conversation: bot },
            { role: 'assistant', conversation: bot, conversationAt: 1_000 },
            { role: 'user', conversation: thread },
            { role: 'assistant', conversation: thread, conversationAt: 2_000 },
        ];
        expect(continueSessionForSend(bot, messages, 2_000 + 60_000)).toBe(thread);
    });

    it('sends the bot id for a clearly new message after the person dismissed the thread', () => {
        const messages = [
            { role: 'assistant', conversation: thread, conversationAt: 2_000 },
        ];
        expect(continueSessionForSend(bot, messages, 2_500, { main: true })).toBe(bot);
    });

    it('sends the bot id when the last settled turn was in the main conversation', () => {
        const messages = [
            { role: 'assistant', conversation: thread, conversationAt: 1_000 },
            { role: 'user', conversation: bot },
            { role: 'assistant', conversation: bot, conversationAt: 3_000 },
        ];
        expect(lastSettledConversation(bot, messages)).toBe(bot);
        expect(continueSessionForSend(bot, messages, 3_500)).toBe(bot);
    });

    it('sends the bot id after the sticky window when the last thread is stale', () => {
        const messages = [
            { role: 'assistant', conversation: thread, conversationAt: 1_000 },
        ];
        expect(continueSessionForSend(bot, messages, 1_000 + DESKTOP_RECENT_THREAD_MS + 1)).toBe(bot);
    });

    it('uses a thread the person picked, even after the window', () => {
        const messages = [
            { role: 'assistant', conversation: thread, conversationAt: 1_000 },
        ];
        expect(continueSessionForSend(bot, messages, 1_000 + DESKTOP_RECENT_THREAD_MS + 1, { session: thread })).toBe(thread);
    });

    it('ignores conversations that are not this bot', () => {
        expect(desktopThreadOf(bot, 'other#t-1')).toBe('');
        expect(continueSessionForSend(bot, [{ role: 'assistant', conversation: 'other#t-1' }])).toBe('');
    });
});

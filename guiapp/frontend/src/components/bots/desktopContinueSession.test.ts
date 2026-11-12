import { describe, expect, it } from 'vitest';
import { contentRoutingApplies, continueSessionForSend, desktopThreadOf, lastSettledConversation, recentThreadCandidates, DESKTOP_RECENT_THREAD_MS } from './desktopContinueSession';

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

describe('recentThreadCandidates', () => {
    const now = 10 * 24 * 60 * 60 * 1000;
    it('lists finished threads of the last day, newest first, bounded', () => {
        const msgs: any[] = [];
        for (let i = 0; i < 8; i++) {
            msgs.push({ role: 'user', content: `任务${i}` });
            msgs.push({ role: 'assistant', content: `结果${i}`, conversation: `bot_1#t-${i}`, conversationAt: now - (8 - i) * 60_000 });
        }
        msgs.push({ role: 'user', content: '旧的' });
        msgs.push({ role: 'assistant', content: '很早', conversation: 'bot_1#t-old', conversationAt: now - 25 * 60 * 60 * 1000 });
        msgs.push({ role: 'user', content: '在跑' });
        msgs.push({ role: 'assistant', content: '', pending: true, conversation: 'bot_1#t-live', conversationAt: now });
        msgs.push({ role: 'assistant', content: '主线', conversation: 'bot_1', conversationAt: now });
        const got = recentThreadCandidates('bot_1', msgs, now);
        expect(got.map(c => c.session)).toEqual(['bot_1#t-7', 'bot_1#t-6', 'bot_1#t-5', 'bot_1#t-4', 'bot_1#t-3']);
        expect(got[0]).toEqual({ session: 'bot_1#t-7', title: '任务7', goal: undefined, result: '结果7' });
    });
    it('applies only without an explicit choice or sticky thread', () => {
        expect(contentRoutingApplies('bot_1', '')).toBe(true);
        expect(contentRoutingApplies('bot_1', 'bot_1')).toBe(true);
        expect(contentRoutingApplies('bot_1', 'bot_1#t-a')).toBe(false);
        expect(contentRoutingApplies('bot_1', 'bot_1', { main: true })).toBe(false);
        expect(contentRoutingApplies('bot_1', '', { session: 'bot_1#t-a' })).toBe(false);
    });
});

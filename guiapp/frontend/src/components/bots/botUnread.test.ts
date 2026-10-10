// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { appendDesktopBotReport, messagesForBot, saveBotMessages, settlePendingBotReply } from './desktopBots';
import { adoptExistingBotReplies, botWindowIsForeground, discardBotUnreadMemory, setBotWindowForeground, unreadBotReplyCount, watchBotReplies } from './botUnread';

beforeEach(() => {
    localStorage.clear();
    discardBotUnreadMemory();
    watchBotReplies('alice', null);
    setBotWindowForeground(false);
});

function plantHistory() {
    saveBotMessages('alice', 'bot_1', [
        { id: 'u', role: 'user', content: '每分钟向我问好一次。' },
        { id: 'old', role: 'assistant', content: '好的，已为你设定每分钟向你问好一次。', chat: true },
        { id: 'wait', role: 'assistant', content: '', pending: true },
        { id: 'read', role: 'assistant', content: '我先看一下。', ack: true, understood: true },
    ]);
    saveBotMessages('bob', 'bot_9', [
        { id: 'bob-old', role: 'assistant', content: '鲍勃的旧回复。' },
    ]);
}

describe('bot unread replies', () => {
    it('treats replies already in the transcript as seen', () => {
        plantHistory();
        adoptExistingBotReplies();
        expect(unreadBotReplyCount('alice')).toBe(0);
        expect(unreadBotReplyCount('bob')).toBe(0);

        appendDesktopBotReport('alice', 'bot_1', { content: '你好！很高兴见到你。', failed: false, requestId: 'desktop-bot-sched-1' });
        expect(unreadBotReplyCount('alice')).toBe(1);
        appendDesktopBotReport('alice', 'bot_1', { content: '你好！很高兴见到你。', failed: false, requestId: 'desktop-bot-sched-1' });
        expect(unreadBotReplyCount('alice')).toBe(1);
        adoptExistingBotReplies();
        expect(unreadBotReplyCount('alice')).toBe(1);
    });

    it('adds each landed reply and subtracts only the bot being viewed', () => {
        adoptExistingBotReplies();
        appendDesktopBotReport('alice', 'bot_1', { content: '第一句问候。', failed: false, requestId: 'desktop-bot-sched-a' });
        appendDesktopBotReport('alice', 'bot_1', { content: '第二句问候。', failed: false, requestId: 'desktop-bot-sched-b' });
        appendDesktopBotReport('alice', 'bot_2', { content: '另一个 Bot 的回复。', failed: false, requestId: 'desktop-bot-sched-c' });
        appendDesktopBotReport('bob', 'bot_9', { content: '别人的回复。', failed: false, requestId: 'desktop-bot-sched-d' });
        expect(unreadBotReplyCount('alice')).toBe(3);
        expect(unreadBotReplyCount('bob')).toBe(1);

        watchBotReplies('alice', 'bot_1');
        expect(unreadBotReplyCount('alice')).toBe(1);

        appendDesktopBotReport('alice', 'bot_1', { content: '看着的时候到达。', failed: false, requestId: 'desktop-bot-sched-e' });
        expect(unreadBotReplyCount('alice')).toBe(1);

        watchBotReplies('alice', null);
        appendDesktopBotReport('alice', 'bot_1', { content: '窗口在后台时到达。', failed: false, requestId: 'desktop-bot-sched-f' });
        expect(unreadBotReplyCount('alice')).toBe(2);

        watchBotReplies('alice', 'bot_2');
        expect(unreadBotReplyCount('alice')).toBe(1);
    });

    it('counts a pending reply once, when it settles', () => {
        adoptExistingBotReplies();
        saveBotMessages('alice', 'bot_1', [
            { id: 'run', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-run' },
            { id: 'user', role: 'user', content: '查天气' },
            { id: 'ack', role: 'assistant', content: '我先看一下。', ack: true, understood: true },
        ]);
        expect(unreadBotReplyCount('alice')).toBe(0);

        settlePendingBotReply('alice', 'bot_1', { content: '今天晴。', failed: false, requestId: 'desktop-bot-run' });
        expect(unreadBotReplyCount('alice')).toBe(1);
        settlePendingBotReply('alice', 'bot_1', { content: '今天晴。', failed: false, requestId: 'desktop-bot-run' });
        expect(unreadBotReplyCount('alice')).toBe(1);
    });

    it('counts a chat reply and a failed reply, and skips the reading that will be replaced', () => {
        adoptExistingBotReplies();
        saveBotMessages('alice', 'bot_1', [
            { id: 'chat', role: 'assistant', content: '好的，已为你设定每分钟向你问好一次。', chat: true },
            { id: 'fail', role: 'assistant', content: '这句没发出。', failed: true },
            { id: 'read', role: 'assistant', content: '我先看一下。', ack: true, understood: true },
        ]);
        expect(unreadBotReplyCount('alice')).toBe(2);
    });

    it('does not record a bot that has no reply yet', () => {
        adoptExistingBotReplies();
        const before = localStorage.getItem('maclaw.desktopBotUnread.v1');
        watchBotReplies('alice', 'bot_1');
        expect(localStorage.getItem('maclaw.desktopBotUnread.v1')).toBe(before);
        expect(unreadBotReplyCount('alice')).toBe(0);
    });

    it('keeps seen replies when the unread file cannot be stored, and writes that same set later', () => {
        const original = Storage.prototype.setItem;
        let blockUnread = true;
        Storage.prototype.setItem = function (key: string, value: string) {
            if (blockUnread && key === 'maclaw.desktopBotUnread.v1') throw new Error('quota');
            return original.call(this, key, value);
        };
        try {
            plantHistory();
            adoptExistingBotReplies();
            expect(unreadBotReplyCount('alice')).toBe(0);
            expect(localStorage.getItem('maclaw.desktopBotUnread.v1')).toBeNull();

            appendDesktopBotReport('alice', 'bot_1', { content: '第一句新问候。', failed: false, requestId: 'desktop-bot-sched-q1' });
            expect(unreadBotReplyCount('alice')).toBe(1);

            blockUnread = false;
            appendDesktopBotReport('alice', 'bot_1', { content: '第二句新问候。', failed: false, requestId: 'desktop-bot-sched-q2' });
            expect(unreadBotReplyCount('alice')).toBe(2);
            const saved = JSON.parse(localStorage.getItem('maclaw.desktopBotUnread.v1') || '{}') as { adopted?: boolean; seen?: { alice?: { bot_1?: string[] } } };
            expect(saved.adopted).toBe(true);
            const seen = saved.seen?.alice?.bot_1 || [];
            expect(seen).toContain('old');
            const fresh = messagesForBot('alice', 'bot_1').filter(message => message.role === 'assistant' && !message.pending && !message.understood && message.id !== 'old');
            expect(fresh.length).toBe(2);
            for (const message of fresh) expect(seen).not.toContain(message.id);
        } finally {
            Storage.prototype.setItem = original;
            discardBotUnreadMemory();
        }
    });

    it('does not raise the count for a reply that arrives while the bot is open, even if remembering it fails', () => {
        adoptExistingBotReplies();
        watchBotReplies('alice', 'bot_1');
        const before = localStorage.getItem('maclaw.desktopBotUnread.v1');
        const original = Storage.prototype.setItem;
        Storage.prototype.setItem = function (key: string, value: string) {
            if (key === 'maclaw.desktopBotUnread.v1') throw new Error('quota');
            return original.call(this, key, value);
        };
        try {
            appendDesktopBotReport('alice', 'bot_1', { content: '看着的时候到达。', failed: false, requestId: 'desktop-bot-sched-look' });
            expect(unreadBotReplyCount('alice')).toBe(0);
            expect(localStorage.getItem('maclaw.desktopBotUnread.v1')).toBe(before);
            const arrived = messagesForBot('alice', 'bot_1').find(message => message.content === '看着的时候到达。');
            expect(arrived?.id).toBeTruthy();
            discardBotUnreadMemory();
            expect(unreadBotReplyCount('alice')).toBe(1);
        } finally {
            Storage.prototype.setItem = original;
            discardBotUnreadMemory();
        }
    });

    it('stays in front while a child frame has the keyboard, and steps back when the window is hidden', () => {
        const hasFocus = document.hasFocus.bind(document);
        const visibility = Object.getOwnPropertyDescriptor(document, 'visibilityState');
        document.hasFocus = () => false;
        try {
            setBotWindowForeground(true);
            expect(botWindowIsForeground()).toBe(true);
            Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
            expect(botWindowIsForeground()).toBe(false);
        } finally {
            document.hasFocus = hasFocus;
            if (visibility) Object.defineProperty(document, 'visibilityState', visibility);
            else Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
            setBotWindowForeground(false);
        }
    });
});

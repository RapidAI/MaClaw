// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { addDesktopBot, botsForUser, deleteDesktopBot, messagesForBot, renameDesktopBot, saveBotMessages, settlePendingBotReply } from './desktopBots';

beforeEach(() => {
    localStorage.clear();
});

describe('desktop bots', () => {
    it('keeps each user on a separate bot list and desktop identity', () => {
        addDesktopBot('alice', 1);
        addDesktopBot('alice', 2);
        addDesktopBot('bob', 3);

        expect(botsForUser('alice').map(bot => bot.title)).toEqual(['Bot 1', 'Bot 2']);
        expect(botsForUser('bob').map(bot => bot.title)).toEqual(['Bot 1']);
        expect(botsForUser('alice').every(bot => bot.id.startsWith('bot-'))).toBe(true);
    });

    it('renames and deletes only the selected bot', () => {
        const first = addDesktopBot('alice', 1);
        const second = addDesktopBot('alice', 2);

        renameDesktopBot('alice', first.id, '  值班  ', '晚上值守');
        expect(botsForUser('alice').map(bot => bot.title)).toEqual(['值班', 'Bot 2']);
        expect(botsForUser('alice')[0].description).toBe('晚上值守');

        deleteDesktopBot('alice', second.id);
        expect(botsForUser('alice').map(bot => bot.id)).toEqual([first.id]);
        expect(botsForUser('bob')).toEqual([]);
    });

    it('writes a result onto the request that produced it', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-1' },
            { id: 'b', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-2' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '第一句', failed: false, requestId: 'desktop-bot-1' });
        const items = messagesForBot('alice', 'bot_1');
        expect(items[0].content).toBe('第一句');
        expect(items[0].pending).toBe(false);
        expect(items[1].pending).toBe(true);
        expect(items[1].content).toBe('');
    });

    it('leaves the next command alone when an earlier reply arrives late', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '请在这个浏览器里登录', pending: false, requestId: 'desktop-bot-1' },
            { id: 'b', role: 'assistant', content: '', pending: true },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '迟到的旧结果', failed: false, requestId: 'desktop-bot-1', userControl: true });
        const items = messagesForBot('alice', 'bot_1');
        expect(items[0].content).toBe('请在这个浏览器里登录');
        expect(items[0].userControl).toBeUndefined();
        expect(items[1].pending).toBe(true);
        expect(items[1].content).toBe('');
    });
});

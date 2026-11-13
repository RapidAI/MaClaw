import { describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({ WriteDesktopBotAck: vi.fn() }));

import { DESKTOP_BOT_FIRST_ACK_BUDGET_MS, bindAckCommand, desktopBotAck, desktopBotIsPlaceholderAck, desktopBotLine, desktopBotNeutralAck, desktopBotReceiptAck, desktopBotStoppingLine, firstAckCommandKeys, landFirstAck, releaseSettledAckCommands, type DesktopBotAckContext } from './desktopBotAck';
import type { DesktopBotMessage } from './desktopBots';

describe('desktopBotAck', () => {
    it('uses the model line', async () => {
        const write = vi.fn().mockResolvedValue(' 收到《a.pdf》，拿来写摘要。 ');
        await expect(desktopBotAck({ kind: 'accept', message: 'x', lang: 'zh', files: ['a.pdf'] }, write)).resolves.toBe('收到《a.pdf》，拿来写摘要。');
        expect(write.mock.calls[0][0].files).toEqual(['a.pdf']);
    });
    it('falls back to one neutral line when every attempt failed', async () => {
        const fail = vi.fn().mockRejectedValue(new Error('timeout'));
        await expect(desktopBotAck({ kind: 'accept', message: 'x', lang: 'zh-Hans' }, fail)).resolves.toBe('好，我先做着。');
        await expect(desktopBotAck({ kind: 'supplement', message: 'x', lang: 'zh' }, fail)).resolves.toBe('收到。');
        await expect(desktopBotAck({ kind: 'accept', message: 'x', lang: 'en', files: ['a'] }, vi.fn().mockResolvedValue(''))).resolves.toBe('Got it.');
        expect(desktopBotNeutralAck({ kind: 'accept', lang: 'en' })).toBe("OK, I'm on it.");
    });
});

describe('desktopBotStoppingLine', () => {
    it('asks the model for a stopping line in the bot voice', async () => {
        const write = vi.fn().mockResolvedValue('小助手还在收尾，停好了告诉你。');
        await expect(desktopBotStoppingLine({ lang: 'zh-Hans', message: '停下', persona: '小助手' }, write)).resolves.toBe('小助手还在收尾，停好了告诉你。');
        expect(write).toHaveBeenCalledWith(expect.objectContaining({ kind: 'stopping', message: '停下', persona: '小助手' }));
    });
    it('falls back to the minimal line only when the model fails', async () => {
        await expect(desktopBotStoppingLine({ lang: 'zh-Hans' }, vi.fn().mockRejectedValue(new Error('x')))).resolves.toBe('还在停。');
        await expect(desktopBotStoppingLine({ lang: 'en' }, vi.fn().mockResolvedValue('  '))).resolves.toBe('Still stopping.');
    });
});

describe('desktopBotLine', () => {
    it('uses the model line for stopped/idle/pause and the given line only on failure', async () => {
        const write = vi.fn().mockResolvedValue('停好了，小红书那件不做了。');
        await expect(desktopBotLine('stopped', { lang: 'zh-Hans', running_task: '小红书' }, '停了。', write)).resolves.toBe('停好了，小红书那件不做了。');
        expect(write).toHaveBeenCalledWith(expect.objectContaining({ kind: 'stopped', running_task: '小红书' }));
        await expect(desktopBotLine('idle', { lang: 'zh-Hans' }, '没在做事。', vi.fn().mockRejectedValue(new Error('x')))).resolves.toBe('没在做事。');
    });
});

describe('desktopBotAck first-reply budget', () => {
    it('drops a writer that outlives the remaining budget and says the neutral line on time', async () => {
        vi.useFakeTimers();
        try {
            let seen: DesktopBotAckContext | undefined;
            const hung = (ctx: DesktopBotAckContext) => { seen = ctx; return new Promise<string>(() => undefined); };
            let line = '';
            void desktopBotAck({ kind: 'accept', message: '整理周报', lang: 'zh-Hans' }, hung, 1500).then(v => { line = v; });
            await vi.advanceTimersByTimeAsync(1400);
            expect(line).toBe('');
            await vi.advanceTimersByTimeAsync(200);
            expect(line).toBe('好，我先做着。');
            expect(seen?.budget_ms).toBe(1500);
        } finally {
            vi.useRealTimers();
        }
    });

    it('makes no call once the budget is spent, and keeps a model line that is on time', async () => {
        const write = vi.fn(async () => '我把销售表理成本周周报，发给老王。');
        expect(await desktopBotAck({ kind: 'accept', message: 'x', lang: 'zh-Hans' }, write, 0)).toBe('好，我先做着。');
        expect(write).not.toHaveBeenCalled();
        expect(await desktopBotAck({ kind: 'accept', message: 'x', lang: 'zh-Hans' }, write, 4000)).toBe('我把销售表理成本周周报，发给老王。');
        expect(DESKTOP_BOT_FIRST_ACK_BUDGET_MS).toBeLessThanOrEqual(5000);
        expect(desktopBotReceiptAck('zh-Hans')).toBe('收到，正在看。');
    });
});

describe('one ack bubble per command', () => {
    const model = '好的，我先获取最新的 DailyPaper，挑出最热门的一篇做图文综述，发到知乎、CSDN 和小红书。';
    const neutral = '好，我先做着。';
    // DesktopBotMessage is the production transcript row: ack is optional on
    // every variant, so fixtures that omit it still type-check.
    function turn(id: string, requestId: string): DesktopBotMessage[] {
        return [
            { id: 'u-' + id, role: 'user' as const, content: '做这个' },
            { id, role: 'assistant' as const, content: '', pending: true, requestId },
        ];
    }
    it('does not append the neutral line after the model line, then keeps that one bubble when the budget fires', () => {
        const registry = new Map<string, Set<string>>();
        const pending = 'm-bot3';
        const requestId = 'desktop-bot-3';
        bindAckCommand(registry, 'bot_3', [pending, 'local:' + pending]);
        let messages = turn(pending, 'local:' + pending);
        const keys = bindAckCommand(registry, 'bot_3', firstAckCommandKeys(messages, requestId));
        messages = landFirstAck(messages, keys, model, 'zh-Hans', () => 'ack-1');
        const again = landFirstAck(messages, bindAckCommand(registry, 'bot_3', [pending, requestId]), neutral, 'zh-Hans', () => 'ack-2');
        expect(again).toBe(messages);
        expect(messages.filter(item => item.ack).map(item => item.content)).toEqual([model]);
    });
    it('replaces a placeholder in place when the model line arrives later', () => {
        const pending = 'm-late';
        let messages = landFirstAck(turn(pending, 'desktop-bot-late'), [pending, 'desktop-bot-late'], neutral, 'zh-Hans', () => 'ack-p');
        messages = landFirstAck(messages, [pending, 'desktop-bot-late'], model, 'zh-Hans', () => 'ack-m');
        const acks = messages.filter(item => item.ack);
        expect(acks).toHaveLength(1);
        expect(acks[0].id).toBe('ack-p');
        expect(acks[0].content).toBe(model);
    });
    it('does not treat a queue note as the first reply', () => {
        const pending = 'm-q';
        const messages = [
            ...turn(pending, 'desktop-bot-q'),
            { id: 'q1', role: 'assistant' as const, content: '好，这个等手上的做完我就做。', ack: true },
        ];
        const next = landFirstAck(messages, [pending, 'desktop-bot-q'], '先记着。', 'zh-Hans', () => 'ack-q');
        expect(next.filter(item => item.ack).map(item => item.content)).toEqual(['先记着。', '好，这个等手上的做完我就做。']);
    });
    it('keeps each bot’s ack when three commands start together', () => {
        const registry = new Map<string, Set<string>>();
        const bots = ['bot_1', 'bot_2', 'bot_3'];
        const lines = ['我去打开第一个。', '我去打开第二个。', model];
        const stored = bots.map((botId, i) => {
            const pending = 'm-' + botId;
            bindAckCommand(registry, botId, [pending]);
            let messages = turn(pending, 'local:' + pending);
            const keys = bindAckCommand(registry, botId, firstAckCommandKeys(messages, 'req-' + botId));
            messages = landFirstAck(messages, keys, lines[i], 'zh-Hans', () => 'ack-' + botId);
            messages = landFirstAck(messages, bindAckCommand(registry, botId, [pending, 'req-' + botId]), neutral, 'zh-Hans', () => 'late-' + botId);
            return messages;
        });
        stored.forEach((messages, i) => {
            expect(messages.filter(item => item.ack).map(item => item.content)).toEqual([lines[i]]);
        });
        expect(bindAckCommand(registry, 'bot_1', ['m-bot_1'])).not.toContain('req-bot_3');
    });
    it('drops a command’s keys once its bubble is no longer pending', () => {
        const registry = new Map<string, Set<string>>();
        bindAckCommand(registry, 'bot_1', ['m-1', 'desktop-bot-1']);
        bindAckCommand(registry, 'bot_1', ['m-2', 'desktop-bot-2']);
        releaseSettledAckCommands(registry, 'bot_1', [
            { id: 'm-2', role: 'assistant', pending: true, requestId: 'desktop-bot-2' },
            { id: 'm-1', role: 'assistant', pending: false, requestId: 'desktop-bot-1' },
        ]);
        expect(bindAckCommand(registry, 'bot_1', ['m-2'])).toEqual(expect.arrayContaining(['m-2', 'desktop-bot-2']));
        expect(bindAckCommand(registry, 'bot_1', ['fresh'])).not.toContain('m-1');
    });
});

describe('desktopBotIsPlaceholderAck', () => {
    it('recognises the receipt and the failed-model line, not a written understanding', () => {
        expect(desktopBotIsPlaceholderAck('收到，正在看。', 'zh-Hans')).toBe(true);
        expect(desktopBotIsPlaceholderAck('好，我先做着。', 'zh-Hans')).toBe(true);
        expect(desktopBotIsPlaceholderAck('Got it, reading it now.', 'en')).toBe(true);
        expect(desktopBotIsPlaceholderAck('我去网上查黄仁勋的资料，做成带照片的简历发给你。', 'zh-Hans')).toBe(false);
        expect(desktopBotIsPlaceholderAck('好的，我来发。', 'zh-Hans')).toBe(false);
    });
});

import { describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({ WriteDesktopBotAck: vi.fn() }));

import { DESKTOP_BOT_FIRST_ACK_BUDGET_MS, desktopBotAck, desktopBotIsPlaceholderAck, desktopBotLine, desktopBotNeutralAck, desktopBotReceiptAck, desktopBotStoppingLine, type DesktopBotAckContext } from './desktopBotAck';

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

describe('desktopBotIsPlaceholderAck', () => {
    it('recognises the receipt and the failed-model line, not a written understanding', () => {
        expect(desktopBotIsPlaceholderAck('收到，正在看。', 'zh-Hans')).toBe(true);
        expect(desktopBotIsPlaceholderAck('好，我先做着。', 'zh-Hans')).toBe(true);
        expect(desktopBotIsPlaceholderAck('Got it, reading it now.', 'en')).toBe(true);
        expect(desktopBotIsPlaceholderAck('我去网上查黄仁勋的资料，做成带照片的简历发给你。', 'zh-Hans')).toBe(false);
        expect(desktopBotIsPlaceholderAck('好的，我来发。', 'zh-Hans')).toBe(false);
    });
});

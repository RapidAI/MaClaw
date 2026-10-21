import { describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({ WriteDesktopBotAck: vi.fn() }));

import { desktopBotAck, desktopBotNeutralAck } from './desktopBotAck';

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

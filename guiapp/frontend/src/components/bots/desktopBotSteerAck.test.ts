import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { desktopBotAck, desktopBotIsPlaceholderAck, DESKTOP_BOT_FIRST_ACK_BUDGET_MS } from './desktopBotAck';

// Bot 3, 2026-11-11 08:33 (UTC+8): a follow-up 「用公开中文有害文本数据集」 to a
// running job was answered with the stock 「好，我按这个改。」. A steer is
// answered with the bot's own line; there is no stock steer sentence left.
describe('steer (supplement) acknowledgement', () => {
    const workspace = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'DesktopBotWorkspace.tsx'), 'utf8');
    const hub = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../../../desktop_bot_hub.go'), 'utf8');

    it('has no stock steer sentence in the GUI or the receipt', () => {
        for (const stock of ['好，我按这个改', "Got it, I'll work that in"]) {
            expect(workspace).not.toContain(stock);
            expect(hub).not.toContain(stock);
        }
    });

    it('writes a supplement line with the running job and the new instruction', async () => {
        let seen: Record<string, unknown> = {};
        const line = await desktopBotAck({
            kind: 'supplement', message: '用公开中文有害文本数据集', lang: 'zh-Hans',
            running_task: '在远程服务器上训练有害信息文本二分类模型', instruction: '训练数据改用公开中文有害文本数据集',
        }, async ctx => { seen = ctx as unknown as Record<string, unknown>; return '好，训练那件我改用公开的中文有害文本数据集来做，先去找合适的数据。'; }, DESKTOP_BOT_FIRST_ACK_BUDGET_MS);
        expect(seen.running_task).toContain('训练');
        expect(seen.instruction).toContain('公开中文有害文本数据集');
        expect(desktopBotIsPlaceholderAck(line, 'zh-Hans')).toBe(false);
    });

    it('falls back to a neutral word only when the model fails, so a late line can replace it', async () => {
        const failed = await desktopBotAck({ kind: 'supplement', message: 'x', lang: 'zh-Hans' }, async () => { throw new Error('down'); }, 50);
        expect(desktopBotIsPlaceholderAck(failed, 'zh-Hans')).toBe(true);
        const slow = await desktopBotAck({ kind: 'supplement', message: 'x', lang: 'zh-Hans' }, () => new Promise(r => setTimeout(() => r('late line'), 200)), 20);
        expect(desktopBotIsPlaceholderAck(slow, 'zh-Hans')).toBe(true);
    });

    it('replaces a placeholder in the same bubble with a late line', () => {
        expect(workspace).toContain('one more short try');
        expect(workspace).toMatch(/item\.id === assistantMessage\.id \? \{ \.\.\.item, content: late \}/);
        expect(workspace).toMatch(/item\.id === noteId \? \{ \.\.\.item, content: late \}/);
    });
});

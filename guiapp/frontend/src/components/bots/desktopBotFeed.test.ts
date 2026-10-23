import { describe, expect, it } from 'vitest';
import { desktopBotReportFeedText, desktopBotServerRunText } from './desktopBots';

describe('bot feed text', () => {
    it('shows a report in both chats with its files, even after a server wake took it', () => {
        const out = desktopBotReportFeedText({ from: 'b2', target: 'b1', topic: 'artifact', text: '写好了', wake_run_id: 'run_1', files: [{ name: 'a.md' }] }, [{ id: 'b1', title: 'Bot 1' }, { id: 'b2', title: 'mybod' }]);
        expect(out.received).toContain('mybod 交来文件：写好了');
        expect(out.received).toContain('a.md');
        expect(out.received).toContain('服务器');
        expect(out.sent).toContain('发给 Bot 1');
    });
    it('shows a server run output', () => {
        expect(desktopBotServerRunText('wake', 'run_1', '处理好了', '', 'succeeded')).toContain('处理好了');
        expect(desktopBotServerRunText('delegate', 'run_2', '', 'boom', 'failed')).toContain('boom');
    });
});

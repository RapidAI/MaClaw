// @vitest-environment jsdom
import { fireEvent, render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BotFeedLine, botFeedLineOf, botFeedSummary } from './botFeedLine';
import { desktopBotCrossTargetText, desktopBotHubStartedText, desktopBotReportFeedText, desktopBotServerRunText } from './desktopBots';

const bots = [{ id: 'b1', title: 'Bot 1' }, { id: 'b2', title: 'mybod' }];

describe('bot feed line', () => {
    it('leaves normal chat alone', () => {
        expect(botFeedLineOf('你好，我来看看', 'Bot 1')).toBeNull();
    });
    it('shows a delegate as 交代任务 with a short summary and the raw prompt behind the click', () => {
        const prompt = 'Please open the browser, log in to the dashboard and export the full quarterly report as CSV for every region';
        const line = botFeedLineOf(desktopBotCrossTargetText('delegate', prompt), 'mybod')!;
        expect(line).toMatchObject({ from: '主 Bot', to: 'mybod', kind: '交代任务', alert: false });
        expect(line.summary).not.toContain('Please');
        expect(line.full).toContain(prompt);
    });
    it('hides the run id of a Hub-started run', () => {
        const line = botFeedLineOf(desktopBotHubStartedText('run_abc123'), 'mybod')!;
        expect(line.kind).toBe('开始执行');
        expect(line.summary).not.toContain('run_');
        expect(line.full).toContain('run_abc123');
    });
    it('shows files as chips on both sides of a report', () => {
        const out = desktopBotReportFeedText({ from: 'b2', target: 'b1', topic: 'artifact', text: '写好了', wake_run_id: 'run_1', files: [{ name: 'a.md' }] }, bots);
        const got = botFeedLineOf(out.received, 'Bot 1', { feed: out.feed })!;
        expect(got).toMatchObject({ from: 'mybod', to: 'Bot 1', kind: '交来文件', summary: '写好了', files: ['a.md'] });
        const sent = botFeedLineOf(out.sent, 'mybod')!;
        expect(sent).toMatchObject({ from: 'mybod', to: 'Bot 1', kind: '交来文件', files: ['a.md'] });
        expect(botFeedLineOf(desktopBotReportFeedText({ from: 'b2', target: 'b1', topic: 'done', text: 'ok' }, bots).sent, 'mybod')!.kind).toBe('完成');
    });
    it('keeps help and errors prominent', () => {
        const help = desktopBotReportFeedText({ from: 'b2', target: 'b1', topic: 'blocked', text: '登录要验证码' }, bots);
        expect(botFeedLineOf(help.received, 'Bot 1', { feed: help.feed })).toMatchObject({ kind: '求助', alert: true });
        const err = botFeedLineOf(desktopBotServerRunText('delegate', 'run_2', '', 'boom', 'failed'), 'mybod', { failed: true })!;
        expect(err).toMatchObject({ kind: '出错', alert: true, summary: 'boom' });
        expect(botFeedLineOf(desktopBotServerRunText('wake', 'run_1', '处理好了', '', 'succeeded'), 'mybod')).toMatchObject({ kind: '完成', summary: '处理好了' });
    });
    it('caps the summary at about 60 characters', () => {
        expect([...botFeedSummary('很'.repeat(200))].length).toBeLessThanOrEqual(60);
    });
    it('renders one compact line that expands to the full text', () => {
        const line = botFeedLineOf(desktopBotHubStartedText('run_x9'), 'mybod')!;
        const view = render(<BotFeedLine line={line} />);
        const details = view.getByTestId('desktop-bot-feed') as HTMLDetailsElement;
        expect(details.open).toBe(false);
        expect(view.getByTestId('desktop-bot-feed-summary').textContent).not.toContain('run_x9');
        fireEvent.click(details.querySelector('summary')!);
        expect(view.getByTestId('desktop-bot-feed-full').textContent).toContain('run_x9');
    });
});

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BotCollabStatus } from './BotCollabStatus';
import { BotSaidInline, DesktopBotProcessNote } from './BotSaidInline';

const bots = [{ id: 'm', title: '主管', primary: true }, { id: 'a', title: '小A' }];

describe('BotCollabStatus', () => {
    it('shows the status row, prompts and review results', () => {
        render(<BotCollabStatus bots={bots} assignments={[
            { id: '1', owner_bot: 'm', target_bot: 'a', task: '发帖', state: 'progress', last_report: '写草稿', created_at: '' },
            { id: '2', owner_bot: 'm', target_bot: 'a', task: '登录', state: 'needs_login', created_at: '' },
            { id: '3', owner_bot: 'm', target_bot: 'a', task: '改稿', state: 'completed', review: 'returned', review_note: '标题太长', reviewer: 'm', created_at: '' },
        ]} />);
        expect(screen.getByTestId('bot-collab-row').textContent).toContain('小A 正在做：写草稿');
        expect(screen.getByTestId('bot-collab-prompt-needs_login').textContent).toContain('需要你登录');
        fireEvent.click(screen.getByTestId('bot-collab-row'));
        expect(screen.getByTestId('bot-collab-review-3').textContent).toBe('已退回：标题太长');
    });
    it('counts a bot busy on its own run, not only assignments', () => {
        render(<BotCollabStatus bots={bots} assignments={[
            { id: '3', owner_bot: 'm', target_bot: 'a', task: '改稿', state: 'completed', created_at: '' },
        ]} busy={[{ bot: 'm', phase: 'execute' }]} />);
        expect(screen.getByTestId('bot-collab-row').textContent).toContain('主管 正在做');
        expect(screen.getByTestId('bot-collab-row').textContent).not.toContain('没有机器人在忙');
    });
    it('renders nothing with no assignments', () => {
        const { container } = render(<BotCollabStatus bots={bots} assignments={[]} />);
        expect(container.innerHTML).toBe('');
    });
});

describe('BotSaidInline', () => {
    it('shows speaker name with each message', () => {
        render(<BotSaidInline bots={bots} lookOf={() => ({ color: '#888', shape: 'circle' } as never)} items={[{ id: 's1', from: 'a', target: 'human', kind: 'say', text: '做完了', created_at: '' }]} />);
        const el = screen.getByTestId('bot-said-s1');
        expect(el.textContent).toContain('小A');
        expect(el.textContent).toContain('做完了');
    });
});

describe('DesktopBotProcessNote', () => {
    it('keeps a process message collapsed and out of a bubble', () => {
        const log = '后台任务结束了，下面是最后的输出\nNotOpenSSLWarning\nSome weights newly initialized\ntrain=1 dev=2 device=mps';
        render(<BotSaidInline
            bots={bots}
            lookOf={() => ({ color: '#888', shape: 'circle' } as never)}
            items={[{ id: 'job1', from: 'a', target: 'human', kind: 'say', message_kind: 'process', text: log, created_at: '' }]}
        />);
        const row = screen.getByTestId('desktop-bot-process-job1');
        expect(row.tagName).toBe('DETAILS');
        expect(row.hasAttribute('open')).toBe(false);
        expect(row.querySelector('summary')?.textContent).toBe('后台任务结束了，下面是最后的输出');
        expect(row.textContent).toContain('device=mps');
        expect(screen.getByTestId('bot-said-job1').querySelector('.desktop-bot-chat__bubble')).toBeNull();
        render(<DesktopBotProcessNote id="alone" text={log} />);
        expect(screen.getByTestId('desktop-bot-process-alone').querySelector('.desktop-bot-chat__bubble')).toBeNull();
    });
});

describe('BotSaidInline files', () => {
    it('shows a said note\'s files as chips that open from Hub', async () => {
        const { render, screen, fireEvent } = await import('@testing-library/react');
        const { BotSaidInline } = await import('./BotSaidInline');
        const opened: string[] = [];
        render(<BotSaidInline
            items={[{ id: 'n1', from: 'b1', target: 'human', kind: 'say', text: '训练结束，**报告**见附件', created_at: '', files: [{ name: 'REPORT.md', size: 3, sha256: 'abc' }] }]}
            bots={[{ id: 'b1', title: 'Bot 1' } as any]}
            lookOf={() => ({} as any)}
            onOpenFile={async (note, name) => { opened.push(note + '/' + name); }}
        />);
        fireEvent.click(screen.getByTestId('bot-said-file-n1-REPORT.md'));
        expect(opened).toEqual(['n1/REPORT.md']);
        expect(screen.queryByText(/\*\*报告\*\*/)).toBeNull();
    });
});

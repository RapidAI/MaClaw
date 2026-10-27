import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BotCollabStatus } from './BotCollabStatus';
import { BotSaidInline } from './BotSaidInline';

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

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BotCollabStatus } from './BotCollabStatus';
import { BotSaidInline, saidFileIsImage, saidPictureSource } from './BotSaidInline';

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
    it('shows a png as a picture, not a file chip', async () => {
        const { render, screen, fireEvent } = await import('@testing-library/react');
        const { BotSaidInline } = await import('./BotSaidInline');
        const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
        const opened: string[] = [];
        render(<BotSaidInline
            items={[{ id: 'n2', from: 'b4', target: 'human', kind: 'say', text: '文件：snake-desktop.png（181 KB）', created_at: '', files: [{ name: 'snake-desktop.png', mime: 'image/png', size: 185000, sha256: 'abc' }] }]}
            bots={[{ id: 'b4', title: 'Bot 4' } as any]}
            lookOf={() => ({} as any)}
            loadFile={async () => ({ mime: 'image/png', data: png })}
            onOpenShot={src => { opened.push(src); }}
        />);
        expect(screen.getByText(/文件：snake-desktop.png/)).toBeTruthy();
        const img = await screen.findByTestId('desktop-bot-screenshot');
        expect(screen.queryByText(/文件：/)).toBeNull();
        expect(img.getAttribute('src')).toBe(`data:image/png;base64,${png}`);
        expect(screen.queryByTestId('bot-said-file-n2-snake-desktop.png')).toBeNull();
        fireEvent.click(screen.getByTestId('bot-said-image-n2-snake-desktop.png'));
        expect(opened[0]).toContain('data:image/png;base64,');
    });
    it('keeps the file chip when the picture cannot be loaded', async () => {
        const { render, screen } = await import('@testing-library/react');
        const { BotSaidInline } = await import('./BotSaidInline');
        render(<BotSaidInline
            items={[{ id: 'n3', from: 'b4', target: 'human', kind: 'say', text: '文件：snake-desktop.png（181 KB）', created_at: '', files: [{ name: 'snake-desktop.png', size: 185000, sha256: 'abc' }] }]}
            bots={[{ id: 'b4', title: 'Bot 4' } as any]}
            lookOf={() => ({} as any)}
            loadFile={async () => { throw new Error('no'); }}
        />);
        expect(await screen.findByTestId('bot-said-file-n3-snake-desktop.png')).toBeTruthy();
        expect(screen.getByText(/文件：snake-desktop.png/)).toBeTruthy();
    });
    it('keeps a non-image payload named like a picture as a file chip', () => {
        expect(saidFileIsImage({ name: 'report.png', mime: 'application/pdf' })).toBe(false);
        expect(saidFileIsImage({ name: 'snake-desktop.png' })).toBe(true);
        expect(saidFileIsImage({ name: 'a.jpg', mime: 'image/jpg' })).toBe(true);
        expect(saidFileIsImage({ name: 'notes.md', mime: 'text/plain' })).toBe(false);
        const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
        expect(saidPictureSource('shot.png', { data: png })).toBe(`data:image/png;base64,${png}`);
        expect(saidPictureSource('shot.png', { mime: 'image/jpg', data: png })).toBe(`data:image/jpeg;base64,${png}`);
        expect(saidPictureSource('shot.png', { mime: 'image/png', data: '!!!!' })).toBe('');
    });
});

/** @vitest-environment jsdom */
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { RemoteAssistantActivity } from '../RemoteAssistantActivity';
import { overlayTheme } from '../aiAssistantPanelTheme';
import type { RemoteAssistantSession } from '../../layout/remoteAssistantSession';

function session(overrides: Partial<RemoteAssistantSession> = {}): RemoteAssistantSession {
    return {
        grantId: 'g1',
        machineName: 'DESKTOP-X',
        projectPath: 'C:\\work\\demo',
        taskName: 'demo',
        canSend: true,
        offline: false,
        error: '',
        busy: true,
        messages: [],
        ...overrides,
    };
}

function renderActivity(value: RemoteAssistantSession, lang = 'zh-Hans') {
    return render(<RemoteAssistantActivity session={value} lang={lang} theme={overlayTheme} />);
}

describe('RemoteAssistantActivity', () => {
    it('shows the running label with workflow phase and status as the object', () => {
        renderActivity(session({
            activity: {
                workflow: { status: 'active', phase: 'execute' },
                lastActivityAt: new Date().toISOString(),
                busySince: Date.now() - 5000,
            },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('对方正在执行…');
        expect(screen.getByTestId('assistant-reasoning-object').textContent).toBe('Execute · 进行中');
        expect(screen.getByTestId('assistant-reasoning-panel').getAttribute('data-live')).toBe('true');
        expect(screen.getByTestId('remote-activity-meta').textContent).toContain('对方最近活动');
        expect(screen.getByTestId('remote-activity-meta').textContent).toContain('已用时');
    });

    it('labels a send that is still waiting for the remote to pick it up', () => {
        renderActivity(session({
            activity: { awaitingReply: true, workflow: null },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('已发送，等待对方开始执行…');
    });

    it('labels an in-flight stop instead of "waiting to start"', () => {
        renderActivity(session({
            activity: { awaitingReply: true, stopping: true, workflow: { status: 'active', phase: 'execute' } },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('正在停止对方任务…');
    });

    it('labels a paused remote task instead of a generic running hint', () => {
        renderActivity(session({
            activity: { workflow: { status: 'paused', phase: 'execute' } },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('对方任务已暂停');
    });

    it('keeps a paused or review-needed panel visible after the run idles', () => {
        renderActivity(session({
            busy: false,
            activity: { workflow: { status: 'paused', phase: 'execute' } },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('对方任务已暂停');
        expect(screen.getByTestId('assistant-reasoning-panel').getAttribute('data-live')).toBe('false');
    });

    it('labels a remote task that is waiting for review', () => {
        renderActivity(session({
            activity: { workflow: { status: 'active', phase: 'review', pending_review: true } },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('等待对方处理审核…');
    });

    it('opens the panel body on the newest reasoning tail while busy', () => {
        renderActivity(session({
            activity: { reasoningTail: '正在编译 snake_game.cpp' },
        }));
        expect(screen.getByTestId('remote-activity-thinking').textContent).toBe('正在编译 snake_game.cpp');
    });

    it('keeps produced artifacts visible when the task is idle', () => {
        renderActivity(session({
            busy: false,
            activity: {
                artifacts: [
                    { title: 'snake_game.cpp', updatedAt: new Date().toISOString() },
                    { title: 'build.log' },
                ],
            },
        }));
        expect(screen.queryByTestId('assistant-reasoning-panel')).toBeNull();
        const block = screen.getByTestId('remote-artifacts');
        expect(block.textContent).toContain('对方已产出文件');
        expect(block.textContent).toContain('snake_game.cpp');
        expect(block.textContent).toContain('build.log');
    });

    it('renders nothing while idle with no artifacts', () => {
        const { container } = renderActivity(session({ busy: false, activity: {} }));
        expect(container.firstChild).toBeNull();
    });
});

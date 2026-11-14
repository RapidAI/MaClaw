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

    it('shows a plain live status row without an expandable body', () => {
        renderActivity(session({
            activity: { workflow: { status: 'active', phase: 'execute' } },
        }));
        // Thinking lives in the transcript's 思考过程 blocks and produced
        // files stay on the other computer — the status row does not expand.
        expect(screen.getByTestId('assistant-reasoning-panel').getAttribute('data-live')).toBe('true');
        expect(screen.queryByTestId('assistant-reasoning-toggle-state')).toBeNull();
    });

    it('renders the mirrored workbench checklist with live step states', () => {
        renderActivity(session({
            workbench: {
                steps_revision: 2,
                step_statuses: [
                    { index: 1, title: '分析需求', status: 'passed' },
                    { index: 2, title: '实现功能', status: 'running' },
                    { index: 3, title: '验证', status: 'pending' },
                ],
            },
            activity: { workflow: { status: 'active', phase: 'execute' } },
        }));
        const steps = screen.getByTestId('remote-workbench-steps');
        expect(steps.textContent).toContain('T1 分析需求');
        expect(steps.textContent).toContain('T2 实现功能');
        expect(steps.textContent).toContain('T3 验证');
        expect(steps.textContent).toContain('进行中');
        // The running step also names the header object.
        expect(screen.getByTestId('assistant-reasoning-object').textContent).toContain('T2 实现功能');
    });

    it('flags a mirrored plan that still waits for approval', () => {
        renderActivity(session({
            busy: false,
            workbench: {
                pending_approval: true,
                step_statuses: [{ index: 1, title: '先做这个', status: 'pending' }],
            },
            activity: { workflow: { status: 'active', phase: 'plan' } },
        }));
        expect(screen.getByTestId('remote-workbench-approval')).toBeTruthy();
    });

    it('renders mirrored coding-agent progress through the feed renderer', () => {
        renderActivity(session({
            live: {
                requestId: 'r1',
                displayText: '',
                streamDone: false,
                responded: false,
                baseline: -1,
                lastEventAt: Date.now(),
                parts: [{
                    kind: 'progress',
                    seq: 1,
                    text: 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","title":"读取文件","detail":"read_file","files":["main.go"]}',
                }],
            },
            activity: { workflow: { status: 'active', phase: 'execute' } },
        }));
        expect(screen.getByTestId('coding-agent-progress')).toBeTruthy();
    });

    it('keeps non-coding progress lines as plain trail text', () => {
        renderActivity(session({
            live: {
                requestId: 'r1',
                displayText: '',
                streamDone: false,
                responded: false,
                baseline: -1,
                lastEventAt: Date.now(),
                parts: [{ kind: 'progress', seq: 1, text: '正在安装依赖' }],
            },
            activity: { workflow: { status: 'active', phase: 'execute' } },
        }));
        expect(screen.getByTestId('remote-progress').textContent).toContain('正在安装依赖');
    });

    it('labels a settled checklist as a plan instead of a live run', () => {
        renderActivity(session({
            busy: false,
            workbench: {
                step_statuses: [{ index: 1, title: '已完成的事', status: 'passed' }],
            },
            activity: {},
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('对方任务的工作清单');
        expect(screen.getByTestId('assistant-reasoning-panel').getAttribute('data-live')).toBe('false');
    });

    it('renders nothing while idle', () => {
        const { container } = renderActivity(session({ busy: false, activity: {} }));
        expect(container.firstChild).toBeNull();
    });
});

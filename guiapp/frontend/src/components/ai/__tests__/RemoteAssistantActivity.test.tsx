/** @vitest-environment jsdom */
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { RemoteAssistantActivity, RemoteCodingPlanChecklist } from '../RemoteAssistantActivity';
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

    it('names the cloud as the target instead of the other computer', () => {
        renderActivity(session({
            cloud: true,
            activity: {
                workflow: { status: 'active', phase: 'execute' },
                lastActivityAt: new Date().toISOString(),
            },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('云端正在执行…');
        expect(screen.getByTestId('remote-activity-meta').textContent).toContain('云端最近活动');
    });

    it('says the output is catching up while a cursor hole is open', () => {
        renderActivity(session({
            syncing: true,
            activity: { workflow: { status: 'active', phase: 'execute' } },
            live: {
                requestId: 'r1',
                displayText: '',
                parts: [{ kind: 'text', seq: 1, text: '甲' }],
                streamDone: false,
                responded: false,
                baseline: 0,
                lastEventAt: Date.now(),
            },
        }));
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('正在补齐中断的输出…');
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

    it('names the running step and leaves the list off the message stream', () => {
        const value = session({
            workbench: {
                steps_revision: 2,
                step_statuses: [
                    { index: 1, title: '分析需求', status: 'passed' },
                    { index: 2, title: '实现功能', status: 'running' },
                    { index: 3, title: '验证', status: 'pending' },
                ],
            },
            activity: { workflow: { status: 'active', phase: 'execute' } },
        });
        renderActivity(value);
        expect(screen.queryByTestId('remote-workbench-steps')).toBeNull();
        expect(screen.queryByTestId('coding-agent-plan-checklist')).toBeNull();
        expect(screen.getByTestId('assistant-reasoning-object').textContent).toContain('T2 实现功能');

        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        const checklist = bar.getByTestId('coding-agent-plan-checklist');
        expect(checklist.textContent).toContain('T2');
        expect(checklist.textContent).toContain('1/3');
        expect(bar.queryByTestId('coding-agent-plan-approve')).toBeNull();
        expect(checklist.className).toContain('capc-checklist');
    });

    it('flags a mirrored plan that still waits for approval', () => {
        const value = session({
            busy: false,
            workbench: {
                pending_approval: true,
                step_statuses: [{ index: 1, title: '先做这个', status: 'pending' }],
            },
            activity: { workflow: { status: 'active', phase: 'plan' } },
        });
        renderActivity(value);
        expect(screen.getByTestId('assistant-reasoning-label').textContent).toBe('对方任务等待计划确认…');
        expect(screen.queryByTestId('remote-workbench-approval')).toBeNull();
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.getByTestId('coding-agent-plan-checklist').textContent).toContain('先做这个');
        expect(bar.queryByTestId('coding-agent-plan-approve')).toBeNull();
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

    it('folds a leftover cursor once the remote task is completed or cancelled', () => {
        const completed = session({
            busy: false,
            workbench: {
                requirement_restatement: '继续评估这次的方案。',
                step_statuses: [
                    { index: 1, title: '分析需求', status: 'passed' },
                    { index: 2, title: '实现功能', status: 'running' },
                ],
            },
            activity: { workflow: { status: 'completed', phase: 'execute' } },
        });
        const done = render(<RemoteCodingPlanChecklist session={completed} lang="zh-Hans" theme={overlayTheme} />);
        expect(done.queryByTestId('coding-agent-plan-checklist')).toBeNull();

        const cancelled = session({
            busy: false,
            workbench: { requirement_restatement: '只剩需求理解。', step_statuses: [] },
            activity: { workflow: { status: 'cancelled' } },
        });
        const stopped = render(<RemoteCodingPlanChecklist session={cancelled} lang="zh-Hans" theme={overlayTheme} />);
        expect(stopped.queryByTestId('coding-agent-plan-checklist')).toBeNull();
        expect(stopped.container.textContent || '').not.toContain('只剩需求理解');
    });

    it('keeps the step live while the transcript is still loading', () => {
        const value = session({
            busy: true,
            loading: true,
            workbench: { step_statuses: [{ index: 2, title: '实现功能', status: 'running' }] },
            activity: { workflow: { status: 'active', phase: 'execute' } },
        });
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.container.querySelector('[data-testid="coding-agent-plan-current"]')?.textContent).toContain('进行中');
        expect(bar.container.querySelector('.capc-pulse-dot')).toBeTruthy();
        const activity = render(<RemoteAssistantActivity session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(activity.container.querySelector('[data-testid="remote-activity"]')).toBeNull();
    });

    it('keeps the checklist while the other computer is still working', () => {
        const value = session({
            busy: true,
            workbench: { step_statuses: [{ index: 1, title: '实现功能', status: 'running' }] },
            activity: { workflow: { status: 'completed', phase: 'execute' } },
        });
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.getByTestId('coding-agent-plan-current').textContent).toContain('进行中');
        expect(bar.container.querySelector('.capc-pulse-dot')).toBeTruthy();
    });

    it('keeps a failed step visible after the remote task is completed', () => {
        const value = session({
            busy: false,
            workbench: {
                step_statuses: [
                    { index: 1, title: '实现功能', status: 'failed' },
                    { index: 2, title: '验证', status: 'pending' },
                ],
            },
            activity: { workflow: { status: 'completed' } },
        });
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.getByTestId('coding-agent-plan-checklist').textContent).toContain('实现功能');
    });

    it('keeps an unstarted plan when approval is still open', () => {
        const value = session({
            busy: false,
            workbench: {
                pending_approval: true,
                step_statuses: [{ index: 1, title: '先做这个', status: 'pending' }],
            },
            activity: { workflow: { status: 'completed', phase: 'plan' } },
        });
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.getByTestId('coding-agent-plan-checklist').textContent).toContain('先做这个');
        expect(bar.queryByTestId('coding-agent-plan-approve')).toBeNull();
    });

    it('names the cursor as stopping while a remote stop is in flight', () => {
        const value = session({
            busy: true,
            live: {
                requestId: 'r1',
                displayText: '',
                parts: [{ kind: 'text', seq: 1, text: '还在输出' }],
                streamDone: false,
                responded: false,
                baseline: 0,
                lastEventAt: Date.now(),
            },
            workbench: { step_statuses: [{ index: 2, title: '实现功能', status: 'running' }] },
            activity: { stopping: true, workflow: { status: 'active', phase: 'execute' } },
        });
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.container.querySelector('[data-testid="coding-agent-plan-current"]')?.textContent).toContain('正在停止');
        expect(bar.container.querySelector('.capc-pulse-dot')).toBeNull();

        // The task row can already say cancelled while the stop flag is still
        // set. The cursor stays on "正在停止" until that flag drops.
        const cancelling = session({
            busy: true,
            workbench: { step_statuses: [{ index: 2, title: '实现功能', status: 'running' }] },
            activity: { stopping: true, workflow: { status: 'cancelled', phase: 'execute' } },
        });
        const stillStopping = render(<RemoteCodingPlanChecklist session={cancelling} lang="zh-Hans" theme={overlayTheme} />);
        expect(stillStopping.container.querySelector('[data-testid="coding-agent-plan-current"]')?.textContent).toContain('正在停止');
        expect(stillStopping.container.querySelector('.capc-pulse-dot')).toBeNull();
    });

    it('names a paused or interrupted cursor with that outcome', () => {
        const paused = session({
            busy: false,
            workbench: { step_statuses: [{ index: 2, title: '实现功能', status: 'running' }] },
            activity: { workflow: { status: 'paused', phase: 'execute' } },
        });
        const pausedBar = render(<RemoteCodingPlanChecklist session={paused} lang="zh-Hans" theme={overlayTheme} />);
        expect(pausedBar.container.querySelector('[data-testid="coding-agent-plan-current"]')?.textContent).toContain('已暂停');
        expect(pausedBar.container.querySelector('.capc-pulse-dot')).toBeNull();

        const interrupted = session({
            busy: false,
            workbench: { step_statuses: [{ index: 2, title: '实现功能', status: 'running' }] },
            activity: { workflow: { status: 'interrupted', phase: 'execute' } },
        });
        const interruptedBar = render(<RemoteCodingPlanChecklist session={interrupted} lang="zh-Hans" theme={overlayTheme} />);
        expect(interruptedBar.container.querySelector('[data-testid="coding-agent-plan-current"]')?.textContent).toContain('已中断');
        expect(interruptedBar.container.querySelector('.capc-pulse-dot')).toBeNull();
    });

    it('does not pin a finished checklist over the composer or the transcript', () => {
        const value = session({
            busy: false,
            workbench: {
                step_statuses: [{ index: 1, title: '已完成的事', status: 'passed' }],
            },
            activity: {},
        });
        const activity = renderActivity(value);
        expect(activity.container.firstChild).toBeNull();
        const bar = render(<RemoteCodingPlanChecklist session={value} lang="zh-Hans" theme={overlayTheme} />);
        expect(bar.queryByTestId('coding-agent-plan-checklist')).toBeNull();
    });

    it('renders nothing while idle', () => {
        const { container } = renderActivity(session({ busy: false, activity: {} }));
        expect(container.firstChild).toBeNull();
    });
});

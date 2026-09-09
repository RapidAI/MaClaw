// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RemoteSessionList } from '../RemoteSessionList';

vi.mock('../../layout/backgroundTaskCount', () => ({ countActiveBackgroundLoops: () => 0 }));
vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListBackgroundLoops: vi.fn().mockResolvedValue([]),
    StopBackgroundLoop: vi.fn(),
    StopAllBackgroundLoops: vi.fn(),
    StopAllBackgroundTasks: vi.fn(),
    DismissRemoteSession: vi.fn(),
    ContinueBackgroundLoop: vi.fn(),
    GetBackgroundLoopOutput: vi.fn().mockResolvedValue([]),
}));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(), EventsOff: vi.fn() }));

describe('RemoteSessionList initial tab', () => {
    it('opens the background task tab when requested by another surface', () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            interruptRemoteSession: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            translate: (key: string) => key,
            formatText: (key: string) => key,
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(
            <RemoteSessionList
                initialSessionTab="background"
                {...props}
            />,
        );

        const tabList = screen.getByRole('tablist', { name: '任务监控视图' });
        const backgroundTab = screen.getByRole('tab', { name: '后台' });
        expect(tabList.getAttribute('aria-orientation')).toBe('horizontal');
        expect(backgroundTab.getAttribute('aria-selected')).toBe('true');
        expect(backgroundTab.getAttribute('aria-controls')).toBe('remote-session-panel-background');
        expect((backgroundTab as HTMLElement).style.fontWeight).toBe('700');
        expect(screen.getAllByRole('tab')).toHaveLength(4);
        expect(document.querySelector('.remote-session-tab-items')).not.toBeNull();
        expect(document.querySelector('.remote-session-panel--background')).not.toBeNull();
        expect(screen.getByRole('status').textContent).toContain('当前没有运行中的后台任务');

        fireEvent.keyDown(backgroundTab, { key: 'ArrowLeft' });
        expect(screen.getByRole('tab', { name: '远程' }).getAttribute('aria-selected')).toBe('true');
        expect(document.querySelector('.remote-session-panel--remote')).not.toBeNull();
        expect(screen.getByRole('status').textContent).toContain('当前没有运行中的远程实例');
        expect(document.querySelector('.remote-monitor-empty-state--remote')).not.toBeNull();
    });

    it('does not expose an empty inline preview action for a session without output', () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [{
                id: "session-empty-preview",
                tool: "ssh",
                title: "空输出会话",
                project_path: "C:\\workspace",
                status: "running",
                execution_mode: "sdk",
                raw_output_lines: [],
            }],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            interruptRemoteSession: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            translate: (key: string) => key,
            formatText: (key: string) => key,
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(<RemoteSessionList {...props} />);

        const preview = screen.getByTitle('展开预览') as HTMLButtonElement;
        expect(preview.disabled).toBe(true);
        expect(screen.queryByText('$ _')).toBeNull();
    });
});

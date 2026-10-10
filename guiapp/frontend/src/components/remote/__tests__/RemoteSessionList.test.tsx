// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { RemoteSessionList } from '../RemoteSessionList';
import { DesktopBotAccess, ListDesktopBots } from '../../../../wailsjs/go/main/App';
import { saveBotMessages } from '../../bots/desktopBots';
import { DialogProvider } from '../../CustomDialog';

vi.mock('../../layout/backgroundTaskCount', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../layout/backgroundTaskCount')>();
    return { ...actual, countActiveBackgroundLoops: () => 0 };
});
vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListBackgroundLoops: vi.fn().mockResolvedValue([]),
    StopBackgroundLoop: vi.fn(),
    StopAllBackgroundLoops: vi.fn(),
    StopAllBackgroundTasks: vi.fn(),
    DismissRemoteSession: vi.fn(),
    ContinueBackgroundLoop: vi.fn(),
    GetBackgroundLoopOutput: vi.fn().mockResolvedValue([]),
    DesktopBotAccess: vi.fn().mockResolvedValue({ enabled: false }),
    ListDesktopBots: vi.fn().mockResolvedValue([]),
}));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(), EventsOff: vi.fn() }));

beforeEach(() => {
    vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: false });
    vi.mocked(ListDesktopBots).mockResolvedValue([]);
    localStorage.clear();
});

describe('RemoteSessionList initial tab', () => {
    it('opens the background task tab when requested by another surface', async () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(
            <RemoteSessionList
                initialSessionTab="background"
                {...props}
            />,
        );
        await waitFor(() => {
            expect(screen.getByRole('tab', { name: '后台' })).toBeTruthy();
        });

        const tabList = screen.getByRole('tablist', { name: '任务监控视图' });
        const backgroundTab = screen.getByRole('tab', { name: '后台' });
        expect(tabList.getAttribute('aria-orientation')).toBe('horizontal');
        expect(backgroundTab.getAttribute('aria-selected')).toBe('true');
        expect(backgroundTab.getAttribute('aria-controls')).toBe('remote-session-panel-background');
        expect((backgroundTab as HTMLElement).style.fontWeight).toBe('700');
        expect(screen.getAllByRole('tab')).toHaveLength(3);
        expect(screen.queryByRole('tab', { name: '远程' })).toBeNull();
        expect(document.querySelector('.remote-session-tab-items')).not.toBeNull();
        expect(document.querySelector('.remote-session-panel--background')).not.toBeNull();
        expect(screen.getByRole('status').textContent).toContain('当前没有运行中的后台任务');

        fireEvent.keyDown(backgroundTab, { key: 'ArrowRight' });
        expect(screen.getByRole('tab', { name: '计划任务' }).getAttribute('aria-selected')).toBe('true');
    });

    it('treats the retired remote tab as the background monitor', async () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(
            <RemoteSessionList
                initialSessionTab="remote"
                {...props}
            />,
        );
        await waitFor(() => {
            expect(screen.getByRole('tab', { name: '后台' }).getAttribute('aria-selected')).toBe('true');
        });
        expect(screen.queryByRole('tab', { name: '远程' })).toBeNull();
        expect(document.querySelector('.remote-session-panel--remote')).toBeNull();
    });

    it('does not expose an empty inline preview action for a session without output', async () => {
        const props = {
            lang: "zh-Hans",
            remoteSessions: [{
                id: "session-empty-preview",
                tool: "ssh",
                title: "空输出会话",
                project_path: "C:\\workspace",
                status: "running",
                launch_source: "ai",
                execution_mode: "sdk",
                raw_output_lines: [],
            }],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            localizeText: (en: string, zhHans: string) => zhHans || en,
        };
        render(<RemoteSessionList {...props} />);
        await waitFor(() => {
            expect(screen.getByTitle('展开预览')).toBeTruthy();
        });

        const preview = screen.getByTitle('展开预览') as HTMLButtonElement;
        expect(preview.disabled).toBe(true);
        expect(screen.queryByText('$ _')).toBeNull();
        expect(screen.queryByText('来源')).toBeNull();
        expect(screen.queryByText('远程')).toBeNull();
    });

    it('lists in-progress bot tasks when this account can use bots', async () => {
        vi.mocked(DesktopBotAccess).mockResolvedValue({ enabled: true });
        vi.mocked(ListDesktopBots).mockClear();
        vi.mocked(ListDesktopBots).mockResolvedValue([
            { id: 'bot_1', title: '码卡龙本龙', description: '', instance_id: '', created_at: '' },
        ]);
        const started = Date.now();
        saveBotMessages('alice', 'bot_1', [
            { id: `m-${started.toString(36)}-user`, role: 'user', content: '看看北京天气', phase: 'execute' },
            { id: `m-${started.toString(36)}-ack`, role: 'assistant', content: '收到', ack: true },
            { id: `m-${started.toString(36)}-run`, role: 'assistant', content: '', pending: true, phase: 'execute' },
        ]);
        const props = {
            lang: 'zh-Hans',
            remoteSessions: [],
            remoteInputDrafts: {},
            setRemoteInputDrafts: vi.fn(),
            killRemoteSession: vi.fn(),
            refreshSessionsOnly: vi.fn(),
            showToastMessage: vi.fn(),
            localizeText: (en: string, zhHans: string) => zhHans || en,
            botUserId: 'alice',
        };
        render(<DialogProvider><RemoteSessionList {...props} /></DialogProvider>);

        const botTab = await screen.findByRole('tab', { name: /Bot任务/ });
        expect(screen.getAllByRole('tab')).toHaveLength(4);
        expect(vi.mocked(ListDesktopBots)).not.toHaveBeenCalled();
        fireEvent.click(botTab);
        expect(botTab.getAttribute('aria-selected')).toBe('true');
        expect(await screen.findByText('码卡龙本龙')).toBeTruthy();
        expect(screen.getByText('看看北京天气')).toBeTruthy();
        expect(screen.getByText('进行中')).toBeTruthy();
        expect(screen.getByText('执行')).toBeTruthy();

        fireEvent.keyDown(botTab, { key: 'ArrowLeft' });
        expect(screen.getByRole('tab', { name: '直通任务' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tab', { name: /Bot任务/ })).toBeTruthy();
    });
});

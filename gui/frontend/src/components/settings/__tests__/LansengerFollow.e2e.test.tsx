/**
 * End-to-end style integration tests for:
 * Settings → IM → 蓝信 → 关注 (Follow / watch people)
 *
 * Uses vitest + testing-library (project has no Playwright browser suite).
 * Exercises real LansengerSettings + UtilitiesWatchPanel with mocked Wails APIs.
 */
// @vitest-environment jsdom
import { useState } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LansengerSettings } from '../LansengerSettings';
import { IMSettingsPanel } from '../IMSettingsPanel';
import type { IMSubTab } from '../IMSubTabs';
import { UtilitiesPage } from '../../pages/UtilitiesPage';

type WatchJob = {
    id?: string;
    name: string;
    enabled: boolean;
    group_id: string;
    group_name?: string;
    target_staff_ids: string[];
    target_names?: Record<string, string>;
    record_all: boolean;
    keyword_scope?: string;
    forward_on_target_speech?: boolean;
    forward_channels?: string[];
    keywords: Array<{ keywords: string[]; record_on_match: boolean; reply_text?: string }>;
};

const jobsStore: WatchJob[] = [];

const ListLansengerGroupsMock = vi.fn();
const ListLansengerWatchJobsMock = vi.fn();
const UpsertLansengerWatchJobMock = vi.fn();
const DeleteLansengerWatchJobMock = vi.fn();
const ListLansengerWatchRosterMock = vi.fn();
const ListLansengerWatchChannelsMock = vi.fn();
const GetLansengerWatchStorePathMock = vi.fn();
const ListLansengerWatchTranscriptsMock = vi.fn();
const AddLansengerWatchMemberMock = vi.fn();
const RestartLansengerMock = vi.fn();
const SetLansengerLocalModeMock = vi.fn();
const LoadConfigMock = vi.fn();
const SetLansengerGroupIgnoredMock = vi.fn();
const SetLansengerGroupAllowedMock = vi.fn();
const KnowledgeListSourcesMock = vi.fn();
const SelectVEAllowedDirectoryMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    RestartLansenger: (...args: unknown[]) => RestartLansengerMock(...args),
    SetLansengerLocalMode: (...args: unknown[]) => SetLansengerLocalModeMock(...args),
    ListLansengerGroups: (...args: unknown[]) => ListLansengerGroupsMock(...args),
    SetLansengerGroupIgnored: (...args: unknown[]) => SetLansengerGroupIgnoredMock(...args),
    SetLansengerGroupAllowed: (...args: unknown[]) => SetLansengerGroupAllowedMock(...args),
    KnowledgeListSources: (...args: unknown[]) => KnowledgeListSourcesMock(...args),
    SelectVEAllowedDirectory: (...args: unknown[]) => SelectVEAllowedDirectoryMock(...args),
    ListLansengerWatchJobs: (...args: unknown[]) => ListLansengerWatchJobsMock(...args),
    ListLansengerWatchJobsForBot: (_botProfileId: string) => ListLansengerWatchJobsMock(),
    UpsertLansengerWatchJob: (...args: unknown[]) => UpsertLansengerWatchJobMock(...args),
    UpsertLansengerWatchJobForBot: (_botProfileId: string, raw: string) => UpsertLansengerWatchJobMock(raw),
    DeleteLansengerWatchJob: (...args: unknown[]) => DeleteLansengerWatchJobMock(...args),
    DeleteLansengerWatchJobForBot: (_botProfileId: string, id: string) => DeleteLansengerWatchJobMock(id),
    ListLansengerWatchRoster: (...args: unknown[]) => ListLansengerWatchRosterMock(...args),
    ListLansengerWatchRosterForBot: (_botProfileId: string, groupId: string, query: string) => ListLansengerWatchRosterMock(groupId, query),
    ListLansengerWatchChannels: (...args: unknown[]) => ListLansengerWatchChannelsMock(...args),
    ListLansengerWatchChannelsForBot: (_botProfileId: string) => ListLansengerWatchChannelsMock(),
    ListLansengerWatchForwardResults: vi.fn(async () => JSON.stringify([])),
    ListLansengerWatchForwardResultsForBot: (_botProfileId: string) => JSON.stringify([]),
    TestLansengerWatchForward: vi.fn(async () => undefined),
    TestLansengerWatchForwardForBot: (_botProfileId: string, _channel: string) => undefined,
    GetLansengerWatchStorePath: (...args: unknown[]) => GetLansengerWatchStorePathMock(...args),
    ListLansengerWatchTranscripts: (...args: unknown[]) => ListLansengerWatchTranscriptsMock(...args),
    AddLansengerWatchMember: (...args: unknown[]) => AddLansengerWatchMemberMock(...args),
    AddLansengerWatchMemberForBot: (_botProfileId: string, groupId: string, staffId: string, name: string) => AddLansengerWatchMemberMock(groupId, staffId, name),
    ListSurveys: vi.fn(async () => JSON.stringify({ surveys: [] })),
    RestartQQBot: vi.fn(async () => 'disconnected'),
    SetQQBotLocalMode: vi.fn(async () => undefined),
    RestartTelegramBot: vi.fn(async () => 'disconnected'),
    SetTelegramLocalMode: vi.fn(async () => undefined),
    StopWeixin: vi.fn(),
    SetWeixinLocalMode: vi.fn(async () => undefined),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
    EventsOff: () => {},
    BrowserOpenURL: vi.fn(),
}));

vi.mock('../../remote/IMAuditPanel', () => ({
    IMAuditPanel: ({ platform, onClose }: { platform: string; onClose: () => void }) => (
        <div data-testid="im-audit-panel" data-platform={platform}>
            <button type="button" onClick={onClose}>
                close-audit
            </button>
        </div>
    ),
}));

function lansengerBaseProps(overrides: Record<string, unknown> = {}) {
    return {
        config: {
            lansenger_enabled: true,
            lansenger_app_id: 'app-id',
            lansenger_app_secret: 'secret',
            lansenger_gateway_url: 'https://apigw.lx.qianxin.com',
            im_progress_nudge_enabled: true,
        } as any,
        setConfig: vi.fn(),
        lang: 'zh-Hans',
        saveRemoteConfigField: vi.fn(),
        lansengerStatus: 'connected',
        setLansengerStatus: vi.fn(),
        lansengerLocalMode: true,
        setLansengerLocalModeState: vi.fn(),
        setIMAuditPlatform: vi.fn(),
        ...overrides,
    };
}

function seedWatchMocks() {
	jobsStore.length = 0;
	KnowledgeListSourcesMock.mockResolvedValue([]);
    ListLansengerGroupsMock.mockResolvedValue({
        total: 1,
        groups: [{ group_id: 'g1', name: '产品群', total_members: 3 }],
    });
    ListLansengerWatchJobsMock.mockImplementation(async () => JSON.stringify([...jobsStore]));
    UpsertLansengerWatchJobMock.mockImplementation(async (raw: string) => {
        const job = typeof raw === 'string' ? JSON.parse(raw) : raw;
        if (!job.id) job.id = `job-${jobsStore.length + 1}`;
        const idx = jobsStore.findIndex((j) => j.id === job.id);
        if (idx >= 0) jobsStore[idx] = job;
        else jobsStore.push(job);
        return JSON.stringify(job);
    });
    DeleteLansengerWatchJobMock.mockImplementation(async (id: string) => {
        const idx = jobsStore.findIndex((j) => j.id === id);
        if (idx >= 0) jobsStore.splice(idx, 1);
    });
    ListLansengerWatchRosterMock.mockResolvedValue(
        JSON.stringify({
            members: [
                { staff_id: 'u1', name: '张三' },
                { staff_id: 'u2', name: '李四' },
            ],
        }),
    );
    ListLansengerWatchChannelsMock.mockResolvedValue(
        JSON.stringify([
            { id: 'lansenger', label: '蓝信', online: true },
            { id: 'weixin', label: '微信', online: false },
        ]),
    );
    GetLansengerWatchStorePathMock.mockResolvedValue('C:\\data\\watch');
    ListLansengerWatchTranscriptsMock.mockResolvedValue(JSON.stringify([]));
    AddLansengerWatchMemberMock.mockResolvedValue(undefined);
    RestartLansengerMock.mockResolvedValue('connected');
    SetLansengerLocalModeMock.mockResolvedValue(undefined);
    LoadConfigMock.mockResolvedValue({ lansenger_enabled: true });
}

async function openFollowPanel() {
    render(<LansengerSettings {...lansengerBaseProps()} />);
    fireEvent.click(screen.getByTestId('lansenger-follow-button'));
    return screen.findByTestId('watch-page');
}

describe('Lansenger Follow e2e', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        seedWatchMocks();
        document.body.style.overflow = '';
    });

    it('hides Follow when disconnected and shows it after chat history when connected', () => {
        const { rerender } = render(
            <LansengerSettings {...lansengerBaseProps({ lansengerStatus: 'disconnected' })} />,
        );
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        expect(screen.getByRole('button', { name: '聊天历史' })).toBeTruthy();

        rerender(<LansengerSettings {...lansengerBaseProps({ lansengerStatus: 'connected' })} />);
        const follow = screen.getByTestId('lansenger-follow-button');
        const watchBtn = screen.getByRole('button', { name: '聊天历史' });
        expect(follow.textContent).toBe('盯人');
        expect(watchBtn.compareDocumentPosition(follow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('opens Follow dialog, creates a job, saves via backend APIs', async () => {
        const page = await openFollowPanel();
        expect(page.classList.contains('utilities-page--compact')).toBe(true);

        // Dialog shell from LansengerSettings
        expect(screen.getByRole('dialog', { name: '盯人' })).toBeTruthy();
        expect(document.body.style.overflow).toBe('hidden');

        // Wait for lazy panel + initial load
        await waitFor(() => {
            expect(ListLansengerWatchJobsMock).toHaveBeenCalled();
            expect(ListLansengerGroupsMock).toHaveBeenCalled();
        });

        fireEvent.click(within(page).getByRole('button', { name: '新建任务' }));
        await waitFor(() => {
            expect(within(page).getByDisplayValue('盯人任务')).toBeTruthy();
        });

        // Name
        const nameInput = within(page).getByDisplayValue('盯人任务');
        fireEvent.change(nameInput, { target: { value: '盯产品同学' } });

        // Select group (first combobox is 蓝信群; second is keyword scope)
        const groupSelect = within(page).getAllByRole('combobox')[0] as HTMLSelectElement;
        fireEvent.change(groupSelect, { target: { value: 'g1' } });

        // Roster is debounced 250ms
        await waitFor(
            () => {
                expect(ListLansengerWatchRosterMock).toHaveBeenCalled();
                expect(within(page).getByText('张三')).toBeTruthy();
            },
            { timeout: 2000 },
        );

        fireEvent.click(within(page).getByText('张三'));

        fireEvent.click(within(page).getByRole('button', { name: '保存' }));

        await waitFor(() => {
            expect(UpsertLansengerWatchJobMock).toHaveBeenCalled();
        });

        const savedPayload = JSON.parse(UpsertLansengerWatchJobMock.mock.calls[0][0] as string);
        expect(savedPayload.name).toBe('盯产品同学');
        expect(savedPayload.group_id).toBe('g1');
        expect(savedPayload.target_staff_ids).toContain('u1');
        expect(jobsStore).toHaveLength(1);

        await waitFor(() => {
            expect(within(page).getByText('已保存')).toBeTruthy();
            // Job appears in list after reload
            expect(within(page).getByDisplayValue('盯产品同学')).toBeTruthy();
        });
    });

    it('closes Follow via Escape and restores body scroll', async () => {
        await openFollowPanel();
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
        expect(document.body.style.overflow).toBe('hidden');

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
        expect(document.body.style.overflow).toBe('');
    });

    it('closes Follow when Lansenger disconnects mid-session', async () => {
        const { rerender } = render(<LansengerSettings {...lansengerBaseProps()} />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        rerender(<LansengerSettings {...lansengerBaseProps({ lansengerStatus: 'disconnected' })} />);
        await waitFor(() => {
            expect(screen.queryByTestId('watch-page')).toBeNull();
            expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        });
    });


    it('does not open Follow on Escape during IME composition', async () => {
        await openFollowPanel();
        fireEvent.keyDown(window, { key: 'Escape', isComposing: true });
        expect(screen.getByTestId('watch-page')).toBeTruthy();
        fireEvent.keyDown(window, { key: 'Escape', keyCode: 229 });
        expect(screen.getByTestId('watch-page')).toBeTruthy();
    });

    it('hides manual add until a group is selected', async () => {
        const page = await openFollowPanel();
        fireEvent.click(within(page).getByRole('button', { name: '新建任务' }));
        await waitFor(() => expect(within(page).getByDisplayValue('盯人任务')).toBeTruthy());
        expect(within(page).queryByRole('button', { name: '手动添加 staffId' })).toBeNull();
    });

    it('expands manual add and appends a target by staff ID', async () => {
        const page = await openFollowPanel();
        fireEvent.click(within(page).getByRole('button', { name: '新建任务' }));
        await waitFor(() => expect(within(page).getByDisplayValue('盯人任务')).toBeTruthy());

        fireEvent.change(within(page).getAllByRole('combobox')[0], { target: { value: 'g1' } });
        await waitFor(
            () => expect(within(page).getByText('张三')).toBeTruthy(),
            { timeout: 2000 },
        );

        fireEvent.click(within(page).getByRole('button', { name: '手动添加 staffId' }));
        fireEvent.change(within(page).getByPlaceholderText('成员 staffId'), { target: { value: 'u9' } });
        fireEvent.change(within(page).getByPlaceholderText('姓名（可选）'), { target: { value: '王五' } });
        fireEvent.click(within(page).getByRole('button', { name: '添加盯人对象' }));

        await waitFor(() => expect(AddLansengerWatchMemberMock).toHaveBeenCalledWith('g1', 'u9', '王五'));
        await waitFor(() => expect(within(page).getByTitle('u9')).toBeTruthy());
    });

    it('shows CLI options only after a CLI command is entered', async () => {
        const page = await openFollowPanel();
        fireEvent.click(within(page).getByRole('button', { name: '新建任务' }));
        await waitFor(() => expect(within(page).getByDisplayValue('盯人任务')).toBeTruthy());

        expect(within(page).queryByText('用 CLI 标准输出作为回复')).toBeNull();

        const cli = within(page).getByPlaceholderText('例: python C:\\hooks\\watch.py --who={{speaker_id}}');
        fireEvent.change(cli, { target: { value: 'python watch.py --who={{speaker_id}}' } });

        expect(within(page).getByText('用 CLI 标准输出作为回复')).toBeTruthy();
        expect(within(page).getByText(/LANXIN_WATCH_\*/)).toBeTruthy();
    });
});

describe('IM settings → 蓝信 → 关注 e2e', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        seedWatchMocks();
    });

    function IMHarness({
        showLansenger,
        lansengerStatus,
        initialTab = 'qq' as IMSubTab,
    }: {
        showLansenger: boolean;
        lansengerStatus: string;
        initialTab?: IMSubTab;
    }) {
        const [imSubTab, setImSubTab] = useState<IMSubTab>(initialTab);
        const [imAuditPlatform, setIMAuditPlatform] = useState<string | null>(null);
        return (
            <IMSettingsPanel
                config={
                    {
                        lansenger_enabled: true,
                        lansenger_app_id: 'app',
                        lansenger_app_secret: 'sec',
                        im_progress_nudge_enabled: true,
                    } as any
                }
                setConfig={vi.fn()}
                lang="zh-Hans"
                imSubTab={imSubTab}
                setImSubTab={setImSubTab}
                imAuditPlatform={imAuditPlatform}
                setIMAuditPlatform={setIMAuditPlatform}
                saveRemoteConfigField={vi.fn()}
                showToastMessage={vi.fn()}
                qqBotStatus="disconnected"
                setQQBotStatus={vi.fn()}
                qqBotLocalMode
                setQQBotLocalModeState={vi.fn()}
                telegramStatus="disconnected"
                setTelegramStatus={vi.fn()}
                telegramLocalMode
                setTelegramLocalModeState={vi.fn()}
                weixinStatus="disconnected"
                setWeixinStatus={vi.fn()}
                weixinLocalMode
                setWeixinLocalModeState={vi.fn()}
                thirdPartyGatewayStatus="disconnected"
                setThirdPartyGatewayStatus={vi.fn()}
                thirdPartyGatewayLocalMode
                setThirdPartyGatewayLocalModeState={vi.fn()}
                showLansenger={showLansenger}
                lansengerStatus={lansengerStatus}
                setLansengerStatus={vi.fn()}
                lansengerLocalMode
                setLansengerLocalModeState={vi.fn()}
                weixinQRCode=""
                setWeixinQRCode={vi.fn()}
                weixinQRLoading={false}
                setWeixinQRLoading={vi.fn()}
                weixinQRWaiting={false}
                setWeixinQRWaiting={vi.fn()}
                weixinQRError=""
                setWeixinQRError={vi.fn()}
                qqBotQRCode=""
                setQQBotQRCode={vi.fn()}
                qqBotQRLoading={false}
                setQQBotQRLoading={vi.fn()}
                qqBotQRWaiting={false}
                setQQBotQRWaiting={vi.fn()}
                qqBotQRError=""
                setQQBotQRError={vi.fn()}
            />
        );
    }

    it('does not show 蓝信 tab when showLansenger is false (MaClaw)', () => {
        render(<IMHarness showLansenger={false} lansengerStatus="connected" />);
        expect(screen.queryByRole('tab', { name: '蓝信' })).toBeNull();
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
    });

    it('navigates IM → 蓝信 → 关注 when Lansenger is connected (TigerClaw)', async () => {
        render(<IMHarness showLansenger lansengerStatus="connected" initialTab="qq" />);

        fireEvent.click(screen.getByRole('tab', { name: '蓝信' }));
        expect(screen.getByTestId('lansenger-follow-button')).toBeTruthy();

        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        const page = await screen.findByTestId('watch-page');
        expect(page).toBeTruthy();
        expect(screen.getByRole('dialog', { name: '盯人' })).toBeTruthy();

        await waitFor(() => expect(ListLansengerWatchJobsMock).toHaveBeenCalled());
    });

    it('shows 蓝信 but hides Follow when not connected', () => {
        render(<IMHarness showLansenger lansengerStatus="disconnected" initialTab="lansenger" />);
        expect(screen.getByRole('tab', { name: '蓝信' })).toBeTruthy();
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
    });
});

describe('Utilities page no longer hosts Follow/盯人', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('home grid has survey and meeting cards — no watch card or entry', async () => {
        render(<UtilitiesPage lang="zh-Hans" />);
        expect(screen.getByTestId('utilities-page')).toBeTruthy();
        expect(screen.getByTestId('utilities-survey-card')).toBeTruthy();
        const meetingCard = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        expect(meetingCard).toBeTruthy();
        expect(meetingCard.disabled).toBe(true); // no handler in this isolation render
        expect(screen.getByText('会议记录')).toBeTruthy();
        expect(screen.queryByTestId('utilities-watch-card')).toBeNull();
        expect(screen.queryByText('盯人')).toBeNull();
        expect(screen.queryByText('关注')).toBeNull();
        expect(screen.queryByTestId('watch-page')).toBeNull();
    });

    it('meeting card invokes onStartMeetingRecord', async () => {
        const onStartMeetingRecord = vi.fn().mockResolvedValue(undefined);
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        fireEvent.click(screen.getByTestId('utilities-meeting-card'));
        await waitFor(() => expect(onStartMeetingRecord).toHaveBeenCalledTimes(1));
    });

    it('meeting card ignores double-clicks while starting', async () => {
        let resolveStart: (() => void) | undefined;
        const onStartMeetingRecord = vi.fn(
            () => new Promise<void>((resolve) => { resolveStart = resolve; }),
        );
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        const card = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        fireEvent.click(card);
        fireEvent.click(card);
        fireEvent.click(card);
        expect(onStartMeetingRecord).toHaveBeenCalledTimes(1);
        expect(card.disabled).toBe(true);
        expect(card.getAttribute('aria-busy')).toBe('true');
        expect(screen.getByText('启动中…')).toBeTruthy();
        resolveStart?.();
        await waitFor(() => expect(card.disabled).toBe(false));
        expect(card.getAttribute('aria-busy')).toBeNull();
        expect(screen.getByText('开始')).toBeTruthy();
    });

    it('meeting card re-enables after failure', async () => {
        const onStartMeetingRecord = vi.fn().mockRejectedValue(new Error('create failed'));
        render(<UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />);
        const card = screen.getByTestId('utilities-meeting-card') as HTMLButtonElement;
        fireEvent.click(card);
        await waitFor(() => expect(onStartMeetingRecord).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(card.disabled).toBe(false));
    });

    it('meeting card skips setState after unmount (navigate-away mid-start)', async () => {
        let resolveStart: (() => void) | undefined;
        const onStartMeetingRecord = vi.fn(
            () => new Promise<void>((resolve) => { resolveStart = resolve; }),
        );
        const { unmount } = render(
            <UtilitiesPage lang="zh-Hans" onStartMeetingRecord={onStartMeetingRecord} />,
        );
        fireEvent.click(screen.getByTestId('utilities-meeting-card'));
        expect(onStartMeetingRecord).toHaveBeenCalledTimes(1);
        unmount();
        // Resolving after unmount must not throw (mountedRef guards setState).
        resolveStart?.();
        await Promise.resolve();
    });
});

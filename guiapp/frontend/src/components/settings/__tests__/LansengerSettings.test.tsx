// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LansengerSettings } from '../LansengerSettings';

const RestartLansengerMock = vi.fn();
const SetLansengerLocalModeMock = vi.fn();
const LoadConfigMock = vi.fn();
const ListLansengerGroupsMock = vi.fn();
const ListLansengerGroupsForBotMock = vi.fn();
const ListLansengerBotsMock = vi.fn();
const SaveLansengerBotMock = vi.fn();
const GetLansengerBotStatusMock = vi.fn();
const ListExpertsMock = vi.fn();
const KnowledgeListSourcesMock = vi.fn();
const SelectVEAllowedDirectoryMock = vi.fn();
const SelectWorkingDirMock = vi.fn();
const EventsOnMock = vi.fn() as any;

beforeEach(() => {
    KnowledgeListSourcesMock.mockReset();
    SelectVEAllowedDirectoryMock.mockReset();
    SelectWorkingDirMock.mockReset();
    ListLansengerBotsMock.mockReset();
    EventsOnMock.mockReset();
    EventsOnMock.mockReturnValue(() => {});
    ListLansengerGroupsForBotMock.mockReset();
    SaveLansengerBotMock.mockReset();
    GetLansengerBotStatusMock.mockReset();
    ListExpertsMock.mockReset();
    KnowledgeListSourcesMock.mockResolvedValue([]);
    // Existing settings tests exercise the old desktop API fallback.
    ListLansengerBotsMock.mockRejectedValue(new Error('multi-bot API unavailable'));
    GetLansengerBotStatusMock.mockResolvedValue('disconnected');
    ListExpertsMock.mockResolvedValue('[]');
});

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    RestartLansenger: (...args: unknown[]) => RestartLansengerMock(...args),
    SetLansengerLocalMode: (...args: unknown[]) => SetLansengerLocalModeMock(...args),
    ListLansengerGroups: (...args: unknown[]) => ListLansengerGroupsMock(...args),
    ListLansengerGroupsForBot: (...args: unknown[]) => ListLansengerGroupsForBotMock(...args),
    ListLansengerBots: (...args: unknown[]) => ListLansengerBotsMock(...args),
    SaveLansengerBot: (...args: unknown[]) => SaveLansengerBotMock(...args),
    GetLansengerBotStatus: (...args: unknown[]) => GetLansengerBotStatusMock(...args),
    ListExperts: (...args: unknown[]) => ListExpertsMock(...args),
    DeleteLansengerBot: vi.fn().mockResolvedValue(undefined),
    RestartLansengerBot: vi.fn().mockResolvedValue('connected'),
    KnowledgeListSources: (...args: unknown[]) => KnowledgeListSourcesMock(...args),
    SelectVEAllowedDirectory: (...args: unknown[]) => SelectVEAllowedDirectoryMock(...args),
    SelectWorkingDir: (...args: unknown[]) => SelectWorkingDirMock(...args),
    SetLansengerGroupIgnored: vi.fn().mockResolvedValue(undefined),
    SetLansengerGroupAllowed: vi.fn().mockResolvedValue(undefined),
    SetLansengerGroupFileMaxBytes: vi.fn().mockResolvedValue(undefined),
    SetLansengerBotGroupIgnored: vi.fn().mockResolvedValue(undefined),
    SetLansengerBotGroupAllowed: vi.fn().mockResolvedValue(undefined),
    SetLansengerBotGroupFileMaxBytes: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (eventName: string, callback: (...args: any[]) => void) => EventsOnMock(eventName, callback),
}));

vi.mock('../../pages/UtilitiesWatchPanel', () => ({
    UtilitiesWatchPanel: ({
        onBack,
        compactHeader,
    }: {
        isZh: boolean;
        onBack: () => void;
        compactHeader?: boolean;
    }) => (
        <div data-testid="watch-page" data-compact={compactHeader ? '1' : '0'}>
            <button type="button" onClick={onBack}>
                back
            </button>
        </div>
    ),
}));

const baseProps = () => ({
    config: {
        lansenger_enabled: true,
        lansenger_app_id: 'app-id',
        lansenger_app_secret: 'secret',
        lansenger_gateway_url: 'https://apigw.lx.qianxin.com',
    } as any,
    setConfig: vi.fn(),
    lang: 'zh-Hans',
    saveRemoteConfigField: vi.fn(),
    lansengerStatus: 'connected',
    setLansengerStatus: vi.fn(),
    lansengerLocalMode: true,
    setLansengerLocalModeState: vi.fn(),
    setIMAuditPlatform: vi.fn(),
});

describe('LansengerSettings', () => {
    it('uses the selected bot for group management and isolated chat history', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: '客服机器人', enabled: true, app_id: 'support-app',
            assistant_mode: 'expert', expert_id: 'support-expert', group_policy: 'allowlist',
            document_directories: [], secret_configured: true,
        }]);
        ListLansengerGroupsForBotMock.mockResolvedValue({ total: 0, groups: [] });
        const props = baseProps();

        render(<LansengerSettings {...props} />);
        await screen.findByText('客服机器人');

        fireEvent.click(screen.getByRole('button', { name: '群信息' }));
        await waitFor(() => expect(ListLansengerGroupsForBotMock).toHaveBeenCalledWith('support'));

        fireEvent.click(screen.getByRole('button', { name: '聊天历史' }));
        expect(props.setIMAuditPlatform).toHaveBeenCalledWith('lansenger:support');
    });

    it('renders Group info at the document root with the active theme', async () => {
        const app = document.createElement('div');
        app.id = 'App';
        app.dataset.aiTheme = 'dark';
        app.dataset.aiDarkScheme = 'aurora';
        document.body.append(app);
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        try {
            render(<LansengerSettings {...baseProps()} />);
            fireEvent.click(screen.getByRole('button', { name: '群信息' }));

            const dialog = await screen.findByRole('dialog', { name: '已加入的群' });
            const overlay = dialog.parentElement;
            expect(overlay?.parentElement).toBe(document.body);
            expect(overlay?.getAttribute('data-ai-theme')).toBe('dark');
            expect(overlay?.getAttribute('data-ai-dark-scheme')).toBe('aurora');
        } finally {
            app.remove();
        }
    });

    it('returns focus to the Group info action after closing with Escape', async () => {
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        render(<LansengerSettings {...baseProps()} />);
        const trigger = screen.getByRole('button', { name: '群信息' });
        fireEvent.click(trigger);

        const dialog = await screen.findByRole('dialog', { name: '已加入的群' });
        const closeButton = dialog.querySelector<HTMLButtonElement>('.im-groups-modal__close');
        expect(closeButton).toBeTruthy();
        await waitFor(() => expect(document.activeElement).toBe(closeButton));
        fireEvent.keyDown(window, { key: 'Escape' });

        await waitFor(() => {
            expect(screen.queryByRole('dialog', { name: '已加入的群' })).toBeNull();
            expect(document.activeElement).toBe(trigger);
        });
    });

    it('keeps Tab navigation inside Group info', async () => {
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        render(<LansengerSettings {...baseProps()} />);
        fireEvent.click(screen.getByRole('button', { name: '群信息' }));
        const dialog = await screen.findByRole('dialog', { name: '已加入的群' });
        const closeButton = dialog.querySelector<HTMLButtonElement>('.im-groups-modal__close');
        const focusable = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button:not([disabled])'));
        expect(closeButton).toBeTruthy();
        closeButton?.focus();

        fireEvent.keyDown(closeButton!, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(focusable.at(-1));
    });

    it('updates the selected bot status from its own gateway event', async () => {
        let onStatus: ((botID: string, status: string) => void) | undefined;
        EventsOnMock.mockImplementation((...args: unknown[]) => {
            const callback = args[1] as (botID: string, status: string) => void;
            onStatus = callback;
            return () => {};
        });
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: true, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        GetLansengerBotStatusMock.mockResolvedValue('disconnected');
        render(<LansengerSettings {...baseProps()} />);
        await screen.findByText('Support');
        await waitFor(() => expect(EventsOnMock).toHaveBeenCalledWith('lansenger-bot-status-changed', expect.any(Function)));

        onStatus?.('sales', 'connected');
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        onStatus?.('support', 'connected');
        await screen.findByTestId('lansenger-follow-button');
    });

    it('saves a new bot profile without using legacy global settings', async () => {
        ListLansengerBotsMock.mockResolvedValue([]);
        SaveLansengerBotMock.mockResolvedValue({
            id: 'docs-bot', name: '文档助手', enabled: false, app_id: 'docs-app',
            assistant_mode: 'general', group_policy: 'open', document_directories: [],
        });
        const props = baseProps();

        render(<LansengerSettings {...props} />);
        await waitFor(() => expect(ListLansengerBotsMock).toHaveBeenCalled());
        fireEvent.click(screen.getByRole('button', { name: '添加机器人' }));
        fireEvent.change(screen.getByLabelText('机器人 ID'), { target: { value: 'docs-bot' } });
        fireEvent.change(screen.getByLabelText('名称'), { target: { value: '文档助手' } });
        fireEvent.change(screen.getByLabelText('App ID'), { target: { value: 'docs-app' } });
        fireEvent.click(screen.getByRole('button', { name: '保存机器人' }));

        await waitFor(() => expect(SaveLansengerBotMock).toHaveBeenCalledWith(expect.objectContaining({
            id: 'docs-bot', name: '文档助手', app_id: 'docs-app', app_secret: '',
        })));
        expect(props.saveRemoteConfigField).not.toHaveBeenCalled();
    });

    it('keeps saved bots visible while a new bot draft is being edited', async () => {
        ListLansengerBotsMock.mockResolvedValue([
            { id: 'support', name: '客服机器人', enabled: true, app_id: 'support-app', assistant_mode: 'general', group_policy: 'open', document_directories: [] },
            { id: 'sales', name: '销售机器人', enabled: false, app_id: 'sales-app', assistant_mode: 'general', group_policy: 'open', document_directories: [] },
        ]);

        render(<LansengerSettings {...baseProps()} />);
        await screen.findByText('客服机器人');
        await screen.findByText('销售机器人');
        fireEvent.click(screen.getByRole('button', { name: '添加机器人' }));

        expect(screen.getByText('客服机器人')).toBeTruthy();
        expect(screen.getByText('销售机器人')).toBeTruthy();
        expect(screen.getByText('support')).toBeTruthy();
        expect(screen.getByText('sales')).toBeTruthy();
        expect(screen.getByText('已启用')).toBeTruthy();
        expect(screen.getAllByText('已停用')).toHaveLength(2);
        expect(screen.getByText('新机器人')).toBeTruthy();
        expect(screen.getByText('未保存草稿')).toBeTruthy();
    });

    it('prevents a second draft and lets the user discard the current draft', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: '客服机器人', enabled: true, app_id: 'support-app', assistant_mode: 'general', group_policy: 'open', document_directories: [],
        }]);

        render(<LansengerSettings {...baseProps()} />);
        await screen.findByText('客服机器人');
        const addButton = screen.getByRole('button', { name: '添加机器人' });
        fireEvent.click(addButton);

        expect((addButton as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByText('新机器人实例')).toBeTruthy();
        expect((screen.getByRole('button', { name: '聊天历史' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: '群信息' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: /客服机器人/ }) as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: '放弃草稿' }));

        expect(screen.queryByText('新机器人实例')).toBeNull();
        expect((screen.getByRole('button', { name: '添加机器人' }) as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByText('正在编辑机器人实例: 客服机器人')).toBeTruthy();
        expect((screen.getByRole('button', { name: '聊天历史' }) as HTMLButtonElement).disabled).toBe(false);
    });

    it('does not overwrite a saved bot when a new draft reuses its ID', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: '客服机器人', enabled: true, app_id: 'support-app', assistant_mode: 'general', group_policy: 'open', document_directories: [],
        }]);

        render(<LansengerSettings {...baseProps()} />);
        await screen.findByText('客服机器人');
        fireEvent.click(screen.getByRole('button', { name: '添加机器人' }));
        fireEvent.change(screen.getByLabelText('机器人 ID'), { target: { value: 'support' } });

        const saveButton = screen.getByRole('button', { name: '保存机器人' }) as HTMLButtonElement;
        expect(saveButton.disabled).toBe(true);
        expect(screen.getByRole('alert').textContent).toContain('该机器人 ID 已被使用');
        fireEvent.click(saveButton);
        expect(SaveLansengerBotMock).not.toHaveBeenCalled();
    });

    it('validates a new bot ID before it can be saved', async () => {
        ListLansengerBotsMock.mockResolvedValue([]);

        render(<LansengerSettings {...baseProps()} />);
        await waitFor(() => expect(ListLansengerBotsMock).toHaveBeenCalled());
        fireEvent.click(screen.getByRole('button', { name: '添加机器人' }));
        fireEvent.change(screen.getByLabelText('机器人 ID'), { target: { value: 'invalid bot id!' } });

        const saveButton = screen.getByRole('button', { name: '保存机器人' }) as HTMLButtonElement;
        expect(saveButton.disabled).toBe(true);
        expect(screen.getByRole('alert').textContent).toContain('请输入 1–128 个字母、数字、点、下划线或连字符');
        expect(SaveLansengerBotMock).not.toHaveBeenCalled();
    });

    it('keeps bot directory, knowledge, and web permissions scoped to the bot profile', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], allowed_directories: [], knowledge_source_ids: [], secret_configured: true,
        }]);
        KnowledgeListSourcesMock.mockResolvedValue([{ id: 'support-kb', title: 'Support knowledge' }]);
        const props = { ...baseProps(), lang: 'en' };

        render(<LansengerSettings {...props} />);
        await screen.findByText('Support');
        fireEvent.click(screen.getByRole('button', { name: 'Refresh knowledge sources' }));
        const source = await screen.findByText('Support knowledge');
        fireEvent.click(source);
        fireEvent.click(screen.getByText('Allow public web search'));
        fireEvent.click(screen.getByText('Allow all directories'));
        fireEvent.click(screen.getByRole('button', { name: 'Save bot' }));

        await waitFor(() => expect(SaveLansengerBotMock).toHaveBeenCalledWith(expect.objectContaining({
            id: 'support', knowledge_source_ids: ['support-kb'], allow_web_search: true, allow_all_directories: true,
        })));
        expect(props.saveRemoteConfigField).not.toHaveBeenCalled();
    });

    it('keeps reply-cache policy on the selected bot and saves it with that bot', async () => {
        ListLansengerBotsMock.mockResolvedValue([
            { id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general', group_policy: 'open', document_directories: [], answer_cache: { enabled: true, ttl_days: 7 }, secret_configured: true },
            { id: 'sales', name: 'Sales', enabled: false, app_id: 'sales-app', assistant_mode: 'general', group_policy: 'open', document_directories: [], answer_cache: { enabled: true, ttl_days: 0 }, secret_configured: true },
        ]);
        SaveLansengerBotMock.mockResolvedValue({ id: 'sales', name: 'Sales', enabled: false, app_id: 'sales-app', assistant_mode: 'general', group_policy: 'open', document_directories: [], answer_cache: { enabled: true, ttl_days: 14 } });
        const props = { ...baseProps(), lang: 'en' };

        render(<LansengerSettings {...props} />);
        await screen.findByText('Support');
        expect((screen.getByLabelText('Validity (days)') as HTMLInputElement).value).toBe('7');
        fireEvent.click(screen.getByRole('button', { name: /Sales/ }));
        expect((screen.getByLabelText('Validity (days)') as HTMLInputElement).value).toBe('0');
        fireEvent.change(screen.getByLabelText('Validity (days)'), { target: { value: '14' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save bot' }));

        await waitFor(() => expect(SaveLansengerBotMock).toHaveBeenCalledWith(expect.objectContaining({
            id: 'sales', answer_cache: { enabled: true, ttl_days: 14 },
        })));
        expect(props.saveRemoteConfigField).not.toHaveBeenCalled();
    });

    it('shows reply-cache controls only while editing a bot instance', async () => {
        ListLansengerBotsMock.mockResolvedValue([]);

        render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        await waitFor(() => expect(ListLansengerBotsMock).toHaveBeenCalled());
        expect(screen.queryByText('Reply cache for this bot')).toBeNull();
    });

    it('keeps a disabled bot cache policy off until it is enabled and saved', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], answer_cache: { enabled: false, ttl_days: 7 }, secret_configured: true,
        }]);
        SaveLansengerBotMock.mockResolvedValue({ id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general', group_policy: 'open', document_directories: [], answer_cache: { enabled: true, ttl_days: 7 } });
        const props = { ...baseProps(), lang: 'en' };

        render(<LansengerSettings {...props} />);
        await screen.findByText('Support');
        expect((screen.getByLabelText('Validity (days)') as HTMLInputElement).disabled).toBe(true);
        fireEvent.click(screen.getByRole('checkbox', { name: 'Enable reply cache' }));
        fireEvent.click(screen.getByRole('button', { name: 'Save bot' }));

        await waitFor(() => expect(SaveLansengerBotMock).toHaveBeenCalledWith(expect.objectContaining({
            id: 'support', answer_cache: { enabled: true, ttl_days: 7 },
        })));
        expect(props.saveRemoteConfigField).not.toHaveBeenCalled();
    });

    it('browses for a bot working directory without saving the draft', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        SelectWorkingDirMock.mockResolvedValue('D:\\workprj\\support');

        render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        await screen.findByText('Support');
        fireEvent.click(screen.getByRole('button', { name: 'Browse working directory' }));

        await waitFor(() => expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1));
        expect(await screen.findByDisplayValue('D:\\workprj\\support')).toBeTruthy();
        expect(SaveLansengerBotMock).not.toHaveBeenCalled();
    });

    it('does not open duplicate directory pickers during a rapid double click', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        let resolveDirectory: (value: string) => void;
        SelectWorkingDirMock.mockImplementation(() => new Promise<string>((resolve) => { resolveDirectory = resolve; }));

        render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        await screen.findByText('Support');
        const browseButton = screen.getByRole('button', { name: 'Browse working directory' });
        fireEvent.click(browseButton);
        fireEvent.click(browseButton);

        expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1);
        resolveDirectory!('D:\\workprj\\support');
        expect(await screen.findByDisplayValue('D:\\workprj\\support')).toBeTruthy();
    });

    it('does not update a draft that was discarded while choosing a directory', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        let resolveDirectory: (value: string) => void;
        SelectWorkingDirMock.mockImplementation(() => new Promise<string>((resolve) => { resolveDirectory = resolve; }));

        render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        await screen.findByText('Support');
        fireEvent.click(screen.getByRole('button', { name: 'Add bot' }));
        fireEvent.click(screen.getByRole('button', { name: 'Browse working directory' }));
        fireEvent.click(screen.getByRole('button', { name: 'Discard draft' }));
        resolveDirectory!('D:\\workprj\\discarded');

        await screen.findByText('Editing bot instance: Support');
        await waitFor(() => expect(screen.queryByDisplayValue('D:\\workprj\\discarded')).toBeNull());
    });

    it('ignores a directory-picker result after the settings component unmounts', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        let resolveDirectory: (value: string) => void;
        SelectWorkingDirMock.mockImplementation(() => new Promise<string>((resolve) => { resolveDirectory = resolve; }));

        const rendered = render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        await screen.findByText('Support');
        fireEvent.click(screen.getByRole('button', { name: 'Browse working directory' }));
        rendered.unmount();
        resolveDirectory!('D:\\workprj\\after-unmount');

        await Promise.resolve();
        expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1);
    });

    it('selects a Lansenger expert from the current AI expert catalog', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'general',
            group_policy: 'open', document_directories: [], secret_configured: true,
        }]);
        ListExpertsMock.mockResolvedValue(JSON.stringify([
            { id: 'support-expert', name: '客服专家', description: '产品与工单客服', tools: [], skills: [], builtin: false },
            { id: 'sales-expert', name: '销售专家', description: '销售咨询', tools: [], skills: [], builtin: false },
        ]));
        SaveLansengerBotMock.mockResolvedValue({
            id: 'support', name: 'Support', enabled: false, app_id: 'support-app', assistant_mode: 'expert',
            expert_id: 'support-expert', group_policy: 'open', document_directories: [],
        });

        render(<LansengerSettings {...baseProps()} />);
        await screen.findByText('Support');
        fireEvent.change(screen.getByLabelText('助手类型'), { target: { value: 'expert' } });
        const expertSelect = await screen.findByLabelText('AI 专家');
        expect(expertSelect.tagName).toBe('SELECT');
        expect(screen.getByRole('option', { name: '客服专家 — 产品与工单客服' })).toBeTruthy();
        fireEvent.change(expertSelect, { target: { value: 'support-expert' } });
        fireEvent.click(screen.getByRole('button', { name: '保存机器人' }));

        await waitFor(() => expect(SaveLansengerBotMock).toHaveBeenCalledWith(expect.objectContaining({
            id: 'support', assistant_mode: 'expert', expert_id: 'support-expert',
        })));
    });

    it('shows a degraded state when a bot binding references a deleted expert', async () => {
        ListLansengerBotsMock.mockResolvedValue([{
            id: 'support', name: 'Support', enabled: true, app_id: 'support-app', assistant_mode: 'expert',
            expert_id: 'deleted-expert', group_policy: 'open', document_directories: [],
            status: 'degraded', status_reason: '绑定的 AI 专家已不可用，请在蓝信机器人设置中重新选择。',
        }]);

        render(<LansengerSettings {...{ ...baseProps(), lang: 'en' }} />);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('Status: degraded');
        expect(alert.textContent).toContain('绑定的 AI 专家已不可用');
        expect(GetLansengerBotStatusMock).not.toHaveBeenCalled();
    });

    it('saves fields and exposes restart and audit actions', async () => {
        const props = baseProps();
        RestartLansengerMock.mockResolvedValue('connected');

        render(<LansengerSettings {...props} />);

        fireEvent.click(screen.getByLabelText('\u542f\u7528\u84dd\u4fe1'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_enabled: false });

        fireEvent.change(screen.getByDisplayValue('app-id'), { target: { value: 'new-app' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_app_id: 'new-app' });

        fireEvent.click(screen.getByRole('button', { name: '\u91cd\u542f' }));
        await waitFor(() => expect(props.setLansengerStatus).toHaveBeenCalledWith('connected'));

        fireEvent.click(screen.getByRole('button', { name: '\u804a\u5929\u5386\u53f2' }));
        expect(props.setIMAuditPlatform).toHaveBeenCalledWith('lansenger');

        // Group-chat options must go through saveRemoteConfigField (atomic whitelist).
        fireEvent.change(screen.getByLabelText('\u7fa4\u804a\u7b56\u7565'), { target: { value: 'allowlist' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_policy: 'allowlist' });
        fireEvent.click(screen.getByText('\u9700\u8981 @\u63d0\u53ca'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_require_mention: false });
    });

    it('edits the local-only group permission allowlists', async () => {
        const props = baseProps();
        KnowledgeListSourcesMock.mockResolvedValue([{ id: 'knowledge-a', title: '知识库 A', kind: 'folder' }]);
        SelectVEAllowedDirectoryMock.mockResolvedValue('D:\\approved');

        const first = render(<LansengerSettings {...props} />);

        const source = await screen.findByText('知识库 A');
        fireEvent.click(source);
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_knowledge_source_ids: ['knowledge-a'] });

        fireEvent.click(screen.getByText('允许所有目录'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_allow_all_directories: true });

        fireEvent.click(screen.getByText('允许网络检索与文件下载'));
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_allow_web_search: true });

        // A fresh mount models the persisted controlled config after toggling.
        first.unmount();
        const directoryProps = baseProps();
        render(<LansengerSettings {...directoryProps} config={{ ...directoryProps.config, lansenger_group_allowed_directories: [] } as any} />);
        fireEvent.click(screen.getByRole('button', { name: '添加目录' }));
        await waitFor(() => expect(SelectVEAllowedDirectoryMock).toHaveBeenCalled());
        await waitFor(() => expect(directoryProps.saveRemoteConfigField).toHaveBeenCalledWith({ lansenger_group_allowed_directories: ['D:\\approved'] }));
    });

    it('shows Follow only when Lansenger is connected, after Watch', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        const follow = screen.getByTestId('lansenger-follow-button');
        expect(follow).toBeTruthy();
        expect(follow.textContent).toBe('\u76ef\u4eba');

        // Follow sits after chat history in the toolbar.
        const watchBtn = screen.getByRole('button', { name: '\u804a\u5929\u5386\u53f2' });
        expect(watchBtn.compareDocumentPosition(follow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(follow);
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
    });

    it('keeps the Follow portal aligned with app theme changes', async () => {
        const app = document.createElement('div');
        app.id = 'App';
        app.dataset.aiTheme = 'dark';
        app.dataset.aiDarkScheme = 'violet';
        document.body.append(app);
        try {
            render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
            fireEvent.click(screen.getByTestId('lansenger-follow-button'));
            const overlay = await screen.findByRole('presentation');
            expect(overlay.getAttribute('data-ai-theme')).toBe('dark');
            expect(overlay.getAttribute('data-ai-dark-scheme')).toBe('violet');

            app.dataset.aiTheme = 'light';
            app.dataset.aiDarkScheme = '';
            app.dataset.aiLightScheme = 'github';
            await waitFor(() => {
                expect(overlay.getAttribute('data-ai-theme')).toBe('light');
                expect(overlay.hasAttribute('data-ai-dark-scheme')).toBe(false);
                expect(overlay.getAttribute('data-ai-light-scheme')).toBe('github');
            });
        } finally {
            app.remove();
        }
    });

    it('closes Follow panel when connection drops', async () => {
        const { rerender } = render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        rerender(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        await waitFor(() => {
            expect(screen.queryByTestId('watch-page')).toBeNull();
            expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        });
    });

    it('closes Follow panel via Escape', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('opens Follow in compact embed mode and closes via header ×', async () => {
        ListLansengerGroupsMock.mockResolvedValue({ total: 0, groups: [] });
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        const page = await screen.findByTestId('watch-page');
        expect(page.getAttribute('data-compact')).toBe('1');
        expect(screen.getByRole('dialog', { name: '\u76ef\u4eba' })).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '\u5173\u95ed' }));
        await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
    });

    it('does not open Follow when disconnected', () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="disconnected" />);
        expect(screen.queryByTestId('lansenger-follow-button')).toBeNull();
        expect(screen.queryByTestId('watch-page')).toBeNull();
    });

    it('locks body scroll while Follow dialog is open', async () => {
        const prev = document.body.style.overflow;
        document.body.style.overflow = '';
        try {
            render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
            fireEvent.click(screen.getByTestId('lansenger-follow-button'));
            await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());
            expect(document.body.style.overflow).toBe('hidden');

            fireEvent.keyDown(window, { key: 'Escape' });
            await waitFor(() => expect(screen.queryByTestId('watch-page')).toBeNull());
            expect(document.body.style.overflow).toBe('');
        } finally {
            document.body.style.overflow = prev;
        }
    });

    it('does not close Follow on Escape during IME composition', async () => {
        render(<LansengerSettings {...baseProps()} lansengerStatus="connected" />);
        fireEvent.click(screen.getByTestId('lansenger-follow-button'));
        await waitFor(() => expect(screen.getByTestId('watch-page')).toBeTruthy());

        fireEvent.keyDown(window, { key: 'Escape', isComposing: true });
        expect(screen.getByTestId('watch-page')).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Escape', keyCode: 229 });
        expect(screen.getByTestId('watch-page')).toBeTruthy();
    });
});

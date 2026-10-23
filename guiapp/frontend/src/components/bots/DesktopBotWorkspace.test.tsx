// @vitest-environment jsdom
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DialogProvider } from '../CustomDialog';

// Loose signatures: these mocks are exercised with mockResolvedValue,
// mockImplementation, and promise-resolve captures across tests, so no single
// strict parameter list type-checks for all call sites.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const sendTask = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const understandTask = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
const classifyWorkStop = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
const armSchedule = vi.hoisted(() => vi.fn(async (_botId?: string, _name?: string, _rest?: unknown): Promise<string> => 'task-1'));
const listSchedules = vi.hoisted(() => vi.fn(async (_botId?: string) => [] as Array<{ id: string; name: string; action: string; interval_minutes: number; hour: number; minute: number; day_of_week: number; next_run_at?: string; status?: string }>));
const deleteSchedule = vi.hoisted(() => vi.fn(async (_botId?: string, _taskId?: string) => undefined));
const watchBot = vi.hoisted(() => vi.fn(async (_botId?: string, _epoch?: number): Promise<{ novnc_url: string; user_control: boolean; attention_reason?: string; desktop_mode?: string } | undefined> => ({ novnc_url: '', user_control: false })));
const getScreen = vi.hoisted(() => vi.fn(async (_botId?: string): Promise<{ novnc_url: string; user_control: boolean; attention_reason?: string; desktop_mode: string }> => ({ novnc_url: '', user_control: false, desktop_mode: 'shared' })));
const hubLink = vi.hoisted(() => vi.fn(async () => ({ state: 'connected' }) as Record<string, unknown> | null));
const handBack = vi.hoisted(() => vi.fn(async (_botId?: string) => ({ handed_back: true, accepted: true, run_id: 'run_1', status: 'running' })));
const setDesktopMode = vi.hoisted(() => vi.fn(async (_botId?: string, mode?: string) => mode || 'shared'));
const desktopAdmin = vi.hoisted(() => vi.fn(async () => false));
const releaseWatch = vi.hoisted(() => vi.fn(async (_botId?: string, _epoch?: number) => undefined));
const saveSecret = vi.hoisted(() => vi.fn(async (_name?: string, _value?: string) => undefined));
const recallSecret = vi.hoisted(() => vi.fn(async (_name?: string) => false));
const fillSecret = vi.hoisted(() => vi.fn(async (_botId?: string, _name?: string) => ({ filled: 'SITE_PASSWORD' })));
type HubDesktopBot = { id: string; title: string; description: string; instance_id: string; created_at?: string; primary?: boolean };
const listBots = vi.hoisted(() => vi.fn(async (): Promise<HubDesktopBot[]> => []));
const createBot = vi.hoisted(() => vi.fn(async (name: string, description: string): Promise<HubDesktopBot> => ({ id: 'bot_1', title: name, description, instance_id: '' })));
const renameBot = vi.hoisted(() => vi.fn(async (_id: string, _name: string, _description: string) => undefined));
const deleteBot = vi.hoisted(() => vi.fn(async (_id: string) => undefined));
const openFile = vi.hoisted(() => vi.fn(async (_path?: string) => undefined));
const showFile = vi.hoisted(() => vi.fn(async (_path?: string) => undefined));
const previewFile = vi.hoisted(() => vi.fn(async (_path?: string) => ({ preview_url: '' })));
const readShot = vi.hoisted(() => vi.fn(async (_path?: string) => ({ mime: 'image/png', data: 'iVBORw0KGgo=' })));
type BotLLMView = { mode: string; name?: string; url?: string; model?: string; key_set: boolean; warning?: string };
type BotLLMIn = { mode?: string; name?: string; url?: string; key?: string; model?: string; protocol?: string; wire_api?: string };
const getBotLLM = vi.hoisted(() => vi.fn(async (): Promise<BotLLMView> => ({ mode: 'default', key_set: false })));
type HubCreditsMock = { active: boolean; credits_total?: number; credits_used?: number; credits_remaining?: number };
const getHubCredits = vi.hoisted(() => vi.fn(async (): Promise<HubCreditsMock> => ({ active: false })));
const refreshHubCredits = vi.hoisted(() => vi.fn(async (): Promise<HubCreditsMock> => ({ active: false })));
const getBotCredits = vi.hoisted(() => vi.fn(async () => ({ applies: true, unlimited: true, limit: 0, used: 0, remaining: 0 })));
const loadConfig = vi.hoisted(() => vi.fn(async () => ({ remote_hub_url: 'https://hub.example' })));
const saveBotLLM = vi.hoisted(() => vi.fn(async (input?: BotLLMIn): Promise<BotLLMView> => ({
    mode: input?.mode ?? 'default',
    name: input?.name,
    model: input?.model,
    key_set: !!input?.key,
})));
const listProviders = vi.hoisted(() => vi.fn(async (): Promise<{ providers: Array<Record<string, unknown>>; current: string }> => ({ providers: [], current: '' })));
const fetchProviderModels = vi.hoisted(() => vi.fn(async (..._args: unknown[]): Promise<Array<{ id?: string; name?: string }>> => []));
const persistPreference = vi.hoisted(() => vi.fn(async (_botId?: string, _persona?: string) => undefined));
const queryTaskStatus = vi.hoisted(() => vi.fn(async (_botId?: string, _hint?: string, _lang?: string) => '「下载报表」还在进行。'));
const listBotTasks = vi.hoisted(() => vi.fn(async (_botId?: string) => [] as Array<{ run_id?: string; bot_id?: string; status?: string; task?: string }>));
const listActiveRuns = vi.hoisted(() => vi.fn(async (_botId?: string): Promise<Array<Record<string, unknown>>> => []));
const listTaskRuns = vi.hoisted(() => vi.fn(async (_botId?: string) => [] as Array<Record<string, unknown>>));
const cancelRun = vi.hoisted(() => vi.fn(async (_botId?: string, _runId?: string) => undefined));
const armAdmitCancel = vi.hoisted(() => vi.fn(async (_botId?: string) => undefined));
type DesktopBusyMock = { seq: number; marks: Array<{ owner_id?: string; bot_id: string; count: number }>; runs?: string[] };
const getDesktopBusy = vi.hoisted(() => vi.fn(async (_owner?: string): Promise<DesktopBusyMock> => ({ seq: 0, marks: [] })));
const evaluateSupplement = vi.hoisted(() => vi.fn(async (_taskText?: string, _content?: string, _lang?: string): Promise<{ is_supplement?: boolean; instruction?: string } | null> => null));
const appendSupplement = vi.hoisted(() => vi.fn(async (_botId?: string, _runId?: string, _instruction?: string): Promise<{ accepted?: boolean; stage?: string; message?: string; supplement_id?: string } | null> => null));
const getSupplements = vi.hoisted(() => vi.fn(async (_botId?: string, _runId?: string): Promise<Array<{ id: string; state: string; content?: string }>> => []));
const pullCross = vi.hoisted(() => vi.fn(async () => [] as Array<Record<string, unknown>>));
const postCross = vi.hoisted(() => vi.fn(async (..._args: unknown[]): Promise<{ posted: boolean; ask?: string; primary_line: string; names: string[]; notes: Array<Record<string, unknown>> }> => ({ posted: false, ask: '', primary_line: '', names: [], notes: [] })));
const setPrimary = vi.hoisted(() => vi.fn(async (_id?: string): Promise<HubDesktopBot[]> => []));
const claimCross = vi.hoisted(() => vi.fn(async (_botId?: string, _noteId?: string): Promise<{ claimed: boolean; id?: string; from: string }> => ({ claimed: false, from: '' })));
const armCross = vi.hoisted(() => vi.fn(async (_botId?: string, _noteId?: string) => undefined));
const responseListeners = vi.hoisted(() => ({
    list: [] as Array<(payload: unknown) => void>,
    byEvent: {} as Record<string, (payload: unknown) => void>,
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    SendDesktopBotTask: (...args: unknown[]) => sendTask(...args),
    UnderstandDesktopBotTask: (content: string, phase: string, lang: string, earlier?: string[], persona?: string, catalog?: string) => understandTask(content, phase, lang, earlier || [], persona || '', catalog || ''),
    ClassifyDesktopWorkStop: (text: string, lang: string) => classifyWorkStop(text, lang),
    ArmDesktopBotSchedule: (...args: unknown[]) => armSchedule(...(args as [])) || 'task-1',
    ListDesktopBotSchedules: (botId: string) => listSchedules(botId),
    ListDesktopBotActiveRuns: (botId: string) => listTaskRuns(botId),
    DeleteDesktopBotSchedule: (botId: string, taskId: string) => deleteSchedule(botId, taskId),
    WatchDesktopBot: (arg: string, epoch?: number) => watchBot(arg, epoch),
    ReleaseDesktopBotWatch: (arg: string, epoch?: number) => releaseWatch(arg, epoch),
    GetDesktopBotScreen: (botId: string) => getScreen(botId),
    HandBackDesktopBot: (botId: string) => handBack(botId),
    GetHubConnectionState: () => hubLink(),
    ReconnectHub: () => Promise.resolve({ state: 'connected' }),
    SetDesktopBotDesktopMode: (botId: string, mode: string) => setDesktopMode(botId, mode),
    DesktopBotDesktopAdmin: () => desktopAdmin(),
    SaveBotSecret: (name: string, value: string) => saveSecret(name, value),
    RecallBotSecret: (name: string) => recallSecret(name),
    FillBotSecret: (botId: string, name: string) => fillSecret(botId, name),
    ListDesktopBots: () => listBots(),
    CreateDesktopBot: (name: string, description: string) => createBot(name, description),
    RenameDesktopBot: (id: string, name: string, description: string) => renameBot(id, name, description),
    DeleteDesktopBot: (id: string) => deleteBot(id),
    OpenFileOrShowInFolder: (path: string) => openFile(path),
    ShowItemInFolder: (path: string) => showFile(path),
    PreviewTaskResultFile: (path: string) => previewFile(path),
    ReadDesktopBotShot: (path: string) => readShot(path),
    GetDesktopBotLLM: () => getBotLLM(),
    GetDesktopBotCredits: () => getBotCredits(),
    GetHubLLMServiceStatus: () => getHubCredits(),
    RefreshHubLLMServiceStatus: () => refreshHubCredits(),
    LoadConfig: () => loadConfig(),
    SaveDesktopBotLLM: (input: BotLLMIn) => saveBotLLM(input),
    GetMaclawLLMProviders: () => listProviders(),
    FetchProviderModels: (...args: unknown[]) => fetchProviderModels(...(args as [string, string, string, string])),
    PersistDesktopBotPreference: (botId: string, persona: string) => persistPreference(botId, persona),
    QueryDesktopBotTaskStatus: (botId: string, hint: string, lang: string) => queryTaskStatus(botId, hint, lang),
    ListDesktopBotTasks: (botId: string) => listBotTasks(botId),
    CancelDesktopBotRun: (botId: string, runId: string) => cancelRun(botId, runId),
    ArmDesktopBotAdmitCancel: (botId: string) => armAdmitCancel(botId),
    GetActiveDesktopRuns: (botId: string) => listActiveRuns(botId),
    GetDesktopBotBusy: (owner: string) => getDesktopBusy(owner),
    EvaluateDesktopTaskSupplement: (taskText: string, content: string, lang: string) => evaluateSupplement(taskText, content, lang),
    // stopped/idle/pause come back empty here so the minimal lines are checked.
    WriteDesktopBotAck: (ctx?: { kind?: string }) => Promise.resolve(ctx?.kind === 'stopping' ? '还在收尾，停稳了告诉你。' : ['stopped', 'idle', 'pause', 'queued', 'stop_failed', 'continuing', 'status_notice', 'error', 'interrupted'].includes(ctx?.kind || '') ? '' : "好，这就去办。"),
    AppendDesktopBotSupplement: (botId: string, runId: string, instruction: string) => appendSupplement(botId, runId, instruction),
    GetDesktopBotSupplements: (botId: string, runId: string) => getSupplements(botId, runId),
    SetDesktopBotPrimary: (id: string) => setPrimary(id),
    PostDesktopBotCross: (...args: unknown[]) => postCross(...args),
    PullDesktopBotCrossNotes: () => pullCross(),
    PullDesktopBotFeed: async () => [],
    GetDesktopBotRunOutput: async () => ({ status: 'running', text: '', done: false }),
    ClaimDesktopBotCrossStart: (botId: string, noteId: string) => claimCross(botId, noteId),
    ArmDesktopBotCrossRun: (botId: string, noteId: string) => armCross(botId, noteId),
}));

const browserOpenURL = vi.hoisted(() => vi.fn());

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (url: string) => browserOpenURL(url),
    EventsOn: (event: string, handler: (payload: unknown) => void) => {
        responseListeners.list.push(handler);
        responseListeners.byEvent[event] = handler;
        return () => {
            responseListeners.list = responseListeners.list.filter(item => item !== handler);
            if (responseListeners.byEvent[event] === handler) delete responseListeners.byEvent[event];
        };
    },
}));

import { DesktopBotWorkspace, formatCreated, handleDesktopBotReport, handleDesktopBotResult, handleDesktopBotSupplementState, handleDesktopBotView } from './DesktopBotWorkspace';
import type { BotPhase } from './desktopBots';
import { appendDesktopBotReport, applySupplementOutcomes, messagesForBot, saveBotMessages } from './desktopBots';
import { adoptExistingBotReplies, setBotWindowForeground, unreadBotReplyCount } from './botUnread';

const RETURN_TASK = '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。';
const CONFIRM_TASK = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';
const SAVED_TASK = /^已在本机保存 [A-Z][A-Z0-9_]{0,63}。$/;
const FILLED_TASK = /^[A-Z][A-Z0-9_]{0,63} 已在本地填入当前密码框。/;

function passedThrough(text: string, phase: string): boolean {
    const trimmed = text.trim();
    return !trimmed || trimmed === RETURN_TASK || SAVED_TASK.test(trimmed) || FILLED_TASK.test(trimmed) || (trimmed === CONFIRM_TASK && phase !== 'execute');
}

function childOrder(text: string, phase: BotPhase = 'plan', earlier: string[] = []): string {
    if (passedThrough(text, phase)) return text.trim();
    return `指令:${phase}:${text}:${earlier.join('|')}`;
}

function told(text: string, phase: BotPhase = 'plan'): string {
    if (passedThrough(text, phase)) return text.trim();
    return `表述:${text}`;
}

// While the bot drives, the panel offers only 停止: no takeover controls.
function expectStopOnly() {
    expect(screen.queryByTestId('desktop-bot-takeover')).toBeNull();
    expect(screen.queryByTestId('desktop-bot-take-control')).toBeNull();
    expect(screen.getByTestId('desktop-bot-panel-stop')).toBeTruthy();
}

function renderBots(ui: ReactElement) {
    return render(<DialogProvider>{ui}</DialogProvider>);
}

async function confirmBotDelete(label = '删除') {
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: label }));
}

beforeEach(() => {
    localStorage.clear();
    // Most cases check the noVNC URL; the H.264 player has its own case.
    localStorage.setItem('maclaw.desktop.viewer', 'vnc');
    sendTask.mockReset();
    getSupplements.mockReset();
    getSupplements.mockResolvedValue([]);
    pullCross.mockReset();
    pullCross.mockResolvedValue([]);
    postCross.mockReset();
    postCross.mockResolvedValue({ posted: false, ask: '', primary_line: '', names: [], notes: [] });
    setPrimary.mockReset();
    setPrimary.mockResolvedValue([]);
    claimCross.mockReset();
    claimCross.mockResolvedValue({ claimed: false, from: '' });
    armCross.mockReset();
    armCross.mockResolvedValue(undefined);
    understandTask.mockReset();
    classifyWorkStop.mockReset();
    classifyWorkStop.mockResolvedValue('unknown');
    armSchedule.mockReset();
    armSchedule.mockResolvedValue('task-1');
    listSchedules.mockReset();
    listSchedules.mockResolvedValue([]);
    listTaskRuns.mockReset();
    listTaskRuns.mockResolvedValue([]);
    deleteSchedule.mockReset();
    deleteSchedule.mockResolvedValue(undefined);
    understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
        const text = String(content || '');
        if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
        return { told: `表述:${text}`, instruction: `指令:${phase}:${text}:${earlier.join('|')}`, pace: 'direct' };
    });
    watchBot.mockReset();
    watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
    getScreen.mockReset();
    getScreen.mockResolvedValue({ novnc_url: '', user_control: false, desktop_mode: 'shared' });
    hubLink.mockReset();
    hubLink.mockResolvedValue({ state: 'connected' });
    handBack.mockReset();
    handBack.mockResolvedValue({ handed_back: true, accepted: true, run_id: 'run_1', status: 'running' });
    setDesktopMode.mockReset();
    setDesktopMode.mockImplementation(async (_botId?: string, mode?: string) => mode || 'shared');
    desktopAdmin.mockReset();
    desktopAdmin.mockResolvedValue(false);
    releaseWatch.mockReset();
    releaseWatch.mockResolvedValue(undefined);
    saveSecret.mockReset();
    saveSecret.mockResolvedValue(undefined);
    recallSecret.mockReset();
    recallSecret.mockResolvedValue(false);
    fillSecret.mockReset();
    fillSecret.mockResolvedValue({ filled: 'SITE_PASSWORD' });
    listBots.mockReset();
    listBots.mockResolvedValue([]);
    getDesktopBusy.mockReset();
    getDesktopBusy.mockResolvedValue({ seq: 0, marks: [] });
    listActiveRuns.mockReset();
    listActiveRuns.mockResolvedValue([]);
    cancelRun.mockReset();
    cancelRun.mockResolvedValue(undefined);
    armAdmitCancel.mockReset();
    armAdmitCancel.mockResolvedValue(undefined);
    queryTaskStatus.mockReset();
    queryTaskStatus.mockImplementation(async () => '「下载报表」还在进行。');
    listBotTasks.mockReset();
    listBotTasks.mockResolvedValue([]);
    evaluateSupplement.mockReset();
    evaluateSupplement.mockResolvedValue(null);
    appendSupplement.mockReset();
    appendSupplement.mockResolvedValue(null);
    createBot.mockReset();
    createBot.mockImplementation(async (name: string, description: string) => ({ id: 'bot_1', title: name, description, instance_id: '' }));
    renameBot.mockReset();
    renameBot.mockResolvedValue(undefined);
    deleteBot.mockReset();
    deleteBot.mockResolvedValue(undefined);
    openFile.mockReset();
    openFile.mockResolvedValue(undefined);
    showFile.mockReset();
    showFile.mockResolvedValue(undefined);
    previewFile.mockReset();
    previewFile.mockResolvedValue({ preview_url: '' });
    getBotLLM.mockReset();
    getBotLLM.mockResolvedValue({ mode: 'default', key_set: false });
    getHubCredits.mockReset();
    getHubCredits.mockResolvedValue({ active: false });
    refreshHubCredits.mockReset();
    refreshHubCredits.mockResolvedValue({ active: false });
    getBotCredits.mockReset();
    getBotCredits.mockResolvedValue({ applies: true, unlimited: true, limit: 0, used: 0, remaining: 0 });
    loadConfig.mockReset();
    loadConfig.mockResolvedValue({ remote_hub_url: 'https://hub.example' });
    browserOpenURL.mockReset();
    saveBotLLM.mockReset();
    saveBotLLM.mockImplementation(async (input?: BotLLMIn) => ({
        mode: input?.mode ?? 'default',
        name: input?.name,
        model: input?.model,
        key_set: !!input?.key,
    }));
    listProviders.mockReset();
    listProviders.mockResolvedValue({ providers: [], current: '' });
    fetchProviderModels.mockReset();
    fetchProviderModels.mockResolvedValue([]);
    responseListeners.list = [];
    responseListeners.byEvent = {};
    setBotWindowForeground(false);
});

describe('DesktopBotWorkspace', () => {
    it('lets the same bot continue after a reply never came back', async () => {
        const started = Date.now() - 32 * 60 * 1000;
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: `m-${started.toString(36)}-old`,
                    role: 'assistant',
                    content: '',
                    pending: true,
                    handoffUrl: 'https://hub.example/api/v1/desktop-handoff/token/vnc.html?autoconnect=1',
                    userControl: true,
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-2', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
        const back = await screen.findByTestId('desktop-bot-return-control');
        fireEvent.click(back);
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute'));
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            text: '已在这个浏览器里继续',
            send_to_user: '已在这个浏览器里继续',
        });
        expect(await screen.findByText('已在这个浏览器里继续')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
    });

    it('sends a choice label as written, including 停下, in that turn phase', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-ask',
                    role: 'assistant',
                    content: '继续\n停下',
                    pending: false,
                    phase: 'execute',
                    askUser: { options: ['继续', '停下'], inputType: 'choice' },
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-choice', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const chat = await screen.findByTestId('desktop-bot-chat');
        fireEvent.click(within(chat).getByRole('button', { name: '停下' }));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '停下', 'execute'));
        expect(understandTask).not.toHaveBeenCalled();
        expect(cancelRun).not.toHaveBeenCalled();
    });

    it('starts the next command while a live turn is older than the stale clock', async () => {
        let release: (value: unknown) => void = () => {};
        sendTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '先做这个' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('先做这个')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        const now = Date.now();
        const clock = vi.spyOn(Date, 'now').mockReturnValue(now + 32 * 60 * 1000);
        try {
            fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再做那个' } });
            fireEvent.click(screen.getByTestId('desktop-bot-send'));
            expect(await screen.findByText('再做那个')).toBeTruthy();
            expect(await screen.findByText('表述:再做那个')).toBeTruthy();
            expect(screen.queryByText(/等手头这件做完/)).toBeNull();
            await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
            expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
            expect(document.querySelector('.desktop-bot-chat__bubble.is-typing')).toBeNull();
            expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        } finally {
            clock.mockRestore();
        }
        release({ request_id: 'req-live', deferred: true, session_key: 'alice:bot_1' });
    });

    it('keeps a late earlier reply off the command still using the browser', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-old',
                    role: 'assistant',
                    content: '请在这个浏览器里登录',
                    pending: false,
                    requestId: 'desktop-bot-1',
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        let release: (value: unknown) => void = () => {};
        sendTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '继续操作' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('继续操作')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-1',
            session_key: 'alice:bot_1',
            text: '迟到的旧结果',
            send_to_user: '迟到的旧结果',
            desktop_handoff_url: 'https://hub.example/api/v1/desktop-handoff/token/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(screen.queryByText('迟到的旧结果')).toBeNull();
        expect(screen.getByText('请在这个浏览器里登录')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        release({ request_id: 'desktop-bot-2', deferred: true, session_key: 'alice:bot_1' });
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            text: '已在这个浏览器里继续',
            send_to_user: '已在这个浏览器里继续',
        });
        expect(await screen.findByText('已在这个浏览器里继续')).toBeTruthy();
        expect(screen.getByText('请在这个浏览器里登录')).toBeTruthy();
    });

    it('lists a bot with its description and opens the chat on the right', async () => {
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);

        const lede = screen.getByText('Bot为24小时驻守远程电脑上的数字助理。').closest('.desktop-bot-workspace__lede');
        expect(lede).toBeTruthy();
        expect(lede?.closest('.desktop-bot-workspace__bar-head')).toBeNull();
        const topUp = lede?.querySelector('[data-testid="desktop-bot-list-topup"]');
        expect(topUp?.getAttribute('aria-label')).toBe('去充值');
        expect(topUp?.getAttribute('title')).toBe('去充值');
        expect(topUp?.textContent).toBe('');
        expect(topUp?.querySelector('svg')).toBeTruthy();
        expect(topUp?.parentElement?.classList.contains('desktop-bot-workspace__lede-credits')).toBe(true);
        const titleIcon = screen.getByTestId('desktop-bot-title-icon');
        const heading = titleIcon.closest('h1');
        expect(titleIcon.tagName).toBe('svg');
        expect(titleIcon.getAttribute('width')).toBe('20');
        expect(heading?.firstElementChild).toBe(titleIcon);
        expect(heading?.textContent).toBe('Bot');
        expect(screen.queryByTestId('sidebar-bot-icon')).toBeNull();
        expect(screen.getByTestId('desktop-bot-pick').textContent).toContain('选择一个 Bot');

        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect((await screen.findAllByText('Bot 1')).length).toBeGreaterThan(0);
        const list = screen.getByLabelText('Bot');
        expect(list.textContent).toContain('远程电脑上的7x24小时数字助理。');
        expect(screen.getByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        const editDialog = await screen.findByTestId('bot-edit-dialog');
        fireEvent.change(within(editDialog).getByLabelText('名称'), { target: { value: '值班' } });
        fireEvent.change(within(editDialog).getByLabelText('描述'), { target: { value: '晚上值守' } });
        fireEvent.click(within(editDialog).getByRole('button', { name: '保存' }));
        await waitFor(() => expect(screen.queryByTestId('bot-edit-dialog')).toBeNull());
        expect(await screen.findByText('晚上值守')).toBeTruthy();
        expect(list.textContent).toContain('值班');
        expect(list.textContent).toContain('晚上值守');

        const row = screen.getByText('晚上值守').closest('li');
        const remove = row?.querySelector('button[data-testid^="desktop-bot-delete-"]') as HTMLButtonElement;
        fireEvent.click(remove);
        expect(remove.getAttribute('aria-label')).toBe('删除');
        expect(remove.querySelector('svg')).toBeTruthy();
        const dialog = await screen.findByRole('dialog');
        expect(dialog.textContent).toContain('确定删除 Bot');
        expect(dialog.textContent).toContain('值班');
        expect(dialog.textContent).toContain('此操作不可撤销');
        expect(within(dialog).queryByRole('button', { name: '确认' })).toBeNull();
        fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        expect(deleteBot).not.toHaveBeenCalled();
        expect(screen.getByText('晚上值守')).toBeTruthy();
        fireEvent.click(remove);
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        expect(screen.queryByText('晚上值守')).toBeNull();
        expect(screen.getByTestId('desktop-bot-pick')).toBeTruthy();
    });

    // The reading of a message is a helper for the turn. When the helper
    // cannot answer, the turn must survive it: the chat says what happened and
    // keeps the words, instead of claiming the backend agent is unavailable.
    it('parks an unread message instead of blaming the backend agent', async () => {
        understandTask.mockResolvedValue({ reading: 'unreachable' });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'req-park',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        expect(await screen.findByTestId('desktop-bot-no-reading')).toBeTruthy();
        expect(screen.queryByText('后台 agent 不可用。')).toBeNull();
        expect(screen.getByText('这句没读成安排——帮我读话的服务暂时没有回应。可以照你的原话发过去，或者让我再读一次。')).toBeTruthy();
        // Nothing has been sent, so nothing is occupied: the turn is still
        // this person's to start.
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('sends your own words when you choose them', async () => {
        understandTask.mockResolvedValue({ reading: 'unreachable' });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'req-own',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-no-reading')).toBeTruthy();

        // Reading it again once the model answers is still just a click away,
        // and no reading is asked for a second time here.
        understandTask.mockResolvedValue({ told: '我来安排一下。', instruction: '指令:plan:打开示例网站:', pace: 'direct' });
        fireEvent.click(screen.getByTestId('desktop-bot-reading-verbatim'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith(expect.any(String), '打开示例网站', 'plan'));
        expect(understandTask).toHaveBeenCalledTimes(1);
        // The choice is spent: one click is one send, not a button that can be
        // pressed again on a desktop somebody else is using.
        expect(screen.queryByTestId('desktop-bot-no-reading')).toBeNull();
    });

    it('reads the same sentence again when asked to', async () => {
        understandTask.mockResolvedValue({ reading: 'unread' });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'req-again',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-no-reading')).toBeTruthy();
        expect(screen.getByText('这句没整理成可执行的安排，没有发出。')).toBeTruthy();

        understandTask.mockResolvedValue({ told: '我来安排一下。', instruction: '指令:plan:打开示例网站:', pace: 'direct' });
        fireEvent.click(screen.getByTestId('desktop-bot-reading-again'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith(expect.any(String), '指令:plan:打开示例网站:', 'execute'));
        expect(understandTask).toHaveBeenCalledTimes(2);
        // One choice per parking: the buttons leave with the first click.
        expect(screen.queryByTestId('desktop-bot-no-reading')).toBeNull();
    });

    // Timed work whose time was never named cannot be armed from here, and it
    // cannot be sent as work either — only this person knows when it should run.
    it('offers no shortcut for timed work with no time', async () => {
        understandTask.mockResolvedValue({ reading: 'unscheduled' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '每天早上问好' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        expect(await screen.findByTestId('desktop-bot-no-reading')).toBeTruthy();
        expect(screen.getByText('这句没有整理成定时安排，没有设定。')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-reading-verbatim')).toBeNull();
        expect(screen.getByTestId('desktop-bot-reading-again')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
    });

    // The failure of the helper must not be confused with a failure of the
    // backend: this is the exact bubble the product used to show instead.
    it('never claims the backend agent is unavailable when the reading fails', async () => {
        understandTask.mockRejectedValue('desktop bot reading timed out after 30 seconds');
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        await waitFor(() => expect(screen.getByTestId('desktop-bot-log').textContent).toContain('超时'));
        expect(screen.queryByText('后台 agent 不可用。')).toBeNull();
    });

    it('sends a command to the backend agent and shows the returned result', async () => {
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'req-1',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith(expect.any(String), childOrder('打开示例网站'), 'execute'));
        expect(screen.getByTestId('desktop-bot-log').textContent).not.toContain('正在工作');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        expect(screen.getByTestId('desktop-bot-row-state-bot_1').getAttribute('aria-label')).toBe('工作中');

        const botId = String(sendTask.mock.calls[0][0]);
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();

        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        await waitFor(() => {
            const loginSrc = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
            expect(loginSrc).toContain('view_only=0');
            expect(loginSrc).not.toContain('view_only=1');
        });
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();

        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: false,
        });
        await waitFor(() => {
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('view_only=1');
        });
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
            desktop_handoff_url: 'http://dockerd.example/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });

        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        const userSrc = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(userSrc).toContain('vnc.html');
        expect(userSrc).toContain('view_only=0');
        expect(userSrc).not.toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
        fireEvent.click(screen.getByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith(botId, '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute'));
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            text: '已继续操作',
            send_to_user: '已继续操作',
            desktop_handoff_url: 'http://dockerd.example/vnc.html?autoconnect=1',
        });
        expect(await screen.findByText('已继续操作')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();

        sendTask.mockImplementationOnce(async () => ({ request_id: 'req-2', deferred: true }));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '继续' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask).toHaveBeenLastCalledWith(botId, childOrder('继续', 'plan', ['打开示例网站', RETURN_TASK]), 'execute');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-2',
            session_key: `alice:${botId}`,
            text: '已继续',
            send_to_user: '已继续',
        });
        expect(await screen.findByText('已继续')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
    });

    it('replies like a colleague first and posts the result later', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-ack',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '帮我看看北京天气' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        // The first thing that comes back is the instant colleague reply.
        expect(await screen.findByText(told('帮我看看北京天气'))).toBeTruthy();
        const userEntry = screen.getByText('帮我看看北京天气').closest('.desktop-bot-chat__entry');
        const botEntry = screen.getByText(told('帮我看看北京天气')).closest('.desktop-bot-chat__entry');
        expect(userEntry?.className).toContain('desktop-bot-chat__entry--user');
        expect(botEntry?.className).toContain('desktop-bot-chat__entry--assistant');
        expect(userEntry?.getAttribute('data-message-role')).toBe('user');
        expect(botEntry?.getAttribute('data-message-role')).toBe('assistant');
        // The composer is never locked while the task is running.
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再看看上海' } });
        expect((screen.getByTestId('desktop-bot-send') as HTMLButtonElement).disabled).toBe(false);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-ack',
            session_key: 'alice:bot_1',
            text: '北京今天晴，25 度。',
            send_to_user: '北京今天晴，25 度。',
        });
        expect(await screen.findByText('北京今天晴，25 度。')).toBeTruthy();
        // The accepted-task reply stays in the history; the result is its own bubble.
        expect(screen.getByText(told('帮我看看北京天气'))).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('formats a bot reply and keeps the command on the user side', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-md',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '**不要排版**' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-md',
            session_key: 'alice:bot_1',
            text: '**错误原因分析**\n\n- `app_list` 失败',
            send_to_user: '**错误原因分析**\n\n- `app_list` 失败',
        });
        expect(await screen.findByText('错误原因分析')).toBeTruthy();
        expect(screen.getByText('app_list').tagName).toBe('CODE');
        expect(screen.queryByText(/\*\*错误原因分析\*\*/)).toBeNull();
        expect(screen.getByText('**不要排版**').closest('.desktop-bot-chat__entry')?.className).toContain('desktop-bot-chat__entry--user');
        expect(screen.getByText('错误原因分析').closest('.desktop-bot-chat__entry')?.className).toContain('desktop-bot-chat__entry--assistant');
    });

    it('reports delivery failures the way a colleague would', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockRejectedValue(new Error('bot service is unavailable, contact the administrator'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('这条没送到——我这边暂时连不上我的服务器。麻烦稍后再发一次。')).toBeTruthy();
        expect(screen.queryByText('bot service is unavailable, contact the administrator')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
    });

    it('shows 接管电脑 in the chat when the bot needs a person and enters takeover', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-qr',
                    role: 'assistant',
                    content: '缺少用户扫描二维码登录小红书，笔记尚未发布。',
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const button = await screen.findByTestId('desktop-bot-take-computer');
        expect(button.textContent).toBe('接管电脑');
        fireEvent.click(button);
        const stage = await screen.findByTestId('desktop-bot-stage');
        expect(stage.className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
        expect(screen.queryByTestId('desktop-bot-take-computer')).toBeNull();
    });

    it('warns in the chat when the account credits are too low to finish a task', async () => {
        getHubCredits.mockResolvedValue({ active: true, credits_total: 100, credits_used: 80, credits_remaining: 20 });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const notice = await screen.findByTestId('desktop-bot-credit-notice');
        expect(notice.textContent).toContain('你的 Credits 快用完了');
        expect(refreshHubCredits).not.toHaveBeenCalled();
        const listCredits = await screen.findByTestId('desktop-bot-list-credits');
        expect(listCredits.textContent).toContain('剩余 Credits 20 / 100');
        expect(listCredits.nextElementSibling).toBe(screen.getByTestId('desktop-bot-list-topup'));
        const opened: unknown[] = [];
        const onOpen = (event: Event) => opened.push((event as CustomEvent).detail);
        window.addEventListener('maclaw:open-settings', onOpen);
        fireEvent.click(screen.getByTestId('desktop-bot-list-topup'));
        fireEvent.click(screen.getByTestId('desktop-bot-credit-topup'));
        window.removeEventListener('maclaw:open-settings', onOpen);
        expect(opened).toEqual([{ tab: 'redeem' }, { tab: 'redeem' }]);
        expect(browserOpenURL).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        expect((await screen.findByTestId('desktop-bot-credits-total')).textContent).toBe('100');
        expect(screen.getByTestId('desktop-bot-credits-left').textContent).toBe('20');
        fireEvent.click(screen.getByTestId('desktop-bot-credit-settings'));
        expect(await screen.findByText('Bot 用的模型')).toBeTruthy();
    });

    it('keeps an unlimited account balance quiet', async () => {
        getHubCredits.mockResolvedValue({ active: true });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        expect((await screen.findByTestId('desktop-bot-list-credits')).textContent).toContain('不限制');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        expect((await screen.findByTestId('desktop-bot-credits-total')).textContent).toBe('不限制');
        expect(screen.getByTestId('desktop-bot-credits-left').textContent).toBe('不限制');
        expect(screen.queryByTestId('desktop-bot-credit-notice')).toBeNull();
    });

    it('keeps the account balance in the list when the bot uses its own model', async () => {
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'own', model: 'm', key_set: true });
        getHubCredits.mockResolvedValue({ active: true, credits_total: 100, credits_used: 80, credits_remaining: 20 });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-list-credits')).textContent).toContain('剩余 Credits 20 / 100');
        expect(screen.getByTestId('desktop-bot-list-credits').getAttribute('data-notice')).toBe('low');
        expect(screen.queryByTestId('desktop-bot-credit-notice')).toBeNull();
        const mark = await screen.findByTestId('desktop-bot-custom-model');
        expect(mark.querySelector('svg')).toBeTruthy();
        expect(mark.getAttribute('role')).toBe('img');
        expect(mark.getAttribute('aria-label')).toBe('正在使用自定义模型：own · m');
        const create = screen.getByTestId('desktop-bot-create');
        expect(create.compareDocumentPosition(mark) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    });

    it('paints the custom-model mark from the model dialog when the first read failed', async () => {
        getBotLLM.mockRejectedValueOnce(new Error('llm down'));
        getHubCredits.mockResolvedValue({ active: true, credits_total: 100, credits_used: 0, credits_remaining: 100 });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-list-credits')).textContent).toContain('剩余 Credits 100 / 100');
        expect(screen.queryByTestId('desktop-bot-custom-model')).toBeNull();
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'Qwen', model: 'qwen-max', key_set: true });
        fireEvent.click(screen.getByTestId('desktop-bot-llm'));
        expect((await screen.findByTestId('desktop-bot-custom-model')).getAttribute('aria-label')).toBe('正在使用自定义模型：Qwen · qwen-max');
        expect(await screen.findByTestId('bot-llm-dialog')).toBeTruthy();
    });

    it('shows the custom-model mark when the wallet cannot be read', async () => {
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'Qwen', model: 'qwen-max', key_set: true });
        getHubCredits.mockRejectedValue(new Error('hub down'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-custom-model')).getAttribute('aria-label')).toBe('正在使用自定义模型：Qwen · qwen-max');
        expect(screen.queryByTestId('desktop-bot-list-credits')).toBeNull();
    });

    it('grays out takeover while the bot is driving and fullscreens when idle', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        // While the human watches, the hub hold keeps the desktop alive, so the
        // watch poll keeps serving the same live picture.
        // A URL that already says view_only must be rewritten, not appended.
        // noVNC keeps the last copy, and an empty value is still view-only.
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1&resize=scale&view_only=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-operating',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-operating',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.queryByRole('button', { name: '查看桌面' })).toBeNull();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await screen.findByTestId('desktop-bot-panel-stop');
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-ops').textContent).toContain('Bot 正在操作桌面，需要的话可以先停止它');
        const chatWhileDriving = screen.getByTestId('desktop-bot-chat');
        const panelWhileDriving = screen.getByTestId('desktop-bot-panel');
        const drivingSrc = (await screen.findByTestId('desktop-bot-handoff')).getAttribute('src') || '';
        expect(panelWhileDriving.contains(screen.getByTestId('desktop-bot-stage'))).toBe(true);
        expect(chatWhileDriving.contains(screen.getByTestId('desktop-bot-stage'))).toBe(false);
        expect(chatWhileDriving.contains(screen.getByTestId('desktop-bot-command'))).toBe(true);
        expect(drivingSrc).toContain('view_only=1');
        expect(drivingSrc.split('view_only=').length - 1).toBe(1);
        expect(screen.getByTestId('desktop-bot-screen-shield')).toBeTruthy();
        expectStopOnly();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-operating',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        const idle = await screen.findByTestId('desktop-bot-takeover');
        expect((idle as HTMLButtonElement).disabled).toBe(false);
        const watching = screen.getByTestId('desktop-bot-handoff');
        fireEvent.click(idle);
        // Takeover loads a new noVNC document with the keyboard on, and drops the watch shield.
        const stage = await screen.findByTestId('desktop-bot-stage');
        const controlling = screen.getByTestId('desktop-bot-handoff');
        expect(stage.className).toContain('is-fullscreen');
        expect(stage.className).toContain('is-user');
        expect(controlling).not.toBe(watching);
        expect(controlling.getAttribute('data-input')).toBe('live');
        const liveSrc = controlling.getAttribute('src') || '';
        expect(liveSrc).not.toContain('view_only=1');
        expect(liveSrc).toContain('view_only=0');
        expect(liveSrc.split('&').filter(part => part === 'view_only' || part.startsWith('view_only='))).toEqual(['view_only=0']);
        expect(liveSrc).toContain('autoconnect=1');
        expect(liveSrc).toContain('resize=scale');
        expect(liveSrc.split('view_only=').length - 1).toBe(1);
        expect(screen.queryByTestId('desktop-bot-screen-shield')).toBeNull();
        const sentBeforeExit = sendTask.mock.calls.length;
        fireEvent.click(screen.getByTestId('desktop-bot-exit-takeover'));
        const released = screen.getByTestId('desktop-bot-handoff');
        expect(screen.getByTestId('desktop-bot-stage').className).not.toContain('is-fullscreen');
        expect(released).not.toBe(controlling);
        expect(released.getAttribute('data-input')).toBe('watch');
        expect(released.getAttribute('src') || '').toContain('view_only=1');
        expect((released.getAttribute('src') || '').split('view_only=').length - 1).toBe(1);
        expect(screen.getByTestId('desktop-bot-screen-shield')).toBeTruthy();
        expect(sendTask.mock.calls.length).toBe(sentBeforeExit);
    });

    it('lets a person take over an idle bot while another bot is using its own desktop', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: `desktop-bot-operating-${botId}`,
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await screen.findByTestId('desktop-bot-bot_1');
        openRow('bot_1');
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-operating-bot_1',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
        });
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await screen.findByTestId('desktop-bot-panel-stop'); expectStopOnly();
        expect(screen.getByTestId('desktop-bot-ops').textContent).toContain('Bot 正在操作桌面，需要的话可以先停止它');

        openRow('bot_2');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const open = await screen.findByTestId('desktop-bot-takeover');
        expect((open as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId('desktop-bot-ops').textContent || '').not.toContain('正在操作桌面');
        fireEvent.click(open);
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=0');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
    });

    it('keeps a takeover when another bot starts using its own desktop', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: `desktop-bot-operating-${botId}`,
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await screen.findByTestId('desktop-bot-bot_2');
        openRow('bot_2');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(await screen.findByTestId('desktop-bot-takeover'));
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');

        openRow('bot_1');
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());

        openRow('bot_2');
        const held = await screen.findByTestId('desktop-bot-takeover');
        expect((held as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-exit-takeover')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=0');
    });

    it('locks takeover only while this bot is using its own desktop', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await screen.findByTestId('desktop-bot-bot_2');
        openRow('bot_2');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = await screen.findByTestId('desktop-bot-takeover');
        expect((takeover as HTMLButtonElement).disabled).toBe(false);

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', busy: true });
        });
        expect((screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId('desktop-bot-ops').textContent || '').not.toContain('正在操作桌面');

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', busy: true });
        });
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-ops').textContent).toContain('Bot 正在操作桌面，需要的话可以先停止它');

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', busy: false });
        });
        expect((screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement).disabled).toBe(false);
    });

    it('keeps this bot locked until its own overlapping schedules finish', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = () => screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement;
        expect((await screen.findByTestId('desktop-bot-takeover') as HTMLButtonElement).disabled).toBe(false);

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'bob', busy: true });
        });
        expect(takeover().disabled).toBe(false);

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', busy: true });
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', busy: true });
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', owner_id: 'alice', busy: true });
        });
        expectStopOnly();

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'bob', busy: false });
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', busy: false });
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', owner_id: 'alice', busy: false });
        });
        expectStopOnly();

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', busy: false });
        });
        expect(takeover().disabled).toBe(false);
    });

    it('does not lock takeover when another bot schedule is already driving', async () => {
        getDesktopBusy.mockResolvedValue({ seq: 4, marks: [{ owner_id: 'alice', bot_id: 'bot_1', count: 1 }] });
        listBots.mockResolvedValue([
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('日报'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = () => screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement;
        await screen.findByTestId('desktop-bot-takeover');
        await waitFor(() => expect(takeover().disabled).toBe(false));
    });

    it('locks takeover when this bot schedule is already driving', async () => {
        getDesktopBusy.mockResolvedValue({ seq: 4, marks: [{ owner_id: 'alice', bot_id: 'bot_2', count: 1 }] });
        listBots.mockResolvedValue([
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('日报'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = () => screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement;
        await screen.findByTestId('desktop-bot-panel-stop');
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', owner_id: 'alice', busy: false, count: 0, seq: 3 });
        });
        expectStopOnly();

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_2', owner_id: 'alice', busy: false, count: 0, seq: 5 });
        });
        expect(takeover().disabled).toBe(false);
    });

    it('locks takeover only for the bot whose run is still on its desktop', async () => {
        const stale = Date.now() - 31 * 60 * 1000 - 5000;
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{ id: `m-${stale.toString(36)}-stale`, role: 'assistant', content: '', pending: true, phase: 'execute' }],
            },
        }));
        getDesktopBusy.mockResolvedValue({ seq: 2, marks: [], runs: ['bot_1'] });
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await screen.findByTestId('desktop-bot-bot_2');
        openRow('bot_2');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = () => screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement;
        await screen.findByTestId('desktop-bot-takeover');
        await waitFor(() => expect(takeover().disabled).toBe(false));

        openRow('bot_1');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await waitFor(() => expectStopOnly());
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');

        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: false });
        });
        await waitFor(() => expect(takeover().disabled).toBe(false));
    });

    it('does not treat an older login handoff as the keyboard while that bot is driving', async () => {
        const now = Date.now();
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [
                    { id: `m-${now.toString(36)}-old`, role: 'assistant', content: '请在这个浏览器里登录。', userControl: true, attentionReason: 'login', handoffUrl: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1' },
                    { id: `m-${now.toString(36)}-run`, role: 'assistant', content: '', pending: true, phase: 'execute' },
                ],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await screen.findByTestId('desktop-bot-panel-stop');
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('watch');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
    });

    it('keeps takeover locked when this bot schedule is running over an older login', async () => {
        const now = Date.now();
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{ id: `m-${now.toString(36)}-old`, role: 'assistant', content: '请在这个浏览器里登录。', userControl: true, attentionReason: 'login', handoffUrl: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1' }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const takeover = () => screen.getByTestId('desktop-bot-takeover') as HTMLButtonElement;
        await screen.findByTestId('desktop-bot-takeover');
        await act(async () => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', busy: true });
        });
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-ops').textContent).toContain('Bot 正在操作桌面，需要的话可以先停止它');
    });

    it('keeps a login handoff when another bot is using its own desktop', async () => {
        const now = Date.now();
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_2: [{
                    id: `m-${now.toString(36)}-hand`,
                    role: 'assistant',
                    content: '请在这个浏览器里登录。',
                    userControl: true,
                    attentionReason: 'login',
                    handoffUrl: liveDesktop,
                }],
            },
        }));
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockImplementation(async (botId?: string) => ({
            novnc_url: liveDesktop,
            user_control: botId === 'bot_2',
            attention_reason: botId === 'bot_2' ? 'login' : '',
        }));
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: `desktop-bot-operating-${botId}`,
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await screen.findByTestId('desktop-bot-bot_2');
        openRow('bot_2');
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect((await screen.findByTestId('desktop-bot-takeover') as HTMLButtonElement).disabled).toBe(false);

        openRow('bot_1');
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());

        openRow('bot_2');
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        const open = await screen.findByTestId('desktop-bot-takeover');
        expect((open as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByTestId('desktop-bot-ops').textContent || '').not.toContain('正在操作桌面');
        fireEvent.click(open);
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=0');
    });

    it('fullscreens the computer preview from the corner control and restores it without taking the keyboard', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const expand = await screen.findByTestId('desktop-bot-screen-expand');
        expect(expand.getAttribute('aria-label')).toBe('放大');
        const stage = screen.getByTestId('desktop-bot-stage');
        expect(stage.className).not.toContain('is-fullscreen');
        expect(stage.querySelector('.desktop-bot-stage__bar')?.contains(expand)).toBe(false);
        expect(stage.querySelector('.desktop-bot-stage__frame')?.contains(expand)).toBe(true);
        expect(stage.textContent).toContain('正在观看桌面。观看期间桌面保持开启。');
        expect(screen.queryByTestId('desktop-bot-exit-takeover')).toBeNull();

        fireEvent.click(expand);
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('正在观看桌面。观看期间桌面保持开启。');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('tabindex')).toBe('-1');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('watch');
        const shield = screen.getByTestId('desktop-bot-screen-shield');
        expect(shield.contains(screen.getByTestId('desktop-bot-screen-restore'))).toBe(false);
        expect(screen.getByTestId('desktop-bot-chat').inert).toBe(true);
        expect(screen.queryByTestId('desktop-bot-screen-expand')).toBeNull();
        const restore = screen.getByTestId('desktop-bot-screen-restore');
        expect(restore.textContent).toContain('恢复');
        expect(sendTask).not.toHaveBeenCalled();

        fireEvent.click(restore);
        expect(screen.getByTestId('desktop-bot-stage').className).not.toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-chat').inert).toBe(false);
        expect(screen.getByTestId('desktop-bot-screen-expand')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');

        fireEvent.click(screen.getByTestId('desktop-bot-screen-expand'));
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.getByTestId('desktop-bot-stage').className).not.toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('tabindex')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-screen-expand'));
        expect(screen.getByTestId('desktop-bot-chat').inert).toBe(true);
        await act(async () => {
            responseListeners.byEvent['desktop-bot-view']?.({
                request_id: 'desktop-bot-clear',
                session_key: 'alice:bot_1',
                cleared: true,
            });
        });
        expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
        expect(screen.getByTestId('desktop-bot-chat').inert).toBe(false);
    });

    it('keeps a watched desktop alive by polling the desktop watch', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        // The hold keeps this desktop up for the watcher, so every poll answers
        // with the live picture until the panel is closed.
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-watch',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-watch',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        await waitFor(() => expect(watchBot).toHaveBeenCalledWith('bot_1', expect.any(Number)));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-watch',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        // The panel stays open and keeps refreshing the hold after the reply.
        await waitFor(() => expect(watchBot.mock.calls.length).toBeGreaterThanOrEqual(1));
        expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy();
        // Collapsing the panel stops the watch loop.
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull());
        const callsAfterClose = watchBot.mock.calls.length;
        await new Promise(resolve => setTimeout(resolve, 1000));
        expect(watchBot.mock.calls.length).toBe(callsAfterClose);
    });

    it('starts a later job while an earlier one is still running', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-q${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(told('打开示例网站'))).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');

        // The first job is still running. This one starts beside it. The
        // running job is not cancelled, and the chat is not held until it ends.
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('表述:顺便截一张桌面')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder('顺便截一张桌面', 'plan', ['打开示例网站']), 'execute');

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q1',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(2);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q2',
            session_key: 'alice:bot_1',
            text: '已截图',
            send_to_user: '已截图',
        });
        expect(await screen.findByText('已截图')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('folds a follow-up into the one live run it changes, even when that run is not the first', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({ request_id: 'desktop-bot-live', deferred: true, session_key: `alice:${botId}` }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        listActiveRuns.mockResolvedValue([
            { run_id: 'run-arm', task_text: '优化 arm64' },
            { run_id: 'run-head', task_text: '换工具头' },
        ]);
        evaluateSupplement.mockImplementation(async (taskText?: string) => (
            taskText === '换工具头' ? { is_supplement: true, instruction: '只要前5条' } : { is_supplement: false }
        ));
        appendSupplement.mockResolvedValue({ accepted: true, stage: 'queued', message: '已收到，会在换工具头的下一步生效。', supplement_id: 'sup-head' });

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '只要前5条' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('已收到，会在换工具头的下一步生效。')).toBeTruthy();
        await waitFor(() => expect(appendSupplement).toHaveBeenCalledWith('bot_1', 'run-head', '只要前5条'));
        expect(evaluateSupplement.mock.calls.map(call => call[0])).toEqual(['优化 arm64', '换工具头']);
        expect(sendTask).toHaveBeenCalledTimes(1);
    });

    it('starts a different job when none of the live runs claim it', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-side${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        listActiveRuns.mockResolvedValue([
            { run_id: 'run-arm', task_text: '优化 arm64' },
            { run_id: 'run-head', task_text: '换工具头' },
        ]);
        evaluateSupplement.mockResolvedValue({ is_supplement: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '在 mac mini 上优化推理' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(told('在 mac mini 上优化推理'))).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(appendSupplement).not.toHaveBeenCalled();
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder('在 mac mini 上优化推理', 'plan', ['打开示例网站']), 'execute');
    });

    it('asks which job a follow-up belongs to when more than one run matches', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({ request_id: 'desktop-bot-live', deferred: true, session_key: `alice:${botId}` }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        listActiveRuns.mockResolvedValue([
            { run_id: 'run-arm', task_text: '优化 arm64' },
            { run_id: 'run-head', task_text: '换工具头' },
        ]);
        evaluateSupplement.mockResolvedValue({ is_supplement: true, instruction: '只要前5条' });
        appendSupplement.mockResolvedValue({ accepted: false, stage: 'ambiguous', message: '你有多个任务正在执行，请说明这条是补充给哪一个。' });

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '只要前5条' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('你有多个任务正在执行，请说明这条是补充给哪一个。')).toBeTruthy();
        expect(await screen.findByTestId('desktop-bot-reading-verbatim')).toBeTruthy();
        expect(appendSupplement).toHaveBeenCalledWith('bot_1', '', '只要前5条');
        expect(sendTask).toHaveBeenCalledTimes(1);
    });

    it('sends a follow-up on its own when the run it matched has already ended', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-ended${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        listActiveRuns.mockResolvedValue([{ run_id: 'run-head', task_text: '换工具头' }]);
        evaluateSupplement.mockResolvedValue({ is_supplement: true, instruction: '只要前5条' });
        appendSupplement.mockResolvedValue({ accepted: false, stage: 'no_active_run', message: '那个任务已经结束了，这条会作为新任务发送。' });

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '只要前5条' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(screen.queryByText('那个任务已经结束了，这条会作为新任务发送。')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-no-reading')).toBeNull();
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder('只要前5条', 'plan', ['打开示例网站']), 'execute');
    });

    it('runs queued desktop commands in send order when a later reading finishes first', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-order${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        let releaseFirst: () => void = () => {};
        const firstGate = new Promise<void>(resolve => { releaseFirst = resolve; });
        const answer = '我是你在这台云桌面上的同事。';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (text === '先打开百度') await firstGate;
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (text === '你是谁呀?') return { told: answer, instruction: '', pace: 'reply' as const };
            return { told: `表述:${text}`, instruction: childOrder(text, phase === 'execute' ? 'execute' : 'plan', earlier), pace: 'direct' as const };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '先打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '你是谁呀?' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        expect(await screen.findByText(answer)).toBeTruthy();
        expect(await screen.findByText('收到，「再截一张桌面」记下了。先排在前面那条后面。')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        expect(screen.queryByText(/记下了。等你把键盘/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);

        releaseFirst();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask.mock.calls[1][1]).toBe(childOrder('先打开百度', 'plan', ['打开示例网站']));
        expect(sendTask.mock.calls[2][1]).toBe(childOrder('再截一张桌面', 'plan', ['打开示例网站', '先打开百度', '你是谁呀?']));
        expect(understandTask.mock.calls.filter(call => call[0] === '再截一张桌面')).toHaveLength(1);
    });

    it('does not start a new command ahead of a follow-up that is still being read', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-gap${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        let releaseFirst: () => void = () => {};
        const firstGate = new Promise<void>(resolve => { releaseFirst = resolve; });
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (text === '先打开百度') await firstGate;
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            return { told: `表述:${text}`, instruction: childOrder(text, phase === 'execute' ? 'execute' : 'plan', earlier), pace: 'direct' as const };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '先打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-gap1',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('安排中');
        expect(sendTask).toHaveBeenCalledTimes(1);

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('收到，「再截一张桌面」记下了。先排在前面那条后面。')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);

        releaseFirst();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask.mock.calls[1][1]).toBe(childOrder('先打开百度', 'plan', ['打开示例网站']));
        expect(sendTask.mock.calls[2][1]).toBe(childOrder('再截一张桌面', 'plan', ['打开示例网站', '先打开百度']));
    });

    it('holds an ordinary queued message while a login handoff still has the keyboard', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-holdq${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(told('打开示例网站'))).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('表述:顺便截一张桌面')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder('顺便截一张桌面', 'plan', ['打开示例网站']), 'execute');

        // The screenshot job finishes on its own. The login reply then hands
        // the keyboard over. The screenshot is not sent again.
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-holdq2',
            session_key: 'alice:bot_1',
            text: '已截图',
            send_to_user: '已截图',
        });
        expect(await screen.findByText('已截图')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-holdq1',
            session_key: 'alice:bot_1',
            text: '需要你登录',
            send_to_user: '需要你登录',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(2);

        fireEvent.click(screen.getByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute');
        expect(sendTask.mock.calls.filter(call => String(call[1]).includes('顺便截一张桌面'))).toHaveLength(1);

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-holdq3',
            session_key: 'alice:bot_1',
            text: '登录后的页面到了。',
            send_to_user: '登录后的页面到了。',
        });
        expect(await screen.findByText('登录后的页面到了。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(3);
    });

    it('keeps the login handoff usable while another job is still running', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-beside${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));

        await act(async () => {
            responseListeners.byEvent['ai-assistant-response']?.({
                request_id: 'desktop-bot-beside1',
                session_key: 'alice:bot_1',
                text: '需要你登录',
                send_to_user: '需要你登录',
                desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
                desktop_user_control: true,
            });
            // The screenshot job is still a live run. That signal used to
            // take the login keyboard back and hide this button.
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
        expect(sendTask).toHaveBeenCalledTimes(2);

        fireEvent.click(screen.getByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute');
        expect(sendTask.mock.calls.filter(call => String(call[1]).includes('顺便截一张桌面'))).toHaveLength(1);
    });

    it('does not let another job quiet view end a login handoff', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-quiet${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-quiet1')).toBe(true));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-quiet2')).toBe(true));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-quiet1',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-quiet2',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: false,
        });
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
    });

    it('keeps the login picture when a later job fails to send', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        let failSecond: (() => void) | undefined;
        sendTask.mockImplementation((botId: string) => {
            counter += 1;
            if (counter === 2) {
                return new Promise((_resolve, reject) => {
                    failSecond = () => reject(new Error('bot service is unavailable, contact the administrator'));
                });
            }
            return Promise.resolve({ request_id: `desktop-bot-keep${counter}`, deferred: true, session_key: `alice:${botId}` });
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-keep1')).toBe(true));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-keep1',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        if (!failSecond) throw new Error('second send did not start');
        failSecond();
        expect(await screen.findByText('这条没送到——我这边暂时连不上我的服务器。麻烦稍后再发一次。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
    });

    it('keeps the desktop picture when another job reports cleared', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-clear${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-clear1')).toBe(true));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-clear2')).toBe(true));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect(await screen.findByTestId('desktop-bot-stage')).toBeTruthy();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-clear2',
            session_key: 'alice:bot_1',
            cleared: true,
        });
        expect(screen.getByTestId('desktop-bot-stage')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('vnc.html');
    });

    it('keeps a login keyboard when a watch poll returns after a later job started', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        let releaseWatch: ((view: { novnc_url: string; user_control: boolean }) => void) | undefined;
        watchBot.mockImplementation(() => new Promise(resolve => { releaseWatch = resolve; }));
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-poll${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-poll1')).toBe(true));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await waitFor(() => expect(watchBot).toHaveBeenCalled());
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-poll2')).toBe(true));
        if (!releaseWatch) throw new Error('desktop watch did not start');
        releaseWatch({ novnc_url: liveDesktop, user_control: true });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('live');
        const login = messagesForBot('alice', 'bot_1').find(item => item.requestId === 'desktop-bot-poll1');
        const beside = messagesForBot('alice', 'bot_1').find(item => item.requestId === 'desktop-bot-poll2');
        expect(login?.userControl).toBe(true);
        expect(beside?.userControl).not.toBe(true);
    });

    it('refreshes the credit balance once for each finished job', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-credit${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-credit1')).toBe(true));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-credit2')).toBe(true));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-credit1',
            session_key: 'alice:bot_1',
            text: '第一件做完了',
            send_to_user: '第一件做完了',
        });
        await waitFor(() => expect(refreshHubCredits).toHaveBeenCalledTimes(1));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-credit2',
            session_key: 'alice:bot_1',
            text: '第二件做完了',
            send_to_user: '第二件做完了',
        });
        await waitFor(() => expect(refreshHubCredits).toHaveBeenCalledTimes(2));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-credit1',
            session_key: 'alice:bot_1',
            text: '第一件做完了',
            send_to_user: '第一件做完了',
        });
        await waitFor(() => expect(screen.getByText('第二件做完了')).toBeTruthy());
        expect(refreshHubCredits).toHaveBeenCalledTimes(2);
    });

    it('answers small talk during a login handoff and holds a new desktop command', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const answer = '我是你在这台云桌面上的同事。';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (text === '你是谁呀?') return { told: answer, instruction: '', pace: 'reply' as const };
            return { told: `表述:${text}`, instruction: childOrder(text, phase === 'execute' ? 'execute' : 'plan', earlier), pace: 'direct' as const };
        });
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-handoff-chat-${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-handoff-chat-1',
            session_key: 'alice:bot_1',
            text: '需要你登录',
            send_to_user: '需要你登录',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '你是谁呀?' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(answer)).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('收到，「顺便截一张桌面」记下了。等你把键盘交还后我再处理这个。')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();

        fireEvent.click(screen.getByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask.mock.calls[1][1]).toBe(RETURN_TASK);
        expect(sendTask.mock.calls[1][2]).toBe('execute');
        expect(sendTask.mock.calls[2][1]).toBe(childOrder('顺便截一张桌面', 'plan', ['打开示例网站', '你是谁呀?', RETURN_TASK]));

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-handoff-chat-2',
            session_key: 'alice:bot_1',
            text: '登录后的页面到了。',
            send_to_user: '登录后的页面到了。',
        });
        expect(await screen.findByText('登录后的页面到了。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(3);
    });

    it('sends a queued login continuation before an ordinary message when the reply is still a handoff', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-resumeq${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('表述:顺便截一张桌面')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask.mock.calls[1][1]).toBe(childOrder('顺便截一张桌面', 'plan', ['打开示例网站']));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-resumeq2',
            session_key: 'alice:bot_1',
            text: '已截图',
            send_to_user: '已截图',
        });
        expect(await screen.findByText('已截图')).toBeTruthy();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-resumeq1',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        fireEvent.click(await screen.findByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute');
        expect(sendTask.mock.calls.filter(call => String(call[1]).includes('顺便截一张桌面'))).toHaveLength(1);

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-resumeq1',
            session_key: 'alice:bot_1',
            text: '需要你登录',
            send_to_user: '需要你登录',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(await screen.findByText('需要你登录')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(3);
    });

    it('keeps a collapsed desktop panel closed until a keyboard handoff', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-collapse',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-computer')).toBeTruthy();
        expect(screen.getByText('值班 的屏幕')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull());
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        const watched = watchBot.mock.calls.length;
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(watchBot.mock.calls.length).toBe(watched);
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: false,
        });
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            text: '需要你登录',
            send_to_user: '需要你登录',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        const stage = await screen.findByTestId('desktop-bot-stage');
        expect(stage.className).toContain('is-user');
        const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(src).toContain('view_only=0');
        expect(src).not.toContain('view_only=1');
    });

    it('opens the next login after a collapsed handoff is released without the screen popping open on the quiet report', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-next',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-next',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
            user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull());
        // The next turn's first poll already sees the keyboard back. That report
        // ends the collapsed handoff and must not itself open the picture.
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-next',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
            user_control: false,
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-later',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
            user_control: true,
        });
        const stage = await screen.findByTestId('desktop-bot-stage');
        expect(stage.className).toContain('is-user');
        expect(screen.getByTestId('desktop-bot-computer')).toBeTruthy();
    });

    it('re-dispatches accepted tasks that survived a page reload', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-recover', deferred: true, session_key: 'alice:bot_1' });
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [
                    { id: 'm-rec1', role: 'user', content: '打开示例网站', queued: true },
                    { id: 'm-rec2', role: 'assistant', content: '收到，「打开示例网站」记下了。等手头这件做完我就来处理这个。', ack: true },
                ],
            },
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        // No user action needed: an accepted-but-unsent task dispatches on its
        // own when the page comes back.
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('打开示例网站'), 'execute'));
        // The queue marker clears so a later reload cannot send it twice.
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, Array<{ id: string; queued?: boolean }>>>;
        const accepted = (stored.alice.bot_1 || []).find(item => item.id === 'm-rec1');
        expect(accepted?.queued).toBe(false);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-recover',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        // The recovery dispatch happened without selecting the bot; opening its
        // chat shows the recorded acceptance and the settled result.
        fireEvent.click(await screen.findByText('值班'));
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.getByText('收到，「打开示例网站」记下了。等手头这件做完我就来处理这个。')).toBeTruthy();
    });

    it('clears local history when a bot is deleted', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-1', role: 'user', content: '旧任务' }] },
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const row = screen.getByTestId('desktop-bot-bot_1');
        const removeButton = row?.querySelector('button[data-testid^="desktop-bot-delete-"]') as HTMLButtonElement;
        fireEvent.click(removeButton);
        expect(removeButton.getAttribute('aria-label')).toBe('删除');
        expect(removeButton.querySelector('svg')).toBeTruthy();
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, unknown[]>>;
        expect((stored.alice?.bot_1 || []).length).toBe(0);
    });

    it('ignores a second delete click until the first request finishes', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '早上', instance_id: 'inst_2' },
        ]);
        let release: (value?: undefined | PromiseLike<undefined>) => void = () => {};
        deleteBot.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const remove = screen.getByTestId('desktop-bot-bot_1').querySelector('[data-testid^="desktop-bot-delete-"]') as HTMLButtonElement;
        fireEvent.click(remove);
        fireEvent.click(remove);
        expect(screen.getAllByRole('dialog')).toHaveLength(1);
        expect(remove.getAttribute('aria-label')).toBe('删除');
        expect(remove.querySelector('svg')).toBeTruthy();
        expect(deleteBot).not.toHaveBeenCalled();
        await confirmBotDelete();
        expect(deleteBot).toHaveBeenCalledTimes(1);
        fireEvent.click(remove);
        expect(screen.queryByRole('dialog')).toBeNull();
        expect(deleteBot).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByText('日报'));
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).placeholder).toBe('给 日报 发消息');
        await act(async () => { release(); });
        expect(screen.queryByTestId('desktop-bot-bot_1')).toBeNull();
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).placeholder).toBe('给 日报 发消息');
    });

    it('keeps the bot when delete fails and allows another try', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        deleteBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const remove = screen.getByTestId('desktop-bot-delete-bot_1');
        fireEvent.click(remove);
        await confirmBotDelete();
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.getByTestId('desktop-bot-bot_1')).toBeTruthy();
        deleteBot.mockResolvedValueOnce(undefined);
        fireEvent.click(remove);
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        expect(deleteBot).toHaveBeenCalledTimes(2);
    });

    it('clears local history when the page closes before delete returns', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-1', role: 'user', content: '旧任务' }] },
        }));
        let release: (value?: undefined | PromiseLike<undefined>) => void = () => {};
        deleteBot.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-delete-bot_1'));
        await confirmBotDelete();
        expect(deleteBot).toHaveBeenCalledWith('bot_1');
        view.unmount();
        await act(async () => { release(); });
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, unknown[]>>;
        expect((stored.alice?.bot_1 || []).length).toBe(0);
    });

    it('keeps the next account\'s queued message when a delete finishes late', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-alice', role: 'user', content: '爱丽丝的任务' }] },
        }));
        let release: (value?: undefined | PromiseLike<undefined>) => void = () => {};
        deleteBot.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-delete-bot_1'));
        await confirmBotDelete();
        expect(deleteBot).toHaveBeenCalledWith('bot_1');

        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-alice', role: 'user', content: '爱丽丝的任务' }] },
            bob: {
                bot_1: [
                    { id: 'm-bob', role: 'user', content: '鲍勃的任务', queued: true },
                    { id: 'm-busy', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-busy' },
                ],
            },
        }));
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-bob', deferred: true, session_key: 'bob:bot_1' });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_2' }]);
        view.rerender(<DialogProvider><DesktopBotWorkspace lang="zh-Hans" userId="bob" /></DialogProvider>);
        expect(await screen.findByText('值班')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        await act(async () => { release(); });
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, unknown[]>>;
        expect((stored.alice?.bot_1 || []).length).toBe(0);
        expect((stored.bob?.bot_1 || []).length).toBeGreaterThan(0);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-busy',
            session_key: 'bob:bot_1',
            text: '先做完了',
            send_to_user: '先做完了',
        });
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('鲍勃的任务'), 'execute'));
    });

    it('shows the Hub error when the bot list cannot be loaded', async () => {
        listBots.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.queryByTestId('desktop-bot-empty')).toBeNull();
    });

    it('keeps the bot when renaming fails', async () => {
        listBots.mockResolvedValueOnce([{ id: 'bot_1', title: 'Bot 1', description: '值班', instance_id: 'inst_1' }]);
        renameBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('Bot 1')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        const editDialog = await screen.findByTestId('bot-edit-dialog');
        fireEvent.change(within(editDialog).getByLabelText('名称'), { target: { value: '新名字' } });
        fireEvent.click(within(editDialog).getByRole('button', { name: '保存' }));
        expect(await screen.findByText('MaClawSrv 不可达')).toBeTruthy();
        expect(screen.getByTestId('bot-edit-dialog')).toBeTruthy();
        fireEvent.click(within(editDialog).getByRole('button', { name: '取消' }));
        expect(screen.queryByTestId('bot-edit-dialog')).toBeNull();
        expect(screen.getByText('Bot 1')).toBeTruthy();
        expect(screen.queryByText('新名字')).toBeNull();
    });

    it('edits bot info through the modal dialog and cancels cleanly', async () => {
        listBots.mockResolvedValueOnce([{ id: 'bot_1', title: 'Bot 1', description: '值班', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('Bot 1')).toBeTruthy();

        // The dialog owns the fields; the row never turns into inputs.
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        const dialog = screen.getByTestId('bot-edit-dialog');
        expect(within(dialog).getByLabelText('名称')).toBeTruthy();
        expect(within(screen.getByTestId('desktop-bot-bot_1')).queryByLabelText('名称')).toBeNull();

        // An empty name cannot be saved.
        const name = within(dialog).getByLabelText('名称') as HTMLInputElement;
        fireEvent.change(name, { target: { value: '  ' } });
        expect((within(dialog).getByRole('button', { name: '保存' }) as HTMLButtonElement).disabled).toBe(true);

        // Escape cancels without touching the backend.
        fireEvent.change(name, { target: { value: '值班' } });
        fireEvent.keyDown(dialog, { key: 'Escape' });
        expect(screen.queryByTestId('bot-edit-dialog')).toBeNull();
        expect(renameBot).not.toHaveBeenCalled();
        expect(screen.getByText('Bot 1')).toBeTruthy();

        // Reopen and save: the rename reaches the backend and refreshes the row.
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        const reopened = screen.getByTestId('bot-edit-dialog');
        fireEvent.change(within(reopened).getByLabelText('名称'), { target: { value: '值班' } });
        fireEvent.change(within(reopened).getByLabelText('描述'), { target: { value: '晚上值守' } });
        fireEvent.click(within(reopened).getByRole('button', { name: '保存' }));
        await waitFor(() => expect(renameBot).toHaveBeenCalledWith('bot_1', '值班', '晚上值守'));
        expect(await screen.findByText('晚上值守')).toBeTruthy();
        expect(screen.queryByTestId('bot-edit-dialog')).toBeNull();
    });

    it('shows the result that arrived after leaving the bots page', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-1',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        handleDesktopBotResult({
            request_id: 'desktop-bot-1',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.queryByText('正在工作')).toBeNull();
    });

    it('shows a desktop screenshot inside the bot reply', async () => {
        const png = 'iVBORw0KGgo=';
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-shot',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '看一下环境' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('看一下环境')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-shot',
            session_key: 'alice:bot_1',
            text: '桌面截图已生成',
            send_to_user: '桌面截图已生成',
            desktop_images: [
                { mime: 'image/png', data: png },
                { mime: 'text/html', data: png },
            ],
        });
        const img = await screen.findByTestId('desktop-bot-screenshot');
        expect(img.getAttribute('src')).toBe(`data:image/png;base64,${png}`);
        expect(img.getAttribute('alt')).toBe('桌面截图');
        expect(screen.getByText('桌面截图已生成')).toBeTruthy();

        fireEvent.click(screen.getByTestId('desktop-bot-screenshot-open'));
        const preview = await screen.findByTestId('desktop-bot-shot-preview');
        expect(preview.getAttribute('role')).toBe('dialog');
        expect(preview.getAttribute('aria-modal')).toBe('true');
        expect(preview.getAttribute('aria-label')).toBe('桌面截图');
        const full = within(preview).getByTestId('desktop-bot-shot-preview-image');
        expect(full.getAttribute('src')).toBe(`data:image/png;base64,${png}`);
        expect(full.getAttribute('alt')).toBe('');
        expect(screen.getByTestId('desktop-bot-shot-preview-close')).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: 'Tab' });
        expect(screen.getByTestId('desktop-bot-shot-preview-close')).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
        expect(screen.getByTestId('desktop-bot-command')).not.toBe(document.activeElement);
        fireEvent.mouseDown(full);
        fireEvent.click(full);
        expect(screen.getByTestId('desktop-bot-shot-preview')).toBeTruthy();

        fireEvent.click(screen.getByTestId('desktop-bot-shot-preview-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull());
        expect(screen.getByTestId('desktop-bot-screenshot-open')).toBe(document.activeElement);

        fireEvent.click(screen.getByTestId('desktop-bot-screenshot-open'));
        fireEvent.keyDown(document, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull());

        fireEvent.click(screen.getByTestId('desktop-bot-screenshot-open'));
        const again = screen.getByTestId('desktop-bot-shot-preview');
        fireEvent.mouseDown(again);
        fireEvent.click(again);
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull());
        expect(document.body.style.overflow).toBe('');
    });

    it('shows a screenshot from the local file and does not store the bytes', async () => {
        const png = 'iVBORw0KGgo=';
        const shotPath = 'C:\\Users\\alice\\.maclaw\\data\\bot-files\\bot_1\\screenshot.png';
        readShot.mockResolvedValue({ mime: 'image/png', data: png });
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-shot-file',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '看一下环境' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('看一下环境')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-shot-file',
            session_key: 'alice:bot_1',
            text: '桌面截图已生成',
            send_to_user: '桌面截图已生成',
            desktop_shot_paths: ['../secret.png', shotPath],
            desktop_images: [{ mime: 'image/png', data: 'SHOULD_NOT_STORE' }],
        });
        const img = await screen.findByTestId('desktop-bot-screenshot');
        expect(img.getAttribute('src')).toBe(`data:image/png;base64,${png}`);
        fireEvent.click(screen.getByRole('button', { name: '查看大图' }));
        expect((await screen.findByTestId('desktop-bot-shot-preview-image')).getAttribute('src')).toBe(`data:image/png;base64,${png}`);
        fireEvent.click(screen.getByRole('button', { name: '关闭' }));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull());
        expect(readShot).toHaveBeenCalledWith(shotPath);
        const stored = localStorage.getItem('maclaw.desktopBotMessages.v1') || '';
        expect(stored).toContain(JSON.stringify(shotPath));
        expect(stored).not.toContain('SHOULD_NOT_STORE');
        expect(stored).not.toContain(png);
    });

    it('drops the screenshot preview when the account changes', async () => {
        const png = 'iVBORw0KGgo=';
        listBots.mockResolvedValue([
            { id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '早上', instance_id: 'inst_2' },
        ]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-shot-switch',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '看一下环境' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('看一下环境')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-shot-switch',
            session_key: 'alice:bot_1',
            text: '桌面截图已生成',
            send_to_user: '桌面截图已生成',
            desktop_images: [{ mime: 'image/png', data: png }],
        });
        fireEvent.click(await screen.findByTestId('desktop-bot-screenshot-open'));
        expect(await screen.findByTestId('desktop-bot-shot-preview')).toBeTruthy();
        fireEvent.click(screen.getByText('日报'));
        expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull();
        expect(document.body.style.overflow).toBe('');

        fireEvent.click(screen.getByText('Bot 1'));
        fireEvent.click(await screen.findByTestId('desktop-bot-screenshot-open'));
        expect(screen.getByTestId('desktop-bot-shot-preview')).toBeTruthy();
        view.rerender(<DialogProvider><DesktopBotWorkspace lang="zh-Hans" userId="bob" /></DialogProvider>);
        expect(screen.queryByTestId('desktop-bot-shot-preview')).toBeNull();
        expect(document.body.style.overflow).toBe('');
    });

    it('shows a document download on the bot reply', async () => {
        const docx = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-file',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '生成一篇word文档发我' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('生成一篇word文档发我')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-file',
            session_key: 'alice:bot_1',
            text: '文件已放在这条回复里',
            send_to_user: '文件已放在这条回复里',
            desktop_files: [
                { name: '../secret.docx', mime: docx, data: 'd29yZA==' },
                { name: '自我描述.docx', mime: docx, data: 'd29yZA==' },
            ],
        });
        expect(await screen.findByTestId('desktop-bot-file')).toBeTruthy();
        expect(screen.getByText('下载 自我描述.docx')).toBeTruthy();
        expect(screen.queryByText('下载 ../secret.docx')).toBeNull();
    });

    it('shows a pdf download on the bot reply', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-pdf',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '把北京天气的 PDF 发给我' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('把北京天气的 PDF 发给我')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-pdf',
            session_key: 'alice:bot_1',
            text: '天气文件已附在这条消息上',
            send_to_user: '天气文件已附在这条消息上',
            desktop_files: [
                { name: '../secret.pdf', mime: 'application/pdf', data: 'd29yZA==' },
                { name: '北京天气.pdf', mime: 'application/pdf', data: 'd29yZA==' },
            ],
        });
        expect(await screen.findByTestId('desktop-bot-file')).toBeTruthy();
        expect(screen.getByText('下载 北京天气.pdf')).toBeTruthy();
        expect(screen.queryByText('下载 ../secret.pdf')).toBeNull();
    });

    it('shows a saved document in the result area', async () => {
        const docx = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
        const docPath = 'C:\\Users\\alice\\MaClaw\\bot-files\\自我描述.docx';
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-saved',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '生成一篇word文档发我' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('生成一篇word文档发我')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-saved',
            session_key: 'alice:bot_1',
            text: '文件已放在这条回复里',
            send_to_user: '文件已放在这条回复里',
            local_file_paths: ['../secret.docx', docPath],
            desktop_files: [
                { name: '自我描述.docx', mime: docx, data: 'd29yZA==' },
            ],
        });
        expect(await screen.findByTestId('task-result-view-btn')).toBeTruthy();
        expect(screen.getByText('查看文档')).toBeTruthy();
        expect(screen.getByTestId('task-result-export-btn').textContent).toContain('导出');
        expect(screen.getByText('自我描述.docx')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-file')).toBeNull();
        expect(screen.queryByText('../secret.docx')).toBeNull();
        const stored = localStorage.getItem('maclaw.desktopBotMessages.v1') || '';
        expect(stored).toContain(JSON.stringify(docPath));
        expect(stored).not.toContain('d29yZA==');
        fireEvent.click(screen.getByTestId('task-result-preview-btn'));
        const preview = await screen.findByTestId('bot-file-preview');
        expect(preview.getAttribute('aria-label')).toBe('文件预览');
        expect(screen.getByTestId('bot-file-preview-title').textContent).toBe('自我描述.docx');
        expect(previewFile).toHaveBeenCalledWith(docPath);
        expect(await screen.findByTestId('docx-preview-error')).toBeTruthy();
        expect(openFile).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('bot-file-preview-close'));
        await waitFor(() => expect(screen.queryByTestId('bot-file-preview')).toBeNull());
        fireEvent.click(screen.getByTestId('task-result-view-btn'));
        expect(openFile).toHaveBeenCalledWith(docPath);
        expect(showFile).not.toHaveBeenCalled();
    });

    it('shows the login desktop again after leaving the bots page', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-2',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        handleDesktopBotView({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const loginSrc = (await screen.findByTestId('desktop-bot-handoff')).getAttribute('src') || '';
        expect(loginSrc).toContain('view_only=0');
        expect(loginSrc).not.toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
    });

    it('shows a result that arrives after the bot page is open again', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-3',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-3',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
    });

    it('shows the Hub error when a bot cannot be created', async () => {
        createBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.getByTestId('desktop-bot-empty')).toBeTruthy();
    });

    it('keeps the browser on screen when the command times out', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-4',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-4',
            session_key: 'alice:bot_1',
            error: 'MaClawSrv 没有在时限内返回结果',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        // The timeout wording differs from the unreachable wording so the
        // colleague excuse matches what actually happened.
        expect(await screen.findByText('这个任务等了太久，我这边超时了。稍后再发一次试试。')).toBeTruthy();
        expect(screen.queryByText('MaClawSrv 没有在时限内返回结果')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
        expect(watchBot).not.toHaveBeenCalled();
    });

    it('says the bot credit allowance is used up instead of the server sentence', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-credits',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-credits',
            session_key: 'alice:bot_1',
            error: 'selected model grant credits are exhausted',
        });
        expect(await screen.findByText('你的 Credits 已经用完。请充值，或把这个 Bot 改成自己的模型。')).toBeTruthy();
        expect(screen.queryByText('selected model grant credits are exhausted')).toBeNull();
    });

    it('confirms only the latest finished plan and that button is the execute path', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => ({
            told: `安排:${content}`,
            instruction: childOrder(String(content), phase === 'execute' ? 'execute' : 'plan', earlier),
            pace: 'confirm' as const,
        }));
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-plan-${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '开始吧' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('安排:开始吧')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        const first = await screen.findByTestId('desktop-bot-confirm');
        expect((first as HTMLButtonElement).disabled).toBe(false);
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '改成只看首页' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('安排:改成只看首页')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        const confirmButtons = screen.getAllByTestId('desktop-bot-confirm');
        expect(confirmButtons).toHaveLength(1);
        expect(confirmButtons[0].closest('.desktop-bot-chat__entry')?.textContent || '').toContain('安排:改成只看首页');
        expect(screen.getByText('安排:开始吧')).toBeTruthy();
        fireEvent.click(confirmButtons[0]);
        const order = childOrder('改成只看首页', 'plan', ['开始吧']);
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', order, 'execute');
        expect(understandTask.mock.calls.some(call => call[0] === CONFIRM_TASK)).toBe(false);
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.queryByText(order)).toBeNull();
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        expect((screen.getByTestId('desktop-bot-confirm') as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).disabled).toBe(false);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-plan-1',
            session_key: 'alice:bot_1',
            text: '首页已打开。',
            send_to_user: '首页已打开。',
        });
        expect(await screen.findByText('首页已打开。')).toBeTruthy();
        expect(screen.getByText('安排:改成只看首页')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
    });

    it('does not turn planning prose into an arrange button while an earlier job is running', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (text === '开始吧') {
                return {
                    told: '先打开网站，再告诉你结果。',
                    instruction: childOrder(text, phase === 'execute' ? 'execute' : 'plan', earlier),
                    pace: 'confirm' as const,
                };
            }
            return { told: `表述:${text}`, instruction: childOrder(text, phase === 'execute' ? 'execute' : 'plan', earlier), pace: 'direct' as const };
        });
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-planlive-${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '开始吧' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('先打开网站，再告诉你结果。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.getByText('先打开网站，再告诉你结果。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
    });

    it('does not offer to execute a screenshot that is already in the reply', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-shot',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '截屏发我' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('截屏发我'), 'execute'));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-shot',
            session_key: 'alice:bot_1',
            text: '已截屏并发送（1440×900，含当前前台窗口）。图片已附在上方，可直接查看。',
            send_to_user: '已截屏并发送（1440×900，含当前前台窗口）。图片已附在上方，可直接查看。',
            desktop_images: [{ mime: 'image/png', data: 'iVBORw0KGgo=' }],
        });
        expect(await screen.findByTestId('desktop-bot-screenshot')).toBeTruthy();
        expect(screen.getByText(told('截屏发我'))).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('states how this word file will be made, then sends that order to the bot', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-word',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        const ask = '生成一份word文档，内容为你的自述，发我。';
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(told(ask))).toBeTruthy();
        expect(told(ask)).toContain('自述');
        expect(told(ask)).not.toContain('看一下环境');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder(ask), 'execute'));
        expect(sendTask.mock.calls[0][1]).not.toBe(ask);
        expect(screen.getByText(ask).closest('.desktop-bot-chat__entry')?.className).toContain('desktop-bot-chat__entry--user');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-word',
            session_key: 'alice:bot_1',
            text: '确认后我会写成文档，附在这条对话里。',
            send_to_user: '确认后我会写成文档，附在这条对话里。',
        });
        expect(await screen.findByText('确认后我会写成文档，附在这条对话里。')).toBeTruthy();
        expect(screen.getByText(told(ask))).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(understandTask).toHaveBeenCalledWith(ask, 'plan', 'zh-Hans', [], '', JSON.stringify({ self: 'bot_1', bots: [{ id: 'bot_1', name: 'Bot 1', primary: false }] }));
    });

    it('keeps one confirmation on the arrangement and does not send it yet', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '查询 北京天所，生成pdf';
        const arrangement = '确认后我会检索北京天所相关资料，整理成 PDF 文件附在这条对话里发给你。';
        understandTask.mockImplementation(async () => ({
            told: arrangement,
            instruction: childOrder(ask),
            pace: 'confirm' as const,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(arrangement)).toBeTruthy();
        expect(screen.queryByText(told(ask))).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        const confirm = screen.getByTestId('desktop-bot-confirm');
        expect(confirm.closest('.desktop-bot-chat__entry')?.textContent || '').toContain(arrangement);
        expect(screen.getAllByText(arrangement)).toHaveLength(1);
        expect(sendTask).not.toHaveBeenCalled();
    });

    it('does a clear request now and does not ask for confirmation', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '查询 北京天所，生成pdf';
        understandTask.mockImplementation(async (content: string, _phase: string, _lang: string, earlier: string[] = []) => ({
            told: '我现在去查北京天所，整理成 PDF 发在这里。',
            instruction: `指令:execute:${content}:${earlier.join('|')}`,
            pace: 'direct',
        }));
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-direct',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('我现在去查北京天所，整理成 PDF 发在这里。')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', `指令:execute:${ask}:`, 'execute'));
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-direct',
            session_key: 'alice:bot_1',
            text: 'PDF 已附在这条对话里。',
            send_to_user: 'PDF 已附在这条对话里。',
        });
        expect(await screen.findByText('PDF 已附在这条对话里。')).toBeTruthy();
        expect(screen.getByText('我现在去查北京天所，整理成 PDF 发在这里。')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('asks once when a fact is missing and does not start the work', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async () => ({
            told: '你要处理哪一个文件？',
            instruction: '你要处理哪一个文件？',
            pace: 'ask',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '帮我处理一下' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('你要处理哪一个文件？')).toBeTruthy();
        expect(screen.getAllByText('你要处理哪一个文件？')).toHaveLength(1);
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('keeps a persona and does not turn it into desktop work', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        const line = '你叫：码卡龙真龙 ，小名：真龙';
        const kept = '这个 Bot 叫码卡龙真龙，小名真龙。';
        const ack = '我记下了。我是码卡龙真龙，小名真龙。';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = [], persona = '') => {
            const text = String(content || '');
            if (text === line) return { told: ack, instruction: '', persona: kept, pace: 'confirm' };
            if (text === '不用保留这些身份了') return { told: '人设已去掉。', instruction: '', persona: '-', pace: 'confirm' };
            return { told: `表述:${text}`, instruction: `指令:${phase}:${persona}`, pace: 'direct' };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-persona-work',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: line } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(ack)).toBeTruthy();
        expect(screen.getAllByText(ack)).toHaveLength(1);
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotPersona.v1') || '{}') as { alice?: { bot_1?: string } };
        expect(stored.alice?.bot_1).toBe(kept);
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        expect(screen.getByTestId('desktop-bot-persona').textContent).toBe(kept);

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '写一份自我介绍' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', `指令:plan:${kept}`, 'execute'));
        expect(understandTask).toHaveBeenLastCalledWith('写一份自我介绍', 'plan', 'zh-Hans', [line], kept, JSON.stringify({ self: 'bot_1', bots: [{ id: 'bot_1', name: 'Bot 2', primary: false }] }));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-persona-work',
            session_key: 'alice:bot_1',
            text: '自我介绍写好了。',
            send_to_user: '自我介绍写好了。',
        });
        expect(await screen.findByText('自我介绍写好了。')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '不用保留这些身份了' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('人设已去掉。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.getByTestId('desktop-bot-persona').textContent).toBe('还没有人设。');
        const cleared = JSON.parse(localStorage.getItem('maclaw.desktopBotPersona.v1') || '{}') as { alice?: { bot_1?: string } };
        expect(cleared.alice?.bot_1 || '').toBe('');
    });

    it('answers small talk here and does not send it to the desktop', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async () => ({
            told: '我是你在这台云桌面上的同事。',
            instruction: '',
            pace: 'reply',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '你是谁呀?' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('我是你在这台云桌面上的同事。')).toBeTruthy();
        expect(screen.getAllByText('我是你在这台云桌面上的同事。')).toHaveLength(1);
        expect(screen.getByText('你是谁呀?')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(armSchedule).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('answers a status question from the task query and does not start work', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async () => ({
            told: '',
            instruction: '下载报表',
            pace: 'status',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '那个下载报表怎么样了' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('「下载报表」还在进行。')).toBeTruthy();
        expect(queryTaskStatus).toHaveBeenCalledWith('bot_1', '下载报表', 'zh-Hans');
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('answers a generic status question from the live run before the reading', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockResolvedValue([{ run_id: 'run-1', task_text: '安装 Evince' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '后台在运行吗？' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('在做。这件是「安装 Evince」。')).toBeTruthy();
        expect(screen.getByText('后台在运行吗？')).toBeTruthy();
        expect(understandTask).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
        expect(queryTaskStatus).not.toHaveBeenCalled();
        expect(evaluateSupplement).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        expect(screen.queryByText('没有找到和这句话对应的任务。')).toBeNull();
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).disabled).toBe(false);
    });

    it('answers status while a reading is still open and leaves that row up', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockResolvedValue([{ run_id: 'run-1', task_text: '安装 Evince' }]);
        let releaseReading: () => void = () => {};
        const reading = new Promise<void>(resolve => { releaseReading = resolve; });
        understandTask.mockImplementation(async (content: string) => {
            if (content === '打开首页') await reading;
            return { told: `表述:${content}`, instruction: `指令:plan:${content}:`, pace: 'direct' };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开首页' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-processing')).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '后台在运行吗？' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('在做。这件是「安装 Evince」。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-processing')).toBeTruthy();
        expect(understandTask.mock.calls.map(call => call[0])).not.toContain('后台在运行吗？');
        expect(queryTaskStatus).not.toHaveBeenCalled();
        releaseReading();
    });

    it('asks hub with an empty needle when this window has no run', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        queryTaskStatus.mockImplementation(async (_botId?: string, hint?: string) => (
            hint ? '「下载报表」还在进行。' : '现在没有在做的事。'
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '任务现在怎么样了' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('现在没有在做的事。')).toBeTruthy();
        expect(queryTaskStatus).toHaveBeenCalledWith('bot_1', '', 'zh-Hans');
        expect(understandTask).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
    });

    it('says it could not ask when the status lookup does not return', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        let finishStatus: (value: string) => void = () => {};
        queryTaskStatus.mockImplementation(() => new Promise<string>(resolve => { finishStatus = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '后台在运行吗？' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('刚才没问到后台，你再说一次。', undefined, { timeout: 2500 })).toBeTruthy();
        expect(understandTask).not.toHaveBeenCalled();
        expect(screen.queryByText('在跑')).toBeNull();
        expect(screen.queryByText('没在跑')).toBeNull();
        finishStatus('现在没有在做的事。');
    });

    it('says the run was just accepted when it has no run id yet', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        let releaseRuns: (rows: Array<Record<string, unknown>>) => void = () => {};
        listActiveRuns.mockImplementation(() => new Promise(resolve => { releaseRuns = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '后台在运行吗？' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('刚接下，还在交给后台。', undefined, { timeout: 1500 })).toBeTruthy();
        expect(queryTaskStatus).not.toHaveBeenCalled();
        expect(understandTask).not.toHaveBeenCalled();
        releaseRuns([]);
    });

    it('stops the live run and does not start it', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-1', task_text: '安装 Evince' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。安装 Evince 不再继续。')).toBeTruthy();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-1');
        expect(understandTask).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
    });

    it('classifies a stop in another language and cancels the live run', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-1', task_text: '安装 Evince' }]
        ));
        classifyWorkStop.mockResolvedValue('stop');
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-1', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: 'yamete' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。安装 Evince 不再继续。')).toBeTruthy();
        expect(classifyWorkStop).toHaveBeenCalledWith('yamete', 'zh-Hans');
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-1');
        expect(understandTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
    });

    it('pauses on an ambiguous hold and asks instead of saying the work stopped', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-1', task_text: '安装 Evince' }]
        ));
        classifyWorkStop.mockResolvedValue('pause');
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-1', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: 'hold on' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('我先停一下。是整个停下，还是只改正在做的内容？')).toBeTruthy();
        expect(screen.queryByText('停了。安装 Evince 不再继续。')).toBeNull();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-1');
        expect(understandTask).not.toHaveBeenCalled();
    });

    it('keeps going when stop is about the content, not the job', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        classifyWorkStop.mockResolvedValue('continue_other');
        understandTask.mockResolvedValue({ told: '好，标题不用粗体。', instruction: '标题不用粗体', pace: 'direct' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-1', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: 'stop using bold text' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(understandTask.mock.calls.map(call => call[0])).toContain('stop using bold text'));
        expect(cancelRun).not.toHaveBeenCalled();
        expect(await screen.findByText('好，标题不用粗体。')).toBeTruthy();
    });

    it('cancels when the reading claims the work stopped and the classifier did not answer', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-xhs', task_text: '去小红书上给大家打个招呼' }]
        ));
        classifyWorkStop.mockRejectedValue(new Error('timeout'));
        understandTask.mockResolvedValue({ told: '好的，已停下，不会继续了。', instruction: '', pace: 'reply' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-xhs', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '算了别弄了' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。去小红书上给大家打个招呼 不再继续。')).toBeTruthy();
        expect(screen.queryByText('好的，已停下，不会继续了。')).toBeNull();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-xhs');
        expect(classifyWorkStop).toHaveBeenCalled();
    });

    it('does not classify an exact stop phrase', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-xhs', task_text: '去小红书上给大家打个招呼' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-xhs', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停下，不干了。' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。去小红书上给大家打个招呼 不再继续。')).toBeTruthy();
        expect(classifyWorkStop).not.toHaveBeenCalled();
        expect(understandTask).not.toHaveBeenCalled();
    });

    it('stops 停下，不干了 through cancel and does not let the model claim it stopped', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-xhs', task_text: '去小红书上给大家打个招呼' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        act(() => {
            responseListeners.byEvent['desktop-bot-activity']?.({ bot_id: 'bot_1', run_id: 'run-xhs', activity: '正在看屏幕' });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停下，不干了。' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。去小红书上给大家打个招呼 不再继续。')).toBeTruthy();
        expect(screen.queryByText('好的，已停下，不会去小红书打招呼了。')).toBeNull();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-xhs');
        expect(understandTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).not.toContain('工作中');
    });

    it('says it is still stopping until the desktop run is actually gone', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockResolvedValue([{ run_id: 'run-xhs', task_text: '去小红书上给大家打个招呼' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停下，不干了。' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('还在收尾，停稳了告诉你。')).toBeTruthy();
        expect(screen.queryByText('停了。去小红书上给大家打个招呼 不再继续。')).toBeNull();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-xhs');
        listActiveRuns.mockResolvedValue([]);
        expect(await screen.findByText('停了。去小红书上给大家打个招呼 不再继续。')).toBeTruthy();
    });

    it('stops the desktop from the pause mark above the composer', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockResolvedValue({ told: '好的，这就去小红书。', instruction: '去小红书打招呼', pace: 'direct' });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-xhs', deferred: true, session_key: 'alice:bot_1' });
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-xhs', task_text: '去小红书打招呼' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '去小红书上给大家打个招呼' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        const pause = await screen.findByTestId('desktop-bot-pause');
        expect(pause.getAttribute('aria-label')).toBe('停下');
        fireEvent.click(pause);
        expect(await screen.findByText('停了。去小红书打招呼 不再继续。')).toBeTruthy();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-xhs');
    });

    it('stops a run this window did not start, found on Hub, once per tap burst', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockResolvedValue({ told: '好的，这就去知乎。', instruction: '做摘要发知乎', pace: 'direct' });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-zh', deferred: true, session_key: 'alice:bot_1' });
        listActiveRuns.mockResolvedValue([]);
        listTaskRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run_eafb6f42', bot_id: 'bot_1', source: 'desktop', status: 'running', task: '做摘要发知乎' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '做今天的摘要发知乎' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        const pause = await screen.findByTestId('desktop-bot-pause');
        fireEvent.click(pause);
        fireEvent.click(pause);
        fireEvent.click(pause);
        await waitFor(() => expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run_eafb6f42'));
        expect(await screen.findByText('停了。做摘要发知乎 不再继续。')).toBeTruthy();
        expect(cancelRun).toHaveBeenCalledTimes(1);
        expect(screen.queryByText(/nothing|Say plainly/i)).toBeNull();
    });

    it('offers only 停止 while the bot drives and brings takeover back after the stop', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '码卡龙本龙', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1&resize=scale&view_only=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-xhs', deferred: true, session_key: 'alice:bot_1' });
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-xhs', task_text: '去小红书打招呼' }]
        ));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('码卡龙本龙'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '去小红书上给大家打个招呼' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const stop = await screen.findByTestId('desktop-bot-panel-stop');
        expectStopOnly();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('data-input')).toBe('watch');
        fireEvent.click(stop);
        await waitFor(() => expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-xhs'));
        // Once the run is gone the takeover controls come back.
        expect(await screen.findByTestId('desktop-bot-takeover')).toBeTruthy();
    });

    it('cancels the live run and does not start the order that was waiting on a reading', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-1', task_text: '安装 Evince' }]
        ));
        let releaseReading: () => void = () => {};
        const reading = new Promise<void>(resolve => { releaseReading = resolve; });
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            if (content === '打开首页' || content === '再截一张桌面') await reading;
            return { told: `表述:${content}`, instruction: childOrder(content, 'plan', earlier), pace: 'direct' };
        });
        sendTask.mockImplementation(async (botId: string) => ({ request_id: 'desktop-bot-after-stop', deferred: true, session_key: `alice:${botId}` }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开首页' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-processing')).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('停了。后面还有一件记下的，你要的话我再做。')).toBeTruthy();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-1');
        expect(understandTask.mock.calls.map(call => call[0])).not.toContain('停');
        releaseReading();
        await waitFor(() => expect(sendTask.mock.calls.some(call => String(call[1]).includes('打开首页'))).toBe(true));
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 40)); });
        expect(sendTask.mock.calls.some(call => String(call[1]).includes('再截一张桌面'))).toBe(false);
        expect(screen.getByText('停了。后面还有一件记下的，你要的话我再做。')).toBeTruthy();
    });

    it('does not cancel or drop a waiting order when nothing is running', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        let releaseReading: () => void = () => {};
        const reading = new Promise<void>(resolve => { releaseReading = resolve; });
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            if (content === '打开首页' || content === '再截一张桌面') await reading;
            return { told: `表述:${content}`, instruction: childOrder(content, 'plan', earlier), pace: 'direct' };
        });
        sendTask.mockImplementation(async (botId: string) => ({ request_id: `desktop-bot-idle-${String(botId)}`, deferred: true, session_key: `alice:${botId}` }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开首页' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-processing')).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('现在没有在做的事，不用停。')).toBeTruthy();
        expect(cancelRun).not.toHaveBeenCalled();
        releaseReading();
        await waitFor(() => {
            const sent = sendTask.mock.calls.map(call => String(call[1]));
            expect(sent.some(item => item.includes('打开首页'))).toBe(true);
            expect(sent.some(item => item.includes('再截一张桌面'))).toBe(true);
        });
    });

    it('stops an admitting run locally and cancels it when the run id arrives', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('好，这个等手上的做完我就做。')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('还在收尾，停稳了告诉你。')).toBeTruthy();
        expect(screen.queryByText('没停下来：还在交给后台。')).toBeNull();
        expect(armAdmitCancel).toHaveBeenCalledWith('bot_1');
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: false });
        });
        expect(await screen.findByText('停了。后面还有一件记下的，你要的话我再做。')).toBeTruthy();
        expect(cancelRun).not.toHaveBeenCalled();
        expect(understandTask.mock.calls.map(call => call[0])).not.toContain('停');
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 50)); });
        expect(sendTask).not.toHaveBeenCalled();
    });

    it('says the stop failed and still does not start the next order', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listActiveRuns.mockImplementation(async () => (
            cancelRun.mock.calls.length > 0 ? [] : [{ run_id: 'run-1', task_text: '安装 Evince' }]
        ));
        cancelRun.mockRejectedValue(new Error('桌面正忙'));
        let releaseReading: () => void = () => {};
        const reading = new Promise<void>(resolve => { releaseReading = resolve; });
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            if (content === '打开首页' || content === '再截一张桌面') await reading;
            return { told: `表述:${content}`, instruction: childOrder(content, 'plan', earlier), pace: 'direct' };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开首页' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-processing')).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '停' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('没停下来：桌面正忙。')).toBeTruthy();
        expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run-1');
        expect(armAdmitCancel).not.toHaveBeenCalled();
        releaseReading();
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 40)); });
        expect(sendTask.mock.calls.some(call => String(call[1]).includes('再截一张桌面'))).toBe(false);
    });

    it('still reads a stop that names what to stop', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '别装了' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(understandTask.mock.calls.map(call => call[0])).toContain('别装了'));
        expect(cancelRun).not.toHaveBeenCalled();
    });

    it('shows a result that finished while this GUI was offline', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        const report = {
            request_id: 'desktop-bot-deliver-run_done',
            session_key: 'alice:bot_1',
            delivery_report: true,
            text: '报表已下载',
            send_to_user: '报表已下载',
        };
        responseListeners.byEvent['desktop-bot-report']?.(report);
        responseListeners.byEvent['desktop-bot-report']?.(report);
        expect(await screen.findByText('报表已下载')).toBeTruthy();
        expect(screen.getAllByText('报表已下载')).toHaveLength(1);
    });

    it('arms a repeating request and reports the result in this chat', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async () => ({
            told: '每分钟问好已经设好了。到点我会在这里问候你。',
            instruction: '向用户问好一次。',
            pace: 'schedule',
            schedule: { name: '每分钟问好', mode: 'create', interval_minutes: 1 },
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '每分钟向我问好一次。' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('每分钟问好已经设好了。到点我会在这里问候你。')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(armSchedule).toHaveBeenCalledWith('bot_1', '每分钟问好', 'create', '向用户问好一次。', 1, -1, -1, -1);
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');

        const report = {
            request_id: 'desktop-bot-sched-task-1-1',
            session_key: 'alice:bot_1',
            schedule_report: true,
            text: '你好。',
            send_to_user: '你好。',
        };
        responseListeners.byEvent['desktop-bot-report']?.(report);
        responseListeners.byEvent['desktop-bot-report']?.(report);
        expect(await screen.findByText('你好。')).toBeTruthy();
        expect(screen.getAllByText('你好。')).toHaveLength(1);
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('arms a schedule while a task is running and does not queue it', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        const job = '查询 北京天所，生成pdf';
        understandTask.mockImplementation(async (content: string) => {
            if (String(content) === '每分钟向我问好一次。') {
                return {
                    told: '每分钟问好已经设好了。',
                    instruction: '向用户问好一次。',
                    pace: 'schedule',
                    schedule: { name: '每分钟问好', mode: 'create', interval_minutes: 1 },
                };
            }
            return {
                told: '我现在去查北京天所，整理成 PDF 发在这里。',
                instruction: `指令:execute:${content}:`,
                pace: 'direct',
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-busy-schedule',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: job } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '每分钟向我问好一次。' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('每分钟问好已经设好了。')).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(armSchedule).toHaveBeenCalledWith('bot_1', '每分钟问好', 'create', '向用户问好一次。', 1, -1, -1, -1);
        expect(sendTask).toHaveBeenCalledTimes(1);
    });

    it('keeps a schedule report that arrives while the bot page is closed', () => {
        const payload = {
            request_id: 'desktop-bot-sched-closed-1',
            session_key: 'alice:bot_1',
            schedule_report: true,
            text: '你好。',
            send_to_user: '你好。',
        };
        handleDesktopBotReport(payload);
        handleDesktopBotReport(payload);
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as { alice?: { bot_1?: Array<{ content?: string; phase?: string; requestId?: string }> } };
        expect(stored.alice?.bot_1).toHaveLength(1);
        expect(stored.alice?.bot_1?.[0].content).toBe('你好。');
        expect(stored.alice?.bot_1?.[0].phase).toBeUndefined();
        expect(stored.alice?.bot_1?.[0].requestId).toBe('desktop-bot-sched-closed-1');
    });

    it('lists this bot schedules and deletes one', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listSchedules.mockResolvedValue([
            { id: 'sched-1', name: '每分钟问好', action: '向用户问好一次。', interval_minutes: 1, hour: 0, minute: 0, day_of_week: -1, status: 'active' },
            { id: 'sched-2', name: '早间简报', action: '整理早间简报。', interval_minutes: 0, hour: 8, minute: 30, day_of_week: 1, status: 'active' },
        ]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(await screen.findByTestId('desktop-bot-tab-schedules'));
        expect(await screen.findByText('每分钟问好')).toBeTruthy();
        expect(screen.getByText('每分钟')).toBeTruthy();
        expect(screen.getByText('周一 08:30')).toBeTruthy();
        expect(screen.getByText('向用户问好一次。')).toBeTruthy();
        expect(listSchedules).toHaveBeenCalledWith('bot_1');

        fireEvent.click(screen.getByTestId('desktop-bot-schedule-delete-sched-1'));
        await confirmBotDelete();
        await waitFor(() => expect(deleteSchedule).toHaveBeenCalledWith('bot_1', 'sched-1'));
        await waitFor(() => expect(screen.queryByText('每分钟问好')).toBeNull());
        expect(screen.getByText('早间简报')).toBeTruthy();
        expect(screen.getByText('周一 08:30')).toBeTruthy();
    });

    it('lists ongoing tasks and stops, cancels queued and closes stale ones', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        listTaskRuns.mockResolvedValue([
            { run_id: 'run_a', bot_id: 'bot_1', source: 'desktop', status: 'running', task: '打开知乎发文章', step: '正在打开网页', started_at: '2026-10-22T07:05:00Z' },
            { run_id: 'run_old', bot_id: 'bot_1', source: 'server', status: 'running', task: '', stale: true, started_at: '2026-10-22T01:00:00Z' },
            { run_id: '', bot_id: 'bot_1', source: 'desktop', status: 'queued', task: '下一件事' },
        ]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(await screen.findByTestId('desktop-bot-tab-tasks'));
        expect(await screen.findByText('打开知乎发文章')).toBeTruthy();
        expect(screen.getByText('正在打开网页')).toBeTruthy();
        expect(screen.getByText(/进行中 · 开始 10-22 15:05/)).toBeTruthy();
        expect(screen.getByTitle('run_a').textContent).toBe('a');
        expect(screen.getByText(/可能已中断 · 开始 10-22 09:00/)).toBeTruthy();
        expect(screen.getByText(/排队中/)).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-task-stop-run_a').textContent).toBe('停止');
        expect(screen.getByTestId('desktop-bot-task-stop-run_old').textContent).toBe('取消');
        expect(screen.getByTestId('desktop-bot-task-stop-queued').textContent).toBe('取消');

        fireEvent.click(screen.getByTestId('desktop-bot-task-stop-run_a'));
        await waitFor(() => expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run_a'));
        fireEvent.click(screen.getByTestId('desktop-bot-task-stop-run_old'));
        await waitFor(() => expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run_old'));
        fireEvent.click(screen.getByTestId('desktop-bot-task-stop-queued'));
        await waitFor(() => expect(armAdmitCancel).toHaveBeenCalledWith('bot_1'));
        expect(listTaskRuns).toHaveBeenCalledWith('bot_1');
    });

    it('shows a failure when a direct reading has no work order', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async () => ({
            told: '我现在去做。',
            instruction: '',
            pace: 'direct',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('这句没整理成可执行的安排，没有发出。')).toBeTruthy();
        expect(screen.queryByText('我现在去做。')).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('answers small talk while a task is running and does not queue it', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        const job = '查询 北京天所，生成pdf';
        const answer = '我是你在这台云桌面上的同事。';
        understandTask.mockImplementation(async (content: string, _phase: string, _lang: string, earlier: string[] = []) => {
            if (String(content) === '你是谁呀?') return { told: answer, instruction: '', pace: 'reply' };
            return {
                told: '我现在去查北京天所，整理成 PDF 发在这里。',
                instruction: `指令:execute:${content}:${earlier.join('|')}`,
                pace: 'direct',
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-busy-chat',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: job } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('工作中');

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '你是谁呀?' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(answer)).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-busy-chat',
            session_key: 'alice:bot_1',
            text: 'PDF 已附在这条对话里。',
            send_to_user: 'PDF 已附在这条对话里。',
        });
        expect(await screen.findByText('PDF 已附在这条对话里。')).toBeTruthy();
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 20));
        });
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.getByText(answer)).toBeTruthy();
        expect(screen.getByText('你是谁呀?')).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('does not queue a direct reading that forgot the work order', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        const job = '查询 北京天所，生成pdf';
        understandTask.mockImplementation(async (content: string, _phase: string, _lang: string, earlier: string[] = []) => {
            if (String(content) === '打开百度') return { told: '我现在去做。', instruction: '', pace: 'direct' as const };
            return {
                told: '我现在去查北京天所，整理成 PDF 发在这里。',
                instruction: `指令:execute:${content}:${earlier.join('|')}`,
                pace: 'direct' as const,
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-busy-empty',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: job } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('这句没整理成可执行的安排，没有发出。')).toBeTruthy();
        expect(screen.queryByText('我现在去做。')).toBeNull();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        expect(sendTask).toHaveBeenCalledTimes(1);

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-busy-empty',
            session_key: 'alice:bot_1',
            text: 'PDF 已附在这条对话里。',
            send_to_user: 'PDF 已附在这条对话里。',
        });
        expect(await screen.findByText('PDF 已附在这条对话里。')).toBeTruthy();
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 20));
        });
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect(screen.getByText('这句没整理成可执行的安排，没有发出。')).toBeTruthy();
    });

    // A parked sentence is still the person's order. While another job is
    // running, choosing their own words starts that order beside it.
    it('sends the verbatim resend while another task is still running', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 2', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockImplementation(async (content: string) => {
            if (String(content) === '打开百度') return { reading: 'unreachable' };
            return { told: '我现在去查并整理成 PDF 发在这里。', instruction: `指令:plan:${content}`, pace: 'direct' as const };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-busy-verbatim',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 2'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '查询 北京天气，生成pdf' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-no-reading')).toBeTruthy();

        // The other job is still running. These words start beside it.
        fireEvent.click(screen.getByTestId('desktop-bot-reading-verbatim'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-no-reading')).toBeNull());
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '打开百度', 'plan');

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-busy-verbatim',
            session_key: 'alice:bot_1',
            text: 'PDF 已附在这条对话里。',
            send_to_user: 'PDF 已附在这条对话里。',
        });
        expect(await screen.findByText('PDF 已附在这条对话里。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(2);
    });

    it('starts the stored order on the button and does not re-read it into chat', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '把桌面上的旧办公软件卸掉并清掉数据';
        const arrangement = '这件事要清掉旧文件再重装。确认后我按这个做。';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (phase === 'execute') return { told: '按前面的安排做。', instruction: '', persona: '这个 Bot 叫 Bot 2。', pace: 'reply' };
            return {
                told: arrangement,
                instruction: childOrder(text, 'plan', earlier),
                pace: 'confirm' as const,
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-empty-exec',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(arrangement)).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        fireEvent.click(await screen.findByTestId('desktop-bot-confirm'));
        await waitFor(() => expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder(ask), 'execute'));
        expect(understandTask.mock.calls.some(call => call[1] === 'execute')).toBe(false);
        expect(understandTask.mock.calls.some(call => call[0] === CONFIRM_TASK)).toBe(false);
        expect(screen.queryByText('按前面的安排做。')).toBeNull();
        expect(screen.queryByText('这句没整理成可执行的安排，没有发出。')).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.getByText(arrangement)).toBeTruthy();
    });

    it('keeps the arrangement button when a later message is answered here', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '把桌面上的旧办公软件卸掉并清掉数据';
        const answer = '我是你在这台云桌面上的同事。';
        const question = '这份要发给谁？';
        const missed = '这句没整理成可执行的安排，没有发出。';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (text === '你是谁呀?') return { told: answer, instruction: '', pace: 'reply' as const };
            if (text === '收件人是谁') return { told: question, instruction: '', pace: 'ask' as const };
            if (text === '打开百度') return { told: '我现在去做。', instruction: '', pace: 'direct' as const };
            return {
                told: '这件事要清掉旧文件再重装。确认后我按这个做。',
                instruction: childOrder(text, 'plan', earlier),
                pace: 'confirm' as const,
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-idle-note',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('这件事要清掉旧文件再重装。确认后我按这个做。')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '你是谁呀?' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(answer)).toBeTruthy();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '收件人是谁' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(question)).toBeTruthy();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(missed)).toBeTruthy();
        expect(screen.queryByText('我现在去做。')).toBeNull();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);
        expect((screen.getByTestId('desktop-bot-confirm') as HTMLButtonElement).disabled).toBe(false);
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('keeps the arrangement button when a side failure or question lands first', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '把桌面上的旧办公软件卸掉并清掉数据';
        const arrangement = '这件事要清掉旧文件再重装。确认后我按这个做。';
        const missed = '这句没整理成可执行的安排，没有发出。';
        const question = '这份要发给谁？';
        let release: (value: { told: string; instruction: string; pace: 'confirm' }) => void = () => {};
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (text === ask) return new Promise(resolve => { release = resolve; });
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (phase === 'execute') return { told: `表述:${text}`, instruction: childOrder(text, 'execute', earlier), pace: 'direct' as const };
            if (text === '打开百度') return { told: '我现在去做。', instruction: '', pace: 'direct' as const };
            if (text === '收件人是谁') return { told: question, instruction: '', pace: 'ask' as const };
            return {
                told: arrangement,
                instruction: childOrder(text, 'plan', earlier),
                pace: 'confirm' as const,
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-side-note',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(ask)).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开百度' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(missed)).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '收件人是谁' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(question)).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();

        await act(async () => {
            release({ told: arrangement, instruction: childOrder(ask), pace: 'confirm' });
        });
        expect(await screen.findByText(arrangement)).toBeTruthy();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);
        expect((screen.getByTestId('desktop-bot-confirm') as HTMLButtonElement).disabled).toBe(false);
        expect(screen.getByText(missed)).toBeTruthy();
        expect(screen.getByText(question)).toBeTruthy();
        expect(screen.queryByText(/记下了/)).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();

        fireEvent.click(screen.getByTestId('desktop-bot-confirm'));
        await waitFor(() => expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder(ask), 'execute'));
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.getByText(arrangement)).toBeTruthy();
    });

    it('confirms a large change once and that button starts the work', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '把桌面上的旧办公软件卸掉并清掉数据';
        understandTask.mockImplementation(async (content: string, phase: string, _lang: string, earlier: string[] = []) => {
            const text = String(content || '');
            if (passedThrough(text, phase)) return { told: text.trim(), instruction: text.trim() };
            if (phase === 'execute') return { told: `表述:${text}`, instruction: childOrder(text, 'execute', earlier), pace: 'direct' };
            return {
                told: '这件事要清掉旧文件再重装。确认后我按这个做。',
                instruction: childOrder(text, 'plan', earlier),
                pace: 'confirm',
            };
        });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-large',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        const arrangement = '这件事要清掉旧文件再重装。确认后我按这个做。';
        expect(await screen.findByText(arrangement)).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
        expect(screen.getAllByTestId('desktop-bot-confirm')).toHaveLength(1);
        fireEvent.click(screen.getByTestId('desktop-bot-confirm'));
        await waitFor(() => expect(sendTask).toHaveBeenLastCalledWith('bot_1', childOrder(ask), 'execute'));
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.getByText(arrangement)).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-large',
            session_key: 'alice:bot_1',
            text: '旧办公软件已卸掉，数据已清掉。',
            send_to_user: '旧办公软件已卸掉，数据已清掉。',
        });
        expect(await screen.findByText('旧办公软件已卸掉，数据已清掉。')).toBeTruthy();
        expect(screen.getByText(arrangement)).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(screen.queryByTestId('desktop-bot-processing')).toBeNull();
    });

    it('shows an understanding failure and does not send a stock order', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockRejectedValueOnce(new Error('LLM not configured'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '开发个c++版的hello world程序，运行后发我运行屏幕截图' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('LLM not configured')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText('你要当前桌面的截屏。我现在截，截到就发在这里。')).toBeNull();
    });

    it('keeps an early result on the job that is still waiting for its id', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            const requestId = `desktop-bot-cross${counter}`;
            if (counter === 2) {
                responseListeners.byEvent['ai-assistant-response']?.({
                    request_id: requestId,
                    session_key: `alice:${botId}`,
                    text: '第二件做完了',
                    send_to_user: '第二件做完了',
                });
            }
            return { request_id: requestId, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(messagesForBot('alice', 'bot_1').some(item => item.requestId === 'desktop-bot-cross1')).toBe(true));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('第二件做完了')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        const stored = messagesForBot('alice', 'bot_1');
        expect(stored.some(item => item.pending && item.requestId === 'desktop-bot-cross1')).toBe(true);
        expect(stored.some(item => item.content === '第二件做完了' && item.requestId === 'desktop-bot-cross2' && !item.pending)).toBe(true);
    });

    it('does not attach a late result to a turn that has not been sent', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '开发个c++版的hello world程序，运行后发我运行屏幕截图';
        let release: (value: { told: string; instruction: string; pace?: 'direct' }) => void = () => {};
        understandTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-new', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(ask)).toBeTruthy();
        const stray = {
            request_id: 'desktop-bot-old',
            session_key: 'alice:bot_1',
            text: '上一轮的旧结果',
            send_to_user: '上一轮的旧结果',
        };
        handleDesktopBotResult(stray);
        const strayView = {
            request_id: 'desktop-bot-old',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
            attention_reason: '需要登录',
        };
        handleDesktopBotView(strayView);
        responseListeners.byEvent['ai-assistant-response']?.({
            ...stray,
        });
        responseListeners.byEvent['desktop-bot-view']?.(strayView);
        expect(screen.queryByText('上一轮的旧结果')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();
        await act(async () => { release({ told: told(ask), instruction: childOrder(ask), pace: 'direct' }); });
        expect(await screen.findByText(told(ask))).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder(ask), 'execute'));
        expect(screen.queryByText('上一轮的旧结果')).toBeNull();
    });

    it('does not send the understood order after the account changes', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let release: (value: { told: string; instruction: string }) => void = () => {};
        understandTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_2' }]);
        view.rerender(<DialogProvider><DesktopBotWorkspace lang="zh-Hans" userId="bob" /></DialogProvider>);
        expect(await screen.findByText('值班')).toBeTruthy();
        await act(async () => { release({ told: told('打开示例网站'), instruction: childOrder('打开示例网站') }); });
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText(told('打开示例网站'))).toBeNull();
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, Array<{ content?: string; failed?: boolean }>>>;
        const alice = stored.alice?.bot_1 || [];
        expect(alice.some(item => item.failed && item.content === '这条没有发出。发出前账号已经切换。')).toBe(true);
        expect(alice.some(item => String(item.content || '').includes('表述:'))).toBe(false);
        expect(stored.bob?.bot_1 || []).toHaveLength(0);
    });

    it('still sends when the bots page leaves and comes back during the reading', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let release: (value: { told: string; instruction: string; pace?: 'direct' }) => void = () => {};
        understandTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-back', deferred: true, session_key: 'alice:bot_1' });
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        await act(async () => { release({ told: told('打开示例网站'), instruction: childOrder('打开示例网站'), pace: 'direct' }); });
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('打开示例网站'), 'execute'));
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, Array<{ content?: string }>>>;
        const alice = stored.alice?.bot_1 || [];
        expect(alice.some(item => String(item.content || '').includes('账号已经切换'))).toBe(false);
    });

    it('does not send the stock confirm sentence, and still sends resume and fill on the plan phase', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-discuss',
            text: '好，我按讨论来。',
            send_to_user: '好，我按讨论来。',
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: CONFIRM_TASK } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).value).toBe(''));
        expect(sendTask).not.toHaveBeenCalled();
        expect(understandTask).not.toHaveBeenCalled();
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
        expect(await screen.findByText('点安排下面那个按钮就行，打这句不会开始。')).toBeTruthy();
        const sentences = [
            '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。',
            'SITE_PASSWORD 已在本地填入当前密码框。请沿用这个结果继续，不要向我索要它的内容。',
        ];
        let calls = 0;
        for (const sentence of sentences) {
            calls += 1;
            fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: sentence } });
            fireEvent.click(screen.getByTestId('desktop-bot-send'));
            await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(calls));
            expect(sendTask).toHaveBeenLastCalledWith('bot_1', sentence, 'plan');
            await waitFor(() => expect(screen.getAllByText('好，我按讨论来。')).toHaveLength(calls));
        }
        expect(screen.queryByText(CONFIRM_TASK)).toBeNull();
    });

    it('keeps a queued resume on the execute phase', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let release: (value: unknown) => void = () => {};
        sendTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '先看环境' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('先看环境')).toBeTruthy();
        release({ request_id: 'desktop-bot-hold', deferred: true, session_key: 'alice:bot_1' });
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-hold',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        fireEvent.click(await screen.findByTestId('desktop-bot-return-control'));
        expect(sendTask).toHaveBeenCalledTimes(1);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-hold',
            session_key: 'alice:bot_1',
            text: '安排好了。',
            send_to_user: '安排好了。',
        });
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。', 'execute');
    });

    it('shows a plan secret control without filling, and fills only during execution', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-sec-${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        // This sentence stays on the plan phase. A secret asked there is saved
        // and not typed; typing happens only after the turn is execute.
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: RETURN_TASK } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', RETURN_TASK, 'plan');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-sec-1',
            session_key: 'alice:bot_1',
            text: '需要 SITE_PASSWORD。',
            send_to_user: '需要 SITE_PASSWORD。',
            ask_user_input_type: 'secret_fill',
            ask_user_secret_name: 'SITE_PASSWORD',
        });
        expect(await screen.findByTestId('desktop-bot-secret')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-log').contains(screen.getByTestId('desktop-bot-secret'))).toBe(true);
        expect(fillSecret).not.toHaveBeenCalled();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: 's3cret-value' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(sendTask).toHaveBeenCalledTimes(1);
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).value).toBe('s3cret-value');
        fireEvent.change(screen.getByTestId('desktop-bot-secret-input'), { target: { value: 's3cret-value' } });
        fireEvent.click(screen.getByTestId('desktop-bot-secret-save'));
        await waitFor(() => expect(saveSecret).toHaveBeenCalledWith('SITE_PASSWORD', 's3cret-value'));
        await waitFor(() => expect(sendTask).toHaveBeenLastCalledWith('bot_1', '已在本机保存 SITE_PASSWORD。', 'plan'));
        expect(fillSecret).not.toHaveBeenCalled();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(localStorage.getItem('maclaw.desktopBotMessages.v1') || '').not.toContain('s3cret-value');
        expect(screen.getByTestId('desktop-bot-log').textContent || '').not.toContain('s3cret-value');

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-sec-2',
            session_key: 'alice:bot_1',
            text: '安排：打开登录页后填写已保存的密码。',
            send_to_user: '安排：打开登录页后填写已保存的密码。',
        });
        expect(await screen.findByText('安排：打开登录页后填写已保存的密码。')).toBeTruthy();
        const secretConfirm = screen.getByTestId('desktop-bot-confirm');
        expect((secretConfirm as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(secretConfirm);
        await waitFor(() => expect(sendTask.mock.calls.some(call => call[2] === 'execute')).toBe(true));
        recallSecret.mockResolvedValue(true);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-sec-3',
            session_key: 'alice:bot_1',
            text: '请填写密码。',
            send_to_user: '请填写密码。',
            ask_user_input_type: 'secret_fill',
            ask_user_secret_name: 'SITE_PASSWORD',
        });
        await waitFor(() => expect(fillSecret).toHaveBeenCalledWith('bot_1', 'SITE_PASSWORD'));
        expect(screen.queryByTestId('desktop-bot-secret')).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenLastCalledWith('bot_1', 'SITE_PASSWORD 已在本地填入当前密码框。请沿用这个结果继续，不要向我索要它的内容。', 'execute'));
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(localStorage.getItem('maclaw.desktopBotMessages.v1') || '').not.toContain('s3cret-value');
    });

    it('keeps the chat and composer on the left and opens the profile from the identity chip', async () => {
        const listedAt = '2024-01-15T00:30:00Z';
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '', instance_id: 'inst_1', created_at: listedAt },
            { id: 'bot_2', title: '日报', description: '早上', instance_id: 'inst_2', created_at: '2024-06-02T03:04:00Z' },
        ]);
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        await screen.findByTestId('desktop-bot-bot_1');
        openRow('bot_1');
        const box = await screen.findByTestId('desktop-bot-command') as HTMLTextAreaElement;
        expect(box.placeholder).toBe('给 值班 发消息');
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
        expect(screen.queryByRole('button', { name: '查看桌面' })).toBeNull();
        expect(screen.getByTestId('desktop-bot-open-desktop').getAttribute('aria-pressed')).toBe('false');
        expect(screen.getByTestId('desktop-bot-open-desktop').contains(screen.getByTestId('desktop-bot-identity-chevron'))).toBe(true);
        const log = screen.getByTestId('desktop-bot-log');
        const form = box.closest('form');
        expect(form).toBeTruthy();
        expect(log.parentElement).toBe(form?.parentElement);
        expect(log.nextElementSibling).toBe(form);
        expect(screen.getByLabelText('Bot').contains(screen.getByTestId('desktop-bot-bot_1'))).toBe(true);
        const botRow = screen.getByTestId('desktop-bot-bot_1');
        expect(within(botRow).getByRole('button', { name: '编辑信息' }).querySelector('svg')).toBeTruthy();
        expect(within(botRow).getByRole('button', { name: '删除' }).querySelector('svg')).toBeTruthy();
        expect(botRow.textContent).not.toContain('编辑信息');
        expect(botRow.textContent).not.toContain('删除');

        let shiftPrevented = false;
        const markPrevented = (event: Event) => { shiftPrevented = event.defaultPrevented; };
        document.addEventListener('keydown', markPrevented);
        fireEvent.keyDown(box, { key: 'Enter', shiftKey: true });
        expect(shiftPrevented).toBe(false);
        expect(sendTask).not.toHaveBeenCalled();
        fireEvent.change(box, { target: { value: '看一下' } });
        fireEvent.keyDown(box, { key: 'Enter' });
        expect(shiftPrevented).toBe(true);
        document.removeEventListener('keydown', markPrevented);
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('看一下'), 'execute'));

        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const panel = screen.getByTestId('desktop-bot-panel');
        expect(screen.getByTestId('desktop-bot-open-desktop').getAttribute('aria-pressed')).toBe('true');
        expect(panel.querySelector('.desktop-bot-panel__avatar')?.getAttribute('data-shape')).toBeTruthy();
        expect(panel.querySelector('.desktop-bot-panel__avatar')?.className).toContain('desktop-bot-panel__avatar');
        expect(panel.textContent).toContain('远程电脑上的7x24小时数字助理。');
        expect(screen.getAllByRole('tab')).toHaveLength(4);
        expect(screen.getByRole('tab', { name: '任务' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: '定时任务' })).toBeTruthy();
        expect(screen.queryByRole('tab', { name: '资料库' })).toBeNull();
        expect(screen.getByTestId('desktop-bot-computer')).toBeTruthy();
        expect(screen.getByText('值班 的屏幕')).toBeTruthy();
        const screenCard = screen.getByTestId('desktop-bot-screen');
        const caption = screen.getByTestId('desktop-bot-computer').querySelector('.desktop-bot-screen__caption');
        expect(caption).toBeTruthy();
        expect(screenCard.compareDocumentPosition(caption as Node) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0);
        expect(screen.getByTestId('desktop-bot-stage-loading')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-chat').contains(box)).toBe(true);
        expect(panel.contains(box)).toBe(false);
        const workingRow = screen.getByTestId('desktop-bot-processing');
        expect(log.nextElementSibling).toBe(workingRow);
        expect(workingRow.nextElementSibling).toBe(form);
        expect(panel.contains(workingRow)).toBe(false);
        expect(panel.textContent).not.toContain('编辑信息');

        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const details = screen.getByTestId('desktop-bot-details');
        expect(details.textContent).toContain('状态');
        expect(details.textContent).toContain('工作中');
        expect(details.textContent).toContain('描述');
        expect(details.textContent).toContain('远程电脑上的7x24小时数字助理。');
        expect(details.textContent).toContain('创建时间');
        expect(details.textContent).toContain(formatCreated(Date.parse(listedAt)));
        expect(details.textContent).not.toContain(formatCreated(Date.now()));
        expect(details.textContent).toContain('Bot为24小时驻守远程电脑上的数字助理。');
        expect(details.querySelector('.desktop-bot-panel__sheet')).toBeTruthy();
        expect(details.querySelector('.desktop-bot-panel__badge')?.textContent).toContain('工作中');
        expect(details.querySelector('.desktop-bot-panel__note')?.textContent).toContain('Bot为24小时驻守远程电脑上的数字助理。');
        expect(screen.queryByTestId('desktop-bot-computer')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        expect(screen.getByTestId('desktop-bot-open-desktop').getAttribute('aria-pressed')).toBe('false');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-computer')).toBeNull();

        openRow('bot_2');
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        expect((screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement).placeholder).toBe('给 日报 发消息');
        openRow('bot_1');
        expect(screen.getByTestId('desktop-bot-panel').textContent).toContain('值班');
        expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-panel').textContent).not.toContain('早上');
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-from-details',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        expect(await screen.findByTestId('desktop-bot-computer')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-details')).toBeNull();
        expect(screen.getByText('值班 的屏幕')).toBeTruthy();
        const handoffSrc = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(handoffSrc).toContain('view_only=0');
        expect(handoffSrc).not.toContain('view_only=1');
        view.unmount();
    });

    it('uses message-this-bot copy in English and traditional Chinese', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Duty', description: 'Shared desk', instance_id: 'inst_1' }]);
        const english = renderBots(<DesktopBotWorkspace lang="en" userId="alice" />);
        fireEvent.click(await screen.findByText('Duty'));
        expect((await screen.findByTestId('desktop-bot-command') as HTMLTextAreaElement).placeholder).toBe('Message Duty');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('Idle');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect(screen.getByRole('tab', { name: 'Details' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Computer' })).toBeTruthy();
        expect(screen.getByText("Duty's screen")).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const details = screen.getByTestId('desktop-bot-details');
        expect(details.textContent).toContain('Status');
        expect(details.textContent).toContain('Idle');
        expect(details.textContent).toContain('Description');
        expect(details.textContent).toContain('Shared desk');
        expect(details.textContent).toContain('Created');
        expect(details.textContent).toContain('A bot is a digital assistant stationed on the remote computer around the clock.');
        english.unmount();

        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hant" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        expect((await screen.findByTestId('desktop-bot-command') as HTMLTextAreaElement).placeholder).toBe('給 值班 發消息');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        expect(screen.getByRole('tab', { name: '詳情' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: '電腦' })).toBeTruthy();
        expect(screen.getByText('值班 的螢幕')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('shows a retry on the computer card when the desktop picture fails', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            expect(screen.getByTestId('desktop-bot-stage-loading')).toBeTruthy();
            expect(screen.getByText('值班 的屏幕')).toBeTruthy();
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-retry')).toBeTruthy();
            expect(screen.getByTestId('desktop-bot-panel').contains(screen.getByTestId('desktop-bot-retry'))).toBe(true);
            expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
            expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
            const sends = sendTask.mock.calls.length;
            const epoch = watchBot.mock.calls[0][1];
            fireEvent.click(screen.getByTestId('desktop-bot-retry'));
            await act(async () => { await Promise.resolve(); });
            expect(sendTask.mock.calls.length).toBe(sends);
            expect(watchBot).toHaveBeenLastCalledWith('bot_1', epoch);
            expect(releaseWatch).not.toHaveBeenCalled();
            const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
            watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-retry')).toBeNull();
            expect(sendTask.mock.calls.length).toBe(sends);
            expect(releaseWatch).not.toHaveBeenCalled();
            fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
            await act(async () => { await Promise.resolve(); });
            expect(releaseWatch).toHaveBeenCalledWith('bot_1', watchBot.mock.calls[0][1]);
        } finally {
            vi.useRealTimers();
        }
    });

    it('frames the H.264 player by default and switches back to noVNC', async () => {
        localStorage.removeItem('maclaw.desktop.viewer');
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const cached = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        getScreen.mockResolvedValue({ novnc_url: cached, user_control: false, desktop_mode: 'shared' });
        watchBot.mockResolvedValue({ novnc_url: cached, user_control: false, desktop_mode: 'shared' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        const frame = await screen.findByTestId('desktop-bot-handoff');
        const src = frame.getAttribute('src') || '';
        expect(src).toContain('/abc/h264.html?view_only=1&vnc=');
        expect(new URL(src).searchParams.get('vnc')).toBe('vnc.html?autoconnect=1&view_only=1');
        expect(frame.getAttribute('allow')).toContain('clipboard-write');
        fireEvent.click(screen.getByTestId('desktop-bot-viewer-switch'));
        await waitFor(() => {
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toBe(cached + '&view_only=1');
        });
        expect(localStorage.getItem('maclaw.desktop.viewer')).toBe('vnc');
    });

    it('shows a retry when the watch fails even if the screen list still has a picture', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const cached = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        getScreen.mockResolvedValue({ novnc_url: cached, user_control: false, desktop_mode: 'shared' });
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false, desktop_mode: 'shared' });
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain(cached);
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-retry')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('keeps my desktop up when an isolated bot watch fails', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '自己的桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        const mine = 'http://hub.example/api/v1/desktop-handoff/mine/vnc.html?autoconnect=1';
        const isolated = 'http://hub.example/api/v1/desktop-handoff/bot-1/vnc.html?autoconnect=1';
        let bot1Up = true;
        watchBot.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_1' ? (bot1Up ? isolated : '') : mine,
            user_control: false,
            desktop_mode: botId === 'bot_1' ? 'isolated' : 'shared',
        }));
        getScreen.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_1' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_1' ? 'isolated' : 'shared',
        }));
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await Promise.resolve(); });
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('bot-1');
            fireEvent.click(screen.getByTestId('desktop-screen-mine'));
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('mine');
            bot1Up = false;
            await act(async () => { await vi.advanceTimersByTimeAsync(8000); });
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('mine');
            expect(screen.queryByTestId('desktop-bot-retry')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('keeps the shared desktop when another open bot takes the stage', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '共用桌面', instance_id: 'inst_2' },
        ]);
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        const openRow = (botId: string) => {
            const row = screen.getByTestId(`desktop-bot-${botId}`);
            const button = row.querySelector('button.desktop-bot-workspace__open');
            if (!button) throw new Error(`missing row button for ${botId}`);
            fireEvent.click(button);
        };
        await screen.findByTestId('desktop-bot-bot_1');
        openRow('bot_1');
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await waitFor(() => expect(watchBot).toHaveBeenCalledWith('bot_1', expect.any(Number)));
        const firstEpoch = watchBot.mock.calls[0][1];
        openRow('bot_2');
        await waitFor(() => expect(releaseWatch).toHaveBeenCalledWith('bot_1', firstEpoch));
        expect(releaseWatch).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy());
        const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src');
        openRow('bot_1');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toBe(src);
        expect(screen.queryByTestId('desktop-bot-stage-loading')).toBeNull();
        await act(async () => { await Promise.resolve(); });
        expect(releaseWatch).toHaveBeenCalledTimes(1);
        await waitFor(() => expect(watchBot).toHaveBeenLastCalledWith('bot_1', expect.any(Number)));
        const returnedEpoch = watchBot.mock.calls.at(-1)?.[1];
        expect(returnedEpoch).not.toBe(firstEpoch);
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await act(async () => { await Promise.resolve(); });
        expect(releaseWatch).toHaveBeenCalledTimes(2);
        expect(releaseWatch).toHaveBeenLastCalledWith('bot_1', returnedEpoch);
        watchBot.mockImplementation(() => new Promise(() => undefined));
        openRow('bot_2');
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.getByTestId('desktop-bot-stage-loading')).toBeTruthy();
    });

    it('does not open the stage again when a retry finishes after it was hidden', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-retry')).toBeTruthy();
            let resolveRetry: (value: { novnc_url: string; user_control: boolean }) => void = () => {};
            watchBot.mockImplementation(() => new Promise(resolve => { resolveRetry = resolve; }));
            fireEvent.click(screen.getByTestId('desktop-bot-retry'));
            fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
            await act(async () => { await Promise.resolve(); });
            resolveRetry({ novnc_url: liveDesktop, user_control: false });
            await act(async () => { await Promise.resolve(); });
            expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
            expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('shows the computer when a watch poll first reports a handoff during details', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await Promise.resolve(); await Promise.resolve(); });
            fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
            expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
            watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: true });
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-computer')).toBeTruthy();
            const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
            expect(src).toContain('view_only=0');
            expect(src).not.toContain('view_only=1');
            fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-computer')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('leaves fullscreen without finishing a login, and drops the prompt when the keyboard comes back', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-login',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-login',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
            user_control: true,
        });
        const loginSrc = (await screen.findByTestId('desktop-bot-handoff')).getAttribute('src') || '';
        expect(loginSrc).toContain('view_only=0');
        expect(loginSrc).not.toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('请在这个桌面的浏览器里完成登录或验证');
        // The person has not taken over, so the screen bar must not offer a
        // button that would send 登录完成.
        expect(screen.queryByTestId('desktop-bot-exit-takeover')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-takeover'));
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        const sentBeforeRelease = sendTask.mock.calls.length;
        fireEvent.click(screen.getByTestId('desktop-bot-exit-takeover'));
        expect(screen.getByTestId('desktop-bot-stage').className).not.toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-panel')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=0');
        expect(sendTask.mock.calls.length).toBe(sentBeforeRelease);

        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-login',
            session_key: 'alice:bot_1',
            novnc_url: liveDesktop,
            user_control: false,
        });
        await waitFor(() => {
            expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
        });
        expect(screen.getByTestId('desktop-bot-panel')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('Bot 正在操作桌面');
    });

    it('clears a latched login when the watch poll reports the keyboard back', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: true });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-watch-login',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
            fireEvent.click(screen.getByTestId('desktop-bot-send'));
            await act(async () => { await Promise.resolve(); });
            await act(async () => {
                responseListeners.byEvent['desktop-bot-view']?.({
                    request_id: 'desktop-bot-watch-login',
                    session_key: 'alice:bot_1',
                    novnc_url: liveDesktop,
                    user_control: true,
                });
            });
            expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
            fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
            expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
            await act(async () => { await Promise.resolve(); await Promise.resolve(); });
            watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false, attention_reason: '' });
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-details')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
            expect(screen.queryByTestId('desktop-bot-computer')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('keeps the listed bots when a create finishes before the list returns', async () => {
        const listedAt = '2024-01-15T00:30:00Z';
        const createdAt = '2023-11-04T16:45:00Z';
        let resolveList: (bots: HubDesktopBot[]) => void = () => {};
        listBots.mockReturnValue(new Promise(resolve => { resolveList = resolve; }));
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: createdAt,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        await act(async () => {
            resolveList([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1', created_at: listedAt }]);
        });
        expect(await screen.findByTestId('desktop-bot-bot_1')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-bot_new')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-bot_1').querySelector('button.desktop-bot-workspace__open') as HTMLButtonElement);
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        expect(screen.getByTestId('desktop-bot-details').textContent).toContain(formatCreated(Date.parse(listedAt)));
        fireEvent.click(screen.getByTestId('desktop-bot-bot_new').querySelector('button.desktop-bot-workspace__open') as HTMLButtonElement);
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const details = screen.getByTestId('desktop-bot-details');
        expect(details.textContent).toContain(formatCreated(Date.parse(createdAt)));
        expect(details.textContent).not.toContain(formatCreated(Date.parse(listedAt)));
    });

    it('does not dispatch a queued message again when the bot list arrives', async () => {
        let resolveList: (bots: HubDesktopBot[]) => void = () => {};
        listBots.mockReturnValue(new Promise(resolve => { resolveList = resolve; }));
        let resolveFirst: (value: { request_id: string; deferred: boolean; session_key: string }) => void = () => {};
        let calls = 0;
        sendTask.mockImplementation(async (botId: string) => {
            calls += 1;
            if (calls === 1) return new Promise(resolve => { resolveFirst = resolve; });
            return { request_id: `desktop-bot-q${calls}`, deferred: true, session_key: `alice:${botId}` };
        });
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: '2023-11-04T16:45:00Z',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        await screen.findByTestId('desktop-bot-bot_new');
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText(told('打开示例网站'))).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('表述:顺便截一张桌面')).toBeTruthy();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_new', childOrder('顺便截一张桌面', 'plan', ['打开示例网站']), 'execute');
        await act(async () => { resolveList([]); });
        expect(sendTask).toHaveBeenCalledTimes(2);
        await act(async () => { resolveFirst({ request_id: 'desktop-bot-q1', deferred: true, session_key: 'alice:bot_new' }); });
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q1',
            session_key: 'alice:bot_new',
            text: '已打开 example.org',
            send_to_user: '已打开 example.org',
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(2);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q2',
            session_key: 'alice:bot_new',
            text: '已截图',
            send_to_user: '已截图',
        });
        expect(await screen.findByText('已截图')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(2);
    });

    it('shows the hub created time from the list and from the create response', async () => {
        const listedAt = '2024-01-15T00:30:00Z';
        const createdAt = '2023-11-04T16:45:00Z';
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1', created_at: listedAt }]);
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: createdAt,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const listedText = formatCreated(Date.parse(listedAt));
        expect(screen.getByTestId('desktop-bot-details').textContent).toContain(listedText);
        expect(screen.getByTestId('desktop-bot-details').textContent).not.toContain(formatCreated(Date.now()));

        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const createdText = formatCreated(Date.parse(createdAt));
        const details = screen.getByTestId('desktop-bot-details');
        expect(details.textContent).toContain(createdText);
        expect(details.textContent).not.toContain(listedText);
        expect(details.textContent).not.toContain(formatCreated(Date.now()));
    });

    it('places the panel beside the chat when wide and over the chat when narrow', () => {
        const fromFrontend = join(process.cwd(), 'src/components/bots/DesktopBotWorkspace.css');
        const cssPath = existsSync(fromFrontend) ? fromFrontend : join(process.cwd(), 'guiapp/frontend/src/components/bots/DesktopBotWorkspace.css');
        const css = readFileSync(cssPath, 'utf8');
        const wideAt = css.indexOf('/* wide-panel:');
        const narrowAt = css.indexOf('/* narrow-panel:');
        expect(wideAt).toBeGreaterThan(0);
        expect(narrowAt).toBeGreaterThan(wideAt);
        const wide = css.slice(wideAt, narrowAt);
        const narrow = css.slice(narrowAt);
        expect(wide).toContain('.desktop-bot-panel');
        expect(wide).toContain('400px');
        expect(wide).toMatch(/grid-template-columns:[^;]*400px/);
        expect(css).toContain('--desktop-bot-list-width: 300px');
        expect(css).toMatch(/grid-template-columns:\s*var\(--desktop-bot-list-width\)\s+minmax\(0,\s*1fr\)/);
        expect(wide).toMatch(/var\(--desktop-bot-list-width\)/);
        expect(narrow).toContain('var(--desktop-bot-list-width, 300px)');
        expect(narrow).toContain('.desktop-bot-panel');
        expect(narrow).toContain('position: absolute');
        expect(narrow).not.toMatch(/\.desktop-bot-workspace__list-pane[^{]*\{[^}]*display:\s*none/);
        expect(css).toMatch(/\.desktop-bot-chat__chevron\s*\{[^}]*opacity:\s*0/);
        expect(css).toContain('.desktop-bot-chat__identity-btn:hover .desktop-bot-chat__chevron');
        expect(css).toContain('.desktop-bot-chat__identity-btn:focus-visible .desktop-bot-chat__chevron');
        expect(css).toMatch(/\.desktop-bot-workspace__actions\s*\{[^}]*position:\s*absolute/);
        expect(css).toMatch(/\.desktop-bot-workspace__actions\s*\{[^}]*pointer-events:\s*none/);
        expect(css).toMatch(/\.bot-edit-dialog__input\s*\{[^}]*border-radius:\s*8px/);
        expect(css).toMatch(/\.bot-edit-dialog__notice\s*\{[^}]*var\(--theme-danger/);
        expect(css).toMatch(/\.desktop-bot-workspace__actions button\s*\{[^}]*width:\s*28px/);
        expect(css).toMatch(/\.desktop-bot-workspace__actions button\s*\{[^}]*height:\s*28px/);
        expect(css).toMatch(/\.desktop-bot-workspace__actions button\s*\{[^}]*padding:\s*0/);
        expect(css).toMatch(/\.desktop-bot-chat__identity-btn\s*\{[^}]*cursor:\s*grab/);
        expect(css).toMatch(/\.desktop-bot-chat__header\s*\{[^}]*touch-action:\s*none/);
        expect(css).toMatch(/\.desktop-bot-workspace\.is-identity-dragging iframe\s*\{[^}]*pointer-events:\s*none/);
        expect(css).toContain('--desktop-bot-gutter: 8%');
        expect(css).toMatch(/\.desktop-bot-chat__log-body\s*\{[^}]*display:\s*flex/);
        expect(css).toMatch(/\.desktop-bot-chat \.desktop-bot-chat__processing\s*\{[^}]*var\(--desktop-bot-gutter/);
        expect(css).toMatch(/\.desktop-bot-chat \.desktop-bot-chat__processing\s*\{[^}]*min\(720px/);
        expect(css).toMatch(/\.desktop-bot-chat__processing-label\s*\{[^}]*background-clip:\s*text/);
        expect(css).toMatch(/\.desktop-bot-chat__processing-label\s*\{[^}]*background-repeat:\s*repeat-x/);
        expect(css).toMatch(/@keyframes desktop-bot-processing-shimmer\s*\{\s*0%\s*\{\s*background-position:\s*100%\s*0;\s*\}\s*100%\s*\{\s*background-position:\s*0%\s*0;\s*\}/);
        expect(css).toMatch(/prefers-reduced-motion:\s*reduce\)\s*\{[\s\S]*?\.desktop-bot-chat__processing-label\s*\{[^}]*-webkit-text-fill-color:\s*currentColor/);
        expect(css).toMatch(/\.desktop-bot-chat__log\.is-following\s*\{[^}]*overflow-anchor:\s*none/);
        expect(css).toMatch(/\.desktop-bot-chat__entry--user\s*\{[^}]*align-self:\s*flex-end/);
        expect(css).toMatch(/\.desktop-bot-chat__bubble--user\s*\{[^}]*theme-primary/);
        expect(css).toMatch(/\.desktop-bot-chat__bubble--assistant\s*\{[^}]*var\(--theme-surface/);
        expect(css).toMatch(/\.desktop-bot-chat__action\s*\{[^}]*appearance:\s*none/);
        expect(css).toMatch(/\.desktop-bot-chat__bubble \+ \.desktop-bot-chat__action\s*,\s*\.desktop-bot-chat__bubble \+ \.desktop-bot-chat__secret\s*,\s*\.desktop-bot-chat__bubble \[data-testid="desktop-bot-take-computer"\]\s*\{[^}]*margin-top:\s*14px/);
        expect(css).toMatch(/\.desktop-bot-chat__shot-open\s*\{[^}]*appearance:\s*none/);
        expect(css).toMatch(/\.desktop-bot-chat__shot-open\s*\{[^}]*cursor:\s*zoom-in/);
        expect(css).toMatch(/\.desktop-bot-chat__shot\s*\{[^}]*max-width:\s*100%/);
        expect(css).toMatch(/\.desktop-bot-shot-preview\s*\{[^}]*position:\s*fixed/);
        expect(css).toMatch(/\.desktop-bot-shot-preview\s*\{[^}]*inset:\s*0/);
        expect(css).toMatch(/\.desktop-bot-shot-preview__close\s*\{[^}]*position:\s*absolute/);
        expect(css).toMatch(/\.desktop-bot-chat__entry--assistant:has\(\.desktop-bot-chat__shot\)\s*\{[^}]*width:\s*100%/);
        expect(css).toMatch(/\.desktop-bot-chat \.desktop-bot-chat__body li > p,[\s\S]*?\{[^}]*margin-top:\s*4px/);
        expect(css).toMatch(/\.desktop-bot-chat \.desktop-bot-chat__pre code\s*\{[^}]*background:\s*transparent/);
        expect(css).toContain('padding: 12px 16px 12px');
        expect(css).toContain('aspect-ratio: 1440 / 900');
        expect(css).toMatch(/\.desktop-bot-panel \.desktop-bot-stage:not\(\.is-fullscreen\)\s*\{[^}]*display:\s*block/);
        expect(css).toMatch(/\.desktop-bot-panel \.desktop-bot-stage:not\(\.is-fullscreen\)\s*\{[^}]*flex:\s*0\s+0\s+auto/);
        expect(css).toMatch(/\.desktop-bot-stage__bar p\s*\{[^}]*pointer-events:\s*auto/);
        expect(css).toMatch(/\.desktop-bot-stage__bar\s*\{[^}]*background:\s*none/);
        expect(css).toMatch(/\.desktop-bot-stage__bar button\s*\{[^}]*appearance:\s*none/);
        expect(css).toMatch(/\.desktop-bot-stage__bar button\s*\{[^}]*background-color:\s*#101828/);
        expect(css).toMatch(/\.desktop-bot-stage__bar button\s*\{[^}]*color:\s*#fff/);
        expect(css).toMatch(/\.desktop-bot-stage__bar button\s*\{[^}]*flex-shrink:\s*0/);
        expect(css).toMatch(/\.desktop-bot-stage\.is-fullscreen\.is-user:has\(\.desktop-bot-stage__bar-actions\)\s*\{[^}]*--desktop-bot-fit-reserve:\s*0px/);
        expect(css).not.toMatch(/\.desktop-bot-stage\.is-user\s*\{[^}]*--desktop-bot-fit-reserve:\s*0px/);
        expect(css).toMatch(/\.desktop-bot-stage__fit\s*\{[^}]*top:\s*6px/);
        expect(css).toMatch(/\.desktop-bot-stage__fit\s*\{[^}]*right:\s*6px/);
        expect(css).toMatch(/\.desktop-bot-panel__toolbar\s*\{[^}]*position:\s*absolute/);
        expect(css).toMatch(/\.desktop-bot-panel__close\s*\{[^}]*padding:\s*0/);
        expect(narrow).toMatch(/\.desktop-bot-panel__who\s*\{[^}]*padding-inline:/);
    });

    it('starts the bot list at five sixths of the old width and resizes it from the divider', () => {
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        const workspace = screen.getByTestId('desktop-bot-workspace');
        const handle = screen.getByTestId('desktop-bot-list-resize');
        expect(handle.getAttribute('role')).toBe('separator');
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('300px');

        fireEvent.pointerDown(handle, { button: 0, clientX: 300, pointerId: 1 });
        fireEvent.pointerMove(handle, { clientX: 372, pointerId: 1 });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('372px');
        fireEvent.pointerUp(handle, { clientX: 372, pointerId: 1 });
        expect(localStorage.getItem('maclaw.desktopBotListWidth.v1')).toBe('372');

        fireEvent.pointerDown(handle, { button: 0, clientX: 372, pointerId: 2 });
        fireEvent.pointerMove(handle, { clientX: 900, pointerId: 2 });
        fireEvent.pointerUp(handle, { clientX: 900, pointerId: 2 });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('480px');

        fireEvent.keyDown(handle, { key: 'ArrowLeft' });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('464px');
        fireEvent.keyDown(handle, { key: 'Home' });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('240px');
        fireEvent.keyDown(handle, { key: 'End' });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('480px');
        fireEvent.doubleClick(handle);
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('300px');
        expect(localStorage.getItem('maclaw.desktopBotListWidth.v1')).toBe('300');
    });

    it('restores a saved bot list width and ignores a value outside the drag range', () => {
        localStorage.setItem('maclaw.desktopBotListWidth.v1', '900');
        const wide = renderBots(<DesktopBotWorkspace lang="en" userId="alice" />);
        expect(screen.getByTestId('desktop-bot-workspace').style.getPropertyValue('--desktop-bot-list-width')).toBe('480px');
        wide.unmount();

        localStorage.setItem('maclaw.desktopBotListWidth.v1', 'nope');
        renderBots(<DesktopBotWorkspace lang="en" userId="alice" />);
        expect(screen.getByTestId('desktop-bot-workspace').style.getPropertyValue('--desktop-bot-list-width')).toBe('300px');
    });

    it('tracks the divider in layout pixels when the shell is scaled', () => {
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        const workspace = screen.getByTestId('desktop-bot-workspace');
        const handle = screen.getByTestId('desktop-bot-list-resize');
        Object.defineProperty(workspace, 'clientWidth', { configurable: true, value: 1000 });
        vi.spyOn(workspace, 'getBoundingClientRect').mockReturnValue({
            x: 0, y: 0, top: 0, left: 0, right: 500, bottom: 100, width: 500, height: 100, toJSON() { return {}; },
        });
        fireEvent.pointerDown(handle, { button: 0, clientX: 150, pointerId: 1 });
        fireEvent.pointerMove(handle, { clientX: 190, pointerId: 1 });
        expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('380px');
    });

    it('keeps a saved list width when the profile panel only clamps the column', async () => {
        localStorage.setItem('maclaw.desktopBotListWidth.v1', '480');
        const original = window.matchMedia;
        window.matchMedia = vi.fn((query: string) => ({
            matches: query.includes('1181'),
            media: query,
            onchange: null,
            addListener: () => {},
            removeListener: () => {},
            addEventListener: () => {},
            removeEventListener: () => {},
            dispatchEvent: () => false,
        })) as unknown as typeof window.matchMedia;
        try {
            listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst', created_at: '2026-01-01T00:00:00Z' }]);
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            const workspace = screen.getByTestId('desktop-bot-workspace');
            Object.defineProperty(workspace, 'clientWidth', { configurable: true, value: 1000 });
            fireEvent.click(await screen.findByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('280px');
            const handle = screen.getByTestId('desktop-bot-list-resize');
            fireEvent.pointerDown(handle, { button: 0, clientX: 280, pointerId: 1 });
            fireEvent.pointerUp(handle, { clientX: 280, pointerId: 1 });
            expect(localStorage.getItem('maclaw.desktopBotListWidth.v1')).toBe('480');
            expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('280px');
            fireEvent.keyDown(handle, { key: 'ArrowRight' });
            expect(localStorage.getItem('maclaw.desktopBotListWidth.v1')).toBe('480');
            expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('280px');
            fireEvent.keyDown(handle, { key: 'ArrowLeft' });
            expect(workspace.style.getPropertyValue('--desktop-bot-list-width')).toBe('264px');
            expect(localStorage.getItem('maclaw.desktopBotListWidth.v1')).toBe('264');
        } finally {
            window.matchMedia = original;
        }
    });

    it('drags the identity chip, remembers the place, and a plain click still opens the desktop', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst', created_at: '2026-01-01T00:00:00Z' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const chat = screen.getByTestId('desktop-bot-chat');
        const header = screen.getByTestId('desktop-bot-identity');
        const button = screen.getByTestId('desktop-bot-open-desktop');
        Object.defineProperty(chat, 'clientWidth', { configurable: true, value: 800 });
        Object.defineProperty(chat, 'clientHeight', { configurable: true, value: 600 });
        Object.defineProperty(header, 'offsetWidth', { configurable: true, value: 160 });
        Object.defineProperty(header, 'offsetHeight', { configurable: true, value: 48 });
        const box = (left: number, top: number, width: number, height: number) => ({
            x: left, y: top, left, top, right: left + width, bottom: top + height, width, height, toJSON() { return {}; },
        });
        vi.spyOn(chat, 'getBoundingClientRect').mockReturnValue(box(0, 0, 800, 600));
        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(320, 10, 160, 48));

        fireEvent.pointerDown(button, { button: 0, clientX: 400, clientY: 30, pointerId: 1 });
        expect(screen.getByTestId('desktop-bot-workspace').className).toContain('is-identity-dragging');
        fireEvent.pointerMove(button, { clientX: 402, clientY: 31, pointerId: 1 });
        fireEvent.pointerUp(button, { clientX: 402, clientY: 31, pointerId: 1 });
        expect(screen.getByTestId('desktop-bot-workspace').className).not.toContain('is-identity-dragging');
        fireEvent.click(button);
        expect(screen.getByTestId('desktop-bot-panel')).toBeTruthy();
        expect(localStorage.getItem('maclaw.desktopBotIdentityPos.v1')).toBeNull();
        fireEvent.click(button);
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();

        fireEvent.pointerDown(button, { button: 0, clientX: 400, clientY: 30, pointerId: 3 });
        fireEvent.pointerMove(button, { clientX: 500, clientY: 70, pointerId: 3 });
        expect(header.style.left).toBe('420px');
        expect(header.style.top).toBe('50px');
        expect(screen.getByTestId('desktop-bot-workspace').className).toContain('is-identity-dragging');
        fireEvent.pointerUp(button, { clientX: 500, clientY: 70, pointerId: 3 });
        fireEvent.click(button);
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        const saved = JSON.parse(localStorage.getItem('maclaw.desktopBotIdentityPos.v1') || '{}') as { nx: number; ny: number };
        expect(saved.nx).toBeCloseTo((420 - 8) / 624);
        expect(saved.ny).toBeCloseTo((50 - 8) / 536);

        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(420, 50, 160, 48));
        fireEvent.pointerDown(button, { button: 0, clientX: 500, clientY: 70, pointerId: 4 });
        fireEvent.pointerMove(button, { clientX: 1500, clientY: 1200, pointerId: 4 });
        expect(header.style.left).toBe('632px');
        expect(header.style.top).toBe('544px');
        fireEvent.pointerUp(button, { clientX: 1500, clientY: 1200, pointerId: 4 });

        const form = chat.querySelector('.desktop-bot-chat__form');
        if (!(form instanceof HTMLElement)) throw new Error('composer missing');
        const formBox = vi.spyOn(form, 'getBoundingClientRect').mockReturnValue(box(64, 400, 672, 72));
        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(632, 544, 160, 48));
        fireEvent.pointerDown(button, { button: 0, clientX: 700, clientY: 560, pointerId: 5 });
        fireEvent.pointerMove(button, { clientX: 700, clientY: 900, pointerId: 5 });
        expect(header.style.left).toBe('632px');
        expect(header.style.top).toBe('344px');
        fireEvent.pointerUp(button, { clientX: 700, clientY: 900, pointerId: 5 });
        fireEvent.click(button);
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });

        // The send box starts above the chip. The drag still cannot cover it.
        formBox.mockReturnValue(box(64, 30, 672, 140));
        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(632, 344, 160, 48));
        fireEvent.pointerDown(button, { button: 0, clientX: 700, clientY: 360, pointerId: 6 });
        fireEvent.pointerMove(button, { clientX: 700, clientY: 900, pointerId: 6 });
        expect(header.style.left).toBe('632px');
        expect(header.style.top).toBe('8px');
        fireEvent.pointerUp(button, { clientX: 700, clientY: 900, pointerId: 6 });

        const kept = localStorage.getItem('maclaw.desktopBotIdentityPos.v1');
        fireEvent.doubleClick(button);
        expect(localStorage.getItem('maclaw.desktopBotIdentityPos.v1')).toBe(kept);
        expect(header.className).toContain('is-placed');
        fireEvent.click(button);
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
        fireEvent.click(button);
        expect(screen.getByTestId('desktop-bot-panel')).toBeTruthy();

        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(8, 8, 160, 48));
        fireEvent.pointerDown(button, { button: 0, clientX: 40, clientY: 20, pointerId: 7 });
        fireEvent.pointerMove(button, { clientX: 80, clientY: 80, pointerId: 7 });
        fireEvent.pointerCancel(button, { clientX: 80, clientY: 80, pointerId: 7 });
        fireEvent.click(button);
        expect(screen.queryByTestId('desktop-bot-panel')).toBeNull();
    });

    it('drags the identity chip in layout pixels when the shell is scaled', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst', created_at: '2026-01-01T00:00:00Z' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const chat = screen.getByTestId('desktop-bot-chat');
        const header = screen.getByTestId('desktop-bot-identity');
        const button = screen.getByTestId('desktop-bot-open-desktop');
        Object.defineProperty(chat, 'clientWidth', { configurable: true, value: 800 });
        Object.defineProperty(chat, 'clientHeight', { configurable: true, value: 600 });
        Object.defineProperty(header, 'offsetWidth', { configurable: true, value: 160 });
        Object.defineProperty(header, 'offsetHeight', { configurable: true, value: 48 });
        const box = (left: number, top: number, width: number, height: number) => ({
            x: left, y: top, left, top, right: left + width, bottom: top + height, width, height, toJSON() { return {}; },
        });
        vi.spyOn(chat, 'getBoundingClientRect').mockReturnValue(box(0, 0, 400, 300));
        vi.spyOn(header, 'getBoundingClientRect').mockReturnValue(box(160, 5, 80, 24));
        fireEvent.pointerDown(button, { button: 0, clientX: 200, clientY: 20, pointerId: 1 });
        fireEvent.pointerMove(button, { clientX: 250, clientY: 40, pointerId: 1 });
        expect(header.style.left).toBe('420px');
        expect(header.style.top).toBe('50px');
    });

    it('restores a saved identity place and ignores a place that cannot be read', async () => {
        const clientWidth = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(800);
        const clientHeight = vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(600);
        const offsetWidth = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(160);
        const offsetHeight = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(48);
        try {
            localStorage.setItem('maclaw.desktopBotIdentityPos.v1', JSON.stringify({ nx: 0, ny: 1 }));
            listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst', created_at: '2026-01-01T00:00:00Z' }]);
            const placed = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            fireEvent.click(await screen.findByText('值班'));
            const header = screen.getByTestId('desktop-bot-identity');
            expect(header.style.left).toBe('8px');
            expect(header.style.top).toBe('544px');
            expect(header.className).toContain('is-placed');
            placed.unmount();

            localStorage.setItem('maclaw.desktopBotIdentityPos.v1', '{');
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            fireEvent.click(await screen.findByText('值班'));
            expect(screen.getByTestId('desktop-bot-identity').style.left).toBe('');
        } finally {
            clientWidth.mockRestore();
            clientHeight.mockRestore();
            offsetWidth.mockRestore();
            offsetHeight.mockRestore();
        }
    });

    it('remembers typed commands for this account and does not touch the assistant history', async () => {
        localStorage.setItem('ai-assistant-prompt-history', JSON.stringify(['助手里的一句']));
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '早上', instance_id: 'inst_2' },
        ]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: `desktop-bot-${botId}`,
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '再截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        const box = screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement;
        await waitFor(() => expect(box.value).toBe(''));
        fireEvent.keyDown(box, { key: 'ArrowUp' });
        await waitFor(() => expect(box.value).toBe('再截一张桌面'));
        box.setSelectionRange(box.value.length, box.value.length);
        fireEvent.keyDown(box, { key: 'ArrowUp' });
        await waitFor(() => expect(box.value).toBe('打开示例网站'));

        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotInputHistory.v1') || '{}') as { alice?: string[] };
        expect(stored.alice).toEqual(['打开示例网站', '再截一张桌面']);
        expect(JSON.parse(localStorage.getItem('ai-assistant-prompt-history') || '[]')).toEqual(['助手里的一句']);

        fireEvent.click(screen.getByText('日报'));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '' } });
        const other = screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement;
        other.setSelectionRange(0, 0);
        fireEvent.keyDown(other, { key: 'ArrowUp' });
        await waitFor(() => expect(other.value).toBe('再截一张桌面'));

        view.rerender(<DialogProvider><DesktopBotWorkspace lang="zh-Hans" userId="bob" /></DialogProvider>);
        fireEvent.click(await screen.findByText('值班'));
        const bobBox = screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement;
        fireEvent.change(bobBox, { target: { value: '' } });
        bobBox.setSelectionRange(0, 0);
        fireEvent.keyDown(bobBox, { key: 'ArrowUp' });
        expect(bobBox.value).toBe('');
        expect(screen.queryByTestId('desktop-bot-input-history')).toBeNull();
    });

    it('follows the chat tail when the transcript grows and stays put after a scroll up', async () => {
        let height = 480;
        const tops = new WeakMap<HTMLElement, number>();
        const scrollHeight = vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function (this: HTMLElement) {
            return this.dataset?.testid === 'desktop-bot-log' ? height : 0;
        });
        const clientHeight = vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function (this: HTMLElement) {
            return this.dataset?.testid === 'desktop-bot-log' ? 120 : 0;
        });
        const ownedScrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTop');
        Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
            configurable: true,
            get() { return tops.get(this) || 0; },
            set(value: number) { tops.set(this, value); },
        });
        const callbacks: ResizeObserverCallback[] = [];
        class FakeResizeObserver {
            private cb: ResizeObserverCallback;
            constructor(cb: ResizeObserverCallback) {
                this.cb = cb;
                callbacks.push(cb);
            }
            observe() { /* size changes are fired by the test */ }
            unobserve() { /* no per-element tracking */ }
            disconnect() { /* observe() after disconnect still notifies */ }
        }
        vi.stubGlobal('ResizeObserver', FakeResizeObserver);
        const fireResize = () => {
            for (const cb of [...callbacks]) cb([], {} as ResizeObserver);
        };
        const flushFrame = () => act(async () => {
            await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
        });
        try {
            localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
                alice: {
                    bot_1: [
                        { id: 'm1', role: 'user', content: '早' },
                        { id: 'm2', role: 'assistant', content: '在。' },
                    ],
                },
            }));
            listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst' }]);
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            fireEvent.click(await screen.findByText('值班'));
            expect(await screen.findByText('在。')).toBeTruthy();
            const log = screen.getByTestId('desktop-bot-log');
            await flushFrame();
            expect(log.scrollTop).toBe(480 - 120);
            expect(log.className).toContain('is-following');

            fireEvent.pointerDown(log);
            height = 640;
            await act(async () => { fireResize(); });
            await flushFrame();
            expect(log.scrollTop).toBe(640 - 120);

            tops.set(log, 0);
            fireEvent.scroll(log);
            expect(log.className).not.toContain('is-following');
            height = 900;
            await act(async () => { fireResize(); });
            await flushFrame();
            expect(log.scrollTop).toBe(0);

            tops.set(log, 900 - 120);
            fireEvent.scroll(log);
            expect(log.className).toContain('is-following');
            height = 1000;
            await act(async () => { fireResize(); });
            await flushFrame();
            expect(log.scrollTop).toBe(1000 - 120);

            tops.set(log, 0);
            fireEvent.scroll(log);
            fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再看一眼' } });
            fireEvent.click(screen.getByTestId('desktop-bot-send'));
            expect(await screen.findByText('再看一眼')).toBeTruthy();
            await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
            await flushFrame();
            expect(log.scrollTop).toBe(1000 - 120);
        } finally {
            scrollHeight.mockRestore();
            clientHeight.mockRestore();
            if (ownedScrollTop) Object.defineProperty(HTMLElement.prototype, 'scrollTop', ownedScrollTop);
            else delete (HTMLElement.prototype as { scrollTop?: number }).scrollTop;
            vi.unstubAllGlobals();
        }
    });

    it('marks the open bot seen when this window is in front, including while the desktop frame has the keyboard', async () => {
        const hasFocus = document.hasFocus.bind(document);
        const visibility = Object.getOwnPropertyDescriptor(document, 'visibilityState');
        document.hasFocus = () => false;
        try {
            adoptExistingBotReplies();
            appendDesktopBotReport('alice', 'bot_1', { content: '你好！很高兴见到你。', failed: false, requestId: 'desktop-bot-sched-seen' });
            expect(unreadBotReplyCount('alice')).toBe(1);
            listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            fireEvent.click(await screen.findByText('值班'));
            expect(unreadBotReplyCount('alice')).toBe(1);

            await act(async () => { window.dispatchEvent(new Event('focus')); });
            expect(unreadBotReplyCount('alice')).toBe(0);
            appendDesktopBotReport('alice', 'bot_1', { content: '第二句问候。', failed: false, requestId: 'desktop-bot-sched-seen-2' });
            expect(unreadBotReplyCount('alice')).toBe(0);

            await act(async () => { window.dispatchEvent(new Event('blur')); });
            appendDesktopBotReport('alice', 'bot_1', { content: '窗口到了后面。', failed: false, requestId: 'desktop-bot-sched-seen-3' });
            expect(unreadBotReplyCount('alice')).toBe(1);

            Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
            await act(async () => { window.dispatchEvent(new Event('focus')); });
            expect(unreadBotReplyCount('alice')).toBe(1);

            Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
            await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
            expect(unreadBotReplyCount('alice')).toBe(0);
        } finally {
            document.hasFocus = hasFocus;
            setBotWindowForeground(false);
            if (visibility) Object.defineProperty(document, 'visibilityState', visibility);
            else Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        }
    });

    it('puts the model entry in the header, to the right of 新建', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '', instance_id: '' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        const create = await screen.findByTestId('desktop-bot-create');
        const model = screen.getByTestId('desktop-bot-llm');
        expect(model.compareDocumentPosition(create) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-custom-model')).toBeNull();
    });

    it('sends the whole provider route when a bot owner picks one', async () => {
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [
                { name: '空的', url: 'https://empty.example/v1', key: '', model: 'x' },
                { name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: 'qwen-max', protocol: 'openai', wire_api: '' },
            ],
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        // The provider carries its own model, so the common case needs no typing.
        expect(within(dialog).getByTestId('bot-llm-model')).toHaveProperty('value', 'qwen-max');
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith({
            mode: 'custom',
            name: 'Qwen',
            url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
            key: 'sk-qwen',
            model: 'qwen-max',
            protocol: 'openai',
            wire_api: '',
        }));
        await waitFor(() => expect(screen.queryByTestId('bot-llm-dialog')).toBeNull());
    });

    // The model is picked, not typed: the dropdown is seeded from the provider's
    // cached models and refreshed from the service, and the saved model stays
    // selectable even when the provider default has since moved on.
    it('offers the provider models as a dropdown, saved model included', async () => {
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'Qwen', model: 'qwen-turbo', key_set: true });
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{
                name: 'Qwen',
                url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
                key: 'sk-qwen',
                model: 'qwen-max',
                protocol: 'openai',
                models: ['qwen-plus'],
            }],
        });
        fetchProviderModels.mockResolvedValue([{ id: 'qwen3-max' }, { id: 'qwen-plus' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        const model = within(dialog).getByTestId('bot-llm-model') as HTMLSelectElement;
        expect(model.tagName).toBe('SELECT');
        // Saved model first, then the live catalog, cached models merged in.
        await waitFor(() => expect(Array.from(model.options).map(o => o.value)).toEqual(['qwen-turbo', 'qwen3-max', 'qwen-plus']));
        fireEvent.change(model, { target: { value: 'qwen3-max' } });
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith(expect.objectContaining({ mode: 'custom', model: 'qwen3-max' })));
    });

    // The dropdown must never flash "no models" while the service is still
    // answering: the first frame is seeded from the provider itself, with the
    // loading hint standing in until the live catalog lands.
    it('seeds the dropdown and says it is loading before the service answers', async () => {
        fetchProviderModels.mockReturnValue(new Promise(() => { /* never settles */ }));
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{
                name: 'Qwen',
                url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
                key: 'sk-qwen',
                model: 'qwen-max',
                protocol: 'openai',
                models: ['qwen-plus'],
            }],
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        const model = within(dialog).getByTestId('bot-llm-model') as HTMLSelectElement;
        expect(Array.from(model.options).map(o => o.value)).toEqual(['qwen-max', 'qwen-plus']);
        expect(model).toHaveProperty('value', 'qwen-max');
        expect(within(dialog).getByText('正在获取模型列表…')).toBeTruthy();
        expect(within(dialog).queryByText('这个服务商暂时没有可选模型。')).toBeNull();
    });

    // Switching provider re-points the model at the new provider's own list in
    // the same commit: the two selects never disagree, and a model picked for
    // the previous provider is not carried over.
    it('re-points the model at the new provider instead of carrying the old pick', async () => {
        fetchProviderModels.mockResolvedValue([]);
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [
                { name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: 'qwen-max', protocol: 'openai' },
                { name: 'DeepSeek', url: 'https://api.deepseek.com/v1', key: 'sk-ds', model: 'deepseek-chat', protocol: 'openai' },
            ],
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        const provider = within(dialog).getByTestId('bot-llm-provider') as HTMLSelectElement;
        const model = within(dialog).getByTestId('bot-llm-model') as HTMLSelectElement;
        expect(model).toHaveProperty('value', 'qwen-max');
        fireEvent.change(provider, { target: { value: 'DeepSeek' } });
        expect(model).toHaveProperty('value', 'deepseek-chat');
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith(expect.objectContaining({ mode: 'custom', name: 'DeepSeek', model: 'deepseek-chat' })));
    });

    // A provider with no model anywhere still lets the owner save: the list
    // shows the reason instead of a blank box that fails validation.
    it('says so when the provider exposes no models to pick', async () => {        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{ name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: '', protocol: 'openai' }],
        });
        fetchProviderModels.mockResolvedValue([]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        expect(await within(dialog).findByText('这个服务商暂时没有可选模型。')).toBeTruthy();
        expect(within(dialog).getByTestId('bot-llm-save')).toHaveProperty('disabled', true);
    });

    // Hub-resolved providers are served by the service, not by a public models
    // endpoint; their saved model stays selectable without a fetch.
    it('keeps the saved hub model selectable without calling the models endpoint', async () => {
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'Hub 线路', model: 'hub-alias-1', key_set: true });
        listProviders.mockResolvedValue({
            current: 'Hub 线路',
            providers: [{ name: 'Hub 线路', url: 'https://hub.example/v1', key: 'hub-key', model: 'hub-alias-1', protocol: 'openai', is_hub_service: true }],
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        const model = within(dialog).getByTestId('bot-llm-model') as HTMLSelectElement;
        await waitFor(() => expect(Array.from(model.options).map(o => o.value)).toEqual(['hub-alias-1']));
        expect(fetchProviderModels).not.toHaveBeenCalled();
    });

    // What "默认" restores is whatever the service resolves this owner right now,
    // so the request must carry nothing else — no remembered provider or model
    // to fall back to, whether the default comes from the system or an admin.
    it('asks for plain default when the owner leaves their own model behind', async () => {
        getBotLLM.mockResolvedValue({ mode: 'custom', name: 'Qwen', model: 'qwen-max', key_set: true });
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{ name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: 'qwen-max', protocol: 'openai' }],
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        await waitFor(() => expect(within(dialog).getByTestId('bot-llm-mode-custom')).toHaveProperty('checked', true));
        expect(within(dialog).getByTestId('bot-llm-model')).toHaveProperty('value', 'qwen-max');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-default'));
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith({ mode: 'default' }));
        await waitFor(() => expect(screen.queryByTestId('bot-llm-dialog')).toBeNull());
    });

    it('keeps the dialog open and shows why when the hub refuses a provider', async () => {
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{ name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: 'qwen-max' }],
        });
        saveBotLLM.mockRejectedValue(new Error('服务商名已被占用'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        expect(await within(dialog).findByText('服务商名已被占用')).toBeTruthy();
        expect(screen.queryByTestId('bot-llm-dialog')).not.toBeNull();
    });

    // The hub keeps the choice even when it cannot reach MaClawSrv at that
    // moment. Closing would look like nothing happened, so the dialog repeats
    // the reason instead of pretending the change landed everywhere.
    it('says so when the choice was stored but reached no bot yet', async () => {
        listProviders.mockResolvedValue({
            current: 'Qwen',
            providers: [{ name: 'Qwen', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', key: 'sk-qwen', model: 'qwen-max' }],
        });
        saveBotLLM.mockResolvedValue({ mode: 'custom', name: 'Qwen', model: 'qwen-max', key_set: true, warning: '这台云服务器暂时连不上，下次打开 bot 时会带上。' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByTestId('desktop-bot-llm'));
        const dialog = await screen.findByTestId('bot-llm-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        expect(await within(dialog).findByText('这台云服务器暂时连不上，下次打开 bot 时会带上。')).toBeTruthy();
        expect(screen.queryByTestId('bot-llm-dialog')).not.toBeNull();
        expect(screen.getByTestId('desktop-bot-custom-model').getAttribute('aria-label')).toBe('正在使用自定义模型：Qwen · qwen-max');
    });
    it('upgrades a queued supplement receipt when the run reports the outcome', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'm-task', role: 'assistant', content: '正在打开订单页面', pending: false },
            { id: 'm-sup', role: 'assistant', content: '已收到，会在当前任务的下一步生效。', ack: true, understood: true, supplementId: 'sup-1', supplementRunId: 'run-42' },
        ]);
        handleDesktopBotSupplementState({
            bot_id: 'bot_1',
            session_key: 'alice:bot_1',
            run_id: 'run-42',
            items: [{ id: 'sup-1', state: 'missed', content: '只要前5条' }],
        });
        const sup = messagesForBot('alice', 'bot_1').find(item => item.id === 'm-sup');
        expect(sup?.supplementOutcome).toBe('missed');
        expect(sup?.failed).toBe(true);
        expect(sup?.content).toBe('任务已结束，这条补充没能生效。');
        expect(sup?.noReading?.text).toBe('只要前5条');
        expect(sup?.noReading?.kind).toBe('unrouted');
        // The task bubble next to it is untouched.
        expect(messagesForBot('alice', 'bot_1').find(item => item.id === 'm-task')?.content).toBe('正在打开订单页面');
    });

    it('marks an applied supplement without parking anything', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'm-sup', role: 'assistant', content: '已收到，会在当前任务的下一步生效。', ack: true, supplementId: 'sup-9', supplementRunId: 'run-9' },
        ]);
        applySupplementOutcomes('alice', 'bot_1', 'run-9', [{ id: 'sup-9', state: 'applied' }]);
        const sup = messagesForBot('alice', 'bot_1').find(item => item.id === 'm-sup');
        expect(sup?.supplementOutcome).toBe('applied');
        expect(sup?.content).toBe('补充已生效。');
        expect(sup?.failed).toBeUndefined();
        expect(sup?.noReading).toBeUndefined();
    });

    it('renders with Hub reconnecting, offline, or answering nothing', async () => {
        for (const state of [{ state: 'connecting', next_retry_ms: 3000 }, { state: 'offline' }, null]) {
            hubLink.mockResolvedValue(state as Record<string, unknown> | null);
            const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            expect(await screen.findByTestId('desktop-bot-workspace')).toBeTruthy();
            if (state?.state === 'connecting') expect(await screen.findByTestId('desktop-bot-hub-link')).toBeTruthy();
            if (!state) expect(screen.queryByTestId('desktop-bot-hub-link')).toBeNull();
            view.unmount();
        }
    });

    it('renders with an empty bot list and odd stored messages', async () => {
        listBots.mockResolvedValue(null as unknown as never);
        saveBotMessages('alice', 'bot_1', [
            { id: 'odd-1', role: 'assistant', content: '' },
            { id: 'odd-2', role: 'user', content: undefined as unknown as string },
            { id: 'odd-3', role: 'assistant', content: 'x', pending: true, requestId: 'gone' },
        ]);
        renderBots(<DesktopBotWorkspace lang="en" userId="alice" />);
        expect(await screen.findByTestId('desktop-bot-workspace')).toBeTruthy();
    });

    it('answers a bare 继续 during the resumed run once, without queueing it', async () => {
        listBotTasks.mockResolvedValue([{ run_id: 'run-1', bot_id: 'bot_1', status: 'running', task: '知乎回答' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '继续' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('正在接着做。')).toBeTruthy();
        expect(screen.queryByText('好，这个等手上的做完我就做。')).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();
        act(() => {
            responseListeners.byEvent['desktop-bot-queued']?.({ bot_id: 'bot_1', owner_id: 'alice' });
            responseListeners.byEvent['desktop-bot-queued']?.({ bot_id: 'bot_1', owner_id: 'alice' });
        });
        expect(screen.getAllByText('正在接着做。')).toHaveLength(1);
        expect(screen.queryByText(/等手上的做完/)).toBeNull();
    });

    it('waits for the desktop that is already driving, then sends the order once', async () => {
        listBotTasks.mockResolvedValue([{ run_id: 'run-1', bot_id: 'bot_1', status: 'running', task: '早报' }]);
        sendTask.mockResolvedValue({ request_id: 'req-queued', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('好，这个等手上的做完我就做。')).toBeTruthy();
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: false });
        });
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        expect(sendTask).toHaveBeenCalledWith('bot_1', childOrder('打开示例网站'), 'execute');
    });

    it('never rewrites a receipt that already has its terminal answer', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'm-sup', role: 'assistant', content: '补充已生效。', ack: true, supplementId: 'sup-2', supplementRunId: 'run-2', supplementOutcome: 'applied' },
        ]);
        applySupplementOutcomes('alice', 'bot_1', 'run-2', [{ id: 'sup-2', state: 'missed', content: '原文' }]);
        const sup = messagesForBot('alice', 'bot_1').find(item => item.id === 'm-sup');
        expect(sup?.supplementOutcome).toBe('applied');
        expect(sup?.content).toBe('补充已生效。');
    });

    it('starts the work at once and shows a model-written line when a direct reading has no sentence', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '打开示例网站\n再把标题记下来';
        understandTask.mockImplementation(async () => ({ told: '', instruction: '打开示例网站并记下标题', pace: 'direct' as const }));
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-oneline', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        const line = await screen.findByText('好，这就去办。');
        expect(line.closest('[data-message-role]')?.getAttribute('data-message-role')).toBe('assistant');
        expect(screen.queryByText('打开示例网站')).toBeNull();
        expect(document.querySelector('[data-message-role="user"]')?.textContent).toContain('再把标题记下来');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '打开示例网站并记下标题', 'execute'));
        const working = screen.getByTestId('desktop-bot-processing');
        expect(working.textContent).toBe('正在一点点推进');
        expect(working.closest('[data-testid="desktop-bot-log"]')).toBeNull();
        const form = screen.getByTestId('desktop-bot-command').closest('form');
        expect(form).toBeTruthy();
        expect(working.compareDocumentPosition(form as Node) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(working.querySelector('.desktop-bot-chat__processing-mark')?.getAttribute('aria-label')).toBe('停下');
        expect(working.querySelector('.desktop-bot-chat__processing-label')?.textContent).toBe('正在一点点推进');
    });

    it('replaces the working line with the step the desktop is actually in', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        understandTask.mockResolvedValue({ told: '', instruction: '打开示例网站', pace: 'direct' });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-activity', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进'));
        const emit = (payload: unknown) => {
            act(() => {
                responseListeners.byEvent['desktop-bot-activity']?.(payload);
            });
        };
        emit({ bot_id: 'bot_1', activity: '正在输入' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在输入');
        emit({ bot_id: 'bot_2', activity: '正在点击' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在输入');
        emit({ bot_id: 'bot_1', activity: '正在输入 hello' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在输入');
        emit({ bot_id: 'bot_1', activity: '正在处理' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在输入');
        emit(JSON.stringify({ bot_id: 'bot_1', activity: '正在点击' }));
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在点击');
        emit({ bot_id: 'bot_1', activity: '' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
        emit({ bot_id: 'bot_1', run_id: 'run_a', activity: '正在编辑' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在编辑');
        emit({ bot_id: 'bot_1', run_id: 'run_b', activity: '' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在编辑');
        emit({ bot_id: 'bot_1', run_id: 'run_a', activity: '' });
        expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在一点点推进');
    });

    it('keeps the install button when a plan reading says to start now', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        const ask = '安装一个pdf阅读器';
        understandTask.mockResolvedValue({ told: '好，装这个。', instruction: '安装一个pdf阅读器', pace: 'direct' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: ask } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByTestId('desktop-bot-confirm')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-confirm').textContent).toBe('按这个安排执行');
        expect(sendTask).not.toHaveBeenCalled();
        expect(screen.queryByText('安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。')).toBeNull();
    });

    it('marks exactly one primary bot and moves the mark from the details tab', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        setPrimary.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: false },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: true },
        ]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByTestId('desktop-bot-primary-bot_1')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-primary-bot_6')).toBeNull();
        expect(screen.getByTestId('desktop-bot-primary-bot_1').textContent).toBe('主');
        fireEvent.click(screen.getByText('值班'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const current = screen.getByTestId('desktop-bot-set-primary');
        expect(current.textContent).toBe('已是主 Bot');
        expect(current.hasAttribute('disabled')).toBe(true);
        expect(screen.getByTestId('desktop-bot-edit-bot_1')).toBeTruthy();
        fireEvent.click(screen.getByText('Bot 6'));
        fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const next = screen.getByTestId('desktop-bot-set-primary');
        expect(next.textContent).toBe('设为主 Bot');
        expect(next.hasAttribute('disabled')).toBe(false);
        fireEvent.click(next);
        await waitFor(() => expect(setPrimary).toHaveBeenCalledWith('bot_6'));
        expect(await screen.findByTestId('desktop-bot-primary-bot_6')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-primary-bot_1')).toBeNull();
        expect(screen.getByTestId('desktop-bot-set-primary').textContent).toBe('已是主 Bot');
    });

    it('refuses a handoff from a bot that is not primary and does not post it', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        understandTask.mockResolvedValue({
            told: '我去说',
            instruction: '打开示例网站',
            pace: 'direct',
            cross: { kind: 'notify', target: 'bot_1', text: '到了' },
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 6'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '跟值班说一声' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('我不能去叫别的 Bot。你跟主 Bot 说，或者直接打开那个 Bot。')).toBeTruthy();
        expect(postCross).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
    });

    it('shows the primary line after a handoff and the quiet receipt for several bots', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
            { id: 'bot_7', title: '记录', description: '', instance_id: 'inst_7', primary: false },
        ]);
        let releasePost: (value: { posted: boolean; ask?: string; primary_line: string; names: string[]; notes: Array<Record<string, unknown>> }) => void = () => {};
        postCross.mockImplementation(() => new Promise(resolve => { releasePost = resolve; }));
        understandTask.mockResolvedValue({
            told: '我去说',
            instruction: '',
            pace: 'reply',
            cross: { kind: 'notify', target: 'all', text: '到了' },
        });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '跟所有的bot说一声' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-processing').textContent).toBe('正在询问 这些 Bot'));
        await act(async () => {
            releasePost({
                posted: true,
                primary_line: '已发消息给 2 个 Bot',
                names: ['Bot 6', '记录'],
                notes: [
                    { id: 'n1', from: 'bot_1', target: 'bot_6', kind: 'notify', text: '到了' },
                    { id: 'n2', from: 'bot_1', target: 'bot_7', kind: 'notify', text: '到了' },
                ],
            });
        });
        expect(await screen.findByText('已发消息给 2 个 Bot')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-notice-bots').querySelectorAll('.desktop-bot-chat__notice-avatar')).toHaveLength(2);
        expect(sendTask).not.toHaveBeenCalled();
        expect(postCross).toHaveBeenCalledWith('bot_1', 'notify', 'all', '到了', '', '');
        const catalog = String(understandTask.mock.calls[0][5] || '');
        expect(catalog).toContain('"id":"bot_6"');
        expect(catalog).toContain('"primary":true');
    });

    it('lands a destructive handoff as a button and starts only after that bot claims it', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        pullCross.mockResolvedValue([
            { id: 'n1', from: 'bot_1', target: 'bot_6', kind: 'delegate', text: '装一下', order: '安装一个pdf阅读器' },
        ]);
        claimCross.mockResolvedValue({ claimed: true, from: 'bot_1', id: 'n1' });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-cross', deferred: true, session_key: 'alice:bot_6' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 6'));
        expect(await screen.findByText('主 Bot 交代：装一下')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-confirm').textContent).toBe('按这个安排执行');
        expect(claimCross).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('desktop-bot-confirm'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_6', '安装一个pdf阅读器', 'execute'));
        expect(claimCross).toHaveBeenCalledWith('bot_6', 'n1');
        expect(armCross).toHaveBeenCalledWith('bot_6', 'n1');
        expect(claimCross.mock.invocationCallOrder[0]).toBeLessThan(armCross.mock.invocationCallOrder[0]);
        expect(armCross.mock.invocationCallOrder[0]).toBeLessThan(sendTask.mock.invocationCallOrder[0]);
    });

    it('does not arm or send a handoff another client already claimed', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        pullCross.mockResolvedValue([
            { id: 'n1', from: 'bot_1', target: 'bot_6', kind: 'delegate', text: '装一下', order: '安装一个pdf阅读器' },
        ]);
        claimCross.mockResolvedValue({ claimed: false, from: 'bot_1', id: 'n1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 6'));
        fireEvent.click(await screen.findByTestId('desktop-bot-confirm'));
        await waitFor(() => expect(claimCross).toHaveBeenCalledWith('bot_6', 'n1'));
        expect(armCross).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
    });

    it('claims a non-destructive handoff once the target is idle and writes the result back', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        pullCross.mockResolvedValue([
            { id: 'n9', from: 'bot_1', target: 'bot_6', kind: 'delegate', text: '打开示例网站', order: '打开示例网站' },
        ]);
        claimCross.mockResolvedValue({ claimed: true, from: 'bot_1', id: 'n9' });
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-open', deferred: true, session_key: 'alice:bot_6' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 6'));
        expect(await screen.findByText('主 Bot 交代：打开示例网站')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_6', '打开示例网站', 'execute'));
        expect(claimCross).toHaveBeenCalledWith('bot_6', 'n9');
        expect(armCross).toHaveBeenCalledWith('bot_6', 'n9');
        act(() => {
            responseListeners.byEvent['ai-assistant-response']?.({
                request_id: 'desktop-bot-open',
                session_key: 'alice:bot_6',
                text: '页面开好了。',
                send_to_user: '页面开好了。',
            });
        });
        expect(await screen.findByText('页面开好了。')).toBeTruthy();
        await waitFor(() => expect(postCross).toHaveBeenCalledWith('bot_6', 'result', '', '页面开好了。', '', 'n9'));
    });

    it('switches to a bot display, notifies on handoff, and hands the desktop back', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: 'Bot B', description: '自己的桌面', instance_id: 'inst_2' },
        ]);
        const mine = 'http://hub.example/api/v1/desktop-handoff/mine/vnc.html?autoconnect=1';
        const isolated = 'http://hub.example/api/v1/desktop-handoff/bot-b/vnc.html?autoconnect=1';
        watchBot.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        getScreen.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        desktopAdmin.mockResolvedValue(true);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(await screen.findByTestId('desktop-bot-open-desktop'));
        expect(await screen.findByTestId('desktop-screen-switch')).toBeTruthy();
        expect(screen.getByTestId('desktop-screen-mine').textContent).toBe('我的桌面');
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_1'));
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_2'));
        expect(screen.getByTestId('desktop-screen-bot-bot_1').textContent).toContain('共享');
        expect(screen.getByTestId('desktop-screen-bot-bot_2').textContent).not.toContain('共享');

        fireEvent.click(screen.getByTestId('desktop-screen-bot-bot_2'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        expect(screen.queryByTestId('desktop-bot-handback')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const toggle = await screen.findByTestId('desktop-bot-isolated-toggle');
        fireEvent.click(toggle.querySelector('input') as HTMLInputElement);
        await waitFor(() => expect(setDesktopMode).toHaveBeenCalledWith('bot_1', 'isolated'));

        act(() => {
            responseListeners.byEvent['desktop-bot-view']?.({
                request_id: 'desktop-bot-login',
                session_key: 'alice:bot_2',
                novnc_url: isolated,
                desktop_handoff: true,
                attention_reason: '请登录 Google',
            });
        });
        const notice = await screen.findByTestId('desktop-bot-handoff-notice');
        expect(notice.textContent).toContain('请登录 Google');
        fireEvent.click(notice);
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        fireEvent.click(screen.getByTestId('desktop-bot-handback'));
        await waitFor(() => expect(handBack).toHaveBeenCalledWith('bot_2'));
    });

    it('says a failed hand-back in the persona instead of raw red text', async () => {
        handBack.mockRejectedValue(new Error('rpc error: code=500 instance offline'));
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: 'Bot B', description: '自己的桌面', instance_id: 'inst_2' },
        ]);
        const mine = 'http://hub.example/api/v1/desktop-handoff/mine/vnc.html?autoconnect=1';
        const isolated = 'http://hub.example/api/v1/desktop-handoff/bot-b/vnc.html?autoconnect=1';
        watchBot.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        getScreen.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        desktopAdmin.mockResolvedValue(true);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(await screen.findByTestId('desktop-bot-open-desktop'));
        expect(await screen.findByTestId('desktop-screen-switch')).toBeTruthy();
        expect(screen.getByTestId('desktop-screen-mine').textContent).toBe('我的桌面');
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_1'));
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_2'));
        expect(screen.getByTestId('desktop-screen-bot-bot_1').textContent).toContain('共享');
        expect(screen.getByTestId('desktop-screen-bot-bot_2').textContent).not.toContain('共享');

        fireEvent.click(screen.getByTestId('desktop-screen-bot-bot_2'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        expect(screen.queryByTestId('desktop-bot-handback')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const toggle = await screen.findByTestId('desktop-bot-isolated-toggle');
        fireEvent.click(toggle.querySelector('input') as HTMLInputElement);
        await waitFor(() => expect(setDesktopMode).toHaveBeenCalledWith('bot_1', 'isolated'));

        act(() => {
            responseListeners.byEvent['desktop-bot-view']?.({
                request_id: 'desktop-bot-login',
                session_key: 'alice:bot_2',
                novnc_url: isolated,
                desktop_handoff: true,
                attention_reason: '请登录 Google',
            });
        });
        const notice = await screen.findByTestId('desktop-bot-handoff-notice');
        expect(notice.textContent).toContain('请登录 Google');
        fireEvent.click(notice);
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        fireEvent.click(screen.getByTestId('desktop-bot-handback'));
        await waitFor(() => expect(handBack).toHaveBeenCalledWith('bot_2'));
        expect(await screen.findByText('桌面没有交还给 bot。')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-handback-error')).toBeNull();
        expect(screen.queryByText(/rpc error/)).toBeNull();
    });

    it('treats a hand-back Hub already runs as done, with no error', async () => {
        handBack.mockRejectedValue(new Error('rpc error: code=500'));
        listTaskRuns.mockResolvedValue([{ run_id: 'run_9', status: 'running', task: '登录完成，继续' }]);
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' },
            { id: 'bot_2', title: 'Bot B', description: '自己的桌面', instance_id: 'inst_2' },
        ]);
        const mine = 'http://hub.example/api/v1/desktop-handoff/mine/vnc.html?autoconnect=1';
        const isolated = 'http://hub.example/api/v1/desktop-handoff/bot-b/vnc.html?autoconnect=1';
        watchBot.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        getScreen.mockImplementation(async (botId?: string) => ({
            novnc_url: botId === 'bot_2' ? isolated : mine,
            user_control: false,
            desktop_mode: botId === 'bot_2' ? 'isolated' : 'shared',
        }));
        desktopAdmin.mockResolvedValue(true);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(await screen.findByTestId('desktop-bot-open-desktop'));
        expect(await screen.findByTestId('desktop-screen-switch')).toBeTruthy();
        expect(screen.getByTestId('desktop-screen-mine').textContent).toBe('我的桌面');
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_1'));
        await waitFor(() => expect(getScreen).toHaveBeenCalledWith('bot_2'));
        expect(screen.getByTestId('desktop-screen-bot-bot_1').textContent).toContain('共享');
        expect(screen.getByTestId('desktop-screen-bot-bot_2').textContent).not.toContain('共享');

        fireEvent.click(screen.getByTestId('desktop-screen-bot-bot_2'));
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        expect(screen.queryByTestId('desktop-bot-handback')).toBeNull();

        fireEvent.click(screen.getByTestId('desktop-bot-tab-details'));
        const toggle = await screen.findByTestId('desktop-bot-isolated-toggle');
        fireEvent.click(toggle.querySelector('input') as HTMLInputElement);
        await waitFor(() => expect(setDesktopMode).toHaveBeenCalledWith('bot_1', 'isolated'));

        act(() => {
            responseListeners.byEvent['desktop-bot-view']?.({
                request_id: 'desktop-bot-login',
                session_key: 'alice:bot_2',
                novnc_url: isolated,
                desktop_handoff: true,
                attention_reason: '请登录 Google',
            });
        });
        const notice = await screen.findByTestId('desktop-bot-handoff-notice');
        expect(notice.textContent).toContain('请登录 Google');
        fireEvent.click(notice);
        await waitFor(() => expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('bot-b'));
        fireEvent.click(screen.getByTestId('desktop-bot-handback'));
        await waitFor(() => expect(handBack).toHaveBeenCalledWith('bot_2'));
        await waitFor(() => expect(listTaskRuns).toHaveBeenCalled());
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
        expect(screen.queryByTestId('desktop-bot-handback-error')).toBeNull();
        expect(screen.queryByText('桌面没有交还给 bot。')).toBeNull();
    });
});

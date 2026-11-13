// @vitest-environment jsdom
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest';
import type { ReactElement } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DialogProvider } from '../CustomDialog';

// Loose signatures: these mocks are exercised with mockResolvedValue,
// mockImplementation, and promise-resolve captures across tests, so no single
// strict parameter list type-checks for all call sites.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const sendTask = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
const continueSessionSent = vi.hoisted(() => vi.fn<(session: string) => void>());
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const understandTask = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
const classifyWorkStop = vi.hoisted(() => vi.fn<(...args: any[]) => any>());
const armSchedule = vi.hoisted(() => vi.fn(async (_botId?: string, _name?: string, _rest?: unknown): Promise<string> => 'task-1'));
const listSchedules = vi.hoisted(() => vi.fn(async (_botId?: string) => [] as Array<{ id: string; name: string; action: string; interval_minutes: number; hour: number; minute: number; day_of_week: number; next_run_at?: string; status?: string }>));
const deleteSchedule = vi.hoisted(() => vi.fn(async (_botId?: string, _taskId?: string) => undefined));
const wakeBot = vi.hoisted(() => vi.fn(async (_botId?: string, _epoch?: number): Promise<{ novnc_url: string; user_control: boolean } | undefined> => ({ novnc_url: '', user_control: false })));
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
type HubDesktopBot = { id: string; title: string; description: string; instance_id: string; created_at?: string; primary?: boolean; color?: string; shape?: string };
const listBots = vi.hoisted(() => vi.fn(async (): Promise<HubDesktopBot[]> => []));
const createBot = vi.hoisted(() => vi.fn(async (name: string, description: string): Promise<HubDesktopBot> => ({ id: 'bot_1', title: name, description, instance_id: '' })));
const setAppearance = vi.hoisted(() => vi.fn(async (_botId?: string, _color?: string, _shape?: string): Promise<{ color?: string; shape?: string }> => ({})));
const renameBot = vi.hoisted(() => vi.fn(async (_id: string, _name: string, _description: string) => undefined));
const deleteBot = vi.hoisted(() => vi.fn(async (_id: string) => undefined));
const openFile = vi.hoisted(() => vi.fn(async (_path?: string) => undefined));
const showFile = vi.hoisted(() => vi.fn(async (_path?: string) => undefined));
const previewFile = vi.hoisted(() => vi.fn(async (_path?: string) => ({ preview_url: '' })));
const readShot = vi.hoisted(() => vi.fn(async (_path?: string) => ({ mime: 'image/png', data: 'iVBORw0KGgo=' })));
type BotLLMRole = { unused?: boolean; official?: boolean; name?: string; url?: string; key?: string; model?: string; protocol?: string; wire_api?: string };
type BotLLMView = { mode: string; name?: string; url?: string; model?: string; key_set: boolean; warning?: string; roles?: { foreground?: BotLLMRole; background?: BotLLMRole; computer_use?: BotLLMRole; grounder?: BotLLMRole } };
type BotLLMIn = { mode?: string; name?: string; url?: string; key?: string; model?: string; protocol?: string; wire_api?: string; roles?: BotLLMView['roles'] };
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
const evaluateSupplement = vi.hoisted(() => vi.fn(async (_taskText?: string, _content?: string, _lang?: string): Promise<{ is_supplement?: boolean; decision?: string; instruction?: string } | null> => null));
const appendSupplement = vi.hoisted(() => vi.fn(async (_botId?: string, _runId?: string, _instruction?: string): Promise<{ accepted?: boolean; stage?: string; message?: string; supplement_id?: string } | null> => null));
const getSupplements = vi.hoisted(() => vi.fn(async (_botId?: string, _runId?: string): Promise<Array<{ id: string; state: string; content?: string }>> => []));
const pullCross = vi.hoisted(() => vi.fn(async () => [] as Array<Record<string, unknown>>));
const postCross = vi.hoisted(() => vi.fn(async (..._args: unknown[]): Promise<{ posted: boolean; ask?: string; primary_line: string; names: string[]; notes: Array<Record<string, unknown>> }> => ({ posted: false, ask: '', primary_line: '', names: [], notes: [] })));
const setPrimary = vi.hoisted(() => vi.fn(async (_id?: string): Promise<HubDesktopBot[]> => []));
const claimCross = vi.hoisted(() => vi.fn(async (_botId?: string, _noteId?: string): Promise<{ claimed: boolean; id?: string; from: string }> => ({ claimed: false, from: '' })));
const armCross = vi.hoisted(() => vi.fn(async (_botId?: string, _noteId?: string) => undefined));
// 跟进路由（1b132e0d 起）：默认没有接管者，走正常发送链；测试可覆写。
const routeFollowUp = vi.hoisted(() => vi.fn(async (_botId?: string, _content?: string): Promise<{ routed: boolean; to_bot?: string; ask?: string } | null> => null));
const responseListeners = vi.hoisted(() => ({
    list: [] as Array<(payload: unknown) => void>,
    byEvent: {} as Record<string, (payload: unknown) => void>,
}));
// The GUI's own desktop watch reports the same display the live events
// describe, so every screen event keeps this in step for WatchDesktopBot.
const liveWatchView = vi.hoisted(() => ({
    current: { novnc_url: '', user_control: false, attention_reason: '' } as { novnc_url: string; user_control: boolean; attention_reason?: string; desktop_mode?: string },
}));
// Mirror screenOf's field mapping (desktopBotResultStore.ts).
const syncLiveWatchView = (payload: unknown) => {
    try {
        const data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown> | null;
        if (!data || typeof data !== 'object') return;
        const url = String(data.novnc_url || data.NovncURL || data.desktop_handoff_url || data.DesktopHandoffURL || '');
        const controlKeys = ['user_control', 'desktop_user_control', 'UserControl', 'DesktopUserControl', 'desktop_handoff', 'DesktopHandoff'];
        const hasControlKey = controlKeys.some(key => Object.prototype.hasOwnProperty.call(data, key));
        if (data.cleared === true || data.Cleared === true) {
            liveWatchView.current = { novnc_url: '', user_control: false };
        } else if (url || hasControlKey) {
            liveWatchView.current = {
                novnc_url: url,
                user_control: controlKeys.some(key => data[key] === true),
                attention_reason: String(data.attention_reason || data.desktop_attention_reason || data.AttentionReason || ''),
            };
        }
    } catch { /* malformed payloads still reach the handler */ }
};

vi.mock('../../../wailsjs/go/main/App', () => ({
    SendDesktopBotTask: (...args: unknown[]) => sendTask(...args),
    SendDesktopBotTaskTo: (botId: string, text: string, phase: string, continueSession?: string, askRun?: string) => {
        continueSessionSent(String(continueSession || ''));
        return askRun ? sendTask(botId, text, phase, askRun) : sendTask(botId, text, phase);
    },
    UnderstandDesktopBotTask: (content: string, phase: string, lang: string, earlier?: string[], persona?: string, catalog?: string) => understandTask(content, phase, lang, earlier || [], persona || '', catalog || ''),
    ClassifyDesktopWorkStop: (text: string, lang: string) => classifyWorkStop(text, lang),
    ArmDesktopBotSchedule: (...args: unknown[]) => armSchedule(...(args as [])) || 'task-1',
    ListDesktopBotSchedules: (botId: string) => listSchedules(botId),
    ListDesktopBotActiveRuns: (botId: string) => listTaskRuns(botId),
    DeleteDesktopBotSchedule: (botId: string, taskId: string) => deleteSchedule(botId, taskId),
    WatchDesktopBot: (arg: string, epoch?: number) => watchBot(arg, epoch),
    WakeDesktopBot: (arg: string, epoch?: number) => wakeBot(arg, epoch),
    ReleaseDesktopBotWatch: (arg: string, epoch?: number) => releaseWatch(arg, epoch),
    GetDesktopBotScreen: (botId: string) => getScreen(botId),
    HandBackDesktopBot: (botId: string) => handBack(botId),
    GetHubConnectionState: () => hubLink(),
    ReconnectHub: () => Promise.resolve({ state: 'connected' }),
    SetDesktopBotDesktopMode: (botId: string, mode: string) => setDesktopMode(botId, mode),
    DesktopBotDesktopAdmin: () => desktopAdmin(),
    SaveBotSecret: (name: string, value: string) => saveSecret(name, value),
    StageDesktopBotSecret: vi.fn(),
    RecallBotSecret: (name: string) => recallSecret(name),
    FillBotSecret: (botId: string, name: string) => fillSecret(botId, name),
    ListDesktopBots: () => listBots(),
    CreateDesktopBot: (name: string, description: string) => createBot(name, description),
    SetDesktopBotAppearance: (botId: string, color: string, shape: string) => setAppearance(botId, color, shape),
    GetDesktopBotStyle: vi.fn(),
    SaveDesktopBotStyle: vi.fn(),
    RollbackDesktopBotStyle: vi.fn(),
    RenameDesktopBot: (id: string, name: string, description: string) => renameBot(id, name, description),
    DeleteDesktopBot: (id: string) => deleteBot(id),
    OpenFileOrShowInFolder: (path: string) => openFile(path),
    ShowItemInFolder: (path: string) => showFile(path),
    PreviewTaskResultFile: (path: string) => previewFile(path),
    ReadDesktopBotShot: (path: string) => readShot(path),
    GetDesktopBotLLM: () => getBotLLM(),
    SyncDesktopBotLLM: () => getBotLLM(),
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
    FetchDesktopBotMailFile: async () => ({ name: '', mime: '', data: '' }),
    GetDesktopBotRunOutput: async () => ({ status: 'running', text: '', done: false }),
    ClaimDesktopBotCrossStart: (botId: string, noteId: string) => claimCross(botId, noteId),
    ArmDesktopBotCrossRun: (botId: string, noteId: string) => armCross(botId, noteId),
    RouteDesktopBotFollowUp: (botId: string, content: string) => routeFollowUp(botId, content),
    GetDesktopProxyMode: async () => ({ mode: 'smart', locked: false }),
    SaveDesktopProxyMode: async (mode: string) => ({ mode: mode || 'smart', locked: false }),
    GetDesktopComputer: async () => ({ restart_allowed: true, reset_allowed: true, active_runs: 0 }),
    ResetDesktopComputer: async () => ({ busy: false }),
    RestartDesktopComputer: async () => ({ busy: false }),
    LogFrontendDiagnostic: async () => undefined,
    UploadDesktopBotMaterialChunk: async (_botId: string, _uploadId: string, _name: string, _offset: number, total: number) => ({ received: total, done: true, path: '~/素材/bot_1/demo.png' }),
}));

const browserOpenURL = vi.hoisted(() => vi.fn());

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (url: string) => browserOpenURL(url),
    EventsOn: (event: string, handler: (payload: unknown) => void) => {
        const fn = event === 'desktop-bot-view' || event === 'desktop-bot-report' || event === 'ai-assistant-response'
            ? (payload: unknown) => {
                syncLiveWatchView(payload);
                handler(payload);
            }
            : handler;
        responseListeners.list.push(fn);
        responseListeners.byEvent[event] = fn;
        return () => {
            responseListeners.list = responseListeners.list.filter(item => item !== fn);
            if (responseListeners.byEvent[event] === fn) delete responseListeners.byEvent[event];
        };
    },
}));

import { DesktopBotWorkspace, setDesktopBotParallelRuns, formatCreated, handleDesktopBotReport as persistDesktopBotReport, handleDesktopBotResult as persistDesktopBotResult, handleDesktopBotSupplementState, handleDesktopBotView as persistDesktopBotView } from './DesktopBotWorkspace';

// Direct store-level calls describe the same desktop the GUI's own watch
// later reports, so they update liveWatchView exactly like the events do.
const handleDesktopBotView = (payload: Parameters<typeof persistDesktopBotView>[0]) => { syncLiveWatchView(payload); return persistDesktopBotView(payload); };
const handleDesktopBotReport = (payload: Parameters<typeof persistDesktopBotReport>[0]) => { syncLiveWatchView(payload); return persistDesktopBotReport(payload); };
const handleDesktopBotResult = (payload: Parameters<typeof persistDesktopBotResult>[0]) => { syncLiveWatchView(payload); return persistDesktopBotResult(payload); };
import { appearancesForUser, resetBotLookCacheForTest, saveBotAppearance, setBotLookLoaderForTest } from './botAppearance';
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

// 行内操作已收进 ⋯ 菜单：先点触发器，再点弹出菜单里的目标项。
function openBotRowMenu(botId: string) {
    fireEvent.click(screen.getByTestId(`desktop-bot-more-${botId}`));
}

function clickBotAction(botId: string, action: 'edit' | 'style' | 'delete') {
    openBotRowMenu(botId);
    // 点击会把菜单关掉，节点引用必须先抓住再点。
    const item = screen.getByTestId(`desktop-bot-${action}-${botId}`);
    fireEvent.click(item);
    return item;
}

beforeEach(() => {
    localStorage.clear();
    resetBotLookCacheForTest();
    setAppearance.mockReset();
    setAppearance.mockResolvedValue({});
    // Most cases check the noVNC URL; the H.264 player has its own case.
    localStorage.setItem('maclaw.desktop.viewer', 'vnc');
    sendTask.mockReset();
    continueSessionSent.mockReset();
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
    routeFollowUp.mockReset();
    routeFollowUp.mockResolvedValue(null);
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
    liveWatchView.current = { novnc_url: '', user_control: false };
    watchBot.mockImplementation(() => Promise.resolve({ ...liveWatchView.current }));
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

    it('forwards only explicit task picks and leaves routing to the server', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-thread',
                    role: 'assistant',
                    content: '报表在桌上',
                    pending: false,
                    conversation: 'bot_1#t-k-b',
                    conversationAt: Date.now(),
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-follow', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        // No local guess: a recent thread does not make the page pick it.
        expect(screen.queryByTestId('desktop-bot-continue')).toBeNull();
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '把日期也加上' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith(''));
        // The person picks the task: that choice is forwarded.
        continueSessionSent.mockClear();
        fireEvent.click(screen.getAllByTestId('desktop-bot-continue-thread')[0]);
        expect(await screen.findByTestId('desktop-bot-continue')).toBeTruthy();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再把金额加上' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith('bot_1#t-k-b'));
        // 改为新的事 is forwarded as main.
        continueSessionSent.mockClear();
        fireEvent.click(screen.getAllByTestId('desktop-bot-continue-thread')[0]);
        fireEvent.click(await screen.findByTestId('desktop-bot-continue-clear'));
        expect(screen.queryByTestId('desktop-bot-continue')).toBeNull();
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '另外一件事' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith('bot_1'));
    });

    it("shows the server's route on the person's line and undoes it as a new matter", async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-routed', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '把报表金额改成两位小数' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith(''));
        await waitFor(() => expect(responseListeners.byEvent['desktop-bot-routed']).toBeTruthy());
        act(() => {
            responseListeners.byEvent['desktop-bot-routed']?.({ bot_id: 'bot_1', request_id: 'desktop-bot-routed', session: 'bot_1#t-old', via: 'content', title: '整理上周订单报表' });
        });
        expect((await screen.findByTestId('desktop-bot-routed')).textContent).toContain('接着「整理上周订单报表」这件事');
        continueSessionSent.mockClear();
        fireEvent.click(screen.getByTestId('desktop-bot-routed-undo'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith('bot_1'));
        expect(screen.queryByTestId('desktop-bot-routed')).toBeNull();
    });

    it('sends a choice label as written, including 停下, in that turn phase', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-ask',
                    role: 'assistant',
                    content: '继续\n停下',
                    pending: false,
                    phase: 'plan',
                    runId: 'run-ask',
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
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '停下', '', 'run-ask'));
        expect(understandTask).not.toHaveBeenCalled();
        expect(cancelRun).not.toHaveBeenCalled();
    });

    it('sends an answer to a plan-phase question in execute', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-ask-plan',
                    role: 'assistant',
                    content: '要我现在启动桌面并继续吗',
                    pending: false,
                    phase: 'plan',
                    runId: 'run-wake',
                    askUser: { options: ['确认', '取消'], inputType: 'choice', question: '要我现在启动桌面并继续吗' },
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-wake', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const chat = await screen.findByTestId('desktop-bot-chat');
        fireEvent.click(within(chat).getByRole('button', { name: '确认' }));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '确认', '', 'run-wake'));
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

        clickBotAction('bot_1', 'edit');
        const editDialog = await screen.findByTestId('bot-edit-dialog');
        fireEvent.change(within(editDialog).getByLabelText('名称'), { target: { value: '值班' } });
        fireEvent.change(within(editDialog).getByLabelText('描述'), { target: { value: '晚上值守' } });
        fireEvent.click(within(editDialog).getByRole('button', { name: '保存' }));
        await waitFor(() => expect(screen.queryByTestId('bot-edit-dialog')).toBeNull());
        expect(await screen.findByText('晚上值守')).toBeTruthy();
        expect(list.textContent).toContain('值班');
        expect(list.textContent).toContain('晚上值守');

        const row = screen.getByText('晚上值守').closest('li');
        const botId = row?.getAttribute('data-testid')?.replace('desktop-bot-', '') ?? '';
        const remove = clickBotAction(botId, 'delete');
        expect(remove.textContent).toBe('删除');
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
        clickBotAction(botId, 'delete');
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        expect(screen.queryByText('晚上值守')).toBeNull();
        expect(screen.getByTestId('desktop-bot-pick')).toBeTruthy();
    });

    // The reading of a message is a helper for the turn. When the helper
    // cannot answer, the turn must survive it: the chat says what happened and
    // keeps the words, instead of claiming the backend agent is unavailable.



    // Timed work whose time was never named cannot be armed from here, and it
    // cannot be sent as work either — only this person knows when it should run.

    // The failure of the helper must not be confused with a failure of the
    // backend: this is the exact bubble the product used to show instead.



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

    it('shows a line drawing of a sleeping computer (no screen, no viewer chrome) when the desktop is asleep', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.click(await screen.findByTestId('desktop-bot-open-desktop'));
        const sleep = await screen.findByTestId('desktop-bot-sleep');
        expect(sleep.textContent || '').not.toContain('休眠中 Zzz');
        const zzz = screen.getByTestId('desktop-bot-wake');
        expect(zzz.getAttribute('aria-label')).toContain('休眠');
        const art = sleep.querySelector('.desktop-bot-sleep__art');
        expect(art).toBeTruthy();
        expect(art?.querySelector('[pathLength]')).toBeNull();
        expect(sleep.querySelectorAll('.desktop-bot-sleep__z').length).toBe(3);
        expect(sleep.querySelector('.desktop-bot-sleep__breath')).toBeTruthy();
        const bezel = art?.querySelector('rect');
        const bezelRight = Number(bezel?.getAttribute('x')) + Number(bezel?.getAttribute('width'));
        const z1 = Number((art?.querySelector('.desktop-bot-sleep__z--1 path')?.getAttribute('d') || '').match(/M(-?\d+(?:\.\d+)?)/)?.[1]);
        expect(z1).toBeGreaterThan(bezelRight);
        const deck = art?.querySelector('.desktop-bot-sleep__deck')?.getAttribute('d') || '';
        const deckBox = deck.match(/M(-?\d+(?:\.\d+)?) [\d.]+H(-?\d+(?:\.\d+)?)L(-?\d+(?:\.\d+)?) [\d.]+H(-?\d+(?:\.\d+)?)/);
        expect(deckBox).toBeTruthy();
        const topWidth = Number(deckBox?.[2]) - Number(deckBox?.[1]);
        const bottomWidth = Number(deckBox?.[3]) - Number(deckBox?.[4]);
        expect(bottomWidth).toBeGreaterThan(topWidth);
        const fromFrontend = join(process.cwd(), 'src/components/bots/DesktopBotWorkspace.css');
        const cssPath = existsSync(fromFrontend) ? fromFrontend : join(process.cwd(), 'guiapp/frontend/src/components/bots/DesktopBotWorkspace.css');
        const css = readFileSync(cssPath, 'utf8');
        expect(css).toMatch(/@keyframes desktop-bot-sleep-draw-52/);
        expect(css).toMatch(/@keyframes desktop-bot-sleep-draw-39/);
        expect(css).toMatch(/@keyframes desktop-bot-sleep-draw-26/);
        expect(css).toMatch(/@keyframes desktop-bot-sleep-float/);
        expect(css).toMatch(/@keyframes desktop-bot-sleep-breath/);
        expect(css).toMatch(/stroke-dashoffset:\s*52/);
        expect(css).toMatch(/stroke-dashoffset:\s*39/);
        expect(css).toMatch(/stroke-dashoffset:\s*26/);
        expect(css).toMatch(/stroke-dashoffset:\s*68/);
        expect(css).toMatch(/aspect-ratio:\s*200\s*\/\s*128/);
        expect(css).not.toMatch(/desktop-bot-sleep__z path\s*\{[^}]*stroke-dasharray:\s*1\s*;/);
        expect(css).toMatch(/prefers-reduced-motion:\s*reduce\)\s*\{[^@]*?\.desktop-bot-sleep__z--1 path[\s\S]*?animation:\s*none/);
        expect(css).toMatch(/prefers-reduced-motion:\s*reduce\)\s*\{[^@]*?\.desktop-bot-sleep__z--1 path[\s\S]*?stroke-dashoffset:\s*0/);
        expect(sleep.querySelectorAll('button').length).toBe(1);
        expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-watch-only')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-take-control')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-screen')).toBeNull();
        wakeBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        watchBot.mockResolvedValue({ novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1', user_control: false });
        fireEvent.click(zzz);
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        expect(wakeBot).toHaveBeenCalledWith('bot_1', expect.any(Number));
        expect(screen.queryByTestId('desktop-bot-sleep')).toBeNull();
    });

    it('follows the real desktop state on an open 电脑 panel: asleep, live while the bot drives it, asleep again, without waking', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
        wakeBot.mockClear();
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await vi.advanceTimersByTimeAsync(10); });
            expect(screen.getByTestId('desktop-bot-sleep')).toBeTruthy();
            // A bot step starts using the desktop: the next read-only poll shows it.
            const live = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
            watchBot.mockResolvedValue({ novnc_url: live, user_control: false });
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-sleep')).toBeNull();
            // The bot moved on: back to the sleeping drawing, no stale picture.
            watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-sleep')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
            // Watching never asked for a wake.
            expect(wakeBot).not.toHaveBeenCalled();
        } finally {
            vi.useRealTimers();
        }
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
        expect(stage.textContent).toContain('正在观看桌面。');
        expect(screen.queryByTestId('desktop-bot-exit-takeover')).toBeNull();

        fireEvent.click(expand);
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('正在观看桌面。');
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


    it('clears local history when a bot is deleted', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-1', role: 'user', content: '旧任务' }] },
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const row = screen.getByTestId('desktop-bot-bot_1');
        expect(row?.querySelector('.desktop-bot-workspace__more')).toBeTruthy();
        const removeButton = clickBotAction('bot_1', 'delete');
        expect(removeButton.textContent).toBe('删除');
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
        // 快速重复点删除项也只弹一个确认框（removeLock 顶住）。
        clickBotAction('bot_1', 'delete');
        const remove = clickBotAction('bot_1', 'delete');
        expect(await screen.findAllByRole('dialog')).toHaveLength(1);
        expect(remove.textContent).toBe('删除');
        expect(remove.querySelector('svg')).toBeTruthy();
        expect(deleteBot).not.toHaveBeenCalled();
        await confirmBotDelete();
        expect(deleteBot).toHaveBeenCalledTimes(1);
        clickBotAction('bot_1', 'delete');
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
        clickBotAction('bot_1', 'delete');
        await confirmBotDelete();
        const alert = await screen.findByRole('dialog');
        expect(alert.textContent).toContain('无法删除 Bot');
        expect(alert.textContent).toContain('MaClawSrv 不可达');
        fireEvent.click(within(alert).getByRole('button', { name: /OK|确定/ }));
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.getByTestId('desktop-bot-bot_1')).toBeTruthy();
        deleteBot.mockResolvedValueOnce(undefined);
        clickBotAction('bot_1', 'delete');
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        expect(deleteBot).toHaveBeenCalledTimes(2);
    });

    it('warns that deleting a bot will stop its running task', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        listTaskRuns.mockResolvedValue([{ run_id: 'run_1', status: 'running', task: '训练模型' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        clickBotAction('bot_1', 'delete');
        const dialog = await screen.findByRole('dialog');
        expect(dialog.textContent).toContain('确定删除 Bot');
        expect(dialog.textContent).toContain('值班');
        expect(dialog.textContent).toContain('正在运行的任务');
        expect(dialog.textContent).toContain('删除将停止该任务');
        fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        expect(deleteBot).not.toHaveBeenCalled();
        expect(screen.getByTestId('desktop-bot-bot_1')).toBeTruthy();
    });

    it('explains a running-task delete failure in an alert and keeps the bot', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        deleteBot.mockRejectedValueOnce(new Error('this bot has a running task'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        clickBotAction('bot_1', 'delete');
        await confirmBotDelete();
        const alert = await screen.findByRole('dialog');
        expect(alert.textContent).toContain('无法删除 Bot');
        expect(alert.textContent).toContain('有正在运行的任务');
        expect(alert.textContent).toContain('没能停下来');
        fireEvent.click(within(alert).getByRole('button', { name: /OK|确定/ }));
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toContain('有正在运行的任务');
        expect(screen.getByTestId('desktop-bot-bot_1')).toBeTruthy();
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
        clickBotAction('bot_1', 'delete');
        await confirmBotDelete();
        expect(deleteBot).toHaveBeenCalledWith('bot_1');
        view.unmount();
        await act(async () => { release(); });
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, unknown[]>>;
        expect((stored.alice?.bot_1 || []).length).toBe(0);
    });


    it('shows the Hub error when the bot list cannot be loaded', async () => {
        listBots.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.queryByTestId('desktop-bot-empty')).toBeNull();
    });

    it('offers row actions from the ⋯ menu and closes on Escape or outside pointer', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '值班', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('Bot 1')).toBeTruthy();
        // 收起时行内没有零散的操作按钮，只有一枚 ⋯。
        expect(screen.queryByTestId('desktop-bot-edit-bot_1')).toBeNull();
        const more = screen.getByTestId('desktop-bot-more-bot_1');
        expect(more.getAttribute('aria-haspopup')).toBe('menu');
        expect(more.getAttribute('aria-expanded')).toBe('false');
        expect(more.getAttribute('aria-label')).toBe('更多操作');
        expect(more.getAttribute('title')).toBe('更多操作');
        fireEvent.click(more);
        expect(more.getAttribute('aria-expanded')).toBe('true');
        const menu = screen.getByRole('menu');
        expect(within(menu).getByTestId('desktop-bot-edit-bot_1').textContent).toBe('编辑信息');
        expect(within(menu).getByTestId('desktop-bot-style-bot_1').textContent).toBe('性格与说话风格');
        expect(within(menu).getByTestId('desktop-bot-delete-bot_1').textContent).toBe('删除');

        // Escape 关闭，并把焦点还给触发器。
        fireEvent.keyDown(menu, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
        expect(document.activeElement).toBe(more);

        // 点击菜单之外（document pointerdown）也会关闭。
        openBotRowMenu('bot_1');
        expect(screen.getByRole('menu')).toBeTruthy();
        fireEvent.pointerDown(document.body);
        await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
    });

    it('keeps only one row menu open at a time', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' },
            { id: 'bot_2', title: '日报', description: '早上', instance_id: 'inst_2' },
        ]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        openBotRowMenu('bot_1');
        expect(screen.getAllByRole('menu')).toHaveLength(1);
        // 真实浏览器里 pointerdown 先关上一份菜单，再点开下一行。
        fireEvent.pointerDown(screen.getByTestId('desktop-bot-more-bot_2'));
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_2'));
        expect(screen.getAllByRole('menu')).toHaveLength(1);
        expect(screen.getByTestId('desktop-bot-delete-bot_2')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-delete-bot_1')).toBeNull();

        // 键盘 Enter 激活不经过 pointerdown：父组件抓手互斥，仍只留一份。
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
        expect(screen.getAllByRole('menu')).toHaveLength(1);
        expect(screen.getByTestId('desktop-bot-delete-bot_1')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-delete-bot_2')).toBeNull();
    });

    it('keeps the bot when renaming fails', async () => {
        listBots.mockResolvedValueOnce([{ id: 'bot_1', title: 'Bot 1', description: '值班', instance_id: 'inst_1' }]);
        renameBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('Bot 1')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
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
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
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
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
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

    it('clears the busy row whichever path delivers the final reply', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-final',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '做两页 PDF' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('做两页 PDF')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        expect(await screen.findByTestId('desktop-bot-processing')).toBeTruthy();
        // The result reaches the transcript through the store (as a timer pull
        // or a restart recovery does), not through this turn's live claim.
        handleDesktopBotResult({
            request_id: 'desktop-bot-final',
            session_key: 'alice:bot_1',
            text: '两页 PDF 做好了',
            send_to_user: '两页 PDF 做好了',
        });
        act(() => { window.dispatchEvent(new CustomEvent('maclaw-bot-synced', { detail: { userId: 'alice', botId: 'bot_1' } })); });
        expect(await screen.findByText('两页 PDF 做好了')).toBeTruthy();
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-processing')).toBeNull());
    });

    it('splits a reply into beats and corrects an attachment that never arrived', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-beats',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '把天气 PDF 发给我' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('把天气 PDF 发给我')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-beats',
            session_key: 'alice:bot_1',
            text: '天气 PDF 做好了。\n---\n两页 PDF 就附在这条消息里。',
            send_to_user: '天气 PDF 做好了。\n---\n两页 PDF 就附在这条消息里。',
        });
        const beat = await screen.findByTestId('desktop-bot-beat');
        expect(beat.textContent).toBe('天气 PDF 做好了。');
        expect(await screen.findByText(/更正：这条消息实际没有收到附件/)).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-log').textContent).not.toContain('---');
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




    // A parked sentence is still the person's order. While another job is
    // running, choosing their own words starts that order beside it.


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




    it('does not send the stock confirm sentence, and lets the server choose the phase for a typed resume', async () => {
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
            expect(sendTask).toHaveBeenLastCalledWith('bot_1', sentence, '');
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
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('有空');
    });

    it('shows a retry on the computer card when the desktop picture fails', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        watchBot.mockRejectedValue(new Error('desktop picture unavailable'));
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            expect(screen.getByTestId('desktop-bot-stage-loading')).toBeTruthy();
            expect(screen.getByText('值班 的屏幕')).toBeTruthy();
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            const wake = screen.getByTestId('desktop-bot-wake');
            expect(wake).toBeTruthy();
            expect(screen.getByTestId('desktop-bot-panel').contains(wake)).toBe(true);
            expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
            expect(screen.queryByTestId('desktop-bot-stage')).toBeNull();
            const sends = sendTask.mock.calls.length;
            const epoch = watchBot.mock.calls[0][1];
            const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
            wakeBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
            fireEvent.click(wake);
            await act(async () => { await Promise.resolve(); });
            expect(sendTask.mock.calls.length).toBe(sends);
            expect(wakeBot).toHaveBeenLastCalledWith('bot_1', epoch);
            expect(releaseWatch).not.toHaveBeenCalled();
            await act(async () => { await vi.advanceTimersByTimeAsync(0); });
            expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-wake')).toBeNull();
            expect(sendTask.mock.calls.length).toBe(sends);
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
        watchBot.mockRejectedValue(new Error('watch failed'));
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain(cached);
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-wake')).toBeTruthy();
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
        watchBot.mockRejectedValue(new Error('desktop picture unavailable'));
        vi.useFakeTimers();
        try {
            renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
            await act(async () => { await Promise.resolve(); });
            fireEvent.click(screen.getByText('值班'));
            fireEvent.click(screen.getByTestId('desktop-bot-open-desktop'));
            await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
            expect(screen.getByTestId('desktop-bot-wake')).toBeTruthy();
            let resolveWake: (value: { novnc_url: string; user_control: boolean }) => void = () => {};
            wakeBot.mockImplementation(() => new Promise(resolve => { resolveWake = resolve; }));
            fireEvent.click(screen.getByTestId('desktop-bot-wake'));
            fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
            await act(async () => { await Promise.resolve(); });
            resolveWake({ novnc_url: liveDesktop, user_control: false });
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


    it('sends a stored look again when the first copy does not stick', async () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        let calls = 0;
        setAppearance.mockImplementation(() => {
            calls += 1;
            if (calls === 1) return Promise.reject(new Error('offline'));
            return Promise.resolve({ id: 'bot_1', color: '#2563eb', shape: 'cloud' });
        });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_1', '#2563eb', 'cloud');
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
    });

    it('keeps a newer look when an older copy comes back', async () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        let calls = 0;
        let releaseFirst: (value: { color: string; shape: string }) => void = () => {};
        setAppearance.mockImplementation(() => {
            calls += 1;
            if (calls === 1) return new Promise(resolve => { releaseFirst = resolve; });
            return Promise.resolve({});
        });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(1));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_1', '#2563eb', 'cloud');

        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        fireEvent.click(screen.getByTestId('bot-edit-color-16a34a'));
        fireEvent.click(screen.getByTestId('bot-edit-shape-leaf'));
        fireEvent.click(within(screen.getByTestId('bot-edit-dialog')).getByRole('button', { name: '保存' }));
        await waitFor(() => {
            const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
            expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
            expect(avatar?.getAttribute('data-shape')).toBe('leaf');
        });

        await act(async () => { releaseFirst({ color: '#2563eb', shape: 'cloud' }); });
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_1', '#16a34a', 'leaf');
        const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
        expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
        expect(avatar?.getAttribute('data-shape')).toBe('leaf');
        expect(appearancesForUser('alice').bot_1).toEqual({ color: '#16a34a', shape: 'leaf' });
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
    });

    it('sends a later look after the copied one was accepted', async () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        setAppearance.mockImplementation(async (_botId?: string, color?: string, shape?: string) => ({ color, shape }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(1));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_1', '#2563eb', 'cloud');

        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        fireEvent.click(screen.getByTestId('bot-edit-color-16a34a'));
        fireEvent.click(screen.getByTestId('bot-edit-shape-leaf'));
        fireEvent.click(within(screen.getByTestId('bot-edit-dialog')).getByRole('button', { name: '保存' }));
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_1', '#16a34a', 'leaf');
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
        const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
        expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
        expect(avatar?.getAttribute('data-shape')).toBe('leaf');
    });

    it('sends a new bot look that the hub list has not returned, then the color chosen while that copy is out', async () => {
        listBots.mockResolvedValue([]);
        let releaseFirst: (value: { color?: string; shape?: string }) => void = () => {};
        setAppearance.mockImplementation(() => new Promise(resolve => { releaseFirst = resolve; }));
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: '2023-11-04T16:45:00Z',
        }));
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        const dialog = await screen.findByTestId('bot-create-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-edit-color-2563eb'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-shape-cloud'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(1));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_new', '#2563eb', 'cloud');

        setAppearance.mockImplementation(async (_botId?: string, color?: string, shape?: string) => ({ color, shape }));
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_new'));
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_new'));
        fireEvent.click(screen.getByTestId('bot-edit-color-16a34a'));
        fireEvent.click(screen.getByTestId('bot-edit-shape-leaf'));
        fireEvent.click(within(screen.getByTestId('bot-edit-dialog')).getByRole('button', { name: '保存' }));
        await waitFor(() => {
            const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
            expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
        });
        await act(async () => { releaseFirst({ color: '#2563eb', shape: 'cloud' }); });
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        expect(setAppearance).toHaveBeenLastCalledWith('bot_new', '#16a34a', 'leaf');
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
        expect(view.container.querySelector('.desktop-bot-workspace__avatar')?.getAttribute('data-color')).toBe('#16a34a');
        expect(appearancesForUser('alice').bot_new).toEqual({ color: '#16a34a', shape: 'leaf' });
    });

    it('does not send a look the hub already returned on create', async () => {
        let resolveList: (bots: { id: string; title: string; description: string; instance_id: string }[]) => void = () => {};
        listBots.mockReturnValue(new Promise(resolve => { resolveList = resolve; }));
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: '2023-11-04T16:45:00Z',
            color: '#16a34a',
            shape: 'leaf',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        const dialog = await screen.findByTestId('bot-create-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-edit-color-16a34a'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-shape-leaf'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        await act(async () => {
            resolveList([{ id: 'bot_new', title: 'Bot 1', description: '远程电脑上的7x24小时数字助理。', instance_id: 'inst_new' }]);
        });
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).not.toHaveBeenCalled();
    });

    it('sends a new bot look once more when the first copy fails, then stops', async () => {
        listBots.mockResolvedValue([]);
        let calls = 0;
        setAppearance.mockImplementation(() => {
            calls += 1;
            if (calls === 1) return Promise.reject(new Error('offline'));
            if (calls === 2) return Promise.resolve({ color: '#2563eb', shape: 'cloud' });
            return Promise.reject(new Error('offline'));
        });
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: '2023-11-04T16:45:00Z',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        const dialog = await screen.findByTestId('bot-create-dialog');
        fireEvent.click(within(dialog).getByTestId('bot-edit-color-2563eb'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-shape-cloud'));
        fireEvent.click(within(dialog).getByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        expect(setAppearance).toHaveBeenNthCalledWith(1, 'bot_new', '#2563eb', 'cloud');
        expect(setAppearance).toHaveBeenNthCalledWith(2, 'bot_new', '#2563eb', 'cloud');
        await act(async () => { await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
    });

    it('does not keep sending a new bot look after two copies fail', async () => {
        listBots.mockResolvedValue([]);
        setAppearance.mockRejectedValue(new Error('offline'));
        createBot.mockImplementation(async (name: string, description: string) => ({
            id: 'bot_new',
            title: name,
            description,
            instance_id: 'inst_new',
            created_at: '2023-11-04T16:45:00Z',
        }));
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(2));
        await act(async () => { await Promise.resolve(); await Promise.resolve(); });
        expect(setAppearance).toHaveBeenCalledTimes(2);
    });

    it('does not restore a look cleared while its copy was in flight', async () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        let calls = 0;
        let releaseFirst: (value: { color: string; shape: string }) => void = () => {};
        setAppearance.mockImplementation(() => {
            calls += 1;
            if (calls === 1) return new Promise(resolve => { releaseFirst = resolve; });
            return Promise.resolve({ color: '#2563eb', shape: 'cloud' });
        });
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        await waitFor(() => expect(setAppearance).toHaveBeenCalledTimes(1));
        clickBotAction('bot_1', 'delete');
        await confirmBotDelete();
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        await act(async () => { releaseFirst({ color: '#2563eb', shape: 'cloud' }); });
        await act(async () => { await Promise.resolve(); });
        expect(appearancesForUser('alice').bot_1).toBeUndefined();
        expect(setAppearance).toHaveBeenCalledTimes(1);
    });

    it('keeps an edited badge when the stored looks arrive afterwards', async () => {
        let releaseHydrate: (file: { looks: Record<string, { color: string; shape: string }>; migrated: string[] }) => void = () => {};
        setBotLookLoaderForTest(() => new Promise(resolve => { releaseHydrate = resolve; }));
        listBots.mockResolvedValue([{
            id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1', color: '#2563eb', shape: 'cloud',
        }]);
        const view = renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('值班')).toBeTruthy();
        await waitFor(() => {
            expect(view.container.querySelector('.desktop-bot-workspace__avatar')?.getAttribute('data-color')).toBe('#2563eb');
        });
        expect(appearancesForUser('alice').bot_1).toEqual({ color: '#2563eb', shape: 'cloud' });
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
        fireEvent.click(screen.getByTestId('desktop-bot-edit-bot_1'));
        fireEvent.click(screen.getByTestId('bot-edit-color-16a34a'));
        fireEvent.click(screen.getByTestId('bot-edit-shape-leaf'));
        fireEvent.click(within(screen.getByTestId('bot-edit-dialog')).getByRole('button', { name: '保存' }));
        await waitFor(() => {
            const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
            expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
            expect(avatar?.getAttribute('data-shape')).toBe('leaf');
        });
        await act(async () => {
            releaseHydrate({ looks: { bot_1: { color: '#2563eb', shape: 'cloud' } }, migrated: [] });
        });
        await act(async () => { await Promise.resolve(); });
        const avatar = view.container.querySelector('.desktop-bot-workspace__avatar');
        expect(avatar?.getAttribute('data-color')).toBe('#16a34a');
        expect(avatar?.getAttribute('data-shape')).toBe('leaf');
        expect(appearancesForUser('alice').bot_1).toEqual({ color: '#16a34a', shape: 'leaf' });
        expect(setAppearance).not.toHaveBeenCalled();
    });

    it('creates a bot when the browser appearance store is over quota', async () => {
        const proto = Object.getPrototypeOf(window.localStorage) as Storage;
        const setItem = proto.setItem;
        proto.setItem = function (key: string, value: string) {
            if (key === 'maclaw.desktopBotAppearance.v1' || key === 'maclaw.desktopBotAppearance.v1.migrated') {
                throw new DOMException("Failed to execute 'setItem' on 'Storage': Setting the value of '" + key + "' exceeded the quota.", 'QuotaExceededError');
            }
            return setItem.call(this, key, value);
        };
        try {
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
            expect(await screen.findByTestId('desktop-bot-bot_new')).toBeTruthy();
            expect(screen.queryByTestId('desktop-bot-list-error')).toBeNull();
            expect(localStorage.getItem('maclaw.desktopBotAppearance.v1')).toBeNull();
        } finally {
            proto.setItem = setItem;
        }
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
        expect(css).toMatch(/\.desktop-bot-workspace__row-menu\s*\{[^}]*position:\s*fixed/);
        expect(css).toMatch(/\.desktop-bot-workspace__more\s*\{[^}]*visibility:\s*hidden/);
        expect(css).toMatch(/prefers-reduced-motion:\s*reduce\)\s*\{[^@]*?\.desktop-bot-workspace__more\s*\{[^}]*transition:\s*none/);
        expect(css).toMatch(/\.desktop-bot-workspace__more\s*\{[^}]*margin:\s*0 6px 0 0/);
        expect(css).toMatch(/\.desktop-bot-workspace__row\.is-selected \.desktop-bot-workspace__more[^{]*\{[^}]*visibility:\s*visible/);
        expect(css).toMatch(/\.desktop-bot-workspace__row-menuitem\.is-danger\s*\{[^}]*var\(--theme-danger/);
        expect(css).toMatch(/\.bot-edit-dialog__input\s*\{[^}]*border-radius:\s*8px/);
        expect(css).toMatch(/\.bot-edit-dialog__notice\s*\{[^}]*var\(--theme-danger/);
        expect(css).toMatch(/\.desktop-bot-workspace__more\s*\{[^}]*width:\s*30px/);
        expect(css).toMatch(/\.desktop-bot-workspace__more\s*\{[^}]*height:\s*30px/);
        expect(css).toMatch(/\.desktop-bot-workspace__more\s*\{[^}]*padding:\s*0/);
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
        expect(within(dialog).getByTestId('bot-role-foreground-model')).toHaveProperty('value', 'qwen-max');
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        const qwen = {
            name: 'Qwen',
            url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
            key: 'sk-qwen',
            model: 'qwen-max',
            protocol: 'openai',
            wire_api: '',
        };
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith({
            mode: 'custom',
            ...qwen,
            roles: {
                foreground: qwen,
                background: qwen,
                computer_use: qwen,
                grounder: { unused: true },
            },
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
        const model = within(dialog).getByTestId('bot-role-foreground-model') as HTMLSelectElement;
        expect(model.tagName).toBe('SELECT');
        // Saved model first, then the live catalog, cached models merged in.
        await waitFor(() => expect(Array.from(model.options).map(o => o.value)).toEqual(['qwen-turbo', 'qwen3-max', 'qwen-plus']));
        fireEvent.change(model, { target: { value: 'qwen3-max' } });
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith(expect.objectContaining({
            mode: 'custom',
            model: 'qwen3-max',
            roles: expect.objectContaining({
                foreground: expect.objectContaining({ model: 'qwen3-max' }),
                background: expect.objectContaining({ model: 'qwen-turbo' }),
            }),
        })));
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
        fireEvent.click(within(dialog).getByTestId('bot-llm-mode-custom'));
        const model = within(dialog).getByTestId('bot-role-foreground-model') as HTMLSelectElement;
        expect(Array.from(model.options).map(o => o.value)).toEqual(['qwen-max', 'qwen-plus']);
        expect(model).toHaveProperty('value', 'qwen-max');
        expect(within(dialog).getAllByText('正在获取模型列表…').length).toBeGreaterThan(0);
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
        const provider = within(dialog).getByTestId('bot-role-foreground-provider') as HTMLSelectElement;
        const model = within(dialog).getByTestId('bot-role-foreground-model') as HTMLSelectElement;
        expect(model).toHaveProperty('value', 'qwen-max');
        fireEvent.change(provider, { target: { value: 'DeepSeek' } });
        expect(model).toHaveProperty('value', 'deepseek-chat');
        expect(within(dialog).getByTestId('bot-role-background-model')).toHaveProperty('value', 'qwen-max');
        fireEvent.click(within(dialog).getByTestId('bot-llm-save'));
        await waitFor(() => expect(saveBotLLM).toHaveBeenCalledWith(expect.objectContaining({
            mode: 'custom',
            name: 'DeepSeek',
            model: 'deepseek-chat',
            roles: expect.objectContaining({
                foreground: expect.objectContaining({ name: 'DeepSeek', model: 'deepseek-chat' }),
                background: expect.objectContaining({ name: 'Qwen', model: 'qwen-max' }),
            }),
        })));
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
        expect((await within(dialog).findAllByText('这个服务商暂时没有可选模型。')).length).toBeGreaterThan(0);
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
        const model = within(dialog).getByTestId('bot-role-foreground-model') as HTMLSelectElement;
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
        expect(within(dialog).getByTestId('bot-role-foreground-model')).toHaveProperty('value', 'qwen-max');
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
        // The bot's own line stays; only the outcome is marked.
        expect(sup?.content).toBe('已收到，会在当前任务的下一步生效。');
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


    it('starts a second job beside the one already running when the bot is under its cap', async () => {
        listBotTasks.mockResolvedValue([{ run_id: 'run-1', bot_id: 'bot_1', status: 'running', task: '早报' }]);
        sendTask.mockResolvedValue({ request_id: 'req-parallel', deferred: true, session_key: 'alice:bot_1' });
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        fireEvent.click(await screen.findByTestId('bot-edit-save'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();
        act(() => {
            responseListeners.byEvent['desktop-bot-busy']?.({ bot_id: 'bot_1', owner_id: 'alice', run_driving: true });
        });
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        expect(screen.queryByText('好，这个等手上的做完我就做。')).toBeNull();
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
        openBotRowMenu('bot_1');
        expect(screen.getByTestId('desktop-bot-edit-bot_1')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
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



    it('starts a destructive handoff without a confirm button, only after that bot claims it', async () => {
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
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_6', '安装一个pdf阅读器', 'execute'));
        expect(claimCross).toHaveBeenCalledWith('bot_6', 'n1');
        expect(armCross).toHaveBeenCalledWith('bot_6', 'n1');
        expect(claimCross.mock.invocationCallOrder[0]).toBeLessThan(armCross.mock.invocationCallOrder[0]);
        expect(armCross.mock.invocationCallOrder[0]).toBeLessThan(sendTask.mock.invocationCallOrder[0]);
    });

    it('renders the server confirm field and does not re-decide an install', async () => {
        listBots.mockResolvedValue([
            { id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1', primary: true },
            { id: 'bot_6', title: 'Bot 6', description: '记录', instance_id: 'inst_6', primary: false },
        ]);
        pullCross.mockResolvedValue([
            { id: 'n-none', from: 'bot_1', target: 'bot_6', kind: 'delegate', text: '装一下', order: '安装一个pdf阅读器', confirm: 'none' },
        ]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        expect(await screen.findByText('交给 Bot 6 了，他现在去做。')).toBeTruthy();
        expect(screen.queryByText('交给 Bot 6 了。装软件还是得你在他那边点一下。')).toBeNull();
        expect(sendTask).not.toHaveBeenCalled();
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
        await waitFor(() => expect(claimCross).toHaveBeenCalledWith('bot_6', 'n1'));
        expect(armCross).not.toHaveBeenCalled();
        expect(sendTask).not.toHaveBeenCalled();
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
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-1', deferred: true, session_key: 'alice:bot_1' });
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
        expect(within(botRow).getByTestId('desktop-bot-more-bot_1').querySelector('svg')).toBeTruthy();
        openBotRowMenu('bot_1');
        const rowActionMenu = screen.getByRole('menu');
        expect(within(rowActionMenu).getByTestId('desktop-bot-edit-bot_1').querySelector('svg')).toBeTruthy();
        expect(within(rowActionMenu).getByTestId('desktop-bot-delete-bot_1').querySelector('svg')).toBeTruthy();
        fireEvent.click(screen.getByTestId('desktop-bot-more-bot_1'));
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
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '看一下', ''));

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

});

describe('the page forwards words and renders the server decision', () => {
    async function openDuty() {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        renderBots(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        await screen.findByTestId('desktop-bot-command');
    }

    async function typeAndSend(text: string) {
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: text } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
    }

    function intake(payload: Record<string, unknown>) {
        act(() => {
            responseListeners.byEvent['desktop-bot-intake']?.(payload);
        });
    }

    function result(payload: Record<string, unknown>) {
        act(() => {
            responseListeners.byEvent['ai-assistant-response']?.(payload);
        });
    }

    it('forwards the typed words unchanged and shows the server ack, then the result', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-ack', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('打开示例网站');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '打开示例网站', ''));
        expect(understandTask).not.toHaveBeenCalled();
        expect(classifyWorkStop).not.toHaveBeenCalled();
        expect(evaluateSupplement).not.toHaveBeenCalled();
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-ack', kind: 'new', ack: '我去打开示例网站。', queued: false, held: false });
        expect(await screen.findByText('我去打开示例网站。')).toBeTruthy();
        result({ request_id: 'desktop-bot-ack', session_key: 'alice:bot_1', text: '网站开好了', send_to_user: '网站开好了' });
        expect(await screen.findByText('网站开好了')).toBeTruthy();
    });

    it('forwards a second command while the first is still pending', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-1', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('先做这个');
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(1));
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-2', deferred: true, session_key: 'alice:bot_1' });
        await typeAndSend('再做那个');
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenNthCalledWith(1, 'bot_1', '先做这个', '');
        expect(sendTask).toHaveBeenNthCalledWith(2, 'bot_1', '再做那个', '');
        expect(screen.queryByText(/等手头这件做完/)).toBeNull();
    });

    it('shows a parked line when the server queues the message', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-q', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('再开一个');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '再开一个', ''));
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-q', kind: 'new', ack: '先记着。', queued: true, held: false });
        expect(await screen.findByText('先记着。')).toBeTruthy();
        expect(await screen.findByText('好，这个等手上的做完我就做。')).toBeTruthy();
    });

    it('shows a parked line when the server holds the message for the keyboard', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-h', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('下一件');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '下一件', ''));
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-h', kind: 'new', ack: '等你交还。', queued: true, held: true });
        expect(await screen.findByText('等你交还。')).toBeTruthy();
        expect(await screen.findByText('好，这个等手上的做完我就做。')).toBeTruthy();
        expect(cancelRun).not.toHaveBeenCalled();
    });

    it('shows a supplement ack and does not fold the run itself', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-sup', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('只要前五条');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '只要前五条', ''));
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-sup', kind: 'supplement', ack: '好，只留前五条。', queued: false, held: false, run_id: 'run_a' });
        expect(await screen.findByText('好，只留前五条。')).toBeTruthy();
        expect(appendSupplement).not.toHaveBeenCalled();
        expect(evaluateSupplement).not.toHaveBeenCalled();
    });

    it('forwards a stop sentence and leaves cancelling to the server', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-stop', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        for (const word of ['停', '停下，不干了。', 'please stop this']) {
            sendTask.mockClear();
            cancelRun.mockClear();
            armAdmitCancel.mockClear();
            await typeAndSend(word);
            await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', word, ''));
            expect(cancelRun).not.toHaveBeenCalled();
            expect(armAdmitCancel).not.toHaveBeenCalled();
            expect(classifyWorkStop).not.toHaveBeenCalled();
        }
    });

    it('still cancels from the stop control, which is an explicit action', async () => {
        listActiveRuns.mockResolvedValue([{ run_id: 'run_live', task_text: '打开示例网站', status: 'running' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-live', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('打开示例网站');
        const mark = await screen.findByTestId('desktop-bot-pause');
        fireEvent.click(mark);
        await waitFor(() => expect(cancelRun).toHaveBeenCalledWith('bot_1', 'run_live'));
        expect(await screen.findByText('还在收尾，停稳了告诉你。')).toBeTruthy();
    });

    it('forwards 继续 instead of treating it as a keyword', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-go', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('继续');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '继续', ''));
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-go', kind: 'continue', ack: '正在接着做。', queued: false, held: false, run_id: 'run_a' });
        expect(await screen.findByText('正在接着做。')).toBeTruthy();
        expect(screen.queryByText('好，这个等手上的做完我就做。')).toBeNull();
    });

    it('forwards small talk and shows the server reply', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-hi', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('你好');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '你好', ''));
        result({ request_id: 'desktop-bot-hi', session_key: 'alice:bot_1', text: '在的，说吧。', send_to_user: '在的，说吧。' });
        expect(await screen.findByText('在的，说吧。')).toBeTruthy();
    });

    it('forwards a status question and shows the server answer', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-st', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('任务现在怎么样了');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '任务现在怎么样了', ''));
        expect(queryTaskStatus).not.toHaveBeenCalled();
        result({ request_id: 'desktop-bot-st', session_key: 'alice:bot_1', text: '「下载报表」还在进行。', send_to_user: '「下载报表」还在进行。' });
        expect(await screen.findByText('「下载报表」还在进行。')).toBeTruthy();
    });

    it('forwards a schedule request and does not arm a timer itself', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-sch', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('每个工作日早上做简报');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '每个工作日早上做简报', ''));
        expect(armSchedule).not.toHaveBeenCalled();
        intake({ bot_id: 'bot_1', request_id: 'desktop-bot-sch', kind: 'schedule', ack: '工作日早上的简报记下了。', queued: false, held: false });
        expect(await screen.findByText('工作日早上的简报记下了。')).toBeTruthy();
    });

    it('forwards a clear or consequential request and shows no confirm button', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-go2', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('今天天气怎样？');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '今天天气怎样？', ''));
        await typeAndSend('安装一个pdf阅读器');
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '安装一个pdf阅读器', ''));
        expect(screen.queryByTestId('desktop-bot-confirm')).toBeNull();
    });

    it('shows one neutral line when the server sends no ack in time', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-late', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('打开示例网站');
        await waitFor(() => expect(sendTask).toHaveBeenCalled());
        expect(await screen.findByText('好，我先做着。', {}, { timeout: 4000 })).toBeTruthy();
    });

    it('shows the server route on the line and undoes it as a new matter', async () => {
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-route', deferred: true, session_key: 'alice:bot_1' });
        await openDuty();
        await typeAndSend('把报表金额改成两位小数');
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith(''));
        act(() => {
            responseListeners.byEvent['desktop-bot-routed']?.({ bot_id: 'bot_1', request_id: 'desktop-bot-route', session: 'bot_1#t-old', via: 'content', title: '整理上周订单报表' });
        });
        expect((await screen.findByTestId('desktop-bot-routed')).textContent).toContain('接着「整理上周订单报表」这件事');
        continueSessionSent.mockClear();
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-undo', deferred: true, session_key: 'alice:bot_1' });
        fireEvent.click(screen.getByTestId('desktop-bot-routed-undo'));
        await waitFor(() => expect(continueSessionSent).toHaveBeenCalledWith('bot_1'));
        expect(screen.queryByTestId('desktop-bot-routed')).toBeNull();
    });
});

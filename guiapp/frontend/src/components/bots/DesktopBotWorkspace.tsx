import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { useDialog } from '../CustomDialog';
import { EventsOn } from '../../../wailsjs/runtime';
import { normalizeSidebarHubCredits } from '../../utils/sidebarHubCredits';
import { openSettingsTab } from '../../utils/settingsNavigation';
import {
    ArmDesktopBotSchedule,
    ArmDesktopBotAdmitCancel,
    CreateDesktopBot,
    CancelDesktopBotRun,
    AppendDesktopBotSupplement,
    DeleteDesktopBot,
    DeleteDesktopBotSchedule,
    DesktopBotDesktopAdmin,
    GetDesktopBotScreen,
    HandBackDesktopBot,
    SetDesktopBotDesktopMode,
    EvaluateDesktopTaskSupplement,
    FillBotSecret,
    GetActiveDesktopRuns,
    GetDesktopBotBusy,
    GetDesktopBotSupplements,
    GetDesktopBotLLM,
    GetHubLLMServiceStatus,
    GetMaclawLLMProviders,
    ArmDesktopBotCrossRun,
    ClaimDesktopBotCrossStart,
    ListDesktopBots,
    ListDesktopBotSchedules,
    ListDesktopBotTasks,
    PostDesktopBotCross,
    PullDesktopBotCrossNotes,
    SetDesktopBotPrimary,
    PersistDesktopBotPreference,
    QueryDesktopBotTaskStatus,
    RecallBotSecret,
    ReleaseDesktopBotWatch,
    RefreshHubLLMServiceStatus,
    RenameDesktopBot,
    SaveDesktopBotLLM,
    OpenFileOrShowInFolder,
    ReadDesktopBotShot,
    SaveBotSecret,
    SendDesktopBotTask,
    ShowItemInFolder,
    UnderstandDesktopBotTask,
    WatchDesktopBot,
} from '../../../wailsjs/go/main/App';
import { desktopModeOf, effectiveScreenPick, mineScreenURL, screenNeedsOwnHold, shownScreenURL, type BotScreen } from './desktopScreens';
import {
    appendDesktopBotReport,
    DESKTOP_BOT_NOT_PRIMARY,
    desktopBotAdmittingSentence,
    desktopBotActivityText,
    desktopBotAskingLabel,
    desktopBotCrossTargetText,
    desktopBotDestructive,
    desktopBotNoticeSeen,
    markDesktopBotNotice,
    rememberDesktopBotCrossRun,
    takeDesktopBotCrossRun,
    desktopBotQueuedSentence,
    desktopBotFiles,
    desktopBotImages,
    desktopBotLocalControl,
    desktopBotLocalPaths,
    desktopBotRunningSentence,
    desktopBotStatusUnreachableSentence,
    desktopBotStopFailedSentence,
    desktopBotStopIdleSentence,
    desktopBotStoppedSentence,
    desktopBotTypedConfirmSentence,
    desktopBotWaitingSentence,
    desktopBotWorkingLabel,
    desktopTaskStep,
    foldPlanConfirmation,
    forgetBotPersona,
    clearLiveBotTurn,
    messagesForBot,
    desktopCreditReadout,
    formatDesktopCredit,
    messageNeedsComputerTakeover,
    type DesktopCreditReadout,
    noteLiveBotTurn,
    personaForBot,
    personasForUser,
    saveBotPersona,
    botMessageStartedAt,
    pendingBotReplyIsStale,
    sideNote,
    userRequestBefore,

    BOT_CONFIRM_TASK,
    protocolControl,
    stripConfirmedOrderLead,
    BOT_RETURN_TASK,
    BOT_SAVED_TASK,
    notePendingBotDesktop,
    saveBotMessages,
    settlePendingBotReply,
    applySupplementOutcomes,
    type BotPhase,
    type DesktopBot,
    type DesktopBotAsk,
    type DesktopBotNoticeBot,
    type DesktopBotFile,
    type DesktopBotMessage,
    type DesktopBotNoReading,
} from './desktopBots';
import { botWindowIsForeground, setBotWindowForeground, watchBotReplies } from './botUnread';
import { BotMessageBody, botMessageIsStructured } from './botMessageBody';
import { botChatIsNearBottom, botChatLogStamp, pinBotChatToBottom } from './botChatScroll';
import { DesktopBotComposer } from './DesktopBotComposer';
import { BotEditDialog } from './BotEditDialog';
import { BotLLMSettingsDialog, type BotLLMProviderOption, type BotLLMSettingsSubmit } from './BotLLMSettingsDialog';
import { TaskResultArtifacts } from '../ai/assistantTaskArtifacts';
import { BotFilePreview } from './BotFilePreview';
import { BotRailIcon } from '../layout/SidebarNavIcons';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import './DesktopBotWorkspace.css';

// noVNC copies the query value straight onto viewOnly. The string "0" is
// truthy there, so an empty value is what forces the keyboard and pointer on.
function ScreenExpandIcon() {
    return (
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M9 4H4v5M15 4h5v5M20 15v5h-5M4 15v5h5" />
        </svg>
    );
}

function ScreenRestoreIcon() {
    return (
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M9 4v5H4M15 4v5h5M20 15h-5v5M4 15h5v5" />
        </svg>
    );
}

function SharedDesktopIcon() {
    return (
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <rect x="1.5" y="2.5" width="13" height="8.5" rx="1.4" fill="none" stroke="currentColor" strokeWidth="1.3" />
            <path d="M5.5 13.5h5M8 11v2.5" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    );
}

function ScreenWatchIcon() {
    return (
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
            <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" d="M2.5 12s3.5-6.5 9.5-6.5S21.5 12 21.5 12s-3.5 6.5-9.5 6.5S2.5 12 2.5 12Z" />
            <circle cx="12" cy="12" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.8" />
        </svg>
    );
}

function ScreenControlIcon() {
    return (
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
            <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" d="M6.5 3.8 18.6 12.2l-6.5.9-2.9 6.2Z" />
        </svg>
    );
}

function CreditMark() {
    return (
        <svg className="desktop-bot-workspace__credit-mark" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true" focusable="false">
            <circle cx="8" cy="8" r="5.15" fill="none" stroke="currentColor" strokeWidth="1.3" />
            <path d="M8 5.2v5.6" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    );
}

function CreditTopUpIcon() {
    return (
        <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <path d="M8 3.25v9.5M3.25 8h9.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
        </svg>
    );
}

// A chip with one pin per side and a star. It marks a model this person brought.
function CustomModelIcon() {
    return (
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" focusable="false">
            <rect x="7" y="7" width="10" height="10" rx="2.2" fill="none" stroke="currentColor" strokeWidth="1.7" />
            <path fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" d="M12 7V3.75M12 20.25V17M7 12H3.75M20.25 12H17" />
            <path fill="currentColor" d="M12 8.8 12.7 11.3 15.2 12 12.7 12.7 12 15.2 11.3 12.7 8.8 12 11.3 11.3Z" />
        </svg>
    );
}

function customModelDetail(choice: { name?: string; model?: string } | null | undefined): string {
    const name = String(choice?.name ?? '').trim();
    const model = String(choice?.model ?? '').trim();
    if (!name || !model || name === model) return name || model;
    return `${name} · ${model}`;
}

function modelRouteFrom(choice: { mode?: string; name?: string; model?: string } | null | undefined): { route: 'default' | 'custom'; detail: string } {
    if (choice?.mode !== 'custom') return { route: 'default', detail: '' };
    return { route: 'custom', detail: customModelDetail(choice) };
}

// vnc.html treats a checkbox query as on unless the value is 0, no, or false.
// An empty view_only= is therefore still view-only, which is why takeover
// showed the desktop and ignored every click. The page reads the setting once
// at load, and its matcher keeps the last copy, so replace any existing value.
function desktopFrameSrc(url: string, interactive: boolean): string {
    if (!url) return '';
    const hashAt = url.indexOf('#');
    const base = hashAt >= 0 ? url.slice(0, hashAt) : url;
    const hash = hashAt >= 0 ? url.slice(hashAt) : '';
    const queryAt = base.indexOf('?');
    const path = queryAt >= 0 ? base.slice(0, queryAt) : base;
    const kept = queryAt >= 0
        ? base.slice(queryAt + 1).split('&').filter(part => part !== 'view_only' && !part.startsWith('view_only='))
        : [];
    kept.push(interactive ? 'view_only=0' : 'view_only=1');
    return `${path}?${kept.join('&')}${hash}`;
}

const RETURN_COMMAND = BOT_RETURN_TASK;
const CONFIRM_COMMAND = BOT_CONFIRM_TASK;

// Composer text starts as a plan. A clear request is then sent as execute.
// A large change stays a plan until the confirm button. Typing the confirm
// sentence stays a plan; only that button passes execute.
function oneLine(text: string): string {
    const line = text.trim().split(/\r?\n/).find(part => part.trim()) || '';
    const chars = Array.from(line.trim());
    if (chars.length <= 80) return chars.join('');
    return chars.slice(0, 80).join('');
}

// A status or stop question does not wait on the reading. The local run
// record has 200ms. Hub's task sentence, only when that record is empty, has 800ms.
function withinBudget<T>(work: Promise<T>, ms: number): Promise<T | undefined> {
    return new Promise(resolve => {
        let settled = false;
        const finish = (value: T | undefined) => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            resolve(value);
        };
        const timer = setTimeout(() => finish(undefined), ms);
        work.then(value => finish(value), () => finish(undefined));
    });
}

function phaseFor(content: string, requested: BotPhase): BotPhase {
    if (planSaveName(content)) return 'plan';
    return requested === 'execute' ? 'execute' : 'plan';
}

type WorkPace = 'direct' | 'confirm' | 'ask' | 'reply' | 'schedule' | 'status';

// workPace is the foreground decision. direct does the work now. confirm is
// the one approval for a large change. ask is the one question when a fact
// only this person knows is missing. reply is a short answer given here, with
// no desktop worker. schedule arms a timer and does not call the worker now.
// A plan turn that forgot the field still waits. An execute turn that forgot
// it starts, and an execute turn never stays a reply. A schedule stays a
// schedule on an execute turn, because the confirmed work is itself timed.
// The execute override also takes an explicit confirm or reply: the person
// already confirmed, so nothing here asks a second time. Go's desktopBotPace
// deliberately keeps confirm on an execute turn — its gate only decides
// whether an empty order fails — and this override is where the no-second-
// confirmation rule actually lives.
function workPace(understood: Record<string, unknown>, turnPhase: BotPhase): WorkPace {
    const raw = String(understood.pace || understood.Pace || '').trim().toLowerCase();
    if (raw === 'ask') return 'ask';
    if (raw === 'schedule') return 'schedule';
    if (raw === 'status') return 'status';
    if (turnPhase === 'execute') return 'direct';
    if (raw === 'reply' || raw === 'direct' || raw === 'confirm') return raw;
    return 'confirm';
}

// An empty order stays a reply only when the model said so, or when a plan
// turn only keeps or withdraws an identity. A direct or confirm reading that
// forgot the order is a failed reading. An execute turn is never turned into
// chat, even when the reading also carries an identity. Cancelling a schedule
// has no work order.
function emptyOrderPace(
    pace: WorkPace,
    instruction: string,
    personaRaw: string,
    turnPhase: BotPhase,
    scheduleMode = '',
): WorkPace | 'failed' {
    if (instruction || pace === 'ask' || pace === 'reply' || pace === 'status') return pace;
    if (pace === 'schedule' && scheduleMode === 'cancel') return pace;
    if (turnPhase !== 'execute' && personaRaw) return 'reply';
    return 'failed';
}

function missedSchedule(error: unknown): boolean {
    return failureMessage(error).includes('no desktop bot schedule matched');
}

// A Go error crosses the generated binding as its own value, not as an Error.
// Reading it as one drops the reason, and the chat then shows nothing but a
// placeholder: that is how every failure here used to become "the backend
// agent is unavailable", including a plain timeout on the reading model.
function failureMessage(error: unknown): string {
    if (error instanceof Error) return error.message;
    if (typeof error === 'string') return error;
    const raw = error && typeof error === 'object' ? (error as { message?: unknown }).message : undefined;
    return String(raw ?? error ?? '');
}

// One reading of one message. Go decides what came back, where the call is
// actually watched, so nothing here matches English error strings across a
// transport boundary any more.
// - read: an arrangement this person can act on.
// - unread: an answer came back and it was not a usable reading.
// - unscheduled: timed work whose time was never named.
// - unreachable: no answer at all.
// The last three are reports about the helper, not verdicts on the message: in
// every one of them, nothing has been sent to the desktop.
type BotReading =
    | { kind: 'read'; told: string; instruction: string; persona: string; pace: WorkPace; understood: Record<string, unknown> }
    | { kind: 'unread' }
    | { kind: 'unscheduled' }
    | { kind: 'unreachable' };

// The reason a turn has no reading. Not good or bad — just what to tell.
// Shared with DesktopBotMessage so the two cannot drift apart again.
type NoReading = DesktopBotNoReading;

function desktopBotCatalog(selfId: string, bots: DesktopBot[]): string {
    const self = selfId.trim();
    if (!self || bots.length === 0) return '';
    return JSON.stringify({
        self,
        bots: bots.filter(bot => bot.id).map(bot => ({
            id: bot.id,
            name: bot.title,
            primary: bot.primary === true,
        })),
    });
}

function crossOf(understood: Record<string, unknown>): { kind: string; target: string; text: string; order: string } | null {
    const raw = understood.cross ?? understood.Cross;
    if (!raw || typeof raw !== 'object') return null;
    const item = raw as Record<string, unknown>;
    const kind = String(item.kind ?? item.Kind ?? '').trim().toLowerCase();
    if (kind !== 'notify' && kind !== 'delegate') return null;
    return {
        kind,
        target: String(item.target ?? item.Target ?? '').trim(),
        text: String(item.text ?? item.Text ?? '').trim(),
        order: String(item.order ?? item.Order ?? '').trim(),
    };
}

function reportDesktopBotCrossResult(userId: string, botId: string, text: string) {
    const run = takeDesktopBotCrossRun(userId, botId);
    const body = (text || '').trim();
    if (!run || !body) return;
    markDesktopBotNotice(userId, `${run.noteId}:result:${botId}`);
    void PostDesktopBotCross(botId, 'result', '', body, '', run.noteId).then(posted => {
        const id = String(posted?.notes?.[0]?.id || '').trim();
        if (id) markDesktopBotNotice(userId, `${id}:target`);
    }).catch(() => undefined);
}

async function readDesktopBotTurn(
    content: string,
    phase: BotPhase,
    lang: string,
    earlier: string[],
    persona: string,
    catalog = '',
): Promise<BotReading> {
    let understood: Record<string, unknown>;
    try {
        understood = await UnderstandDesktopBotTask(content, phase, lang, earlier, persona, catalog) as Record<string, unknown>;
    } catch (error) {
        // The reading itself could not be attempted: no text, no backend. That
        // is not this person's fault and it is not a reading.
        throw new Error(failureMessage(error).trim() || 'desktop bot reading failed');
    }
    understood = understood ?? {};
    switch (String(understood.reading ?? understood.Reading ?? '').trim()) {
        case 'unreachable': return { kind: 'unreachable' };
        case 'unscheduled': return { kind: 'unscheduled' };
        case 'unread': return { kind: 'unread' };
    }
    const told = String(understood.told ?? understood.Told ?? '').trim();
    const instruction = String(understood.instruction ?? understood.Instruction ?? '').trim();
    const personaRaw = String(understood.persona ?? understood.Persona ?? '').trim();
    let pace = workPace(understood, phase);
    // A destructive order on a plan turn still needs the one button, even when
    // the reading said to start now. The execute turn is that button.
    if (phase !== 'execute' && pace === 'direct' && desktopBotDestructive(`${instruction}\n${content}`)) {
        pace = 'confirm';
    }
    const decided = emptyOrderPace(pace, instruction, personaRaw, phase, scheduleOf(understood).mode);
    if (decided === 'failed') return { kind: 'unread' };
    return { kind: 'read', told, instruction, persona: personaRaw, pace: decided, understood };
}

// The reading failed. Say so in this language and leave the words parked, so
// one click either reads again or sends exactly what was typed.
function noReadingReply(kind: NoReading, words: { orderMissing: string; scheduleMissing: string; readingLost: string }, content: string, phase: BotPhase) {
    return {
        content: kind === 'unscheduled' ? words.scheduleMissing : kind === 'unread' ? words.orderMissing : words.readingLost,
        failed: true,
        phase: undefined,
        noReading: { text: content, phase, kind },
    } as Pick<DesktopBotMessage, 'content' | 'failed' | 'phase' | 'noReading'>;
}

type ScheduleSpec = { name: string; mode: string; interval: number; hour: number; minute: number; day: number };

function scheduleNumber(value: unknown, fallback: number): number {
    if (value === undefined || value === null || value === '') return fallback;
    const n = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(n)) return fallback;
    return Math.trunc(n);
}

function scheduleOf(understood: Record<string, unknown>): ScheduleSpec {
    const raw = understood.schedule ?? understood.Schedule;
    const spec = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
    return {
        name: String(spec.name ?? spec.Name ?? '').trim(),
        mode: String(spec.mode ?? spec.Mode ?? '').trim().toLowerCase(),
        interval: scheduleNumber(spec.interval_minutes ?? spec.IntervalMinutes, 0),
        hour: scheduleNumber(spec.hour ?? spec.Hour, -1),
        minute: scheduleNumber(spec.minute ?? spec.Minute, -1),
        day: scheduleNumber(spec.day_of_week ?? spec.DayOfWeek ?? spec.dayOfWeek, -1),
    };
}

function armDesktopSchedule(botId: string, understood: Record<string, unknown>, instruction: string) {
    const spec = scheduleOf(understood);
    return ArmDesktopBotSchedule(botId, spec.name, spec.mode || 'create', instruction, spec.interval, spec.hour, spec.minute, spec.day);
}

type ScheduleWords = { scheduleSet: string; scheduleStopped: string };

function scheduleAck(told: string, mode: string, words: ScheduleWords): string {
    if (told) return told;
    return mode === 'cancel' ? words.scheduleStopped : words.scheduleSet;
}

export type DesktopBotScheduleInfo = {
    id: string;
    name: string;
    action: string;
    interval_minutes: number;
    hour: number;
    minute: number;
    day_of_week: number;
    next_run_at?: string;
    status?: string;
};

function normalizeSchedules(value: unknown): DesktopBotScheduleInfo[] {
    const list = Array.isArray(value) ? value : [];
    const out: DesktopBotScheduleInfo[] = [];
    for (const item of list) {
        if (!item || typeof item !== 'object') continue;
        const raw = item as Record<string, unknown>;
        const id = String(raw.id ?? raw.ID ?? '').trim();
        if (!id) continue;
        out.push({
            id,
            name: String(raw.name ?? raw.Name ?? '').trim(),
            action: String(raw.action ?? raw.Action ?? '').trim(),
            interval_minutes: scheduleNumber(raw.interval_minutes ?? raw.IntervalMinutes, 0),
            hour: scheduleNumber(raw.hour ?? raw.Hour, 0),
            minute: scheduleNumber(raw.minute ?? raw.Minute, 0),
            day_of_week: scheduleNumber(raw.day_of_week ?? raw.DayOfWeek, -1),
            next_run_at: String(raw.next_run_at ?? raw.NextRunAt ?? '').trim(),
            status: String(raw.status ?? raw.Status ?? '').trim(),
        });
    }
    return out;
}

const WEEKDAY_ZH = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const WEEKDAY_ZH_HANT = ['週日', '週一', '週二', '週三', '週四', '週五', '週六'];
const WEEKDAY_EN = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function scheduleWhen(task: DesktopBotScheduleInfo, lang: string): string {
    const traditional = lang === 'zh-Hant';
    if (task.interval_minutes > 0) {
        const n = task.interval_minutes;
        if (lang === 'en') {
            if (n === 1) return 'Every minute';
            if (n === 60) return 'Every hour';
            if (n === 1440) return 'Every day';
            if (n % 1440 === 0) return `Every ${n / 1440} days`;
            if (n % 60 === 0) return `Every ${n / 60} hours`;
            return `Every ${n} minutes`;
        }
        if (n === 1) return traditional ? '每分鐘' : '每分钟';
        if (n === 60) return traditional ? '每小時' : '每小时';
        if (n === 1440) return '每天';
        if (n % 1440 === 0) return `每 ${n / 1440} 天`;
        if (n % 60 === 0) return traditional ? `每 ${n / 60} 小時` : `每 ${n / 60} 小时`;
        return traditional ? `每 ${n} 分鐘` : `每 ${n} 分钟`;
    }
    const clock = `${String(task.hour).padStart(2, '0')}:${String(task.minute).padStart(2, '0')}`;
    const day = task.day_of_week;
    if (day < 0 || day > 6) {
        return lang === 'en' ? `Every day ${clock}` : `每天 ${clock}`;
    }
    const name = lang === 'en' ? WEEKDAY_EN[day] : (traditional ? WEEKDAY_ZH_HANT[day] : WEEKDAY_ZH[day]);
    return `${name} ${clock}`;
}

function scheduleNext(iso: string, lang: string): string {
    if (!iso) return '';
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    const stamp = `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
    return lang === 'en' ? `Next ${stamp}` : `下次 ${stamp}`;
}

function planSaveName(content: string): string {
    const match = BOT_SAVED_TASK.exec(content.trim());
    return match ? match[1] : '';
}

function saveSentence(name: string): string {
    return `已在本机保存 ${name}。`;
}

function fillSentence(name: string): string {
    return `${name} 已在本地填入当前密码框。请沿用这个结果继续，不要向我索要它的内容。`;
}

function storedQueuePhase(item: DesktopBotMessage): BotPhase {
    if (item.phase === 'execute' || item.phase === 'plan') return item.phase;
    return phaseFor(item.content, 'plan');
}

// Earlier user lines are context for the foreground agent. The line being
// dispatched is the latest message, so it is not repeated here. A message
// still waiting in the queue is a later job, not context for this one.
type PreparedTurn = { told: string; instruction: string; pace: 'confirm' | 'direct'; persona: string; earlier: string[] };
type QueuedSend = { text: string; phase: BotPhase; verbatim?: boolean; prepared?: PreparedTurn; userText?: string; crossNoteId?: string; claimed?: boolean };

function sameContext(left: string[], right: string[]): boolean {
    return left.length === right.length && left.every((item, index) => item === right[index]);
}

function earlierUserAsks(prior: DesktopBotMessage[], content: string, mode: 'now' | 'fromQueue'): string[] {
    const users = prior.filter(item => item.role === 'user' && (item.content || '').trim() && !item.queued);
    if (mode === 'fromQueue') {
        for (let i = users.length - 1; i >= 0; i--) {
            if (users[i].content === content) {
                users.splice(i, 1);
                break;
            }
        }
    }
    return users.map(item => item.content);
}

type LiveDesktop = { url: string; userControl: boolean; attentionReason: string };
type SecretPrompt = { botId: string; messageId: string; name: string; fill: boolean };
type ScreenEvent = { url: string; reported: boolean; userControl: boolean; reason: string; cleared: boolean };

function screenOf(data: Record<string, unknown>): ScreenEvent {
    const url = String(data.novnc_url || data.NovncURL || data.desktop_handoff_url || data.DesktopHandoffURL || '');
    const reported = ['user_control', 'desktop_user_control', 'UserControl', 'DesktopUserControl', 'desktop_handoff', 'DesktopHandoff'].some(key => Object.prototype.hasOwnProperty.call(data, key));
    const userControl = data.user_control === true || data.desktop_user_control === true || data.UserControl === true || data.DesktopUserControl === true || data.desktop_handoff === true || data.DesktopHandoff === true;
    const reason = String(data.attention_reason || data.desktop_attention_reason || data.AttentionReason || '').trim();
    const cleared = data.cleared === true || data.Cleared === true;
    return { url, reported, userControl, reason, cleared };
}

function askFrom(data: Record<string, unknown>): DesktopBotAsk | undefined {
    const inputType = String(data.ask_user_input_type || data.AskUserInputType || '').trim();
    const secretName = String(data.ask_user_secret_name || data.AskUserSecretName || '').trim();
    const question = String(data.ask_user_question || data.AskUserQuestion || '').trim();
    let options: string[] | undefined;
    const raw = data.ask_user_options_json ?? data.AskUserOptionsJSON;
    if (typeof raw === 'string' && raw.trim()) {
        try {
            const parsed = JSON.parse(raw) as unknown;
            if (Array.isArray(parsed)) options = parsed.map(item => String(item)).filter(Boolean);
        } catch {
            options = undefined;
        }
    }
    if (!inputType && !secretName && !question && !(options && options.length)) return undefined;
    return { inputType, secretName, question, options };
}

// The provider list this dialog offers is the desktop's own list, which the
// backend serializes from corelib's struct. Field names arrive lower-cased here,
// but the Wails type is `any`, so every read tolerates both casings.
type DesktopProviderWire = Record<string, unknown> | undefined | null;

function wireField(entry: DesktopProviderWire, lower: string, upper: string): string {
    if (!entry) return '';
    const raw = entry[lower] ?? entry[upper];
    return typeof raw === 'string' ? raw : '';
}

function wireStringList(entry: DesktopProviderWire, lower: string, upper: string): string[] {
    if (!entry) return [];
    const raw = entry[lower] ?? entry[upper];
    if (!Array.isArray(raw)) return [];
    return raw.map(item => (typeof item === 'string' ? item.trim() : '')).filter(item => item !== '');
}

function wireBool(entry: DesktopProviderWire, lower: string, upper: string): boolean {
    if (!entry) return false;
    return entry[lower] === true || entry[upper] === true;
}

// Only providers carrying a key can actually serve the bot; offering the rest
// would push into MaClawSrv a route that cannot call anything. Names the hub
// reserves, or that duplicate one of the account's own providers, are left for
// the hub to refuse — it answers with the reason, and only it knows the list.
function usableDesktopLLMProviders(entries: DesktopProviderWire[]): BotLLMProviderOption[] {
    return entries
        .map(entry => ({
            name: wireField(entry, 'name', 'Name').trim(),
            url: wireField(entry, 'url', 'URL').trim(),
            key: wireField(entry, 'key', 'Key').trim(),
            model: wireField(entry, 'model', 'Model').trim(),
            protocol: wireField(entry, 'protocol', 'Protocol').trim(),
            wireAPI: wireField(entry, 'wire_api', 'WireAPI').trim(),
            models: wireStringList(entry, 'models', 'Models'),
            agentType: wireField(entry, 'agent_type', 'AgentType').trim(),
            isHubService: wireBool(entry, 'is_hub_service', 'IsHubService'),
        }))
        .filter(item => item.name !== '' && item.key !== '');
}

function copy(lang: string) {
    if (lang === 'en') {
        return {
            listTitle: 'Bots',
            shared: 'A bot is a digital assistant stationed on the remote computer around the clock.',
            empty: 'No bots yet.',
            create: 'New',
            editInfo: 'Edit info',
            editTitle: 'Edit bot info',
            close: 'Close',
            save: 'Save',
            cancel: 'Cancel',
            remove: 'Delete',
            removeTitle: 'Delete bot',
            removeAsk: (name: string) => `Delete bot "${name}"? This cannot be undone.`,
            name: 'Name',
            description: 'Description',
            persona: 'Persona',
            personaEmpty: 'No persona yet.',
            defaultDescription: 'A 24/7 digital assistant on the remote computer.',
            pick: 'Select a bot to send a task.',
            pickEmpty: 'No conversation yet — send the first task below.',
            messageTo: (name: string) => `Message ${name}`,
            details: 'Details',
            primaryMark: 'Primary',
            makePrimary: 'Set as primary bot',
            primaryNow: 'Already the primary bot',
            computer: 'Computer',
            status: 'Status',
            created: 'Created',
            credits: 'Credits available',
            creditsLeft: 'Credits remaining',
            creditsUnlimited: 'Unlimited',
            creditLow: 'Your credits are nearly used up. A task may stop partway through.',
            creditEmpty: 'Your credits are used up. A task will stop.',
            creditTopUp: 'Top up',
            creditSettings: 'Change settings',
            screenOf: (name: string) => `${name}'s screen`,
            screenshot: 'Desktop screenshot',
            viewShot: 'View full size',
            closePreview: 'Close',
            download: 'Download',
            send: 'Send',
            working: 'working',
            arranging: 'arranging',
            idle: 'idle',
            workingTag: 'Working',
            arrangingTag: 'Arranging',
            idleTag: 'Idle',
            confirm: 'Run this arrangement',
            secretHint: 'Use the password box below. Do not paste it into the chat.',
            secretSave: 'Save',
            secretCancel: 'Cancel',
            retryConnect: 'Retry connection',
            desktopDown: 'The desktop picture did not connect. Retry the connection — the task is not sent again.',
            fillMiss: 'That password field cannot be filled from here. Open this bot’s computer and type it yourself.',
            secretStoreFailed: 'The password was not saved on this computer.',
            ops: 'Actions',
            hideDesktop: 'Hide desktop',
            takeover: 'Take over',
            takeComputer: 'Take over the computer',
            takeComputerHint: 'Take the keyboard and finish the login or check on this computer.',
            exitTakeover: 'Release',
            takeoverHint: 'Fullscreen the desktop and take the keyboard from the bot.',
            takeoverBusyHint: 'This bot is using its desktop — takeover is unavailable right now.',
            takeoverActive: 'You have the desktop. Release it to hand the keyboard back.',
            myDesktop: 'My desktop',
            sharedScreen: 'Shared',
            screenSwitch: 'Desktops',
            handBack: 'Hand back to the bot',
            handBackBusy: 'Handing the desktop back…',
            handoffFallback: 'This bot needs you on its desktop.',
            handBackFailed: 'The desktop was not handed back.',
            noSharedDesktop: 'No bot is sharing your desktop.',
            isolatedDesktop: 'Own desktop',
            isolatedDesktopHint: 'This bot uses its own virtual desktop. Off means it shares yours.',
            desktopModeFailed: 'The desktop mode was not changed.',
            watching: 'Watching the desktop. It stays open while you watch.',
            viewOnly: 'View only',
            takeControl: 'Take control',
            screenMode: 'Screen mode',
            watchLockedHint: 'The bot handed you the keyboard — finish the login, then press "Login done, continue".',
            expandScreen: 'Expand',
            restoreScreen: 'Restore',
            connectingDesktop: 'Starting the desktop…',
            composerHint: 'Enter to send, Shift+Enter for a new line, ↑↓ for history.',
            resizeList: 'Resize bot list',
            resizeListHint: 'Drag to resize. Double-click to reset.',
            moveIdentity: 'Drag to move. Click to open the desktop.',
            botControl: 'The bot is using the desktop.',
            stillThere: 'The website login is still in this browser. Send another message and this bot continues there.',
            userControl: 'Finish the login or verification in this browser. The login stays here for the bot.',
            returnControl: 'Login done, continue',
            returnCommand: RETURN_COMMAND,
            emptyResult: 'The backend returned no result.',
            unavailable: 'The backend agent is unavailable.',
            orderMissing: 'That message did not become an order, so it was not sent.',
            scheduleMissing: 'That message did not become a schedule, so none was set.',
            readingLost: 'I could not turn that into an arrangement — the service that reads your messages did not answer. You can send your own words as they are, or have me read it again.',
            readingAgain: 'Read it again',
            readingVerbatim: 'Send my own words',
            scheduleNone: 'No matching schedule was found.',
            scheduleSet: 'The schedule is set. The result will be reported here.',
            scheduleStopped: 'The schedule is stopped.',
            schedules: 'Schedules',
            scheduleEmpty: 'No schedules yet.',
            scheduleRemoveTitle: 'Delete schedule',
            scheduleRemoveAsk: (name: string) => `Delete schedule "${name}"?`,
            schedulePaused: 'Paused',
            accountChanged: 'This message was not sent. The account changed before it went out.',
            llmSettings: 'Model',
            llmSettingsHint: 'Choose the model these bots run on',
            llmTitle: 'Model for your bots',
            llmSharedHint: 'Every bot of this account runs on the same model. Changing it here moves all of them.',
            llmUseDefault: 'Default',
            llmDefaultHint: 'Hand the bots back to whatever the service resolves for you — either your own route, or the provider an administrator picked.',
            llmUseProvider: 'Pick a provider from this computer',
            llmProviderLabel: 'Provider',
            llmModelLabel: 'Model',
            llmModelLoading: 'Loading the model list…',
            llmModelEmpty: 'This provider has no models to pick from yet.',
            llmNoProviders: 'No provider on this computer has a key yet. Add one under Settings → Providers first.',
            llmCustomMark: (detail: string) => detail ? `Using your own model: ${detail}` : 'Using your own model',
        };
    }
    if (lang === 'zh-Hant') {
        return {
            listTitle: 'Bot',
            shared: 'Bot為24小時駐守遠端電腦上的數位助理。',
            empty: '還沒有 Bot。',
            create: '新建',
            editInfo: '編輯資訊',
            editTitle: '編輯 Bot 資訊',
            close: '關閉',
            save: '儲存',
            cancel: '取消',
            remove: '刪除',
            removeTitle: '刪除 Bot',
            removeAsk: (name: string) => `確定刪除 Bot「${name}」？此操作不可撤銷。`,
            name: '名稱',
            description: '描述',
            persona: '人設',
            personaEmpty: '還沒有人設。',
            defaultDescription: '遠端電腦上的7x24小時數位助理。',
            pick: '選擇一個 Bot 後發送任務。',
            pickEmpty: '還沒有對話，在下面安排第一件事吧。',
            messageTo: (name: string) => `給 ${name} 發消息`,
            details: '詳情',
            primaryMark: '主',
            makePrimary: '設為主 Bot',
            primaryNow: '已是主 Bot',
            computer: '電腦',
            status: '狀態',
            created: '建立時間',
            credits: '可用 Credits',
            creditsLeft: '剩餘 Credits',
            creditsUnlimited: '不限制',
            creditLow: '你的 Credits 快用完了，繼續跑任務可能會在中途停住。',
            creditEmpty: '你的 Credits 已經用完，繼續跑任務會停住。',
            creditTopUp: '去充值',
            creditSettings: '修改設定',
            screenOf: (name: string) => `${name} 的螢幕`,
            screenshot: '桌面截圖',
            viewShot: '查看大圖',
            closePreview: '關閉',
            download: '下載',
            send: '發送',
            working: '正在工作',
            arranging: '正在安排',
            idle: '待命',
            workingTag: '工作中',
            arrangingTag: '安排中',
            idleTag: '待命',
            confirm: '按這個安排執行',
            secretHint: '密碼請用下面的密碼框，不要貼進對話。',
            secretSave: '儲存',
            secretCancel: '取消',
            retryConnect: '重試連接',
            desktopDown: '桌面畫面連不上。可以重試連接，不用重發任務。',
            fillMiss: '這個密碼框在當前頁面填不了。請打開這個 Bot 的電腦自己輸入。',
            secretStoreFailed: '密碼沒有保存到本機。',
            ops: '操作',
            hideDesktop: '收起桌面',
            takeover: '人類接管',
            takeComputer: '接管電腦',
            takeComputerHint: '接管這台電腦，用鍵盤和滑鼠完成登入或驗證。',
            exitTakeover: '退出接管',
            takeoverHint: '全螢幕顯示桌面，並把鍵盤交給你。',
            takeoverBusyHint: '這個 Bot 正在操作桌面，現在不能接管。',
            takeoverActive: '你已接管桌面。完成後退出接管，把鍵盤交還。',
            myDesktop: '我的桌面',
            sharedScreen: '共享',
            screenSwitch: '桌面',
            handBack: '交還給 Bot',
            handBackBusy: '正在交還桌面…',
            handoffFallback: '這個 Bot 需要你在它的桌面上操作。',
            handBackFailed: '桌面沒有交還給 Bot。',
            noSharedDesktop: '沒有 Bot 在共用你的桌面。',
            isolatedDesktop: '獨立桌面',
            isolatedDesktopHint: '這個 Bot 使用自己的虛擬桌面。關閉後改回共用你的桌面。',
            desktopModeFailed: '桌面模式沒有改成。',
            watching: '正在觀看桌面。觀看期間桌面保持開啟。',
            viewOnly: '僅觀看',
            takeControl: '取得控制權',
            screenMode: '螢幕模式',
            watchLockedHint: 'Bot 已把鍵盤交給你，完成登入後點「登入完成，繼續」。',
            expandScreen: '放大',
            restoreScreen: '恢復',
            connectingDesktop: '正在連接桌面…',
            composerHint: 'Enter 傳送，Shift+Enter 換行，↑↓ 翻看歷史。',
            resizeList: '調整 Bot 列表寬度',
            resizeListHint: '拖動調整寬度，雙擊恢復預設。',
            moveIdentity: '拖動可移動。點擊打開桌面。',
            botControl: 'Bot 正在操作桌面。',
            stillThere: '網站登入還在這個瀏覽器裡。再發一條訊息，這個 bot 會接著操作。',
            userControl: '請在這個桌面的瀏覽器裡完成登入或驗證。登入會留在這個瀏覽器裡，完成後交還。',
            returnControl: '登入完成，繼續',
            returnCommand: RETURN_COMMAND,
            emptyResult: '後台沒有返回結果。',
            unavailable: '後台 agent 不可用。',
            orderMissing: '這句沒整理成可執行的安排，沒有發出。',
            scheduleMissing: '這句沒有整理成定時安排，沒有設定。',
            readingLost: '這句沒讀成安排——幫我讀話的服務暫時沒回應。可以照你的原話發過去，或讓我再讀一次。',
            readingAgain: '再讀一次',
            readingVerbatim: '照原話發出',
            scheduleNone: '沒有找到要取消的定時安排。',
            scheduleSet: '定時安排已設定。到時會在這裡匯報。',
            scheduleStopped: '定時安排已停止。',
            schedules: '定時任務',
            scheduleEmpty: '還沒有定時任務。',
            scheduleRemoveTitle: '刪除定時任務',
            scheduleRemoveAsk: (name: string) => `確定刪除定時任務「${name}」？`,
            schedulePaused: '已暫停',
            accountChanged: '這條沒有發出。發出前帳號已經切換。',
            llmSettings: '模型',
            llmSettingsHint: '設定 Bot 用哪個模型',
            llmTitle: 'Bot 用的模型',
            llmSharedHint: '這個帳號底下的所有 Bot 都跑同一個模型，這裡改了會一起生效。',
            llmUseDefault: '預設',
            llmDefaultHint: '交回服務自己決定：可能是你在 Hub 上的線路，也可能是管理員指定的服務商。',
            llmUseProvider: '從這台電腦的服務商裡挑一個',
            llmProviderLabel: '服務商',
            llmModelLabel: '模型',
            llmModelLoading: '正在取得模型清單…',
            llmModelEmpty: '這個服務商暫時沒有可選模型。',
            llmNoProviders: '這台電腦還沒有填好 key 的服務商。先到設定 → 服務商新增一個。',
            llmCustomMark: (detail: string) => detail ? `正在使用自訂模型：${detail}` : '正在使用自訂模型',
        };
    }
    return {
        listTitle: 'Bot',
        shared: 'Bot为24小时驻守远程电脑上的数字助理。',
        empty: '还没有 Bot。',
        create: '新建',
        editInfo: '编辑信息',
        editTitle: '编辑 Bot 信息',
        close: '关闭',
        save: '保存',
        cancel: '取消',
        remove: '删除',
        removeTitle: '删除 Bot',
        removeAsk: (name: string) => `确定删除 Bot「${name}」？此操作不可撤销。`,
        name: '名称',
        description: '描述',
        persona: '人设',
        personaEmpty: '还没有人设。',
        defaultDescription: '远程电脑上的7x24小时数字助理。',
        pick: '选择一个 Bot 后发送任务。',
        pickEmpty: '还没有对话，在下面安排第一件事吧。',
        messageTo: (name: string) => `给 ${name} 发消息`,
        details: '详情',
        primaryMark: '主',
        makePrimary: '设为主 Bot',
        primaryNow: '已是主 Bot',
        computer: '电脑',
        status: '状态',
        created: '创建时间',
        credits: '可用 Credits',
        creditsLeft: '剩余 Credits',
        creditsUnlimited: '不限制',
        creditLow: '你的 Credits 快用完了，继续跑任务可能会在中途停住。',
        creditEmpty: '你的 Credits 已经用完，继续跑任务会停住。',
        creditTopUp: '去充值',
        creditSettings: '修改设置',
        screenOf: (name: string) => `${name} 的屏幕`,
        screenshot: '桌面截图',
        viewShot: '查看大图',
        closePreview: '关闭',
        download: '下载',
        send: '发送',
        working: '正在工作',
        arranging: '正在安排',
        idle: '待命',
        workingTag: '工作中',
        arrangingTag: '安排中',
        idleTag: '待命',
        confirm: '按这个安排执行',
        secretHint: '密码请用下面的密码框，不要贴进对话。',
        secretSave: '保存',
        secretCancel: '取消',
        retryConnect: '重试连接',
        desktopDown: '桌面画面连不上。可以重试连接，不用重发任务。',
        fillMiss: '这个密码框在当前页面填不了。请打开这个 Bot 的电脑自己输入。',
        secretStoreFailed: '密码没有保存到本机。',
        ops: '操作',
        hideDesktop: '收起桌面',
        takeover: '人类接管',
        takeComputer: '接管电脑',
        takeComputerHint: '接管这台电脑，用键盘和鼠标完成登录或验证。',
        exitTakeover: '退出接管',
        takeoverHint: '全屏显示桌面，并把键盘交给你。',
        takeoverBusyHint: '这个 Bot 正在操作桌面，现在不能接管。',
        takeoverActive: '你已接管桌面。完成后退出接管，把键盘交回。',
        myDesktop: '我的桌面',
        sharedScreen: '共享',
        screenSwitch: '桌面',
        handBack: '交还给 bot',
        handBackBusy: '正在交还桌面…',
        handoffFallback: '这个 bot 需要你在它的桌面上操作。',
        handBackFailed: '桌面没有交还给 bot。',
        noSharedDesktop: '没有 bot 在共用你的桌面。',
        isolatedDesktop: '独立桌面',
        isolatedDesktopHint: '这个 bot 使用自己的虚拟桌面。关闭后改回共用你的桌面。',
        desktopModeFailed: '桌面模式没有改成。',
        watching: '正在观看桌面。观看期间桌面保持开启。',
        viewOnly: '仅观看',
        takeControl: '取得控制权',
        screenMode: '屏幕模式',
        watchLockedHint: 'Bot 已把键盘交给你，完成登录后点“登录完成，继续”。',
        expandScreen: '放大',
        restoreScreen: '恢复',
        connectingDesktop: '正在连接桌面…',
        composerHint: 'Enter 发送，Shift+Enter 换行，↑↓ 翻看历史。',
        resizeList: '调整 Bot 列表宽度',
        resizeListHint: '拖动调整宽度，双击恢复默认。',
        moveIdentity: '拖动可移动。点击打开桌面。',
        botControl: 'Bot 正在操作桌面。',
        stillThere: '网站登录还在这个浏览器里。再发一条消息，这个 bot 会接着操作。',
        userControl: '请在这个桌面的浏览器里完成登录或验证。登录会留在这个浏览器里，完成后交还。',
        returnControl: '登录完成，继续',
        returnCommand: RETURN_COMMAND,
        emptyResult: '后台没有返回结果。',
        unavailable: '后台 agent 不可用。',
            orderMissing: '这句没整理成可执行的安排，没有发出。',
            scheduleMissing: '这句没有整理成定时安排，没有设定。',
            readingLost: '这句没读成安排——帮我读话的服务暂时没有回应。可以照你的原话发过去，或者让我再读一次。',
            readingAgain: '再读一次',
            readingVerbatim: '照原话发出',
        scheduleNone: '没有找到要取消的定时安排。',
        scheduleSet: '定时安排已设定。到时会在这里汇报。',
        scheduleStopped: '定时安排已停止。',
        schedules: '定时任务',
        scheduleEmpty: '还没有定时任务。',
        scheduleRemoveTitle: '删除定时任务',
        scheduleRemoveAsk: (name: string) => `确定删除定时任务「${name}」？`,
        schedulePaused: '已暂停',
        accountChanged: '这条没有发出。发出前账号已经切换。',
        llmSettings: '模型',
        llmSettingsHint: '设置 Bot 用哪个模型',
        llmTitle: 'Bot 用的模型',
        llmSharedHint: '这个账号下的所有 Bot 都跑同一个模型，这里改了会一起生效。',
        llmUseDefault: '默认',
        llmDefaultHint: '交回服务自己决定：可能是你在 Hub 上的线路，也可能是管理员指定的服务商。',
        llmUseProvider: '从这台电脑的服务商里挑一个',
        llmProviderLabel: '服务商',
        llmModelLabel: '模型',
        llmModelLoading: '正在获取模型列表…',
        llmModelEmpty: '这个服务商暂时没有可选模型。',
        llmNoProviders: '这台电脑还没有填好 key 的服务商。先到设置 → 服务商添加一个。',
        llmCustomMark: (detail: string) => detail ? `正在使用自定义模型：${detail}` : '正在使用自定义模型',
    };
}

// The first reply is this turn's understanding and method. The child agent
// receives that order, not the raw sentence. The result still lands later,
// on its own bubble.

// Delivery failures are reported the way a colleague would excuse a network
// hiccup, instead of pasting the operator-facing Hub error into the chat. The
// wording maps at display time (named in friendlyFailure), so storage keeps
// the raw error for the log.
function friendlyFailure(raw: string, lang: string): string {
    const detail = String(raw || '').trim();
    if (!detail) return '';
    if (/没有在时限内|timed? ?out|超时|逾時|deadline exceeded/i.test(detail)) {
        if (lang === 'en') {
            return 'This task took too long and timed out on my side. The desktop is untouched — please send it again in a bit.';
        }
        if (lang === 'zh-Hant') {
            return '這個任務等了太久，我這邊逾時了。稍後再發一次試試。';
        }
        return '这个任务等了太久，我这边超时了。稍后再发一次试试。';
    }
    if (/bot system-free credits limit is exhausted|selected model grant credits are exhausted|requires a grant-backed service group|LLM_SERVICE_CREDITS_EXHAUSTED|LLM_SERVICE_CREDITS_REQUIRED/i.test(detail)) {
        if (lang === 'en') {
            return 'Your credits are used up. Top up, or switch this bot to your own model.';
        }
        if (lang === 'zh-Hant') {
            return '你的 Credits 已經用完。請充值，或把這個 Bot 改成自己的模型。';
        }
        return '你的 Credits 已经用完。请充值，或把这个 Bot 改成自己的模型。';
    }
    if (/LLM_SERVICE_GRANT_EXPIRED|grant has expired/i.test(detail)) {
        if (lang === 'en') {
            return 'Your credits have expired. Top up, or switch this bot to your own model.';
        }
        if (lang === 'zh-Hant') {
            return '你的 Credits 已過期。請充值，或把這個 Bot 改成自己的模型。';
        }
        return '你的 Credits 已过期。请充值，或把这个 Bot 改成自己的模型。';
    }
    if (/unavailable|不可达/i.test(detail)) {
        if (lang === 'en') {
            return 'This one did not go through — I cannot reach my server right now. Please try sending it again in a moment.';
        }
        if (lang === 'zh-Hant') {
            return '這條沒送到——我這邊暫時連不上我的伺服器。麻煩稍後再發一次。';
        }
        return '这条没送到——我这边暂时连不上我的服务器。麻烦稍后再发一次。';
    }
    return detail;
}

// The queued variant is only for a line that cannot start yet: an earlier
// message is still being read, or the person still has the keyboard. A job
// that is already running does not use this copy.
function queuedAckReply(content: string, lang: string, wait: 'task' | 'keyboard' | 'ahead' = 'task'): string {
    const flat = content.replace(/\s+/g, ' ').trim();
    const task = flat.length > 16 ? `${flat.slice(0, 15)}…` : flat;
    if (wait === 'ahead') {
        if (lang === 'en') {
            return task
                ? `Got it — "${task}" is queued behind the earlier message.`
                : "Got it — it's queued behind the earlier message.";
        }
        if (lang === 'zh-Hant') {
            return task ? `收到，「${task}」記下了。先排在前面那條後面。` : '收到，記下了。先排在前面那條後面。';
        }
        return task ? `收到，「${task}」记下了。先排在前面那条后面。` : '收到，记下了。先排在前面那条后面。';
    }
    if (wait === 'keyboard') {
        if (lang === 'en') {
            return task
                ? `Got it — "${task}" is queued. I'll pick it up when you hand the keyboard back.`
                : "Got it — it's queued. I'll pick it up when you hand the keyboard back.";
        }
        if (lang === 'zh-Hant') {
            return task ? `收到，「${task}」記下了。等你把鍵盤交還後我再處理這個。` : '收到，記下了。等你把鍵盤交還後我再處理這個。';
        }
        return task ? `收到，「${task}」记下了。等你把键盘交还后我再处理这个。` : '收到，记下了。等你把键盘交还后我再处理这个。';
    }
    if (lang === 'en') {
        return task
            ? `Got it — "${task}" is queued. I'll pick it up the moment the current task is done.`
            : "Got it — it's queued, and I'll pick it up the moment the current task is done.";
    }
    if (lang === 'zh-Hant') {
        return task ? `收到，「${task}」記下了。等手頭這件做完我就來處理這個。` : '收到，記下了。等手頭這件做完我就來處理這個。';
    }
    return task ? `收到，「${task}」记下了。等手头这件做完我就来处理这个。` : '收到，记下了。等手头这件做完我就来处理这个。';
}

function messageID() {
    return `m-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

const DESKTOP_BOT_WATCH_INTERVAL_MS = 4000;

// One generation per open of the desktop stage. It starts at the clock so a
// reloaded page does not reuse a generation the previous page already closed.
let nextDesktopWatchEpoch = Date.now();
function takeDesktopWatchEpoch(): number {
    nextDesktopWatchEpoch += 1;
    return nextDesktopWatchEpoch;
}

function pendingReplyIsStale(message: DesktopBotMessage, now = Date.now()): boolean {
    return pendingBotReplyIsStale(message, now);
}

let desktopBotResultBound = false;

// Keep the instance result even if the bots page is closed when it arrives.
export function bindDesktopBotResultStore() {
    if (desktopBotResultBound) return;
    desktopBotResultBound = true;
    EventsOn('ai-assistant-response', handleDesktopBotResult);
    EventsOn('desktop-bot-view', handleDesktopBotView);
    EventsOn('desktop-bot-report', handleDesktopBotReport);
    EventsOn('desktop-bot-supplement-state', handleDesktopBotSupplementState);
}

// handleDesktopBotSupplementState receives one finished run's supplement
// outcomes and upgrades the receipts on screen. The payload's items carry the
// original words of missed items so they can be offered once more; the words
// stay in this bubble only and are never written anywhere else.
export function handleDesktopBotSupplementState(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    const botId = String(data.bot_id || data.BotID || '').trim() || (split > 0 ? sessionKey.slice(split + 1) : '');
    if (!botId) return;
    const userId = split > 0 ? sessionKey.slice(0, split) : '';
    if (!userId) return;
    const runId = String(data.run_id || data.RunID || '').trim();
    if (!runId) return;
    const items = Array.isArray(data.items) ? data.items as Array<{ id?: string; state?: string; content?: string }> : [];
    applySupplementOutcomes(userId, botId, runId, items);
}

export function handleDesktopBotView(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (!requestId.startsWith('desktop-bot-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const screen = screenOf(data);
    if (screen.cleared || (!screen.userControl && screen.reason === '')) return;
    notePendingBotDesktop(sessionKey.slice(0, split), sessionKey.slice(split + 1), {
        handoffUrl: screen.url,
        userControl: screen.userControl,
        attentionReason: screen.reason,
        reported: screen.reported,
        requestId,
    });
}

export function handleDesktopBotResult(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (!requestId.startsWith('desktop-bot-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const result = responseText(data, '后台没有返回结果。');
    const screen = screenOf(data);
    const attached = attachedFiles(data, result.failed);
    const resultUser = sessionKey.slice(0, split);
    const resultBot = sessionKey.slice(split + 1);
    settlePendingBotReply(resultUser, resultBot, {
        content: result.content,
        failed: result.failed,
        handoffUrl: screen.userControl || screen.reason ? screen.url : '',
        userControl: screen.userControl,
        attentionReason: screen.reason,
        askUser: askFrom(data),
        requestId,
        images: result.failed ? undefined : imagesFrom(data),
        shotPaths: result.failed ? undefined : shotPathsFrom(data),
        files: attached.files,
        localPaths: attached.localPaths,
    });
    reportDesktopBotCrossResult(resultUser, resultBot, result.content);
}

export function handleDesktopBotReport(payload: unknown) {
    let data: Record<string, unknown>;
    try {
        data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
    } catch {
        return;
    }
    if (!data || typeof data !== 'object') return;
    const schedule = data.schedule_report === true || data.ScheduleReport === true;
    const delivery = data.delivery_report === true || data.DeliveryReport === true;
    if (!schedule && !delivery) return;
    const requestId = String(data.request_id || data.RequestID || '');
    if (schedule && !requestId.startsWith('desktop-bot-sched-')) return;
    if (delivery && !requestId.startsWith('desktop-bot-deliver-')) return;
    const sessionKey = String(data.session_key || data.SessionKey || '');
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0) return;
    const result = responseText(data, '后台没有返回结果。');
    const screen = screenOf(data);
    const attached = attachedFiles(data, result.failed);
    appendDesktopBotReport(sessionKey.slice(0, split), sessionKey.slice(split + 1), {
        content: result.content,
        failed: result.failed,
        handoffUrl: screen.userControl || screen.reason ? screen.url : '',
        userControl: screen.userControl,
        attentionReason: screen.reason,
        askUser: askFrom(data),
        requestId,
        images: result.failed ? undefined : imagesFrom(data),
        shotPaths: result.failed ? undefined : shotPathsFrom(data),
        files: attached.files,
        localPaths: attached.localPaths,
    });
}

function imagesFrom(data: Record<string, unknown>) {
    return desktopBotImages(data.desktop_images ?? data.DesktopImages);
}

function shotPathsFrom(data: Record<string, unknown>) {
    return desktopBotLocalPaths(data.desktop_shot_paths ?? data.DesktopShotPaths);
}

const ChatShot = memo(function ChatShot({ src, alt, openLabel, onOpen }: { src: string; alt: string; openLabel: string; onOpen: (src: string, trigger: HTMLButtonElement) => void }) {
    return (
        <button
            type="button"
            className="desktop-bot-chat__shot-open"
            data-testid="desktop-bot-screenshot-open"
            title={openLabel}
            aria-label={openLabel}
            onClick={event => onOpen(src, event.currentTarget)}
        >
            <img
                className="desktop-bot-chat__shot"
                data-testid="desktop-bot-screenshot"
                alt={alt}
                decoding="async"
                draggable={false}
                src={src}
            />
        </button>
    );
});

// The data URL is built here, not in the workspace render. Typing in the
// composer re-renders that parent, and a screenshot's base64 is up to about
// a megabyte — this boundary skips the copy while the bytes are unchanged.
const InlineChatShot = memo(function InlineChatShot({ mime, data, alt, openLabel, onOpen }: { mime: string; data: string; alt: string; openLabel: string; onOpen: (src: string, trigger: HTMLButtonElement) => void }) {
    return <ChatShot src={`data:${mime};base64,${data}`} alt={alt} openLabel={openLabel} onOpen={onOpen} />;
});

// Above the bot page and the assistant image drawer; alert dialogs stay at 120000.
function BotShotPreview({ src, alt, closeLabel, onClose }: { src: string; alt: string; closeLabel: string; onClose: () => void }) {
    const closeRef = useRef<HTMLButtonElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { backdropProps } = useSafeBackdropDismiss(onClose);

    useEffect(() => {
        closeRef.current?.focus({ preventScroll: true });
        const onKey = (event: globalThis.KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopPropagation();
                onClose();
                return;
            }
            // The preview covers the page. Tab must not walk into the composer
            // or the bot list underneath it.
            if (event.key !== 'Tab') return;
            event.preventDefault();
            event.stopPropagation();
            const controls = Array.from(dialogRef.current?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') || []);
            if (controls.length === 0) return;
            const current = controls.indexOf(document.activeElement as HTMLButtonElement);
            const next = event.shiftKey
                ? controls[(current <= 0 ? controls.length : current) - 1]
                : controls[(current + 1) % controls.length];
            next.focus({ preventScroll: true });
        };
        document.addEventListener('keydown', onKey, true);
        const html = document.documentElement;
        const prev = { body: document.body.style.overflow, html: html.style.overflow };
        document.body.style.overflow = html.style.overflow = 'hidden';
        return () => {
            document.removeEventListener('keydown', onKey, true);
            document.body.style.overflow = prev.body;
            html.style.overflow = prev.html;
        };
    }, [onClose]);

    const overlay = (
        <div
            ref={dialogRef}
            className="desktop-bot-shot-preview"
            role="dialog"
            aria-modal="true"
            aria-label={alt}
            data-testid="desktop-bot-shot-preview"
            {...backdropProps}
        >
            <button
                ref={closeRef}
                type="button"
                className="desktop-bot-shot-preview__close"
                data-testid="desktop-bot-shot-preview-close"
                aria-label={closeLabel}
                title={closeLabel}
                onClick={onClose}
            >
                ×
            </button>
            <img
                className="desktop-bot-shot-preview__image"
                data-testid="desktop-bot-shot-preview-image"
                alt=""
                draggable={false}
                src={src}
            />
        </div>
    );
    if (typeof document === 'undefined') return overlay;
    return createPortal(overlay, document.body);
}

const BotShot = memo(function BotShot({ path, alt, openLabel, onOpen }: { path: string; alt: string; openLabel: string; onOpen: (src: string, trigger: HTMLButtonElement) => void }) {
    const [src, setSrc] = useState('');
    useEffect(() => {
        let live = true;
        void ReadDesktopBotShot(path).then(shot => {
            const record = shot as { mime?: unknown; MIME?: unknown; data?: unknown; Data?: unknown } | undefined;
            const mime = String(record?.mime || record?.MIME || '').trim().toLowerCase();
            const data = String(record?.data || record?.Data || '').replace(/\s+/g, '');
            if (!live || (mime !== 'image/png' && mime !== 'image/jpeg') || !data) return;
            setSrc(`data:${mime};base64,${data}`);
        }).catch(() => undefined);
        return () => { live = false; };
    }, [path]);
    if (!src) return null;
    return <ChatShot src={src} alt={alt} openLabel={openLabel} onOpen={onOpen} />;
});

function filesFrom(data: Record<string, unknown>) {
    return desktopBotFiles(data.desktop_files ?? data.DesktopFiles);
}

function localPathsFrom(data: Record<string, unknown>) {
    return desktopBotLocalPaths(data.local_file_paths ?? data.LocalFilePaths ?? data.local_file_path ?? data.LocalFilePath);
}

// A saved document replaces the in-bubble bytes. The card opens the local file.
function attachedFiles(data: Record<string, unknown>, failed: boolean) {
    if (failed) return { localPaths: undefined, files: undefined };
    const localPaths = localPathsFrom(data);
    return { localPaths, files: localPaths?.length ? undefined : filesFrom(data) };
}

function openBotFile(event: { preventDefault: () => void; stopPropagation: () => void }, filePath: string) {
    event.preventDefault();
    event.stopPropagation();
    void OpenFileOrShowInFolder(filePath).catch(() => ShowItemInFolder(filePath));
}

function saveDesktopFile(file: DesktopBotFile) {
    const binary = atob(file.data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    const blob = new Blob([bytes], { type: file.mime });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = file.name;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
}

function responseText(payload: Record<string, unknown>, emptyResult: string) {
    const text = String(payload.text || payload.Text || '').trim();
    const error = String(payload.error || payload.Error || '').trim();
    if (text) return { content: text, failed: false };
    if (error) return { content: error, failed: true };
    return { content: emptyResult, failed: true };
}

type PendingSend = { botId: string; messageId: string; settled: boolean; admitted: boolean };

// The desktop panel follows the user's intent. 'auto' is set by live events
// so the stage stops flashing away, 'user' by the identity chip / 接管, and
// 'off' by 收起 — an auto event never reopens a panel the person collapsed.
type DesktopPanelState = 'auto' | 'user' | 'off' | undefined;
type PanelSection = 'details' | 'computer' | 'schedules';

function requestOwnsStoredMessage(userId: string, sessionKey: string, requestId: string): boolean {
    if (!requestId) return false;
    const split = sessionKey.lastIndexOf(':');
    const botId = split >= 0 ? sessionKey.slice(split + 1) : sessionKey;
    if (!botId) return false;
    return messagesForBot(userId, botId).some(item => item.requestId === requestId);
}

function sessionBotId(sessionKey: string, userId: string): string {
    const split = sessionKey.lastIndexOf(':');
    if (split <= 0 || sessionKey.slice(0, split) !== userId) return '';
    return sessionKey.slice(split + 1);
}

function botInitial(title: string): string {
    const clean = title.trim();
    if (!clean) return 'B';
    return clean.slice(0, 1).toUpperCase();
}

function botColor(title: string): string {
    let hash = 0;
    for (const ch of title) hash = (hash * 31 + ch.charCodeAt(0)) % 360;
    return `hsl(${hash}deg 62% 46%)`;
}

export function formatCreated(ts: number): string {
    if (!ts) return '';
    const date = new Date(ts);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// Hub sends Bot.created_at as RFC3339. An empty or unreadable value stays blank
// instead of pretending the bot was created when this page loaded.
function createdMillis(value: string | undefined): number {
    const parsed = Date.parse(String(value || '').trim());
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

// The old list track grew to 360px. The default is five sixths of that.
const LIST_WIDTH_DEFAULT = 300;
const LIST_WIDTH_MIN = 240;
const LIST_WIDTH_MAX = 480;
const LIST_WIDTH_KEY = 'maclaw.desktopBotListWidth.v1';
const LIST_PANEL_BREAKPOINT = 1181;
const LIST_PANEL_WIDTH = 400;

function clampListWidth(value: number, containerWidth = 0, panelBeside = false): number {
    const numeric = Number(value);
    let next = Number.isFinite(numeric) ? numeric : LIST_WIDTH_DEFAULT;
    let max = LIST_WIDTH_MAX;
    if (containerWidth > 0) {
        // Leave the conversation a readable column. The profile panel only
        // takes a real column once the viewport matches the wide-panel rule.
        const chatMin = containerWidth <= 960 ? 240 : 320;
        const reserved = panelBeside ? LIST_PANEL_WIDTH : 0;
        max = Math.min(max, Math.max(LIST_WIDTH_MIN, containerWidth - reserved - chatMin));
    }
    return Math.round(Math.min(max, Math.max(LIST_WIDTH_MIN, next)));
}

// The wide-panel rule is a viewport media query. The workspace is narrower
// than the viewport by the nav rail, so its own width cannot decide this.
function botListPanelBeside(open: boolean): boolean {
    if (!open || typeof window.matchMedia !== 'function') return false;
    return window.matchMedia(`(min-width: ${LIST_PANEL_BREAKPOINT}px)`).matches;
}

// clientX is in viewport pixels. The shell scales the layout with
// transform: scale(var(--ui-scale)), so a raw delta overshoots the pointer.
function pointerToLayout(el: HTMLElement | null): number {
    if (!el) return 1;
    const layout = el.clientWidth;
    const visual = el.getBoundingClientRect().width;
    if (!(layout > 0) || !(visual > 0)) return 1;
    const scale = visual / layout;
    return scale > 0 ? scale : 1;
}

// The identity chip starts centered. A drag stores its place as a fraction of
// the chat column, so the next open and a resized window keep that place.
const IDENTITY_POS_KEY = 'maclaw.desktopBotIdentityPos.v1';
const IDENTITY_MARGIN = 8;
const IDENTITY_DRAG_SLOP = 4;

type IdentityPos = { nx: number; ny: number };

function readIdentityPos(): IdentityPos | null {
    try {
        const raw = window.localStorage.getItem(IDENTITY_POS_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as { nx?: unknown; ny?: unknown };
        const nx = Number(parsed?.nx);
        const ny = Number(parsed?.ny);
        if (!Number.isFinite(nx) || !Number.isFinite(ny)) return null;
        return { nx: Math.min(1, Math.max(0, nx)), ny: Math.min(1, Math.max(0, ny)) };
    } catch {
        return null;
    }
}

function identityTravel(chat: HTMLElement, chip: HTMLElement) {
    const width = chat.clientWidth;
    const height = chat.clientHeight;
    const scale = pointerToLayout(chat);
    // The composer stays reachable. Its border box is read in the same layout
    // pixels as the chip's top. offsetTop follows a different offsetParent once
    // an ancestor is transformed, and would let the chip sit on the send box.
    // An unlaid-out composer (height 0) is not a limit. A short column still is:
    // the chip then stays at the top margin instead of covering the send box.
    let limitBottom = height;
    const form = chat.querySelector('.desktop-bot-chat__form');
    if (form instanceof HTMLElement && scale > 0) {
        const chatRect = chat.getBoundingClientRect();
        const formRect = form.getBoundingClientRect();
        if (formRect.height > 0 && chatRect.height > 0) {
            const formTop = (formRect.top - chatRect.top) / scale;
            if (Number.isFinite(formTop)) limitBottom = Math.min(height, Math.max(0, formTop));
        }
    }
    return {
        scale,
        width,
        height,
        maxX: Math.max(0, width - chip.offsetWidth - IDENTITY_MARGIN * 2),
        maxY: Math.max(0, limitBottom - chip.offsetHeight - IDENTITY_MARGIN * 2),
    };
}

function identityPixels(pos: IdentityPos, travel: { maxX: number; maxY: number }) {
    return {
        left: IDENTITY_MARGIN + pos.nx * travel.maxX,
        top: IDENTITY_MARGIN + pos.ny * travel.maxY,
    };
}

function identityFraction(left: number, top: number, travel: { maxX: number; maxY: number }): IdentityPos {
    const nx = travel.maxX > 0 ? (left - IDENTITY_MARGIN) / travel.maxX : 0;
    const ny = travel.maxY > 0 ? (top - IDENTITY_MARGIN) / travel.maxY : 0;
    return { nx: Math.min(1, Math.max(0, nx)), ny: Math.min(1, Math.max(0, ny)) };
}

function RowActionIcon({ name }: { name: 'rename' | 'remove' }) {
    const stroke = {
        fill: 'none',
        stroke: 'currentColor',
        strokeWidth: 1.6,
        strokeLinecap: 'round' as const,
        strokeLinejoin: 'round' as const,
    };
    return (
        <svg className="desktop-bot-workspace__action-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            {name === 'rename' && (
                <>
                    <path {...stroke} d="M9.2 2.8 13.2 6.8 6.2 13.8H2.2V9.8L9.2 2.8Z" />
                    <path {...stroke} d="m8 4 4 4" />
                </>
            )}
            {name === 'remove' && (
                <>
                    <path {...stroke} d="M3 4.2h10" />
                    <path {...stroke} d="M6.2 4.1V2.7h3.6v1.4" />
                    <path {...stroke} d="m4.2 4.2.6 9.2h6.4l.6-9.2" />
                </>
            )}
        </svg>
    );
}

function readListWidth(): number {
    try {
        const raw = window.localStorage.getItem(LIST_WIDTH_KEY);
        if (raw == null || raw.trim() === '') return LIST_WIDTH_DEFAULT;
        return clampListWidth(Number(raw));
    } catch {
        return LIST_WIDTH_DEFAULT;
    }
}

export function DesktopBotWorkspace({ lang, userId }: { lang: string; userId: string }) {
    const text = copy(lang);
    const { showConfirm } = useDialog();
    const [bots, setBots] = useState<DesktopBot[]>([]);
    const botsRef = useRef(bots);
    botsRef.current = bots;
    const pendingCrossNotes = useRef<Array<Record<string, unknown>>>([]);
    const landCrossRef = useRef<(notes: Array<Record<string, unknown>>) => void>(() => undefined);
    const loadGen = useRef(0);
    // Local writes that happen while a list request is in flight. The response
    // is still applied; these overlays keep a create, rename, or delete that
    // the in-flight snapshot has not seen yet.
    const pendingBots = useRef<DesktopBot[]>([]);
    const hiddenBotIds = useRef<Set<string>>(new Set());
    const renamedBots = useRef<Map<string, { title: string; description: string }>>(new Map());
    const [selectedId, setSelectedId] = useState('');
    const userIdRef = useRef(userId);
    userIdRef.current = userId;
    // Non-zero while a confirm dialog or its delete request is in progress.
    // The value is the ticket, so a later account switch can clear the lock
    // without the older request unlocking the new account's delete.
    const removeLock = useRef(0);
    const removeSeq = useRef(0);
    const scheduleDeleteLock = useRef(false);
    const scheduleLoadGen = useRef(0);
    // Edit-bot dialog. Drafts live inside the dialog; the parent holds the
    // bot being edited so the row never mutates into a form.
    const [editBot, setEditBot] = useState<DesktopBot | null>(null);
    const [editSaving, setEditSaving] = useState(false);
    const [editNotice, setEditNotice] = useState('');
    // Model dialog. The choice is per account, not per bot, which is why it is
    // opened from the list header rather than from a row.
    const [llmOpen, setLlmOpen] = useState(false);
    const [creditReadout, setCreditReadout] = useState<DesktopCreditReadout | null>(null);
    const [creditRoute, setCreditRoute] = useState<'default' | 'custom'>('default');
    const [customModelDetailText, setCustomModelDetailText] = useState('');
    const [creditEpoch, setCreditEpoch] = useState(0);
    const creditLoadRef = useRef(0);
    const creditUserRef = useRef(userId);
    const creditSpentRef = useRef<Set<string>>(new Set());
    const [llmProviders, setLlmProviders] = useState<BotLLMProviderOption[]>([]);
    const [llmMode, setLlmMode] = useState<'default' | 'custom'>('default');
    const [llmName, setLlmName] = useState('');
    const [llmModel, setLlmModel] = useState('');
    const [llmSaving, setLlmSaving] = useState(false);
    const [llmNotice, setLlmNotice] = useState('');
    const [messagesByBot, setMessagesByBot] = useState<Record<string, DesktopBotMessage[]>>({});
    const [command, setCommand] = useState('');
    const [listNotice, setListNotice] = useState('');
    const [askingByBot, setAskingByBot] = useState<Record<string, string>>({});
    const [activityByBot, setActivityByBot] = useState<Record<string, { text: string; runId: string }>>({});
    const [listFailed, setListFailed] = useState(false);
    // The picture follows the panel, not the desktop address. 'auto' is a screen
    // handoff, 'user' is the identity chip / 人类接管, 'off' is 收起. Unset stays closed.
    const [desktopPanels, setDesktopPanels] = useState<Record<string, Exclude<DesktopPanelState, undefined>>>( {});
    // Last section this bot left open. Unset opens on 电脑. 详情 and 定时任务 are restored.
    const [sectionByBot, setSectionByBot] = useState<Record<string, PanelSection>>({});
    const [schedulesByBot, setSchedulesByBot] = useState<Record<string, DesktopBotScheduleInfo[]>>({});
    const [scheduleNotice, setScheduleNotice] = useState('');
    const [personaByBot, setPersonaByBot] = useState<Record<string, string>>({});
    const [liveDesktop, setLiveDesktop] = useState<Record<string, LiveDesktop | undefined>>({});
    const [screenByBot, setScreenByBot] = useState<Record<string, BotScreen>>({});
    const [screenPick, setScreenPick] = useState('mine');
    const [screenPickTouched, setScreenPickTouched] = useState(false);
    const [handoffNotice, setHandoffNotice] = useState<{ botId: string; reason: string; url: string } | null>(null);
    const [desktopAdmin, setDesktopAdmin] = useState(false);
    const [modeSaving, setModeSaving] = useState(false);
    const [handingBack, setHandingBack] = useState(false);
    const [handBackError, setHandBackError] = useState('');
    const [modeError, setModeError] = useState('');
    const [takeoverByBot, setTakeoverByBot] = useState<Record<string, boolean>>({});
    // A handoff the person accepted from the chat. The bot asked them to take
    // the computer, so a still-open run must not take the keyboard back.
    const [takeoverHoldByBot, setTakeoverHoldByBot] = useState<Record<string, boolean>>({});
    // A schedule uses that bot's own desktop and has no chat bubble. A positive
    // count means that bot's occurrence is still inside relay.
    const [desktopBusyByBot, setDesktopBusyByBot] = useState<Record<string, number>>({});
    // Chat turns still on the keyboard. A stale bubble is not enough to know
    // the run ended, and a run that asked the person is absent here.
    const [desktopRunByBot, setDesktopRunByBot] = useState<Record<string, boolean>>({});
    // The send closure can be older than the latest busy event. The ref is the
    // gate; the state is what the page paints.
    const desktopRunByBotRef = useRef<Record<string, boolean>>({});
    const drainDeskRef = useRef<(botId: string, fromList?: boolean) => void>(() => undefined);
    const drainAllDeskRef = useRef<() => void>(() => undefined);
    const noteQueuedRef = useRef<(botId: string) => void>(() => undefined);
    const langRef = useRef(lang);
    langRef.current = lang;
    // Seq is the process-wide hold generation. A snapshot older than the last
    // applied event must not put a finished schedule back on the keyboard.
    const busySeqRef = useRef(0);
    const busyFloorRef = useRef(0);
    const runEpochRef = useRef(0);
    // Picture-only fullscreen. It does not take the keyboard. Takeover stays
    // the control that does.
    const [screenExpandedByBot, setScreenExpandedByBot] = useState<Record<string, boolean>>({});
    const [secretPrompt, setSecretPrompt] = useState<SecretPrompt | null>(null);
    const [secretDraft, setSecretDraft] = useState('');
    const [shotPreview, setShotPreview] = useState<{ userId: string; botId: string; src: string } | null>(null);
    const shotTriggerRef = useRef<HTMLButtonElement | null>(null);
    // Drop a picture that belongs to another account or bot during this render.
    // An effect would paint the previous screenshot over the new chat for a frame.
    if (shotPreview && (shotPreview.userId !== userId || shotPreview.botId !== selectedId)) {
        setShotPreview(null);
        shotTriggerRef.current = null;
    }
    const openShot = useCallback((src: string, trigger: HTMLButtonElement) => {
        shotTriggerRef.current = trigger;
        setShotPreview({ userId, botId: selectedId, src });
    }, [userId, selectedId]);
    const closeShot = useCallback(() => {
        const trigger = shotTriggerRef.current;
        shotTriggerRef.current = null;
        setShotPreview(null);
        if (trigger?.isConnected) trigger.focus({ preventScroll: true });
    }, []);
    const visibleShot = shotPreview && shotPreview.userId === userId && shotPreview.botId === selectedId ? shotPreview.src : '';
    const [retryByBot, setRetryByBot] = useState<Record<string, boolean>>({});
    const [preferredListWidth, setPreferredListWidth] = useState(readListWidth);
    const [listContainerWidth, setListContainerWidth] = useState(0);
    const [listResizing, setListResizing] = useState(false);
    const pendingRef = useRef<Map<string, PendingSend>>(new Map());
    const panelsRef = useRef(desktopPanels);
    panelsRef.current = desktopPanels;
    const liveRef = useRef(liveDesktop);
    liveRef.current = liveDesktop;
    // Same handoff stays latched until a poll reports the keyboard back and an
    // empty attention reason. A later true is the next handoff.
    const latchRef = useRef<Record<string, boolean>>({});
    const latchRequestRef = useRef<Record<string, string>>({});
    const secretHandled = useRef<Set<string>>(new Set());
    const pendingFitFocus = useRef<'expand' | 'restore' | null>(null);
    const expandBtnRef = useRef<HTMLButtonElement>(null);
    const restoreBtnRef = useRef<HTMLButtonElement>(null);
    const handoffRef = useRef<HTMLIFrameElement>(null);
    const stageRef = useRef<HTMLDivElement>(null);
    const workspaceRef = useRef<HTMLElement>(null);
    const preferredListWidthRef = useRef(preferredListWidth);
    const listDragRef = useRef<{ x: number; width: number; container: number; panelBeside: boolean; scale: number; moved: boolean; origin: number } | null>(null);
    const [identityPos, setIdentityPos] = useState<IdentityPos | null>(readIdentityPos);
    const [identityDragging, setIdentityDragging] = useState(false);
    const identityPosRef = useRef(identityPos);
    const chatRef = useRef<HTMLElement>(null);
    const logRef = useRef<HTMLDivElement>(null);
    const logBodyRef = useRef<HTMLDivElement>(null);
    // True while the reader is on the latest line. Moving up, away from that
    // line, clears it. A click in the log does not.
    const followLogRef = useRef(true);
    const pinningLogRef = useRef(false);
    const logScrollTopRef = useRef(0);
    const logResizeFrameRef = useRef(0);
    const logObserverRef = useRef<ResizeObserver | null>(null);
    const openBotForScrollRef = useRef<string | null>(null);
    const pinLogRef = useRef(() => {});
    const applyFollowRef = useRef<(follow: boolean) => void>(() => {});
    const identityRef = useRef<HTMLElement>(null);
    const identitySuppressClick = useRef(false);
    const identityDragRef = useRef<{ pointerId: number; startX: number; startY: number; originLeft: number; originTop: number; moved: boolean; scale: number } | null>(null);
    const secretOpenRef = useRef(false);
    secretOpenRef.current = secretPrompt !== null;
    const watchMisses = useRef<Record<string, number>>({});
    const watchEpochByBot = useRef<Record<string, number>>({});
    // The stage watches one bot at a time. A newer open replaces this before
    // the previous bot's release runs, so two open bots keep the shared desktop.
    const stageEpoch = useRef(0);
    const selected = bots.find(bot => bot.id === selectedId) || null;
    const messages = selected ? (messagesByBot[selected.id] || []) : [];
    // pendingRef is the turn this process admitted. Message age is only the
    // escape hatch after a reload, when that map is empty.
    const turnLive = (botId: string) =>
        [...pendingRef.current.values()].some(value => value.botId === botId && !value.settled);
    const botPending = (botId: string) => {
        if (turnLive(botId)) return true;
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        return items.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message));
    };
    const busyPhase = (botId: string, items: DesktopBotMessage[]): BotPhase | '' => {
        const live = turnLive(botId);
        const active = (message: DesktopBotMessage) =>
            message.role === 'assistant' && message.pending && (live || !pendingReplyIsStale(message));
        if (items.some(message => active(message) && message.phase === 'execute')) return 'execute';
        if (items.some(active)) return 'plan';
        if (live) return 'plan';
        return '';
    };
    const running = !!selected && botPending(selected.id);
    const handoffMessage = [...messages].reverse().find(message => message.userControl || !!message.attentionReason);
    // Only the latest assistant reply can ask for the computer. An older
    // QR sentence stays in the transcript after the task has moved on.
    const takeoverMessage = (() => {
        for (let i = messages.length - 1; i >= 0; i--) {
            const message = messages[i];
            if (message.role !== 'assistant') continue;
            if (message.pending && !(message.content || '').trim()) return undefined;
            return messageNeedsComputerTakeover(message) ? message : undefined;
        }
        return undefined;
    })();
    const userHasControl = !!handoffMessage;
    const live = selected ? liveDesktop[selected.id] : undefined;
    const liveControl = live?.userControl === true;
    const liveReason = live?.attentionReason || '';
    // A turn is driving when its current bubble still holds the keyboard.
    // A handoff on that bubble means the person has the keys. A live flag
    // left from an older poll does not: the bubble is the turn.
    const botDrivingDesktop = (botId: string) => {
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        const liveTurn = turnLive(botId);
        const view = liveDesktop[botId];
        const liveHandoff = view?.userControl === true || (view?.attentionReason || '') !== '';
        const personHolds = items.some(message => message.userControl || !!message.attentionReason);
        for (let i = items.length - 1; i >= 0; i--) {
            const message = items[i];
            if (message.role !== 'assistant' || !message.pending) continue;
            if (!liveTurn && pendingReplyIsStale(message)) continue;
            if (message.userControl || message.attentionReason) return false;
            // A second job already going does not take a login's keyboard.
            // A pending bubble left from a reload still does: nobody on this
            // page admitted it beside the handoff.
            const id = message.requestId || '';
            const slot = id ? pendingRef.current.get(id) : undefined;
            if (personHolds && slot && slot.botId === botId && slot.admitted && !slot.settled) continue;
            return true;
        }
        if (personHolds) return false;
        return liveTurn && !liveHandoff;
    };
    // The latest assistant turn, skipping a stale pending bubble left from a
    // reload. An older login further up the transcript is not this turn.
    const latestAssistantTurn = (botId: string) => {
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        const liveTurn = turnLive(botId);
        for (let i = items.length - 1; i >= 0; i--) {
            const message = items[i];
            if (message.role !== 'assistant') continue;
            if (message.pending && !liveTurn && pendingReplyIsStale(message)) continue;
            return message;
        }
        return undefined;
    };
    // Each bot has its own desktop. Takeover closes only while this bot is
    // using it. A schedule still in relay wins over a login left in the
    // transcript: that count is the schedule, and it is cleared when the
    // schedule hands the keys over. A run mark does not win once this turn
    // has handed the keys to the person. Another bot stays on its own display.
    const botUsingDesktop = (botId: string) => {
        if (!botId) return false;
        if (botDrivingDesktop(botId)) return true;
        if ((desktopBusyByBot[botId] || 0) > 0) return true;
        const view = liveDesktop[botId];
        if (view?.userControl === true || (view?.attentionReason || '') !== '') return false;
        const latest = latestAssistantTurn(botId);
        if (latest && (latest.userControl || latest.attentionReason)) return false;
        return desktopRunByBot[botId] === true;
    };
    const agentOperating = !!selected && botUsingDesktop(selected.id);
    const takeoverHeld = !!selected && !!takeoverHoldByBot[selected.id];
    const takeover = !!selected && (takeoverHeld || (!!takeoverByBot[selected.id] && !agentOperating));
    const screenExpanded = !!selected && !!screenExpandedByBot[selected.id] && !takeover;
    const panelState = selected ? desktopPanels[selected.id] : undefined;
    const showDesktop = panelState === 'auto' || panelState === 'user';
    // Updated during render, so a watch cleanup sees the stage that this
    // commit left on screen. A switch to another open bot leaves it true.
    const stageShownRef = useRef(false);
    stageShownRef.current = !!(selectedId && showDesktop);
    const listPanelBeside = botListPanelBeside(showDesktop);
    // The pointer handler writes the ref and the CSS variable on each move.
    // Reading the ref while a drag is active keeps a watch poll from painting
    // the width from before that drag.
    const listWidth = clampListWidth(listResizing ? preferredListWidthRef.current : preferredListWidth, listContainerWidth, listPanelBeside);
    const listWidthMax = clampListWidth(LIST_WIDTH_MAX, listContainerWidth, listPanelBeside);
    // The picture belongs to the user's desktop, not to one bot. Switching to
    // another bot whose stage is already open must keep this iframe: waiting
    // for that bot's own poll unmounts noVNC and the picture drops.
    let sharedOpenUrl = '';
    if (showDesktop && selected && !live?.url) {
        for (const [id, panel] of Object.entries(desktopPanels)) {
            if (id === selected.id || (panel !== 'auto' && panel !== 'user')) continue;
            const url = liveDesktop[id]?.url || '';
            // An isolated bot's picture is not the shared desktop. Borrowing it
            // paints the wrong screen while this bot's own poll is still starting.
            if (url && screenByBot[id]?.mode !== 'isolated') {
                sharedOpenUrl = url;
                break;
            }
        }
    }
    const desktopUrl = showDesktop ? (live?.url || sharedOpenUrl) : '';
    const screenPickNow = effectiveScreenPick(screenPickTouched, screenPick, selected?.id || '', screenByBot[selected?.id || '']?.mode);
    const shownDesktopUrl = showDesktop ? shownScreenURL(screenPickNow, selected?.id || '', bots, screenByBot, desktopUrl) : '';
    const holdBotId = screenNeedsOwnHold(screenPickNow, selectedId, screenByBot, bots);
    // "My desktop" is missing only for an isolated bot: a shared bot's "mine"
    // is its own display and already has retry/loading through its own poll.
    // While a shared-display hold is being fetched, it is still connecting,
    // not missing.
    const ownerDesktopMissing = screenPickNow === 'mine' && !!selected && screenByBot[selected.id]?.mode === 'isolated' && !holdBotId && bots.length > 0 && bots.every(bot => !!screenByBot[bot.id]) && mineScreenURL(bots, screenByBot) === '';
    const storedSection = selected ? sectionByBot[selected.id] : undefined;
    const panelSection: PanelSection = storedSection === 'details' || storedSection === 'schedules' ? storedSection : 'computer';
    const retryForSelected = !!selected && retryByBot[selected.id] === true;
    // Voluntary viewing stays view-only while this bot is using its desktop.
    // A handoff keeps the keyboard when this bot is not using it.
    const botAskedForDesktop = (botId: string) => {
        if (!botId) return false;
        if (handoffNotice?.botId === botId) return true;
        const view = liveDesktop[botId];
        if (view?.userControl || view?.attentionReason) return true;
        return messagesForBot(userId, botId).some(item => item.userControl || !!item.attentionReason);
    };
    // The chat bot's keyboard belongs to the screen this chat is holding.
    // Another isolated display is its own keyboard, so a login there accepts
    // clicks on its own.
    const otherScreen = screenPickNow !== 'mine' && selected && screenPickNow !== selected.id ? screenPickNow : '';
    // Two missed polls mean the display this chat is holding is down. The
    // screen list can still be holding the last address, and that address
    // would keep the iframe up instead of the retry card. "My desktop" on an
    // isolated bot is a different display, so a failure here leaves it up.
    const viewingThisDisplay = !!selected && (screenPickNow === selected.id || (screenPickNow === 'mine' && screenByBot[selected.id]?.mode !== 'isolated'));
    const ownPictureDown = retryForSelected && viewingThisDisplay;
    const otherIsolated = !!otherScreen && screenByBot[otherScreen]?.mode === 'isolated';
    const otherHandoff = otherIsolated && botAskedForDesktop(otherScreen);
    const shownSharedScreen = screenPickNow === 'mine' || screenByBot[screenPickNow]?.mode === 'shared';
    // The handoff live flags belong to the display the selected bot holds —
    // the shared desktop for a shared bot, its own display for an isolated
    // one. They must not unlock a different display that happens to be up.
    const liveOnShownScreen = !!selected && (screenByBot[selected.id]?.mode === 'isolated' ? screenPickNow === selected.id : shownSharedScreen);
    const stageInteractive = showDesktop && (otherIsolated
        ? otherHandoff
        : !agentOperating && (((liveControl || liveReason !== '') && liveOnShownScreen) || takeover));
    // Watch and control are different noVNC documents. Reusing the iframe leaves
    // the first page's viewOnly in place, so takeover looks active and clicks do nothing.
    const frameMode = stageInteractive ? 'live' : 'watch';
    const statusPhase = selected ? busyPhase(selected.id, messages) : '';
    const statusText = statusPhase === 'execute' ? text.workingTag : statusPhase === 'plan' ? text.arrangingTag : text.idleTag;

    // Looking means this conversation is on screen and the window is in front.
    // The right-hand tabs keep the chat mounted, so the selected bot is enough.
    // Focus and blur own the foreground flag. document.hasFocus() is false
    // while the desktop frame has the keyboard, and that frame is still this
    // window. hasFocus() may raise the flag when this page opens in a window
    // that is already in front; it does not lower the flag. Leaving the page,
    // the window, or this bot stops the watch.
    useEffect(() => {
        const sync = () => {
            watchBotReplies(userId, botWindowIsForeground() && selectedId !== '' ? selectedId : null);
        };
        if (document.hasFocus()) setBotWindowForeground(true);
        sync();
        const onFocus = () => {
            setBotWindowForeground(true);
            sync();
        };
        const onBlur = () => {
            setBotWindowForeground(false);
            sync();
        };
        window.addEventListener('focus', onFocus);
        window.addEventListener('blur', onBlur);
        document.addEventListener('visibilitychange', sync);
        return () => {
            window.removeEventListener('focus', onFocus);
            window.removeEventListener('blur', onBlur);
            document.removeEventListener('visibilitychange', sync);
            watchBotReplies(userId, null);
        };
    }, [userId, selectedId]);

    // A supplement receipt whose run ended while this page was closed never
    // saw the push event. One pull per run per session asks the server for the
    // final state and settles the bubble; a failed pull leaves the key out so
    // a later render can ask again.
    const supplementPullsDone = useRef<Set<string>>(new Set());
    useEffect(() => {
        if (!userId || !selectedId) return;
        const runIds = new Set<string>();
        for (const item of messagesForBot(userId, selectedId)) {
            if (!item.supplementId || item.supplementOutcome) continue;
            const runId = String(item.supplementRunId || '').trim();
            if (runId !== '') runIds.add(runId);
        }
        for (const runId of runIds) {
            const key = `${userId}:${selectedId}:${runId}`;
            if (supplementPullsDone.current.has(key)) continue;
            supplementPullsDone.current.add(key);
            void GetDesktopBotSupplements(selectedId, runId).then(items => {
                if (Array.isArray(items) && items.length > 0) applySupplementOutcomes(userId, selectedId, runId, items);
            }).catch(() => {
                supplementPullsDone.current.delete(key);
            });
        }
    }, [userId, selectedId, messagesByBot]);

    useLayoutEffect(() => {
        const el = workspaceRef.current;
        if (!el) return;
        const measure = () => {
            const next = el.clientWidth;
            setListContainerWidth(width => width === next ? width : next);
        };
        measure();
        if (typeof ResizeObserver === 'undefined') {
            window.addEventListener('resize', measure);
            return () => window.removeEventListener('resize', measure);
        }
        const observer = new ResizeObserver(measure);
        observer.observe(el);
        return () => observer.disconnect();
    }, [showDesktop]);

    useEffect(() => {
        if (!listResizing) return;
        const previousCursor = document.body.style.cursor;
        const previousSelect = document.body.style.userSelect;
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        return () => {
            document.body.style.cursor = previousCursor;
            document.body.style.userSelect = previousSelect;
        };
    }, [listResizing]);

    const rememberMessages = (botId: string, next: DesktopBotMessage[]) => {
        saveBotMessages(userId, botId, next);
        setMessagesByBot(prev => ({ ...prev, [botId]: next }));
    };
    // Storage is written synchronously on every mutation, so reads always come
    // from the store; state is just the mirror. Never mutate first and save
    // from inside a state updater — the updater may run later than the write.
    const commit = (botId: string, next: DesktopBotMessage[]) => {
        rememberMessages(botId, next);
    };
    noteQueuedRef.current = (botId: string) => {
        const owner = userIdRef.current;
        const stored = messagesForBot(owner, botId);
        commit(botId, [...stored, { id: messageID(), role: 'assistant', content: desktopBotQueuedSentence(langRef.current), ack: true }]);
    };
    // The model writes the identity. A single hyphen withdraws it. The stored
    // text is what the next understanding call reads, not a phrase list.
    const keepPersona = (owner: string, botId: string, raw: string) => {
        const text = raw.trim();
        if (!text) return;
        if (text === '-') forgetBotPersona(owner, botId);
        else saveBotPersona(owner, botId, text);
        if (userIdRef.current !== owner) return;
        setPersonaByBot(prev => {
            if (text === '-') {
                if (!prev[botId]) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            }
            if (prev[botId] === text) return prev;
            return { ...prev, [botId]: text };
        });
    };
    // The live poll is the keyboard. A stored handoff flag that outlives it
    // keeps 登录完成 on screen and tells the person to type into a view-only frame.
    // Another job of this bot is still running after the keyboard was handed
    // over. A quiet desktop poll from that job is not the login ending.
    const handoffHeldBesideSibling = (botId: string) => {
        if (!messagesForBot(userId, botId).some(item => item.userControl || !!item.attentionReason)) return false;
        let live = 0;
        for (const value of pendingRef.current.values()) {
            if (value.botId === botId && !value.settled && value.admitted) live += 1;
        }
        return live > 1;
    };
    const clearStoredHandoff = (botId: string) => {
        const stored = messagesForBot(userId, botId);
        if (!stored.some(item => item.userControl || item.attentionReason)) return;
        commit(botId, stored.map(item => (
            item.userControl || item.attentionReason ? { ...item, userControl: false, attentionReason: '' } : item
        )));
        // The keyboard is back, so a message that was waiting out this handoff
        // can run. drainIfIdle no-ops while this bot is still in a turn.
        drainIfIdle(botId);
    };

    // The desktop panel follows the user's intent. 'auto' is set by live events
    // so the stage stops flashing away, 'user' by the identity chip / 接管, and
    // 'off' by 收起. An auto event never reopens a panel the person collapsed —
    // only a keyboard-holding handoff (登录/接管) is an attention request that
    // forces its way back, and a takeover click always opens it explicitly.
    const openPanel = (botId: string, state: Exclude<DesktopPanelState, undefined>, force = false) => {
        setDesktopPanels(prev => {
            const current = prev[botId];
            if (current === 'user' && state === 'auto') return prev;
            if (!force && state === 'auto' && current === 'off') return prev;
            if (current === state) return prev;
            return { ...prev, [botId]: state };
        });
    };
    const showComputer = (botId: string) => {
        setSectionByBot(prev => prev[botId] === 'computer' ? prev : { ...prev, [botId]: 'computer' });
    };
    const closePanel = (botId: string) => {
        setDesktopPanels(prev => {
            if (prev[botId] === 'off') return prev;
            return { ...prev, [botId]: 'off' };
        });
        setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        setTakeoverHoldByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
    };
    const clearLiveFlags = (botId: string) => {
        setLiveDesktop(prev => {
            const existing = prev[botId];
            if (!existing || (!existing.userControl && !existing.attentionReason)) return prev;
            return { ...prev, [botId]: { ...existing, userControl: false, attentionReason: '' } };
        });
    };
    const syncLive = (botId: string, view: { url?: string; userControl?: boolean; attentionReason?: string } | null | undefined) => {
        if (view === null || view === undefined) return;
        const url = String(view.url || '');
        if (!url) return;
        const userControl = view.userControl === true;
        const attentionReason = view.attentionReason || '';
        setLiveDesktop(prev => {
            const existing = prev[botId];
            if (existing && existing.url === url && existing.userControl === userControl && existing.attentionReason === attentionReason) return prev;
            return { ...prev, [botId]: { url, userControl, attentionReason } };
        });
    };
    // A URL by itself never opens the stage. Force-open is the rising edge of
    // one handoff. While that handoff is latched, a collapsed panel ignores
    // the same keyboard report and does not write the live flags back.
    const applyScreen = (botId: string, screen: ScreenEvent, requestId = '') => {
        if (!botId) return;
        const foreignQuiet = requestId !== '' && latchRequestRef.current[botId] !== undefined && latchRequestRef.current[botId] !== '' && requestId !== latchRequestRef.current[botId] && handoffHeldBesideSibling(botId);
        // This run's picture ended. Another job of this bot may still be on
        // the same desktop, so the stage stays until that job is done too.
        const anotherAdmittedTurn = requestId === ''
            ? false
            : [...pendingRef.current.entries()].some(([key, value]) => value.botId === botId && !value.settled && value.admitted && key !== requestId);
        if (screen.cleared) {
            if (foreignQuiet || anotherAdmittedTurn) return;
            latchRef.current[botId] = false;
            delete latchRequestRef.current[botId];
            setLiveDesktop(prev => prev[botId] ? { ...prev, [botId]: undefined } : prev);
            setDesktopPanels(prev => prev[botId] === 'user' ? prev : { ...prev, [botId]: 'off' });
            setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            return;
        }
        const attention = screen.userControl || screen.reason !== '';
        const latched = latchRef.current[botId] === true;
        if (screen.reported && !screen.userControl && screen.reason === '' && latched) {
            if (foreignQuiet) return;
            latchRef.current[botId] = false;
            delete latchRequestRef.current[botId];
            setHandoffNotice(prev => prev?.botId === botId ? null : prev);
            const panel = panelsRef.current[botId];
            if (panel === 'auto' || panel === 'user') {
                const url = screen.url || liveRef.current[botId]?.url || '';
                if (url) syncLive(botId, { url, userControl: false, attentionReason: '' });
                else clearLiveFlags(botId);
            }
            clearStoredHandoff(botId);
            return;
        }
        if (!attention) return;
        const panel = panelsRef.current[botId];
        if (!latched) {
            latchRef.current[botId] = true;
            if (requestId) latchRequestRef.current[botId] = requestId;
            syncLive(botId, { url: screen.url || liveRef.current[botId]?.url || '', userControl: screen.userControl, attentionReason: screen.reason });
            setHandoffNotice({ botId, reason: screen.reason, url: screen.url });
            openPanel(botId, 'auto', true);
            showComputer(botId);
            setRetryByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            return;
        }
        if (panel === 'auto' || panel === 'user') {
            syncLive(botId, { url: screen.url || liveRef.current[botId]?.url || '', userControl: screen.userControl, attentionReason: screen.reason });
        }
    };

    useEffect(() => {
        bindDesktopBotResultStore();
    }, []);

    // The watch hold stays for as long as the stage is on screen, including
    // while the picture is still coming up. Two missed polls show the retry
    // card. They do not release the hold or stop asking: a desktop that was
    // just restarted answers a later poll, and releasing here would stop it.
    // Hiding the stage releases the hold. Switching to another bot whose
    // stage is already open does not: both bots share one desktop.
    // Nothing here resends the task.
    useEffect(() => {
        if (!selectedId || !showDesktop) return;
        const botId = selectedId;
        const epoch = takeDesktopWatchEpoch();
        watchEpochByBot.current[botId] = epoch;
        stageEpoch.current = epoch;
        let alive = true;
        let pending = false;
        watchMisses.current[botId] = 0;
        const fail = () => {
            if (!alive) return;
            const misses = (watchMisses.current[botId] || 0) + 1;
            watchMisses.current[botId] = misses;
            if (misses < 2) return;
            // Drop this bot's picture. Another bot showing the same address is
            // the shared desktop, and leaving that copy up hides the retry.
            // A different address is that bot's own display and stays.
            setLiveDesktop(prev => {
                const dead = prev[botId];
                if (!dead && !(botId in prev)) return prev;
                const next = { ...prev };
                delete next[botId];
                const url = dead?.url || '';
                if (url) {
                    for (const [id, view] of Object.entries(next)) {
                        if (view?.url === url) delete next[id];
                    }
                }
                return next;
            });
            setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            setTakeoverHoldByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            setSectionByBot(prev => prev[botId] === 'computer' ? prev : { ...prev, [botId]: 'computer' });
            setRetryByBot(prev => ({ ...prev, [botId]: true }));
        };
        const tick = async () => {
            // One poll at a time. A new call every few seconds used to stack
            // starts, and each one was canceled before the desktop was up.
            if (pending) return;
            pending = true;
            try {
                const requestedAt = Date.now();
                const view = await WatchDesktopBot(botId, epoch) as { novnc_url?: string; user_control?: boolean; attention_reason?: string; desktop_mode?: string } | undefined;
                if (!alive) return;
                const url = String(view?.novnc_url || '');
                if (!url) {
                    fail();
                    return;
                }
                watchMisses.current[botId] = 0;
                setRetryByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
                const panel = panelsRef.current[botId];
                if (panel !== 'auto' && panel !== 'user') return;
                let userControl = view?.user_control === true;
                let reason = String(view?.attention_reason || '');
                // A poll that left before a turn started still describes the previous
                // keyboard. Skip bubbles that started after it left. Only when
                // every live bubble is newer is the report the previous keyboard.
                // An older job beside a newer one keeps the login this poll saw.
                if (userControl || reason !== '') {
                    const stored = messagesForBot(userId, botId);
                    let newerThanPoll = false;
                    let covered = false;
                    for (let i = stored.length - 1; i >= 0; i--) {
                        const message = stored[i];
                        if (message.role !== 'assistant' || !message.pending || pendingReplyIsStale(message)) continue;
                        const started = botMessageStartedAt(message.id);
                        if (started > requestedAt) {
                            newerThanPoll = true;
                            continue;
                        }
                        if (!message.userControl && !message.attentionReason) {
                            commit(botId, stored.map(item => item.id === message.id ? {
                                ...item,
                                handoffUrl: url || item.handoffUrl,
                                userControl,
                                attentionReason: reason,
                            } : item));
                        }
                        covered = true;
                        break;
                    }
                    if (newerThanPoll && !covered) {
                        userControl = false;
                        reason = '';
                    }
                }
                const attention = userControl || reason !== '';
                // A poll that first reports a handoff has to show 电脑. Latch
                // that presentation so later polls do not pull the user back
                // off 详情. A quiet poll means this handoff ended: drop the
                // latch and the login prompt together. The next true is a new handoff.
                if (!attention && latchRef.current[botId] && !handoffHeldBesideSibling(botId)) {
                    latchRef.current[botId] = false;
                    delete latchRequestRef.current[botId];
                    clearStoredHandoff(botId);
                }
                if (attention && !latchRef.current[botId]) {
                    latchRef.current[botId] = true;
                    showComputer(botId);
                }
                syncLive(botId, { url, userControl, attentionReason: reason });
                if (view?.desktop_mode === 'isolated' || view?.desktop_mode === 'shared') {
                    const mode = desktopModeOf(view.desktop_mode);
                    setScreenByBot(prev => {
                        const existing = prev[botId];
                        if (existing && existing.mode === mode && (existing.url === url || !url)) return prev;
                        return { ...prev, [botId]: { url: url || existing?.url || '', mode } };
                    });
                }
            } catch {
                fail();
            } finally {
                pending = false;
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, DESKTOP_BOT_WATCH_INTERVAL_MS);
        return () => {
            alive = false;
            window.clearInterval(timer);
            // Defer the release one microtask. Switching to another bot whose
            // stage is already open installs the next generation first, and
            // that generation keeps the shared desktop. A real close does not
            // install one, so this release still stops the desktop.
            const released = epoch;
            // This commit already decided the stage is gone. Drop the handoff
            // now, before a login event in the same turn paints it again.
            // Waiting for the release below wipes that newer picture.
            if (!stageShownRef.current) {
                setLiveDesktop(prev => Object.keys(prev).length === 0 ? prev : {});
                setRetryByBot(prev => Object.keys(prev).length === 0 ? prev : {});
            }
            queueMicrotask(() => {
                if (stageEpoch.current !== released) return;
                void ReleaseDesktopBotWatch(botId, released).catch(() => undefined);
            });
        };
    }, [selectedId, showDesktop]);

    const botScreenKey = bots.map(bot => bot.id).join('\n');
    useEffect(() => {
        if (!showDesktop || !botScreenKey) return;
        let alive = true;
        const ids = botScreenKey.split('\n');
        void (async () => {
            const loaded = await Promise.all(ids.map(async id => {
                try {
                    const view = await GetDesktopBotScreen(id) as { novnc_url?: string; desktop_mode?: string } | undefined;
                    return [id, { url: String(view?.novnc_url || ''), mode: desktopModeOf(view?.desktop_mode) }] as const;
                } catch {
                    return null;
                }
            }));
            if (!alive) return;
            setScreenByBot(prev => {
                let changed = false;
                const next = { ...prev };
                for (const item of loaded) {
                    if (!item) continue;
                    const [id, screen] = item;
                    // A watch that already published an address is newer than this
                    // list read. Letting the list win swaps in a stale token.
                    const url = next[id]?.url || screen.url;
                    if (next[id]?.url === url && next[id]?.mode === screen.mode) continue;
                    next[id] = { url, mode: screen.mode };
                    changed = true;
                }
                return changed ? next : prev;
            });
        })();
        return () => { alive = false; };
    }, [showDesktop, botScreenKey]);

    useEffect(() => {
        if (!showDesktop) return;
        let alive = true;
        void DesktopBotDesktopAdmin().then(ok => {
            if (alive) setDesktopAdmin(ok === true);
        }).catch(() => {
            if (alive) setDesktopAdmin(false);
        });
        return () => { alive = false; };
    }, [showDesktop]);

    // GET does not start noVNC. Hold a display the chat bot's poll is not
    // already holding. A second epoch on the shared owner display would
    // replace that poll and drop the picture.
    useEffect(() => {
        if (!showDesktop || !holdBotId) return;
        const botId = holdBotId;
        const epoch = takeDesktopWatchEpoch();
        let alive = true;
        let pending = false;
        const tick = async () => {
            // Same rule as the chat bot's poll: a hold that is still opening
            // must not be joined by another start.
            if (pending) return;
            pending = true;
            try {
                const view = await WatchDesktopBot(botId, epoch) as { novnc_url?: string; desktop_mode?: string } | undefined;
                if (!alive) return;
                const url = String(view?.novnc_url || '');
                setScreenByBot(prev => {
                    const existing = prev[botId];
                    const next = {
                        url: url || existing?.url || '',
                        mode: view?.desktop_mode ? desktopModeOf(view.desktop_mode) : (existing?.mode || 'shared'),
                    };
                    if (existing && existing.url === next.url && existing.mode === next.mode) return prev;
                    return { ...prev, [botId]: next };
                });
            } catch {
                // The last picture stays up while this display is still starting.
            } finally {
                pending = false;
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, DESKTOP_BOT_WATCH_INTERVAL_MS);
        return () => {
            alive = false;
            window.clearInterval(timer);
            void ReleaseDesktopBotWatch(botId, epoch).catch(() => undefined);
        };
    }, [showDesktop, holdBotId]);

    useEffect(() => {
        if (!screenExpanded || !selectedId) return;
        const onKey = (event: globalThis.KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            const target = event.target;
            if (target instanceof HTMLElement && target.closest('input, textarea, [contenteditable="true"]')) return;
            event.preventDefault();
            pendingFitFocus.current = 'expand';
            setScreenExpandedByBot(prev => prev[selectedId] ? { ...prev, [selectedId]: false } : prev);
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [screenExpanded, selectedId]);

    useEffect(() => {
        const which = pendingFitFocus.current;
        if (!which) return;
        pendingFitFocus.current = null;
        const node = which === 'restore' ? restoreBtnRef.current : expandBtnRef.current;
        node?.focus({ preventScroll: true });
    }, [screenExpanded]);

    // The stage covers the window. Leave the chat, list, and panel controls
    // out of tab order and hit testing until the picture returns to the column.
    useEffect(() => {
        const stage = stageRef.current;
        if ((!screenExpanded && !takeover) || !stage) return;
        const touched: HTMLElement[] = [];
        let node: HTMLElement | null = stage;
        while (node && node !== document.body) {
            const parent: HTMLElement | null = node.parentElement;
            if (!parent) break;
            for (const sibling of parent.children) {
                if (sibling === node || !(sibling instanceof HTMLElement) || sibling.inert) continue;
                sibling.inert = true;
                touched.push(sibling);
            }
            node = parent;
        }
        return () => {
            for (const element of touched) element.inert = false;
        };
    }, [screenExpanded, takeover, desktopUrl]);

    // This bot starting a drive takes its own keyboard back. A takeover on
    // another bot stays, because that desktop is a different one. A hold is
    // a handoff this bot already asked for, so a still-open run does not
    // clear it.
    useEffect(() => {
        setTakeoverByBot(prev => {
            const next = { ...prev };
            let changed = false;
            for (const id of Object.keys(next)) {
                if (next[id] && !takeoverHoldByBot[id] && botUsingDesktop(id)) {
                    next[id] = false;
                    changed = true;
                }
            }
            return changed ? next : prev;
        });
    }, [takeoverHoldByBot, desktopBusyByBot, desktopRunByBot, bots, messagesByBot, liveDesktop]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            let data: Record<string, unknown>;
            try {
                data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            } catch {
                return;
            }
            const botId = String(data.bot_id || data.BotID || '').trim();
            if (!botId) return;
            const owner = String(data.owner_id || data.OwnerID || '').trim();
            if (owner && owner !== userIdRef.current) return;
            if ('run_driving' in data || 'RunDriving' in data) {
                runEpochRef.current += 1;
                const driving = data.run_driving === true || data.RunDriving === true;
                const prev = desktopRunByBotRef.current;
                let next = prev;
                if (driving) {
                    if (!prev[botId]) next = { ...prev, [botId]: true };
                } else if (prev[botId]) {
                    next = { ...prev };
                    delete next[botId];
                }
                if (next !== prev) {
                    desktopRunByBotRef.current = next;
                    setDesktopRunByBot(next);
                }
                if (!driving && next !== prev) drainAllDeskRef.current();
                if (!('busy' in data) && !('Busy' in data) && !('count' in data) && !('Count' in data)) return;
            }
            const seq = Number(data.seq || data.Seq || 0);
            if (seq > 0 && seq < busySeqRef.current) return;
            const countRaw = data.count ?? data.Count;
            const absolute = seq > 0 && countRaw !== undefined && countRaw !== null && Number.isFinite(Number(countRaw));
            if (seq > 0) {
                busySeqRef.current = seq;
                busyFloorRef.current = seq;
            } else {
                busyFloorRef.current = Math.max(busyFloorRef.current, busySeqRef.current + 1);
            }
            if (absolute) {
                const count = Math.max(0, Number(countRaw));
                setDesktopBusyByBot(prev => {
                    if (count <= 0) {
                        if (!(botId in prev)) return prev;
                        const next = { ...prev };
                        delete next[botId];
                        return next;
                    }
                    if (prev[botId] === count) return prev;
                    return { ...prev, [botId]: count };
                });
                return;
            }
            const busy = data.busy === true || data.Busy === true;
            setDesktopBusyByBot(prev => {
                const count = Math.max(0, (prev[botId] || 0) + (busy ? 1 : -1));
                if (count === (prev[botId] || 0)) return prev;
                if (count === 0) {
                    if (!(botId in prev)) return prev;
                    const next = { ...prev };
                    delete next[botId];
                    return next;
                }
                return { ...prev, [botId]: count };
            });
        };
        const off = EventsOn('desktop-bot-busy', handler);
        return () => { if (typeof off === 'function') off(); };
    }, []);

    // The step the desktop run is in. Empty clears back to the fallback
    // sentence, but only for the run that wrote the step: a second job of
    // this bot keeps the step it reported. A phrase that is not a real step
    // is ignored, so the previous step stays. Asking another bot is a
    // separate label and wins over this.
    useEffect(() => {
        const handler = (payload: unknown) => {
            let data: Record<string, unknown>;
            try {
                data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            } catch {
                return;
            }
            if (!data || typeof data !== 'object') return;
            const botId = String(data.bot_id || data.BotID || '').trim();
            if (!botId) return;
            const runId = String(data.run_id || data.RunID || '').trim();
            const raw = String(data.activity ?? data.Activity ?? '');
            setActivityByBot(prev => {
                const current = prev[botId];
                if (!raw.trim()) {
                    if (!current) return prev;
                    if (runId && current.runId && current.runId !== runId) return prev;
                    const next = { ...prev };
                    delete next[botId];
                    return next;
                }
                const activity = desktopBotActivityText(raw);
                if (!activity || (current?.text === activity && current.runId === runId)) return prev;
                return { ...prev, [botId]: { text: activity, runId } };
            });
        };
        const off = EventsOn('desktop-bot-activity', handler);
        return () => { if (typeof off === 'function') off(); };
    }, []);

    // A send that Hub refused is still the pending bubble. This line is only
    // the wait. The result that arrives later still settles that bubble.
    useEffect(() => {
        const handler = (payload: unknown) => {
            let data: Record<string, unknown>;
            try {
                data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            } catch {
                return;
            }
            if (data.waiting !== true && data.Waiting !== true) return;
            const botId = String(data.bot_id || data.BotID || '').trim();
            if (!botId) return;
            noteQueuedRef.current(botId);
        };
        const off = EventsOn('desktop-bot-queued', handler);
        return () => { if (typeof off === 'function') off(); };
    }, []);

    useEffect(() => {
        let cancelled = false;
        const seenRuns = runEpochRef.current;
        void GetDesktopBotBusy(userId).then(view => {
            if (cancelled || userIdRef.current !== userId) return;
            const seq = Number(view?.seq || 0);
            if (seq >= busyFloorRef.current) {
                busySeqRef.current = Math.max(busySeqRef.current, seq);
                busyFloorRef.current = busySeqRef.current;
                const next: Record<string, number> = {};
                for (const mark of view?.marks || []) {
                    const id = String(mark?.bot_id || '').trim();
                    const count = Number(mark?.count || 0);
                    if (!id || count <= 0) continue;
                    next[id] = count;
                }
                setDesktopBusyByBot(next);
            }
            if (runEpochRef.current === seenRuns) {
                const runs: Record<string, boolean> = {};
                for (const id of view?.runs || []) {
                    const botId = String(id || '').trim();
                    if (botId) runs[botId] = true;
                }
                desktopRunByBotRef.current = runs;
                setDesktopRunByBot(runs);
            }
        }).catch(() => undefined);
        return () => { cancelled = true; };
    }, [userId]);

    // noVNC latches viewOnly for the life of that document. Focusing after load
    // is what puts the keyboard in the new page; a secret box keeps the focus.
    useEffect(() => {
        if (!takeover) return;
        const frame = handoffRef.current;
        if (!frame) return;
        const focusFrame = () => {
            if (secretOpenRef.current) return;
            try {
                frame.focus({ preventScroll: true });
            } catch {
                frame.focus();
            }
        };
        frame.addEventListener('load', focusFrame);
        return () => frame.removeEventListener('load', focusFrame);
    }, [takeover, desktopUrl]);

    useEffect(() => {
        const gen = ++loadGen.current;
        // The previous account's confirm must not keep this account's delete locked.
        removeLock.current = 0;
        setBots([]);
        setSelectedId('');
        setEditBot(null);
        setEditSaving(false);
        setEditNotice('');
        setMessagesByBot({});
        setPersonaByBot(personasForUser(userId));
        setCommand('');
        setDesktopPanels({});
        setSectionByBot({});
        setLiveDesktop({});
        setTakeoverByBot({});
        setTakeoverHoldByBot({});
        setDesktopBusyByBot({});
        desktopRunByBotRef.current = {};
        setDesktopRunByBot({});
        setScreenExpandedByBot({});
        setSecretPrompt(null);
        setSecretDraft('');
        setRetryByBot({});
        latchRef.current = {};
        secretHandled.current = new Set();
        pendingBots.current = [];
        hiddenBotIds.current = new Set();
        renamedBots.current = new Map();
        setListNotice('');
        setListFailed(false);
        const mergeListed = (server: DesktopBot[]) => {
            const hidden = hiddenBotIds.current;
            const renamed = renamedBots.current;
            const merged = server.filter(bot => !hidden.has(bot.id)).map(bot => {
                const edit = renamed.get(bot.id);
                return edit ? { ...bot, title: edit.title, description: edit.description } : bot;
            });
            for (const bot of pendingBots.current) {
                if (!hidden.has(bot.id) && !merged.some(item => item.id === bot.id)) merged.push(bot);
            }
            pendingBots.current = pendingBots.current.filter(bot => !server.some(item => item.id === bot.id) && !hidden.has(bot.id));
            return merged;
        };
        ListDesktopBots().then(items => {
            if (gen !== loadGen.current) return;
            setListFailed(false);
            const list = (items || []).map(item => ({
                id: item.id,
                title: item.title,
                description: item.description || '',
                createdAt: createdMillis(item.created_at),
                primary: item.primary === true,
            }));
            const merged = mergeListed(list);
            botsRef.current = merged;
            setBots(merged);
            if (pendingCrossNotes.current.length > 0) landCrossRef.current(pendingCrossNotes.current);
            // The live queue is this session's authority. Storage is only the
            // reload copy, so a list that arrives after the user already queued
            // a message must not append that message again.
            const recovered: string[] = [];
            for (const bot of merged) {
                if (queueRef.current.has(bot.id)) continue;
                const queued = messagesForBot(userId, bot.id).filter(item => item.role === 'user' && item.queued).map(item => ({ text: item.content, phase: storedQueuePhase(item) }));
                if (queued.length === 0) continue;
                queueRef.current.set(bot.id, queued);
                recovered.push(bot.id);
            }
            setMessagesByBot(prev => {
                let changed = false;
                const next = { ...prev };
                for (const bot of merged) {
                    if (next[bot.id]) continue;
                    next[bot.id] = messagesForBot(userId, bot.id);
                    changed = true;
                }
                return changed ? next : prev;
            });
            for (const botId of recovered) drainIfIdle(botId);
        }).catch((error: unknown) => {
            if (gen !== loadGen.current) return;
            setBots(mergeListed([]));
            setListFailed(true);
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
        });
        return () => { loadGen.current += 1; };
    }, [userId]);

    const screenIsStale = (sessionKey: string, requestId: string, inFlight?: PendingSend) => {
        const botId = sessionBotId(sessionKey, userId);
        if (!botId || !requestId) return false;
        const owner = [...messagesForBot(userId, botId)].reverse().find(item => item.requestId === requestId);
        if (!owner) return false;
        if (inFlight && !inFlight.settled && inFlight.messageId === owner.id) return false;
        if (owner.pending && !pendingReplyIsStale(owner)) return false;
        return true;
    };

    // The bubble can be settled by a result only after SendDesktopBotTask.
    // Until then it is local. A late result from another run must not take it,
    // or the order for this message is never sent.
    const claimLivePending = (requestId: string, sessionKey: string) => {
        const known = requestOwnsStoredMessage(userId, sessionKey, requestId);
        let pending: PendingSend | undefined = requestId ? pendingRef.current.get(requestId) : undefined;
        let pendingKey = requestId;
        if (pending || known || !sessionKey) return { pending, pendingKey, unsent: false };
        const split = sessionKey.lastIndexOf(':');
        const botId = split >= 0 ? sessionKey.slice(split + 1) : sessionKey;
        let unsent = false;
        // One live job still accepts a result whose id it has not stored yet:
        // a desktop event often carries a different id than the chat turn.
        // Two live jobs do not share that guess. A result then belongs only
        // to the send that has not stored its id, never to the other job.
        const admitted: Array<{ key: string; value: PendingSend }> = [];
        const unbound: Array<{ key: string; value: PendingSend }> = [];
        for (const [key, value] of pendingRef.current) {
            if (value.botId !== botId || value.settled) continue;
            if (!value.admitted) {
                unsent = true;
                continue;
            }
            admitted.push({ key, value });
            if (key.startsWith('local:')) unbound.push({ key, value });
        }
        if (admitted.length === 1) return { pending: admitted[0].value, pendingKey: admitted[0].key, unsent: false };
        if (unbound.length === 1) return { pending: unbound[0].value, pendingKey: unbound[0].key, unsent: false };
        return { pending: undefined, pendingKey, unsent: admitted.length === 0 && unsent };
    };

    // One finished turn spends the allowance once. A repeated delivery of the
    // same result must not start another read and cancel the one in flight.
    const noteCreditsSpent = (requestId: string) => {
        const id = requestId.trim();
        if (!id || creditSpentRef.current.has(id)) return;
        creditSpentRef.current.add(id);
        setCreditEpoch(value => value + 1);
    };

    useEffect(() => {
        const handler = (payload: unknown) => {
            const data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            const requestId = String(data.request_id || data.RequestID || '');
            const sessionKey = String(data.session_key || data.SessionKey || '');
            const { pending, pendingKey, unsent } = claimLivePending(requestId, sessionKey);
            if (unsent) return;
            const screen = screenOf(data);
            const staleScreen = screenIsStale(sessionKey, requestId, pending);
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                handleDesktopBotResult(data);
                if (sessionKey.lastIndexOf(':') > 0) noteCreditsSpent(requestId);
            } else {
                pending.settled = true;
                noteCreditsSpent(requestId || pendingKey);
                pendingRef.current.delete(pendingKey);
                const botId = pending.botId;
                const result = responseText(data, text.emptyResult);
                const ask = askFrom(data);
                const attached = attachedFiles(data, result.failed);
                const shots = result.failed ? undefined : shotPathsFrom(data);
                const stored = messagesForBot(userId, botId);
                const attention = screen.userControl || screen.reason !== '';
                const next = stored.map(item => item.id === pending.messageId ? {
                    ...item,
                    content: result.content,
                    pending: false,
                    failed: result.failed,
                    requestId: requestId || item.requestId,
                    handoffUrl: attention ? (screen.url || item.handoffUrl || '') : '',
                    userControl: screen.userControl,
                    attentionReason: screen.reason,
                    askUser: ask || item.askUser,
                    images: result.failed || shots ? undefined : imagesFrom(data),
                    shotPaths: shots,
                    localPaths: attached.localPaths,
                    files: attached.files,
                } : item);
                commit(botId, foldPlanConfirmation(next, next.findIndex(item => item.id === pending.messageId)));
                reportDesktopBotCrossResult(userId, botId, result.content);
                drainIfIdle(botId);
            }
            const botId = sessionBotId(sessionKey, userId);
            if (!botId) return;
            if (!staleScreen) {
                if (screen.userControl || screen.reason || screen.cleared || (screen.reported && latchRef.current[botId])) applyScreen(botId, screen, requestId);
            }
            setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
            drainIfIdle(botId);
        };
        const off = EventsOn('ai-assistant-response', handler);
        return () => { if (typeof off === 'function') off(); };
    }, [text.emptyResult, userId]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            const data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            const requestId = String(data.request_id || data.RequestID || '');
            const sessionKey = String(data.session_key || data.SessionKey || '');
            const { pending, unsent } = claimLivePending(requestId, sessionKey);
            if (unsent) return;
            const screen = screenOf(data);
            const botId = sessionBotId(sessionKey, userId);
            if (botId && !screenIsStale(sessionKey, requestId, pending)) applyScreen(botId, screen, requestId);
            const attention = screen.userControl || screen.reason !== '';
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                if (attention) handleDesktopBotView(data);
                if (!botId) return;
                setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
                return;
            }
            if (!attention) return;
            const stored = messagesForBot(userId, pending.botId);
            const next = stored.map(item => item.id === pending.messageId && item.pending ? {
                ...item,
                handoffUrl: screen.url || item.handoffUrl,
                userControl: screen.reported ? screen.userControl : item.userControl,
                attentionReason: screen.reason || item.attentionReason,
            } : item);
            commit(pending.botId, next);
        };
        const off = EventsOn('desktop-bot-view', handler);
        return () => { if (typeof off === 'function') off(); };
    }, [userId]);

    useEffect(() => {
        const handler = (payload: unknown) => {
            let data: Record<string, unknown>;
            try {
                data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            } catch {
                return;
            }
            if (!data || typeof data !== 'object') return;
            handleDesktopBotReport(data);
            const sessionKey = String(data.session_key || data.SessionKey || '');
            const botId = sessionBotId(sessionKey, userId);
            if (!botId) return;
            if (data.schedule_report === true || data.ScheduleReport === true) {
                const requestId = String(data.request_id || data.RequestID || '');
                if (requestId.startsWith('desktop-bot-sched-')) noteCreditsSpent(requestId);
            }
            const screen = screenOf(data);
            if (screen.userControl || screen.reason) applyScreen(botId, screen);
            setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
        };
        const off = EventsOn('desktop-bot-report', handler);
        return () => { if (typeof off === 'function') off(); };
    }, [userId]);

    useEffect(() => {
        if (!selectedId || panelSection !== 'schedules' || !showDesktop) return;
        const botId = selectedId;
        let live = true;
        const load = () => {
            const gen = ++scheduleLoadGen.current;
            void ListDesktopBotSchedules(botId).then(list => {
                if (!live || gen !== scheduleLoadGen.current) return;
                setSchedulesByBot(prev => ({ ...prev, [botId]: normalizeSchedules(list) }));
                setScheduleNotice('');
            }).catch((error: unknown) => {
                if (!live || gen !== scheduleLoadGen.current) return;
                setScheduleNotice(error instanceof Error && error.message ? error.message : text.unavailable);
            });
        };
        load();
        const off = EventsOn('scheduled-tasks-changed', load);
        return () => {
            live = false;
            if (typeof off === 'function') off();
        };
    }, [selectedId, panelSection, showDesktop, text.unavailable]);

    const removeSchedule = async (botId: string, task: DesktopBotScheduleInfo) => {
        if (scheduleDeleteLock.current) return;
        scheduleDeleteLock.current = true;
        const gen = loadGen.current;
        try {
            const name = task.name || task.action || task.id;
            const confirmed = await showConfirm(text.scheduleRemoveAsk(name), text.scheduleRemoveTitle, {
                confirmText: text.remove,
                cancelText: text.cancel,
                confirmVariant: 'danger',
            });
            if (!confirmed || gen !== loadGen.current || userIdRef.current !== userId) return;
            await DeleteDesktopBotSchedule(botId, task.id);
            scheduleLoadGen.current += 1;
            setSchedulesByBot(prev => ({
                ...prev,
                [botId]: (prev[botId] || []).filter(item => item.id !== task.id),
            }));
            setScheduleNotice('');
        } catch (error) {
            const message = friendlyFailure(failureMessage(error), lang);
            setScheduleNotice(missedSchedule(error) ? text.scheduleNone : message || text.unavailable);
        } finally {
            scheduleDeleteLock.current = false;
        }
    };

    const selectBot = (bot: DesktopBot) => {
        setSelectedId(bot.id);
        setScreenPick('mine');
        setScreenPickTouched(false);
        setHandBackError('');
        setModeError('');
        setMessagesByBot(prev => prev[bot.id] ? prev : { ...prev, [bot.id]: messagesForBot(userId, bot.id) });
    };

    const chooseScreen = (pick: string) => {
        setScreenPickTouched(true);
        setScreenPick(pick);
        setHandBackError('');
        if (pick === 'mine' || screenByBot[pick]?.url) return;
        void GetDesktopBotScreen(pick).then(view => {
            const url = String(view?.novnc_url || '');
            setScreenByBot(prev => ({
                ...prev,
                [pick]: { url: prev[pick]?.url || url, mode: desktopModeOf(view?.desktop_mode) },
            }));
        }).catch(() => undefined);
    };

    const openHandoffScreen = (notice: { botId: string; reason: string; url: string }) => {
        const bot = bots.find(item => item.id === notice.botId);
        if (bot) {
            setSelectedId(bot.id);
            setMessagesByBot(prev => prev[bot.id] ? prev : { ...prev, [bot.id]: messagesForBot(userId, bot.id) });
        }
        if (notice.url) {
            setScreenByBot(prev => ({
                ...prev,
                [notice.botId]: { url: notice.url, mode: prev[notice.botId]?.mode || 'shared' },
            }));
        }
        openPanel(notice.botId, 'user', true);
        showComputer(notice.botId);
        chooseScreen(notice.botId);
    };

    const handBackTarget = screenPickNow !== 'mine' ? screenPickNow : (selected?.id || '');
    // Hand-back admits a continuation, so it belongs to a bot that asked for
    // the person. Looking at another screen must not start that task.
    const showHandBack = botAskedForDesktop(handBackTarget);
    const handBackToBot = async (botId: string) => {
        if (!botId || handingBack) return;
        setHandingBack(true);
        setHandBackError('');
        try {
            const handed = await HandBackDesktopBot(botId);
            const requestId = String(handed?.client_message_id || '').trim();
            const messageId = handed?.accepted && requestId ? messageID() : '';
            if (messageId) pendingRef.current.set(requestId, { botId, messageId, settled: false, admitted: true });
            const stored = messagesForBot(userId, botId).map(item => (
                item.userControl || item.attentionReason ? { ...item, userControl: false, attentionReason: '' } : item
            ));
            if (messageId) stored.push({ id: messageId, role: 'assistant', content: '', pending: true, phase: 'execute', requestId });
            commit(botId, stored);
            setHandoffNotice(prev => prev?.botId === botId ? null : prev);
            clearLiveFlags(botId);
            latchRef.current[botId] = false;
            delete latchRequestRef.current[botId];
            setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            setTakeoverHoldByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        } catch (error) {
            setHandBackError(failureMessage(error).trim() || text.handBackFailed);
        } finally {
            setHandingBack(false);
        }
    };

    const saveDesktopMode = async (botId: string, mode: 'isolated' | 'shared') => {
        setModeSaving(true);
        setModeError('');
        try {
            const saved = await SetDesktopBotDesktopMode(botId, mode);
            const next = desktopModeOf(saved || mode);
            // A mode flip changes which display this entry names. The old URL
            // still points at the previous display — drop it before it paints
            // as the new mode's screen; the GET below refills the right one.
            setScreenByBot(prev => ({ ...prev, [botId]: { url: prev[botId]?.mode === next ? (prev[botId]?.url || '') : '', mode: next } }));
            const view = await GetDesktopBotScreen(botId);
            setScreenByBot(prev => ({
                ...prev,
                [botId]: { url: String(view?.novnc_url || prev[botId]?.url || ''), mode: desktopModeOf(view?.desktop_mode || next) },
            }));
        } catch (error) {
            setModeError(failureMessage(error).trim() || text.desktopModeFailed);
        } finally {
            setModeSaving(false);
        }
    };

    const makePrimary = async (botId: string) => {
        const gen = loadGen.current;
        try {
            const items = await SetDesktopBotPrimary(botId);
            if (gen !== loadGen.current || userIdRef.current !== userId) return;
            const flags = new Map((items || []).map(item => [item.id, item.primary === true]));
            setBots(prev => prev.map(bot => ({ ...bot, primary: flags.get(bot.id) === true })));
        } catch (error) {
            if (gen !== loadGen.current) return;
            setListNotice(failureMessage(error).trim() || text.unavailable);
        }
    };

    const create = async () => {
        const gen = loadGen.current;
        setListNotice('');
        const title = `Bot ${bots.length + 1}`;
        try {
            const created = await CreateDesktopBot(title, text.defaultDescription);
            if (gen !== loadGen.current) return;
            const bot: DesktopBot = {
                id: created.id,
                title: created.title || title,
                description: created.description || text.defaultDescription,
                createdAt: createdMillis(created.created_at),
            };
            pendingBots.current = [...pendingBots.current.filter(item => item.id !== bot.id), bot];
            setBots(prev => prev.some(item => item.id === bot.id) ? prev : [...prev, bot]);
            selectBot(bot);
        } catch (error) {
            if (gen !== loadGen.current) return;
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
        }
    };

    const submitEdit = async (title: string, description: string) => {
        const botId = editBot?.id;
        if (!botId || editSaving) return;
        const gen = loadGen.current;
        setEditSaving(true);
        setEditNotice('');
        try {
            await RenameDesktopBot(botId, title, description);
        } catch (error) {
            if (gen !== loadGen.current) return;
            setEditNotice(error instanceof Error && error.message ? error.message : text.unavailable);
            return;
        } finally {
            setEditSaving(false);
        }
        if (gen !== loadGen.current) return;
        renamedBots.current.set(botId, { title, description });
        setBots(prev => prev.map(bot => bot.id === botId ? { ...bot, title, description } : bot));
        setEditBot(null);
    };

    // Bot calls on the official route spend this account's redeemed credits.
    // A custom model does not. The new-user period window is a separate balance
    // and is not what stops these calls. A failed refresh keeps the last numbers.
    useEffect(() => {
        if (creditUserRef.current !== userId) {
            creditUserRef.current = userId;
            setCreditReadout(null);
            setCreditRoute('default');
            setCustomModelDetailText('');
        }
        let cancelled = false;
        // A finished turn passes fresh so the next paint sees the debit. The
        // poll keeps the cached status; forcing a read every 20s is what the
        // cache is there to avoid.
        const load = async (fresh: boolean) => {
            const ticket = ++creditLoadRef.current;
            const current = () => !cancelled && ticket === creditLoadRef.current;
            try {
                // The mark follows the model choice even when the wallet read fails.
                const [choiceResult, statusResult] = await Promise.allSettled([
                    GetDesktopBotLLM(),
                    fresh ? RefreshHubLLMServiceStatus() : GetHubLLMServiceStatus(),
                ]);
                if (!current()) return;
                if (choiceResult.status === 'fulfilled') {
                    const route = modelRouteFrom(choiceResult.value);
                    setCreditRoute(route.route);
                    setCustomModelDetailText(route.detail);
                }
                if (statusResult.status !== 'fulfilled') return;
                // A custom model does not spend this balance, so the chat stays
                // quiet. The list still shows it: that is the account wallet.
                const wallet = normalizeSidebarHubCredits(statusResult.value);
                if (!wallet || wallet.authorized === false) {
                    setCreditReadout(null);
                    return;
                }
                if (wallet.unlimited) {
                    setCreditReadout({ total: 0, remaining: 0, notice: '', unlimited: true });
                    return;
                }
                setCreditReadout(desktopCreditReadout({
                    mode: 'default',
                    authorized: true,
                    total: wallet.total,
                    remaining: wallet.remaining,
                }));
            } catch {
                // Keep the previous readout.
            }
        };
        void load(creditEpoch > 0);
        const timer = window.setInterval(() => { void load(false); }, 20_000);
        return () => {
            cancelled = true;
            window.clearInterval(timer);
        };
    }, [userId, creditEpoch]);

    // Both halves of the model dialog load together: without the provider list
    // the saved choice cannot be shown as selected, and without the saved choice
    // the list has nothing to point at. Either failing leaves nothing to show.
    const openLLMSettings = async () => {
        if (llmOpen) return;
        const gen = loadGen.current;
        setLlmNotice('');
        try {
            const [current, rawProviders] = await Promise.all([
                GetDesktopBotLLM(),
                GetMaclawLLMProviders() as Promise<{ providers?: DesktopProviderWire[]; Providers?: DesktopProviderWire[] } | null>,
            ]);
            if (gen !== loadGen.current) return;
            const entries = rawProviders?.providers ?? rawProviders?.Providers ?? [];
            const route = modelRouteFrom(current);
            // The dialog and the header read the same response. A failed poll
            // must not leave the mark blank once this read has the choice.
            setCreditRoute(route.route);
            setCustomModelDetailText(route.detail);
            setLlmProviders(usableDesktopLLMProviders(Array.isArray(entries) ? entries : []));
            setLlmMode(route.route);
            setLlmName(String(current?.name ?? '').trim());
            setLlmModel(String(current?.model ?? '').trim());
            setLlmOpen(true);
        } catch (error) {
            if (gen !== loadGen.current) return;
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
        }
    };

    const submitLLM = async (payload: BotLLMSettingsSubmit) => {
        if (llmSaving) return;
        const gen = loadGen.current;
        setLlmSaving(true);
        setLlmNotice('');
        let saved: { mode?: string; name?: string; model?: string; warning?: string } | undefined;
        try {
            if (payload.mode === 'default') {
                saved = await SaveDesktopBotLLM({ mode: 'default' });
            } else if (payload.provider) {
                saved = await SaveDesktopBotLLM({
                    mode: 'custom',
                    name: payload.provider.name,
                    url: payload.provider.url,
                    key: payload.provider.key,
                    model: payload.model,
                    protocol: payload.provider.protocol,
                    wire_api: payload.provider.wireAPI,
                });
            }
        } catch (error) {
            if (gen !== loadGen.current) return;
            setLlmNotice(error instanceof Error && error.message ? error.message : text.unavailable);
            return;
        } finally {
            setLlmSaving(false);
        }
        if (gen !== loadGen.current) return;
        if (!saved) return;
        // A warning still means the choice was stored. The header follows it
        // immediately, and the dialog stays open so that reason is not dismissed.
        // Bumping the load ticket stops an older wallet read from painting the
        // previous mode back over this one.
        creditLoadRef.current += 1;
        const route = modelRouteFrom(saved);
        setCreditRoute(route.route);
        setCustomModelDetailText(route.detail);
        if (route.route === 'custom') {
            setLlmMode('custom');
            setLlmName(String(saved.name ?? '').trim());
            setLlmModel(String(saved.model ?? '').trim());
        } else {
            setLlmMode('default');
            setLlmName('');
            setLlmModel('');
        }
        if (saved.warning) {
            setLlmNotice(saved.warning);
            return;
        }
        setCreditEpoch(value => value + 1);
        setLlmOpen(false);
    };

    const remove = async (botId: string) => {
        if (removeLock.current !== 0) return;
        const ticket = ++removeSeq.current;
        removeLock.current = ticket;
        const gen = loadGen.current;
        const ownerId = userId;
        try {
            const bot = bots.find(item => item.id === botId);
            const name = (bot?.title || '').trim() || botId;
            const confirmed = await showConfirm(text.removeAsk(name), text.removeTitle, {
                confirmText: text.remove,
                cancelText: text.cancel,
                confirmVariant: 'danger',
            });
            if (!confirmed || gen !== loadGen.current) return;
            setListNotice('');
            try {
                await DeleteDesktopBot(botId);
            } catch (error) {
                if (gen !== loadGen.current) return;
                setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
                return;
            }
            // Storage is keyed by the account that confirmed. The live queue is
            // not: after an account switch it belongs to whoever is on screen.
            saveBotMessages(ownerId, botId, []);
            forgetBotPersona(ownerId, botId);
            if (userIdRef.current === ownerId) {
                setPersonaByBot(prev => {
                    if (!prev[botId]) return prev;
                    const next = { ...prev };
                    delete next[botId];
                    return next;
                });
            }
            if (userIdRef.current === ownerId) {
                queueRef.current.delete(botId);
                for (const [key, value] of pendingRef.current) {
                    if (value.botId === botId) pendingRef.current.delete(key);
                }
            }
            if (gen !== loadGen.current) return;
            hiddenBotIds.current.add(botId);
            pendingBots.current = pendingBots.current.filter(bot => bot.id !== botId);
            renamedBots.current.delete(botId);
            setBots(prev => prev.filter(bot => bot.id !== botId));
            // Dropping a bot drops its accepted-but-unsent queue with it, and no
            // later reply for the gone instance settles into a ghost record.
            setDesktopPanels(prev => {
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            setSectionByBot(prev => {
                if (!prev[botId]) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            setLiveDesktop(prev => {
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            setTakeoverByBot(prev => {
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            setTakeoverHoldByBot(prev => {
                if (!prev[botId]) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            setScreenExpandedByBot(prev => {
                if (!prev[botId]) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            });
            if (editBot?.id === botId) setEditBot(null);
            setSelectedId(current => current === botId ? '' : current);
            setMessagesByBot(prev => {
                const copy = { ...prev };
                delete copy[botId];
                return copy;
            });
        } finally {
            if (removeLock.current === ticket) removeLock.current = 0;
        }
    };

    // Several jobs of one bot run at once. A message sent while a run is
    // already admitted starts beside it; neither turn is cancelled to make
    // room. The queue only keeps send order while an earlier line is still
    // being read, and it holds desktop work while the person has the keyboard.
    // MaClawSrv keeps each run's supplements on that run, so a new job is not
    // written into the one already going.
    const queueRef = useRef<Map<string, QueuedSend[]>>(new Map());
    // Readings finish out of order. Slots keep the order the person sent them,
    // so a fast later reading cannot run before an earlier one.
    // verbatim marks a queue entry whose reading already failed once: this
    // person chose their own words, so the queue must not read it again.
    type ReadAhead = { text: string; phase: BotPhase; settled: boolean; enqueue: boolean; drop?: boolean; wanted?: boolean; verbatim?: boolean; prepared?: PreparedTurn };
    const readAheadRef = useRef<Map<string, ReadAhead[]>>(new Map());
    // Set only while a stop is still deciding whether a live run exists.
    // Cleared before the cancel request returns, after the old slots are marked.
    const stopHoldRef = useRef<Set<string>>(new Set());
    // drainIfIdle and releaseReadAhead are defined further down. Stop fills
    // these in before any click, so a held queue can be put back without a
    // forward call.
    const followUps = {
        drain(botId: string) { void botId; },
        release(ownerId: string, botId: string) { void ownerId; void botId; },
    };

    const dropAutoPanel = (botId: string) => {
        // This turn failed before it had a desktop. Another job already on
        // screen, or a login the person is still using, keeps the picture.
        const sibling = [...pendingRef.current.values()].some(value => value.botId === botId && !value.settled);
        const personHolds = messagesForBot(userId, botId).some(item => item.userControl || !!item.attentionReason);
        if (sibling || personHolds || latchRef.current[botId]) return;
        setDesktopPanels(prev => prev[botId] === 'user' ? prev : { ...prev, [botId]: 'off' });
        setLiveDesktop(prev => prev[botId] ? { ...prev, [botId]: undefined } : prev);
        setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        latchRef.current[botId] = false;
        delete latchRequestRef.current[botId];
    };

    // verbatim starts the turn from the person's own words, with no reading.
    // The reading is a helper for the turn, not a gate on it: once it has
    // failed and this person has chosen to send what they typed, that choice
    // is the order.
    const personHoldReason = (botId: string) => {
        const items = messagesForBot(userId, botId);
        for (let i = items.length - 1; i >= 0; i--) {
            const reason = String(items[i]?.attentionReason || '').trim();
            if (reason) return reason;
        }
        return String(liveDesktop[botId]?.attentionReason || '').trim();
    };
    // GetActiveDesktopRuns drops an empty run id, so 「刚接下」 comes from the
    // driving flag instead. A timeout is not the same as an empty list.
    const runsOnThisDesktop = async (botId: string) => {
        const rows = await withinBudget(
            GetActiveDesktopRuns(botId) as Promise<Array<{ run_id?: string; task_text?: string; waiting_for_user?: boolean }> | null>,
            200,
        );
        if (!rows) return undefined;
        return rows.flatMap(item => {
            const runId = String(item?.run_id || '').trim();
            if (!runId) return [];
            const title = oneLine(stripConfirmedOrderLead(String(item?.task_text || ''))) || runId;
            return [{ runId, title, waiting: item?.waiting_for_user === true }];
        });
    };
    // Take the queue out before any await. A reading that settles during the
    // lookup must not start the next order. Nothing is restored until we know
    // there is no run to cancel, and a stop does not leave the hold behind.
    const holdFollowUps = (botId: string) => {
        stopHoldRef.current.add(botId);
        const queued = [...(queueRef.current.get(botId) || [])];
        queueRef.current.delete(botId);
        const slots = readAheadRef.current.get(`${userId}\0${botId}`) || [];
        const hadNext = queued.length > 0 || slots.some(slot => !slot.settled || !!slot.wanted || !!slot.enqueue);
        for (const slot of slots) {
            slot.drop = true;
            slot.enqueue = false;
        }
        let released = false;
        const release = (keepDropped: boolean) => {
            if (released) return;
            released = true;
            if (!keepDropped) {
                for (const slot of slots) {
                    slot.drop = false;
                    if (slot.settled && slot.wanted) slot.enqueue = true;
                }
                if (queued.length > 0) {
                    const current = queueRef.current.get(botId) || [];
                    queueRef.current.set(botId, [...queued, ...current]);
                }
            }
            stopHoldRef.current.delete(botId);
            if (!keepDropped && userIdRef.current === userId) {
                followUps.release(userId, botId);
                followUps.drain(botId);
            }
        };
        return { hadNext, release };
    };
    const statusSpokenRun = async (ownerId: string, botId: string): Promise<string> => {
        const local = await runsOnThisDesktop(botId);
        if (userIdRef.current !== ownerId) return '';
        const running = local?.filter(run => !run.waiting) || [];
        if (local && running.length > 0) return desktopBotRunningSentence(running.map(run => run.title), lang);
        if (local && local.length > 0) return desktopBotWaitingSentence(personHoldReason(botId), lang);
        // The run list already waited. Driving can flip during that wait, and
        // this closure still holds the previous render. The ref is updated
        // with the event, before React renders.
        if (desktopRunByBotRef.current[botId]) return desktopBotAdmittingSentence(lang);
        const remote = String(await withinBudget(QueryDesktopBotTaskStatus(botId, '', lang), 800) || '').trim();
        if (userIdRef.current !== ownerId) return '';
        return remote || desktopBotStatusUnreachableSentence(lang);
    };
    const stopSpokenRun = async (ownerId: string, botId: string, held: { hadNext: boolean; release: (keepDropped: boolean) => void }): Promise<string> => {
        const local = await runsOnThisDesktop(botId);
        if (userIdRef.current !== ownerId) {
            held.release(false);
            return '';
        }
        const admitting = !!desktopRunByBotRef.current[botId];
        if (!local || local.length === 0) {
            if (admitting) {
                // Hub names the run only when the admit call returns, which
                // can take the whole 45-minute budget. Arm the cancel now
                // and drop the next order. The admit path cancels that id.
                held.release(true);
                let reason = '';
                try {
                    await ArmDesktopBotAdmitCancel(botId);
                } catch (error) {
                    reason = failureMessage(error).trim();
                }
                if (userIdRef.current !== ownerId) return '';
                if (reason) return desktopBotStopFailedSentence(reason, lang);
                return desktopBotStoppedSentence([], held.hadNext, lang);
            }
            held.release(false);
            if (!local) return desktopBotStatusUnreachableSentence(lang);
            return desktopBotStopIdleSentence(lang);
        }
        const hadNext = held.hadNext;
        held.release(true);
        let reason = '';
        for (const run of local) {
            try {
                await CancelDesktopBotRun(botId, run.runId);
            } catch (error) {
                reason = failureMessage(error).trim();
                break;
            }
        }
        if (userIdRef.current !== ownerId) return '';
        if (reason) return desktopBotStopFailedSentence(reason, lang);
        return desktopBotStoppedSentence(local.map(run => run.title), hadNext, lang);
    };
    // No pending bubble, so this answer is not the working row
    // 「正在一点点推进」. The question itself is an ordinary user line.
    // A queued copy is already in the thread.
    const answerDesktopControl = (botId: string, content: string, control: 'status' | 'stop', mode: 'now' | 'fromQueue') => {
        if (mode === 'now') {
            const prior = messagesForBot(userId, botId);
            commit(botId, [...prior, { id: messageID(), role: 'user', content }]);
        }
        const ownerId = userId;
        const held = control === 'stop' ? holdFollowUps(botId) : undefined;
        void (async () => {
            const answer = control === 'stop' && held
                ? await stopSpokenRun(ownerId, botId, held)
                : await statusSpokenRun(ownerId, botId);
            if (userIdRef.current !== ownerId || !answer) return;
            const stored = messagesForBot(ownerId, botId);
            commit(botId, [...stored, { id: messageID(), role: 'assistant', content: answer, chat: true }]);
        })();
    };

    const deliverCross = async (botId: string, cross: { kind: string; target: string; text: string; order: string }) => {
        const self = botsRef.current.find(item => item.id === botId);
        const label = desktopBotAskingLabel(cross.target, botsRef.current);
        setAskingByBot(prev => ({ ...prev, [botId]: label }));
        try {
            if (!self?.primary) return { content: DESKTOP_BOT_NOT_PRIMARY, ask: false, noticeBots: undefined as DesktopBotNoticeBot[] | undefined };
            const posted = await PostDesktopBotCross(botId, cross.kind, cross.target, cross.text, cross.order, '');
            if (!posted || posted.ask || !posted.posted) {
                return { content: String(posted?.ask || '').trim() || '你要跟哪一个 Bot 说，说什么？', ask: true, noticeBots: undefined as DesktopBotNoticeBot[] | undefined };
            }
            for (const note of posted.notes || []) {
                const id = String(note?.id || '').trim();
                if (id) markDesktopBotNotice(userId, `${id}:from`);
            }
            const names = (posted.names || []).map(name => String(name || '').trim()).filter(Boolean);
            const noticeBots = names.length > 1
                ? (posted.notes || []).map((note, index) => ({ id: String(note?.target || index), name: names[index] || String(note?.target || '') })).filter(item => item.name)
                : undefined;
            return { content: String(posted.primary_line || '').trim() || names.join('、'), ask: false, noticeBots };
        } catch (error) {
            const raw = failureMessage(error).trim();
            return { content: raw || DESKTOP_BOT_NOT_PRIMARY, ask: false, noticeBots: undefined as DesktopBotNoticeBot[] | undefined };
        } finally {
            setAskingByBot(prev => {
                if (!prev[botId]) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            });
        }
    };

    const sayTypedConfirm = (botId: string) => {
        const prior = messagesForBot(userId, botId);
        commit(botId, [...prior, { id: messageID(), role: 'assistant', content: desktopBotTypedConfirmSentence(lang), chat: true }]);
    };
    const startTask = (botId: string, content: string, phase: BotPhase, mode: 'now' | 'fromQueue', verbatim = false, prepared?: PreparedTurn) => {
        if (content.trim() === CONFIRM_COMMAND) {
            sayTypedConfirm(botId);
            return;
        }
        const control = desktopBotLocalControl(content);
        if (control) {
            answerDesktopControl(botId, content, control, mode);
            return;
        }
        const turnPhase = phaseFor(content, phase);
        // A second job starts beside one that is already going. It must not
        // take the keyboard or the desktop watch away from that job. Only the
        // first job, or the continuation that the person asked to resume,
        // takes this bot's keyboard back. Another bot's desktop stays as it is.
        const siblingLive = [...pendingRef.current.values()].some(value => value.botId === botId && !value.settled);
        const takingKeyboard = !siblingLive || content.trim() === RETURN_COMMAND;
        if (!siblingLive) {
            setActivityByBot(prev => {
                if (!(botId in prev)) return prev;
                const next = { ...prev };
                delete next[botId];
                return next;
            });
        }
        const prior = messagesForBot(userId, botId).map(item => {
            // A turn this page already admitted stays live, however old the
            // bubble looks. Clearing it here would cancel the other job on
            // screen the moment a second one starts.
            const ticket = item.requestId || '';
            const live = ticket !== '' && pendingRef.current.get(ticket)?.settled === false;
            if (item.role === 'assistant' && item.pending && pendingReplyIsStale(item) && !live) {
                return { ...item, pending: false, userControl: false, attentionReason: '', content: item.content || text.userControl };
            }
            if (takingKeyboard && (item.userControl || item.attentionReason)) return { ...item, userControl: false, attentionReason: '' };
            return item;
        });
        const earlier = earlierUserAsks(prior, content, mode);
        const assistantId = messageID();
        // A local id keeps this bubble off the result store until the send
        // admits it. A late result must not settle the first pending message
        // that has no request id, or this order is never sent.
        const assistantMessage: DesktopBotMessage = { id: assistantId, role: 'assistant', content: '', pending: true, phase: turnPhase, requestId: `local:${assistantId}` };
        const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content, phase: turnPhase };
        const next = mode === 'now' ? [...prior, userMessage, assistantMessage] : [...prior, assistantMessage];
        rememberMessages(botId, next);
        noteLiveBotTurn(userId, botId, assistantMessage.id);
        if (takingKeyboard) {
            clearLiveFlags(botId);
            setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
            setTakeoverHoldByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        }
        const pending: PendingSend = { botId, messageId: assistantMessage.id, settled: false, admitted: false };
        const ticket = assistantMessage.requestId || `local:${assistantMessage.id}`;
        pendingRef.current.set(ticket, pending);
        const ownerId = userId;
        // loadGen only tells a stale bot-list response to drop itself. It
        // moves when that request is disposed. Disposal is not a new login.
        // This turn stops only when the login it captured is no longer the
        // one on screen.
        const ownerLeft = () => userIdRef.current !== ownerId;
        const failPending = (raw: string) => {
            if (pending.settled) return;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
            const stored = messagesForBot(ownerId, botId);
            // Nothing was sent. Without a phase this stays a side note, so a
            // confirmation that is still waiting keeps its button.
            commit(botId, stored.map(item => item.id === assistantMessage.id ? { ...item, content: raw, pending: false, failed: true, phase: undefined } : item));
            dropAutoPanel(botId);
            drainIfIdle(botId);
        };
        // No reading was made, so this turn has not started and nothing was
        // sent. The bubble keeps the sentence that was typed and offers the
        // two ways out: read again, or send those words exactly as they are.
        const parkNoReading = (kind: NoReading) => {
            if (pending.settled) return;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
            const stored = messagesForBot(ownerId, botId);
            commit(botId, stored.map(item => item.id === assistantMessage.id
                ? { ...item, ...noReadingReply(kind, text, content, turnPhase), pending: false }
                : item));
            dropAutoPanel(botId);
            drainIfIdle(botId);
        };
        // The message was meant for a run and no run took it — the run had
        // ended, or several were live and picking one would have been a guess.
        // The reason is shown and the words are parked so one click sends them
        // as a new turn; nothing here decides for the person.
        const parkUnrouted = (reason: string, words: string, phase: BotPhase) => {
            if (pending.settled) return;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
            const stored = messagesForBot(ownerId, botId);
            commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                ...item,
                content: reason,
                pending: false,
                failed: true,
                phase: undefined,
                noReading: { text: words, phase, kind: 'unrouted' },
            } : item));
            dropAutoPanel(botId);
            drainIfIdle(botId);
        };
        // Reading the message can take the whole model timeout. If the account
        // changes in that time, the order stays with the account that wrote it.
        const leaveAccount = () => {
            if (pending.settled) return;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
            const stored = messagesForBot(ownerId, botId);
            saveBotMessages(ownerId, botId, stored.map(item => item.id === assistantMessage.id ? { ...item, content: text.accountChanged, pending: false, failed: true, phase: undefined } : item));
        };
        const bubbleGone = () => {
            if (pending.settled) return true;
            if (messagesForBot(ownerId, botId).some(item => item.id === assistantMessage.id && item.pending)) return false;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
            return true;
        };
        void (async () => {
            let instruction = '';
            let told = '';
            let sendPhase: BotPhase = turnPhase;
            if (verbatim) {
                // No reading was made. The person asked for their own words,
                // and those words are still the clearest order there is.
                instruction = content.trim();
            } else if (prepared && prepared.instruction.trim() && (content.trim() === CONFIRM_COMMAND || sameContext(earlier, prepared.earlier))) {
                // A prepared confirm already holds the arrangement they approved.
                // Any other prepared line was read while it waited; a new line
                // that arrived in that wait is read again. An empty instruction
                // is not an order, so it falls through and is read.
                if (ownerLeft()) {
                    leaveAccount();
                    return;
                }
                if (bubbleGone()) return;
                told = prepared.told;
                instruction = prepared.instruction;
                if (prepared.persona) {
                    keepPersona(ownerId, botId, prepared.persona);
                    void PersistDesktopBotPreference(botId, prepared.persona).catch(() => undefined);
                }
                if (prepared.pace === 'direct' && turnPhase !== 'execute') {
                    const stored = messagesForBot(ownerId, botId);
                    commit(botId, stored.map(item => item.id === assistantMessage.id ? { ...item, phase: 'execute' as const } : item));
                }
                sendPhase = prepared.pace === 'direct' ? 'execute' : turnPhase;
            } else {
                let pace: WorkPace = 'confirm';
                let understood: Record<string, unknown> = {};
                let keepIdentity = '';
                try {
                    const reading = await readDesktopBotTurn(content, turnPhase, lang, earlier, personaForBot(ownerId, botId), desktopBotCatalog(botId, botsRef.current));
                    if (reading.kind !== 'read') {
                        // This turn has not started. It stops here with the
                        // sentence that was typed, which is why the reading
                        // failing is not the same as the turn failing.
                        if (!ownerLeft()) parkNoReading(reading.kind);
                        else leaveAccount();
                        return;
                    }
                    understood = reading.understood;
                    told = reading.told;
                    instruction = reading.instruction;
                    pace = reading.pace;
                    keepIdentity = reading.persona;
                    if (ownerLeft()) {
                        leaveAccount();
                        return;
                    }
                    if (bubbleGone()) return;
                    if (keepIdentity) {
                        keepPersona(ownerId, botId, keepIdentity);
                        // The worker runs on the server, so a preference kept
                        // only here would never shape future work. This is a
                        // hint for later turns and must not fail the turn.
                        // A hyphen withdraws it, and that has to reach the
                        // server too: a preference still honoured after being
                        // taken back explains itself nowhere on screen.
                        void PersistDesktopBotPreference(botId, keepIdentity).catch(() => undefined);
                    }
                    const cross = crossOf(understood);
                    if (cross) {
                        const delivered = await deliverCross(botId, cross);
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: delivered.content,
                            pending: false,
                            chat: delivered.ask ? undefined : true,
                            phase: undefined,
                            askUser: delivered.ask ? { question: delivered.content } : undefined,
                            noticeBots: delivered.noticeBots,
                        } : item));
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'ask') {
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const question = told || instruction;
                        const stored = messagesForBot(ownerId, botId);
                        // A question answered here is not a desktop turn. Leave the
                        // phase off so it does not cover an arrangement still waiting.
                        const settled = stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: question,
                            pending: false,
                            phase: undefined,
                            askUser: { question },
                        } : item);
                        commit(botId, settled);
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'status') {
                        let answer = '';
                        try {
                            answer = String(await QueryDesktopBotTaskStatus(botId, instruction || content, lang) || '').trim();
                        } catch (error) {
                            if (ownerLeft()) {
                                leaveAccount();
                                return;
                            }
                            failPending(friendlyFailure(failureMessage(error), lang) || text.unavailable);
                            return;
                        }
                        if (!answer) {
                            failPending(text.unavailable);
                            return;
                        }
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: answer,
                            pending: false,
                            chat: true,
                            phase: undefined,
                        } : item));
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'reply') {
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const answer = told || instruction;
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: answer,
                            pending: false,
                            chat: true,
                            phase: undefined,
                        } : item));
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'schedule') {
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        await armDesktopSchedule(botId, understood, instruction);
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const answer = scheduleAck(told, scheduleOf(understood).mode, text);
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: answer,
                            pending: false,
                            chat: true,
                            phase: undefined,
                        } : item));
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'confirm' && protocolControl(content) !== 'resume') {
                        if (ownerLeft()) {
                            leaveAccount();
                            return;
                        }
                        if (bubbleGone()) return;
                        pending.settled = true;
                        pendingRef.current.delete(ticket);
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        const arrangement = told || oneLine(content);
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? {
                            ...item,
                            content: arrangement,
                            pending: false,
                            phase: 'plan' as const,
                            workOrder: instruction,
                        } : item));
                        drainIfIdle(botId);
                        return;
                    }
                    if (pace === 'direct' && turnPhase !== 'execute') {
                        const stored = messagesForBot(ownerId, botId);
                        commit(botId, stored.map(item => item.id === assistantMessage.id ? { ...item, phase: 'execute' as const } : item));
                    }
                    sendPhase = pace === 'direct' ? 'execute' : turnPhase;
                } catch (error) {
                    // Nothing went to the desktop: either the reading could not
                    // even be attempted, or a timer named by nobody was refused.
                    if (ownerLeft()) {
                        leaveAccount();
                        return;
                    }
                    const raw = missedSchedule(error) ? text.scheduleNone : friendlyFailure(failureMessage(error), lang) || text.unavailable;
                    failPending(raw);
                    return;
                }
            }
            if (ownerLeft()) {
                leaveAccount();
                return;
            }
            if (bubbleGone()) return;
            // direct with no sentence still owes one line of the person's words.
            // A button accept is verbatim and must not grow another bubble.
            let sentence = told.trim();
            if (!sentence && !verbatim && sendPhase === 'execute') sentence = oneLine(content);
            if (sentence && (sentence !== content.trim() || !told.trim())) {
                const stored = messagesForBot(ownerId, botId);
                const at = stored.findIndex(item => item.id === assistantMessage.id);
                if (at >= 0) {
                    const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: sentence, ack: true, understood: true };
                    commit(botId, [...stored.slice(0, at), ackMessage, ...stored.slice(at)]);
                }
            }
            if (ownerLeft() || bubbleGone()) {
                if (ownerLeft()) leaveAccount();
                return;
            }
            // A turn already running may still want this message. Ask before
            // sending it as new work: once words are inside another run's
            // context there is no taking them back. Each live run is asked
            // on its own. One match is that run. Several matches have to be
            // named by the person. None means this is a separate job.
            try {
                const activeRuns = (await GetActiveDesktopRuns(botId) as Array<{ run_id?: string; task_text?: string; waiting_for_user?: boolean }>) || [];
                const candidates = activeRuns.filter(item => item && !item.waiting_for_user && String(item.run_id || '').trim() !== '');
                if (candidates.length > 0 && content) {
                    const checked = await Promise.all(candidates.map(async candidate => {
                        const verdict = await EvaluateDesktopTaskSupplement(String(candidate.task_text || ''), content.trim(), lang) as { is_supplement?: boolean; instruction?: string } | null;
                        return { candidate, verdict };
                    }));
                    const matches = checked.filter(item => item.verdict && item.verdict.is_supplement);
                    if (matches.length > 0) {
                        const targetID = matches.length === 1 ? String(matches[0].candidate.run_id || '') : '';
                        const receipt = await AppendDesktopBotSupplement(botId, targetID, String(matches[0].verdict?.instruction || content.trim())) as { accepted?: boolean; stage?: string; message?: string; supplement_id?: string } | null;
                        const note = String(receipt?.message || '已收到，会在当前任务的下一步生效。');
                        if (receipt?.stage === 'queued') {
                            // Only the receipt's ids go on the bubble. The
                            // terminal answer (applied / missed) arrives later
                            // from the run itself and lands on these ids.
                            const supplementId = String(receipt.supplement_id || '').trim();
                            const settled = messagesForBot(ownerId, botId).map(item => item.id === assistantMessage.id ? {
                                ...item,
                                content: note,
                                pending: false,
                                ack: true,
                                understood: true,
                                supplementId: supplementId || undefined,
                                supplementRunId: targetID || undefined,
                            } : item);
                            const folded = foldPlanConfirmation(settled, settled.findIndex(item => item.id === assistantMessage.id));
                            // Storage alone leaves the open chat on the typing
                            // bubble, so the person never sees that these words
                            // joined the job already running.
                            if (ownerLeft()) saveBotMessages(ownerId, botId, folded);
                            else commit(botId, folded);
                            clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                            pending.settled = true;
                            pendingRef.current.delete(ticket);
                            drainIfIdle(botId);
                            return;
                        }
                        // Several runs matched and the server will not guess.
                        // The words stay here, with the reason, until the
                        // person says which job they belong to.
                        if (receipt?.stage === 'ambiguous') {
                            if (!ownerLeft()) parkUnrouted(note, content.trim(), turnPhase);
                            else leaveAccount();
                            return;
                        }
                        // The run ended, or it is waiting for an answer. The
                        // receipt says this line goes out on its own, so it
                        // is sent. Parking it would leave that promise undone.
                    }
                }
            } catch {
                // Falling through and sending as a new task is honest: it does
                // what the person asked, just without folding into the run.
            }
            if (pending.settled || ownerLeft() || bubbleGone()) {
                if (ownerLeft()) leaveAccount();
                return;
            }
            // Admitted only once this line is actually a new job. Until then
            // a later line stays behind it, so a supplement is not overtaken
            // by the next command.
            pending.admitted = true;
            // This bot's desktop is already driving, so it does not take this
            // execute. Another bot's run is a different display. The reading's
            // sentence is already in the thread. This pending bubble becomes
            // the wait, and the order is sent once this desktop is free. A
            // drain uses fromQueue, so it does not queue again.
            if (mode === 'now' && sendPhase === 'execute' && desktopRunByBotRef.current[botId]) {
                pending.settled = true;
                pendingRef.current.delete(ticket);
                clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                const sentence = desktopBotQueuedSentence(lang);
                const stored = messagesForBot(ownerId, botId);
                commit(botId, stored.map(item => {
                    if (item.id === assistantMessage.id) {
                        return { ...item, content: sentence, pending: false, ack: true, phase: undefined };
                    }
                    if (item.id === userMessage.id) return { ...item, queued: true };
                    return item;
                }));
                const queued = queueRef.current.get(botId) || [];
                queued.push({ text: instruction, phase: sendPhase, verbatim: true, userText: content.trim() });
                queueRef.current.set(botId, queued);
                return;
            }
            try {
                const response = await SendDesktopBotTask(botId, instruction, sendPhase) as { request_id?: string; RequestID?: string; deferred?: boolean; Deferred?: boolean; text?: string; Text?: string; error?: string; Error?: string };
                if (ownerLeft()) {
                    pending.settled = true;
                    pendingRef.current.delete(ticket);
                    const requestId = String(response?.request_id || response?.RequestID || '');
                    const deferred = response?.deferred === true || response?.Deferred === true;
                    const stored = messagesForBot(ownerId, botId);
                    if (!deferred && (response?.text || response?.Text || response?.error || response?.Error)) {
                        const result = responseText(response as Record<string, unknown>, text.emptyResult);
                        const settled = stored.map(item => item.id === assistantMessage.id ? { ...item, content: result.content, pending: false, failed: result.failed, requestId: requestId || item.requestId } : item);
                        saveBotMessages(ownerId, botId, foldPlanConfirmation(settled, settled.findIndex(item => item.id === assistantMessage.id)));
                        clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                        return;
                    }
                    if (requestId) {
                        saveBotMessages(ownerId, botId, stored.map(item => item.id === assistantMessage.id ? { ...item, requestId } : item));
                        return;
                    }
                    clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                    return;
                }
                if (pending.settled) return;
                const requestId = String(response?.request_id || response?.RequestID || '');
                const deferred = response?.deferred === true || response?.Deferred === true;
                if (!deferred && (response?.text || response?.Text || response?.error || response?.Error)) {
                    pending.settled = true;
                    pendingRef.current.delete(ticket);
                    clearLiveBotTurn(userId, botId, assistantMessage.id);
                    const result = responseText(response as Record<string, unknown>, text.emptyResult);
                    const inline = response as Record<string, unknown>;
                    const screen = screenOf(inline);
                    const attention = screen.userControl || screen.reason !== '';
                    const stored = messagesForBot(userId, botId);
                    const settled = stored.map(item => item.id === assistantMessage.id ? {
                        ...item,
                        content: result.content,
                        pending: false,
                        failed: result.failed,
                        handoffUrl: attention ? screen.url : '',
                        userControl: screen.userControl,
                        attentionReason: screen.reason,
                        askUser: askFrom(inline) || item.askUser,
                    } : item);
                    commit(botId, foldPlanConfirmation(settled, settled.findIndex(item => item.id === assistantMessage.id)));
                    if (attention) applyScreen(botId, screen);
                    else if (result.failed) dropAutoPanel(botId);
                    drainIfIdle(botId);
                    return;
                }
                if (requestId) {
                    pendingRef.current.delete(ticket);
                    pendingRef.current.set(requestId, pending);
                    const stored = messagesForBot(userId, botId);
                    const keyed = stored.map(item => item.id === assistantMessage.id ? { ...item, requestId } : item);
                    commit(botId, keyed);
                    // This job is admitted. The next one already read can start
                    // beside it. Waiting for this reply would put the chat back
                    // in one-task mode.
                    drainIfIdle(botId);
                    return;
                }
                // Nothing to follow. The pending bubble ages out on the stale clock.
                clearLiveBotTurn(userId, botId, assistantMessage.id);
            } catch (error) {
                if (pending.settled) return;
                pending.settled = true;
                pendingRef.current.delete(ticket);
                clearLiveBotTurn(ownerId, botId, assistantMessage.id);
                const raw = friendlyFailure(failureMessage(error), lang) || text.unavailable;
                const stored = messagesForBot(ownerId, botId);
                const failed = stored.map(item => item.id === assistantMessage.id ? { ...item, content: raw, pending: false, failed: true } : item);
                if (ownerLeft()) {
                    saveBotMessages(ownerId, botId, failed);
                    return;
                }
                commit(botId, failed);
                dropAutoPanel(botId);
                drainIfIdle(botId);
            }
        })();
    };

    // A handoff reply has settled the HTTP turn, but the person still has the
    // keyboard. The next ordinary command calls openDesktop, which releases
    // that keyboard before the agent runs, so the login page is taken away.
    // Hold those messages. The continuation the person already asked for is
    // the command that is supposed to take the keyboard, so it goes first
    // even when a reload rebuilt the queue in chat order.
    const continuationQueued = (item: { text: string; phase: BotPhase }) =>
        item.phase === 'execute' && item.text === RETURN_COMMAND;
    const takeNextQueued = (botId: string, queue: QueuedSend[]) => {
        const resumeAt = queue.findIndex(continuationQueued);
        if (resumeAt >= 0) return queue.splice(resumeAt, 1)[0];
        if (messagesForBot(userId, botId).some(item => item.userControl || item.attentionReason)) return undefined;
        return queue.shift();
    };
    // A live desktop run holds the next execute. A line that is still being
    // read also holds the queue, so two jobs are not started out of order.
    // fromList is the task list saying the local flag is stale.
    const turnStillReading = (botId: string) =>
        [...pendingRef.current.values()].some(value => value.botId === botId && !value.settled && !value.admitted);
    // A bubble this page has already sent is not a lock. A pending bubble
    // left from a reload is: its run is still the one on screen, and the
    // lines parked behind it wait until that reply arrives.
    const pendingBubbleHoldsQueue = (botId: string) =>
        messagesForBot(userId, botId).some(message => {
            if (message.role !== 'assistant' || !message.pending || pendingReplyIsStale(message)) return false;
            const id = message.requestId || '';
            const slot = id ? pendingRef.current.get(id) : undefined;
            return !(slot && slot.botId === botId && slot.admitted && !slot.settled);
        });
    const drainIfIdle = (botId: string, fromList = false) => {
        if (stopHoldRef.current.has(botId)) return;
        const queue = queueRef.current.get(botId);
        if (!queue || queue.length === 0) return;
        if (!fromList && desktopRunByBotRef.current[botId]) return;
        if (turnStillReading(botId) || pendingBubbleHoldsQueue(botId)) return;
        const nextSend = takeNextQueued(botId, queue);
        if (!nextSend) return;
        queueRef.current.set(botId, queue);
        const dispatchQueued = (item: QueuedSend) => {
            const stored = messagesForBot(userId, botId);
            let cleared = false;
            // Dispatching the continuation means the person already finished this
            // handoff. A late reply can have written the prompt back; leave it
            // set and the next ordinary message waits forever.
            const resuming = continuationQueued(item);
            const next = stored.map(entry => {
                let copy = entry;
                if (resuming && (copy.userControl || copy.attentionReason)) {
                    copy = { ...copy, userControl: false, attentionReason: '' };
                }
                if (!cleared && copy.role === 'user' && copy.queued && (copy.content === item.text || (!!item.userText && copy.content === item.userText))) {
                    cleared = true;
                    copy = { ...copy, queued: false };
                }
                return copy;
            });
            commit(botId, next);
            startTask(botId, item.text, item.phase, 'fromQueue', item.verbatim === true, item.prepared);
        };
        // A handed-off order waits out the turn already on screen. Only this
        // branch is async. Everything else still starts in this turn.
        if (nextSend.crossNoteId) {
            const noteId = nextSend.crossNoteId;
            void (async () => {
                try {
                    if (!nextSend.claimed) {
                        const claim = await ClaimDesktopBotCrossStart(botId, noteId);
                        if (!claim?.claimed) return;
                        rememberDesktopBotCrossRun(userId, botId, noteId, String(claim.from || ''));
                    }
                    await ArmDesktopBotCrossRun(botId, noteId);
                } catch {
                    const current = queueRef.current.get(botId) || [];
                    queueRef.current.set(botId, [nextSend, ...current]);
                    return;
                }
                if (stopHoldRef.current.has(botId)) {
                    const current = queueRef.current.get(botId) || [];
                    queueRef.current.set(botId, [nextSend, ...current]);
                    return;
                }
                dispatchQueued(nextSend);
            })();
            return;
        }
        dispatchQueued(nextSend);
    };
    // A fast later reading must not run before an earlier message that is
    // still being classified. A local answer is not a queue item, and it
    // does not hold the ones behind it once it has settled.
    const releaseReadAhead = (ownerId: string, botId: string) => {
        if (stopHoldRef.current.has(botId)) return;
        const slots = readAheadRef.current.get(`${ownerId}\0${botId}`);
        if (!slots || slots.length === 0) return;
        let queued = queueRef.current.get(botId) || [];
        let added = false;
        while (slots.length > 0 && slots[0].settled) {
            const item = slots.shift();
            if (!item?.enqueue) continue;
            queued = [...queued, { text: item.text, phase: item.phase, verbatim: item.verbatim, prepared: item.prepared }];
            added = true;
        }
        if (slots.length === 0) readAheadRef.current.delete(`${ownerId}\0${botId}`);
        if (!added || userIdRef.current !== ownerId) return;
        queueRef.current.set(botId, queued);
        drainIfIdle(botId);
    };
    followUps.drain = drainIfIdle;
    followUps.release = releaseReadAhead;
    drainDeskRef.current = drainIfIdle;
    drainAllDeskRef.current = () => {
        for (const id of [...queueRef.current.keys()]) drainIfIdle(id);
    };
    landCrossRef.current = (notes) => {
        const bots = botsRef.current;
        if (bots.length === 0) {
            pendingCrossNotes.current = notes;
            return;
        }
        pendingCrossNotes.current = [];
        const known = new Set(bots.map(bot => bot.id));
        const touched = new Set<string>();
        const show = (botId: string) => { if (botId) touched.add(botId); };
        for (const note of notes) {
            const id = String(note.id || '').trim();
            const kind = String(note.kind || '').trim().toLowerCase();
            const from = String(note.from || '').trim();
            const target = String(note.target || '').trim();
            const text = String(note.text || '').trim();
            const order = String(note.order || '').trim();
            if (!id || (kind !== 'notify' && kind !== 'delegate' && kind !== 'result')) continue;
            if (kind === 'result') {
                const replyTo = String(note.reply_to || note.replyTo || '').trim();
                const local = replyTo !== '' && target !== '' && desktopBotNoticeSeen(userId, `${replyTo}:result:${target}`);
                if (target && known.has(target) && !local && !desktopBotNoticeSeen(userId, `${id}:target`)) {
                    appendDesktopBotReport(userId, target, { content: text, failed: false, requestId: `${id}:target`, chat: true });
                    markDesktopBotNotice(userId, `${id}:target`);
                    show(target);
                }
                if (from && known.has(from) && !desktopBotNoticeSeen(userId, `${id}:from`)) {
                    appendDesktopBotReport(userId, from, { content: text, failed: false, requestId: `${id}:from`, chat: true });
                    markDesktopBotNotice(userId, `${id}:from`);
                    show(from);
                }
                continue;
            }
            if (from && known.has(from) && !desktopBotNoticeSeen(userId, `${id}:from`)) {
                const name = bots.find(bot => bot.id === target)?.title || target;
                const line = kind === 'notify'
                    ? `跟 ${name} 说了一声：${text}`
                    : desktopBotDestructive(order || text)
                        ? (desktopBotDestructive(order || text) && (/安装/.test(order || text) || /(?:^|[^a-z])install(?:[^a-z]|$)/i.test(order || text))
                            ? `交给 ${name} 了。装软件还是得你在他那边点一下。`
                            : `交给 ${name} 了。这一步还是得你在他那边点一下。`)
                        : `交给 ${name} 了，他现在去做。`;
                appendDesktopBotReport(userId, from, { content: line, failed: false, requestId: `${id}:from`, chat: true });
                markDesktopBotNotice(userId, `${id}:from`);
                show(from);
            }
            if (!target || !known.has(target) || desktopBotNoticeSeen(userId, `${id}:target`)) continue;
            const body = order || text;
            const destructive = kind === 'delegate' && desktopBotDestructive(body);
            if (destructive) {
                appendDesktopBotReport(userId, target, {
                    content: desktopBotCrossTargetText(kind, text || order),
                    failed: false,
                    requestId: `${id}:target`,
                    phase: 'plan',
                    workOrder: body,
                    crossNoteId: id,
                });
            } else if (kind === 'delegate') {
                appendDesktopBotReport(userId, target, {
                    content: desktopBotCrossTargetText(kind, text || order),
                    failed: false,
                    requestId: `${id}:target`,
                    chat: true,
                });
                const busy = !!desktopRunByBotRef.current[target] || turnStillReading(target) || pendingBubbleHoldsQueue(target);
                const queued = queueRef.current.get(target) || [];
                queueRef.current.set(target, [...queued, { text: body, phase: 'execute', verbatim: true, crossNoteId: id }]);
                if (!busy) drainIfIdle(target);
            } else {
                appendDesktopBotReport(userId, target, {
                    content: desktopBotCrossTargetText(kind, text),
                    failed: false,
                    requestId: `${id}:target`,
                    chat: true,
                });
            }
            markDesktopBotNotice(userId, `${id}:target`);
            show(target);
        }
        if (touched.size === 0) return;
        setMessagesByBot(prev => {
            const next = { ...prev };
            for (const id of touched) next[id] = messagesForBot(userId, id);
            return next;
        });
    };
    useEffect(() => {
        let cancelled = false;
        const pull = async () => {
            let notes: Array<Record<string, unknown>> = [];
            try {
                notes = (await PullDesktopBotCrossNotes()) as Array<Record<string, unknown>>;
            } catch {
                return;
            }
            if (cancelled || userIdRef.current !== userId) return;
            landCrossRef.current(notes || []);
        };
        void pull();
        const timer = window.setInterval(() => { void pull(); }, 4000);
        return () => {
            cancelled = true;
            window.clearInterval(timer);
        };
    }, [userId]);
    useEffect(() => {
        const timer = window.setInterval(() => {
            const queuedIds = [...queueRef.current.entries()].filter(([, items]) => items.length > 0).map(([id]) => id);
            if (queuedIds.length === 0) return;
            const known = new Set<string>(queuedIds);
            for (const bot of botsRef.current) {
                if (bot.id) known.add(bot.id);
            }
            void (async () => {
                let blocked = false;
                for (const id of known) {
                    try {
                        const items = await ListDesktopBotTasks(id);
                        const live = (items || []).some(item => {
                            const status = String(item?.status || '').trim().toLowerCase();
                            return status !== 'succeeded' && status !== 'failed' && status !== 'cancelled';
                        });
                        if (live) blocked = true;
                    } catch {
                        blocked = true;
                    }
                }
                if (blocked) return;
                for (const id of queuedIds) drainDeskRef.current(id, true);
            })();
        }, 400);
        return () => window.clearInterval(timer);
    }, []);
    // A follow-up that is still being read has no queue entry yet. A later
    // command waits for that reading, then starts. It does not wait for the
    // desktop job itself to finish.
    const readAheadOpen = (ownerId: string, botId: string) =>
        (readAheadRef.current.get(`${ownerId}\0${botId}`) || []).some(slot => !slot.settled);

    // verbatim starts the turn from the person's own words, skipping the
    // reading. The reading is a helper for the turn, not a gate on it: once it
    // has failed and this person chose their own words, that choice is the order.
    const send = async (preset?: string, phase: BotPhase = 'plan', verbatim = false, prepared?: PreparedTurn) => {
        if (!selected) return;
        if (!preset && secretOpenRef.current) return;
        const botId = selected.id;
        const content = (preset ?? command).trim();
        if (!content) return;
        // Typing the old approval sentence is not an approval and not a task.
        if (content === CONFIRM_COMMAND) {
            if (!preset) setCommand('');
            sayTypedConfirm(botId);
            return;
        }
        // A generic status question or an exact stop is answered from the live
        // run. It does not wait for a reading that is already in flight, and
        // it does not become a queued order.
        const control = desktopBotLocalControl(content);
        if (control) {
            if (!preset) setCommand('');
            applyFollowRef.current(true);
            answerDesktopControl(botId, content, control, 'now');
            return;
        }
        // Sending is a new line the person just asked to see.
        applyFollowRef.current(true);
        const turnPhase = phaseFor(content, phase);
        // The login handoff is idle, so the composer would otherwise start a
        // desktop turn and take the keyboard back. A short answer still stays
        // here. Desktop work waits until the person returns the keyboard.
        // A button preset is the continuation itself, so it is not held.
        const personHasKeyboard = messagesForBot(userId, botId).some(item => item.userControl || !!item.attentionReason);
        // A running job does not block the next message. Hold only while an
        // earlier line is still being read, or while this person has the
        // keyboard and a new desktop command would take it away.
        const stillReading = turnStillReading(botId) || readAheadOpen(userId, botId);
        const waitingOnPerson = !preset && personHasKeyboard;
        if (stillReading || waitingOnPerson) {
            if (!preset) setCommand('');
            const prior = messagesForBot(userId, botId);
            const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content, phase: turnPhase };
            commit(botId, [...prior, userMessage]);
            const earlier = earlierUserAsks(prior, content, 'now');
            const ownerId = userId;
            const slot: ReadAhead = { text: content, phase: turnPhase, settled: false, enqueue: false, verbatim, prepared };
            const readKey = `${ownerId}\0${botId}`;
            const pendingReads = readAheadRef.current.get(readKey) || [];
            pendingReads.push(slot);
            readAheadRef.current.set(readKey, pendingReads);
            void (async () => {
                let enqueue = false;
                try {
                    if (content.trim() === CONFIRM_COMMAND || desktopBotLocalControl(content)) {
                        return;
                    }
                    if (slot.verbatim || slot.prepared) {
                        // Nothing to read. Verbatim is the person's own words.
                        // A prepared confirm already has the arrangement they
                        // approved, so a second reading cannot drop it.
                        enqueue = true;
                    } else {
                        let pace: WorkPace = 'confirm';
                        let told = '';
                        let instruction = '';
                        let understood: Record<string, unknown> = {};
                        const reading = await readDesktopBotTurn(content, turnPhase, lang, earlier, personaForBot(ownerId, botId), desktopBotCatalog(botId, botsRef.current));
                        if (userIdRef.current !== ownerId || slot.drop) return;
                        if (reading.kind !== 'read') {
                            // Nothing is queued and nothing was sent: the queue
                            // only holds work somebody stated.
                            const stored = messagesForBot(ownerId, botId);
                            commit(botId, [...stored, { id: messageID(), role: 'assistant', ...noReadingReply(reading.kind, text, content, turnPhase) }]);
                            return;
                        }
                        understood = reading.understood;
                        told = reading.told;
                        instruction = reading.instruction;
                        pace = reading.pace;
                        if (reading.persona && userIdRef.current === ownerId) keepPersona(ownerId, botId, reading.persona);
                        const cross = crossOf(understood);
                        if (cross) {
                            if (userIdRef.current !== ownerId || slot.drop) return;
                            const delivered = await deliverCross(botId, cross);
                            if (userIdRef.current !== ownerId || slot.drop) return;
                            const stored = messagesForBot(ownerId, botId);
                            const local: DesktopBotMessage = delivered.ask
                                ? { id: messageID(), role: 'assistant', content: delivered.content, askUser: { question: delivered.content } }
                                : { id: messageID(), role: 'assistant', content: delivered.content, chat: true, noticeBots: delivered.noticeBots };
                            commit(botId, [...stored, local]);
                            return;
                        }
                        if (pace === 'status') {
                            let answer = '';
                            try {
                                answer = String(await QueryDesktopBotTaskStatus(botId, instruction || content, lang) || '').trim();
                            } catch (error) {
                                if (userIdRef.current !== ownerId || slot.drop) return;
                                const raw = friendlyFailure(failureMessage(error), lang) || text.unavailable;
                                const stored = messagesForBot(ownerId, botId);
                                commit(botId, [...stored, { id: messageID(), role: 'assistant', content: raw, failed: true }]);
                                return;
                            }
                            if (userIdRef.current !== ownerId || slot.drop) return;
                            if (!answer) {
                                const stored = messagesForBot(ownerId, botId);
                                commit(botId, [...stored, { id: messageID(), role: 'assistant', content: text.unavailable, failed: true }]);
                                return;
                            }
                            const stored = messagesForBot(ownerId, botId);
                            commit(botId, [...stored, { id: messageID(), role: 'assistant', content: answer, chat: true }]);
                            return;
                        }
                        if (pace === 'schedule') {
                            const spec = scheduleOf(understood);
                            try {
                                await armDesktopSchedule(botId, understood, instruction);
                            } catch (error) {
                                if (userIdRef.current !== ownerId || slot.drop) return;
                                const raw = missedSchedule(error)
                                    ? text.scheduleNone
                                    : friendlyFailure(failureMessage(error), lang) || text.unavailable;
                                const stored = messagesForBot(ownerId, botId);
                                commit(botId, [...stored, { id: messageID(), role: 'assistant', content: raw, failed: true }]);
                                return;
                            }
                            if (userIdRef.current !== ownerId || slot.drop) return;
                            const answer = scheduleAck(told, spec.mode, text);
                            const stored = messagesForBot(ownerId, botId);
                            commit(botId, [...stored, { id: messageID(), role: 'assistant', content: answer, chat: true }]);
                            return;
                        }
                        if (pace === 'confirm' && protocolControl(content) !== 'resume') {
                            if (userIdRef.current !== ownerId || slot.drop) return;
                            const arrangement = told || oneLine(content);
                            const stored = messagesForBot(ownerId, botId);
                            const at = stored.findIndex(item => item.id === userMessage.id);
                            const plan: DesktopBotMessage = {
                                id: messageID(),
                                role: 'assistant',
                                content: arrangement,
                                phase: 'plan',
                                workOrder: instruction,
                            };
                            const user = { ...userMessage, queued: false };
                            const next = at >= 0
                                ? [...stored.slice(0, at), user, plan, ...stored.slice(at + 1)]
                                : [...stored, user, plan];
                            commit(botId, next);
                            return;
                        }
                        // Small talk and a missing-fact question are answered here.
                        // They do not wait for the desktop turn, and they are not sent on.
                        if (pace === 'reply' || pace === 'ask') {
                            const answer = told || instruction;
                            if (!answer) {
                                pace = 'confirm';
                            } else {
                                const stored = messagesForBot(ownerId, botId);
                                const local: DesktopBotMessage = pace === 'ask'
                                    ? { id: messageID(), role: 'assistant', content: answer, askUser: { question: answer } }
                                    : { id: messageID(), role: 'assistant', content: answer, chat: true };
                                commit(botId, [...stored, local]);
                                return;
                            }
                        }
                        slot.prepared = {
                            told,
                            instruction,
                            pace: pace === 'direct' ? 'direct' : 'confirm',
                            persona: reading.persona || '',
                            earlier,
                        };
                        const stored = messagesForBot(ownerId, botId);
                        const at = stored.findIndex(item => item.id === userMessage.id);
                        const queuedUser: DesktopBotMessage = { ...userMessage, queued: true };
                        const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: queuedAckReply(content, lang, waitingOnPerson ? 'keyboard' : 'ahead'), ack: true };
                        const next = at >= 0
                            ? [...stored.slice(0, at), queuedUser, ackMessage, ...stored.slice(at + 1)]
                            : [...stored, queuedUser, ackMessage];
                        commit(botId, next);
                        enqueue = true;
                    }
                } finally {
                    slot.wanted = enqueue && userIdRef.current === ownerId;
                    slot.settled = true;
                    slot.enqueue = !!slot.wanted && !slot.drop;
                    if (!slot.drop) releaseReadAhead(ownerId, botId);
                }
            })();
            return;
        }
        startTask(botId, content, turnPhase, 'now', verbatim, prepared);
        if (!preset) setCommand('');
    };

    const markAskResolved = (botId: string, messageId: string) => {
        const stored = messagesForBot(userId, botId);
        commit(botId, stored.map(item => item.id === messageId ? { ...item, askResolved: true } : item));
    };

    // A parked sentence offers one choice each. Taking one takes the buttons
    // away with it: the choice has been made, and a second click would send
    // the same words to the desktop a second time.
    const consumeNoReading = (botId: string, messageId: string) => {
        const stored = messagesForBot(userId, botId);
        commit(botId, stored.map(item => item.id === messageId && item.noReading ? { ...item, noReading: undefined } : item));
    };

    const appendNote = (botId: string, content: string) => {
        const stored = messagesForBot(userId, botId);
        commit(botId, [...stored, { id: messageID(), role: 'assistant', content, ack: true }]);
    };

    const fillSaved = async (botId: string, name: string, messageId: string) => {
        markAskResolved(botId, messageId);
        try {
            await FillBotSecret(botId, name);
        } catch (error) {
            const raw = error instanceof Error ? error.message : '';
            const code = raw === 'not_password_field' || raw === 'no_focus' || raw === 'unavailable' ? raw : 'unavailable';
            appendNote(botId, code === 'not_password_field' ? text.fillMiss : text.secretStoreFailed);
            return;
        }
        await send(fillSentence(name), 'execute');
    };

    const saveSecret = async () => {
        if (!secretPrompt || !secretDraft) return;
        const { botId, messageId, name, fill } = secretPrompt;
        const value = secretDraft;
        try {
            await SaveBotSecret(name, value);
        } catch {
            setSecretDraft('');
            appendNote(botId, text.secretStoreFailed);
            return;
        }
        setSecretDraft('');
        setSecretPrompt(null);
        if (fill) {
            await fillSaved(botId, name, messageId);
            return;
        }
        markAskResolved(botId, messageId);
        await send(saveSentence(name), 'plan');
    };

    const resumeFromScreenHandoff = (botId: string) => {
        closePanel(botId);
        clearLiveFlags(botId);
        latchRef.current[botId] = true;
        const stored = messagesForBot(userId, botId).map(item => (
            item.userControl || item.attentionReason ? { ...item, userControl: false, attentionReason: '' } : item
        ));
        commit(botId, stored);
        void send(text.returnCommand, 'execute');
    };

    const openForPerson = (botId: string) => {
        openPanel(botId, 'user');
        setRetryByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        const liveTurn = turnLive(botId);
        let handoff: DesktopBotMessage | undefined;
        for (let i = items.length - 1; i >= 0; i--) {
            const item = items[i];
            const pending = item.role === 'assistant' && item.pending && (liveTurn || !pendingReplyIsStale(item));
            if (pending) {
                // The current turn is on the desktop. An older login must not
                // be copied onto the live view, or takeover opens over that turn.
                if (!(item.userControl || item.attentionReason)) return;
                if (item.handoffUrl) handoff = item;
                break;
            }
            if ((item.userControl || item.attentionReason) && item.handoffUrl) {
                handoff = item;
                break;
            }
        }
        if (!handoff?.handoffUrl) return;
        latchRef.current[botId] = true;
        syncLive(botId, { url: handoff.handoffUrl, userControl: handoff.userControl === true, attentionReason: handoff.attentionReason || '' });
    };

    const collapseStage = (botId: string) => {
        const handoff = userHasControl || liveControl || liveReason !== '';
        closePanel(botId);
        clearLiveFlags(botId);
        if (handoff) latchRef.current[botId] = true;
    };

    // Fullscreen is voluntary. Leaving it only returns the panel to the column.
    // Finishing a login is the chat's 登录完成，继续, which closes the panel and
    // sends the continuation. Those are different operations.
    const releaseTakeover = (botId: string) => {
        setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        setTakeoverHoldByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
    };

    const enterTakeover = (botId: string) => {
        if (botUsingDesktop(botId)) return;
        setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        setTakeoverHoldByBot(prev => ({ ...prev, [botId]: true }));
        setTakeoverByBot(prev => ({ ...prev, [botId]: true }));
        openPanel(botId, 'user', true);
        showComputer(botId);
    };

    const expandScreen = (botId: string) => {
        if (screenExpandedByBot[botId]) return;
        pendingFitFocus.current = 'restore';
        setScreenExpandedByBot(prev => prev[botId] ? prev : { ...prev, [botId]: true });
    };

    const restoreScreen = (botId: string) => {
        if (!screenExpandedByBot[botId]) return;
        pendingFitFocus.current = 'expand';
        setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
    };

    const toggleTakeover = () => {
        if (!selected || agentOperating) return;
        const botId = selected.id;
        if (takeoverByBot[botId]) {
            releaseTakeover(botId);
            return;
        }
        setScreenExpandedByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        setTakeoverByBot(prev => ({ ...prev, [botId]: true }));
        openPanel(botId, 'user');
        showComputer(botId);
    };

    const togglePanel = () => {
        if (!selected) return;
        if (showDesktop) collapseStage(selected.id);
        else openForPerson(selected.id);
    };

    const retryConnection = async (botId: string) => {
        // The stage already has a generation. Minting another one makes the
        // polls that are still using this one look stale, and the picture drops.
        const epoch = watchEpochByBot.current[botId];
        if (!epoch) return;
        try {
            const view = await WatchDesktopBot(botId, epoch) as { novnc_url?: string; user_control?: boolean; attention_reason?: string } | undefined;
            // This retry belongs to the open that started it. The stage may
            // have closed while the call was out; opening it again puts the
            // picture back and keeps the desktop up.
            if (stageEpoch.current !== epoch) return;
            const panel = panelsRef.current[botId];
            if (panel !== 'auto' && panel !== 'user') return;
            const url = String(view?.novnc_url || '');
            if (!url) return;
            setRetryByBot(prev => ({ ...prev, [botId]: false }));
            syncLive(botId, { url, userControl: view?.user_control === true, attentionReason: String(view?.attention_reason || '') });
            openPanel(botId, 'user');
        } catch {
            if (stageEpoch.current !== epoch) return;
            const panel = panelsRef.current[botId];
            if (panel !== 'auto' && panel !== 'user') return;
            setRetryByBot(prev => ({ ...prev, [botId]: true }));
        }
    };

    useEffect(() => {
        if (!selected) return;
        const msg = [...messages].reverse().find(item => item.askUser && !item.askResolved && item.askUser.secretName && (item.askUser.inputType === 'secret' || item.askUser.inputType === 'secret_fill'));
        if (!msg?.askUser?.secretName) return;
        if (secretHandled.current.has(msg.id)) return;
        secretHandled.current.add(msg.id);
        const name = msg.askUser.secretName;
        const fill = msg.phase === 'execute' && msg.askUser.inputType === 'secret_fill';
        if (!fill) {
            setSecretPrompt({ botId: selected.id, messageId: msg.id, name, fill: false });
            return;
        }
        void (async () => {
            let exists = false;
            try { exists = await RecallBotSecret(name) === true; } catch { exists = false; }
            if (exists) await fillSaved(selected.id, name, msg.id);
            else setSecretPrompt({ botId: selected.id, messageId: msg.id, name, fill: true });
        })();
    }, [selectedId, messages]);

    const confirmMessage = (() => {
        if (!selected) return null;
        let blocked = false;
        for (let i = messages.length - 1; i >= 0; i--) {
            const item = messages[i];
            if (item.role !== 'assistant' || sideNote(item)) continue;
            if (item.pending && (turnLive(selected.id) || !pendingReplyIsStale(item))) {
                blocked = true;
                continue;
            }
            if (item.pending) continue;
            // A later line still being read can change this plan, so the
            // button waits for that line. An older job that is already
            // running does not: confirming this plan starts beside it.
            if (item.phase === 'plan' && !item.failed && desktopTaskStep(userRequestBefore(messages, i), item) === 'arrange') return { id: item.id, disabled: blocked };
            return null;
        }
        return null;
    })();

    const personAtKeyboard = liveControl || liveReason !== '' || userHasControl;
    const stageStatusText = agentOperating && personAtKeyboard
        ? text.takeoverBusyHint
        : takeover && !liveControl && liveReason === ''
            ? text.takeoverActive
            : personAtKeyboard
                ? text.userControl
                : (statusPhase ? text.botControl : text.watching);

    const secretForSelected = secretPrompt && selected && secretPrompt.botId === selected.id ? secretPrompt : null;

    const persistListWidth = (width: number) => {
        preferredListWidthRef.current = width;
        setPreferredListWidth(width);
        try { localStorage.setItem(LIST_WIDTH_KEY, String(width)); } catch { /* private mode */ }
    };
    const resizeListTo = (next: number) => {
        const container = workspaceRef.current?.clientWidth || listContainerWidth;
        const width = clampListWidth(next, container, botListPanelBeside(showDesktop));
        // The column is already at the room the window can give. Keep a wider
        // saved width so it comes back when the profile panel closes.
        if (next > listWidth && width <= listWidth && preferredListWidthRef.current >= listWidth) return;
        if (width === preferredListWidthRef.current) return;
        persistListWidth(width);
    };
    const onListResizePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
        if (event.button !== 0) return;
        const container = workspaceRef.current?.clientWidth || listContainerWidth;
        const origin = preferredListWidthRef.current;
        // Paint the width that is on screen. A click that never moves puts the
        // saved preference back, so a clamped window does not forget it.
        preferredListWidthRef.current = listWidth;
        listDragRef.current = {
            x: event.clientX,
            width: listWidth,
            container,
            panelBeside: botListPanelBeside(showDesktop),
            scale: pointerToLayout(workspaceRef.current),
            moved: false,
            origin,
        };
        setListResizing(true);
        event.currentTarget.setPointerCapture?.(event.pointerId);
    };
    const onListResizePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
        const drag = listDragRef.current;
        if (!drag) return;
        const next = clampListWidth(drag.width + (event.clientX - drag.x) / drag.scale, drag.container, drag.panelBeside);
        if (next === preferredListWidthRef.current) return;
        drag.moved = true;
        preferredListWidthRef.current = next;
        workspaceRef.current?.style.setProperty('--desktop-bot-list-width', `${next}px`);
        event.currentTarget.setAttribute('aria-valuenow', String(next));
    };
    const finishListResize = (event: ReactPointerEvent<HTMLDivElement>) => {
        const drag = listDragRef.current;
        if (!drag) return;
        listDragRef.current = null;
        if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
        if (!drag.moved) {
            preferredListWidthRef.current = drag.origin;
            setListResizing(false);
            return;
        }
        const width = preferredListWidthRef.current;
        setPreferredListWidth(width);
        setListResizing(false);
        try { localStorage.setItem(LIST_WIDTH_KEY, String(width)); } catch { /* private mode */ }
    };
    const onListResizeKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === 'ArrowLeft') { event.preventDefault(); resizeListTo(listWidth - 16); }
        else if (event.key === 'ArrowRight') { event.preventDefault(); resizeListTo(listWidth + 16); }
        else if (event.key === 'Home') { event.preventDefault(); resizeListTo(LIST_WIDTH_MIN); }
        else if (event.key === 'End') { event.preventDefault(); resizeListTo(listWidthMax); }
    };

    const paintIdentity = (pos: IdentityPos | null) => {
        const header = identityRef.current;
        const chat = chatRef.current;
        if (!header) return;
        // The pointer owns the pixels until it lets go. A resize notification
        // in the middle of a drag would snap the chip back to the last fraction.
        if (identityDragRef.current) return;
        if (!pos || !chat) {
            header.style.left = '';
            header.style.top = '';
            header.style.transform = '';
            header.classList.remove('is-placed');
            return;
        }
        const travel = identityTravel(chat, header);
        if (travel.width <= 0 || travel.height <= 0) return;
        const px = identityPixels(pos, travel);
        header.style.left = `${Math.round(px.left)}px`;
        header.style.top = `${Math.round(px.top)}px`;
        header.style.transform = 'none';
        header.classList.add('is-placed');
    };
    useLayoutEffect(() => {
        paintIdentity(identityPosRef.current);
    }, [identityPos, selectedId]);
    useLayoutEffect(() => {
        const chat = chatRef.current;
        if (!chat || typeof ResizeObserver === 'undefined') return;
        const observer = new ResizeObserver(() => paintIdentity(identityPosRef.current));
        observer.observe(chat);
        const form = chat.querySelector('.desktop-bot-chat__form');
        if (form) observer.observe(form);
        return () => observer.disconnect();
    }, [selectedId]);

    const logStamp = useMemo(() => botChatLogStamp(messages), [messages]);
    const secretMessageId = secretForSelected?.messageId || '';
    applyFollowRef.current = (follow: boolean) => {
        followLogRef.current = follow;
        logRef.current?.classList.toggle('is-following', follow);
    };
    pinLogRef.current = () => {
        if (!followLogRef.current) return;
        const log = logRef.current;
        if (!log) return;
        // Assigning scrollTop fires scroll on this turn. That event is ours.
        pinningLogRef.current = true;
        try {
            pinBotChatToBottom(log);
        } finally {
            pinningLogRef.current = false;
            logScrollTopRef.current = log.scrollTop;
        }
    };
    const onLogScroll = () => {
        const log = logRef.current;
        if (!log || pinningLogRef.current) return;
        const top = log.scrollTop;
        const previous = logScrollTopRef.current;
        logScrollTopRef.current = top;
        if (botChatIsNearBottom(log)) {
            applyFollowRef.current(true);
            return;
        }
        // Growth below the fold keeps scrollTop. Only an upward move leaves the tail.
        if (top < previous - 1) applyFollowRef.current(false);
    };
    useEffect(() => () => {
        logObserverRef.current?.disconnect();
        logObserverRef.current = null;
        if (logResizeFrameRef.current && typeof cancelAnimationFrame === 'function') {
            cancelAnimationFrame(logResizeFrameRef.current);
            logResizeFrameRef.current = 0;
        }
    }, []);
    useLayoutEffect(() => {
        if (openBotForScrollRef.current !== selectedId) {
            openBotForScrollRef.current = selectedId;
            applyFollowRef.current(true);
        } else {
            applyFollowRef.current(followLogRef.current);
        }
        const log = logRef.current;
        const body = logBodyRef.current;
        if (!log || !body) {
            logObserverRef.current?.disconnect();
        } else if (typeof ResizeObserver !== 'undefined') {
            if (!logObserverRef.current) {
                // The body grows when a picture or a wrapped line settles. The
                // log box changes when the column does. One observer covers both.
                logObserverRef.current = new ResizeObserver(() => {
                    const scroller = logRef.current;
                    if (scroller && botChatIsNearBottom(scroller)) applyFollowRef.current(true);
                    if (!followLogRef.current || logResizeFrameRef.current || typeof requestAnimationFrame !== 'function') return;
                    logResizeFrameRef.current = requestAnimationFrame(() => {
                        logResizeFrameRef.current = 0;
                        pinLogRef.current();
                    });
                });
            }
            logObserverRef.current.observe(log);
            logObserverRef.current.observe(body);
        }
        let frame = 0;
        if (followLogRef.current) {
            pinLogRef.current();
            if (typeof requestAnimationFrame === 'function') {
                frame = requestAnimationFrame(() => pinLogRef.current());
            }
        }
        return () => {
            if (frame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frame);
        };
    }, [selectedId, logStamp, running, secretMessageId]);

    const onIdentityPointerDown = (event: ReactPointerEvent<HTMLButtonElement>) => {
        if (event.button !== 0) return;
        const chat = chatRef.current;
        const header = identityRef.current;
        if (!chat || !header) return;
        const scale = pointerToLayout(chat);
        const chatRect = chat.getBoundingClientRect();
        const headerRect = header.getBoundingClientRect();
        identityDragRef.current = {
            pointerId: event.pointerId,
            startX: event.clientX,
            startY: event.clientY,
            originLeft: (headerRect.left - chatRect.left) / scale,
            originTop: (headerRect.top - chatRect.top) / scale,
            moved: false,
            scale,
        };
        // Capture and the iframe shield start inside this handler. The desktop
        // picture is another document; a shield that waits for React to paint
        // is still up only after the pointer can already have entered it.
        event.currentTarget.setPointerCapture?.(event.pointerId);
        workspaceRef.current?.classList.add('is-identity-dragging');
        if (!identityDragging) setIdentityDragging(true);
    };
    const onIdentityPointerMove = (event: ReactPointerEvent<HTMLButtonElement>) => {
        const drag = identityDragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;
        const chat = chatRef.current;
        const header = identityRef.current;
        if (!chat || !header) return;
        const dx = (event.clientX - drag.startX) / drag.scale;
        const dy = (event.clientY - drag.startY) / drag.scale;
        if (!drag.moved && Math.hypot(dx, dy) < IDENTITY_DRAG_SLOP) return;
        const travel = identityTravel(chat, header);
        if (travel.width <= 0 || travel.height <= 0) return;
        drag.moved = true;
        const left = Math.min(IDENTITY_MARGIN + travel.maxX, Math.max(IDENTITY_MARGIN, drag.originLeft + dx));
        const top = Math.min(IDENTITY_MARGIN + travel.maxY, Math.max(IDENTITY_MARGIN, drag.originTop + dy));
        identityPosRef.current = identityFraction(left, top, travel);
        header.style.left = `${Math.round(left)}px`;
        header.style.top = `${Math.round(top)}px`;
        header.style.transform = 'none';
        header.classList.add('is-placed');
    };
    const finishIdentityDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
        const drag = identityDragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;
        identityDragRef.current = null;
        workspaceRef.current?.classList.remove('is-identity-dragging');
        if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
        setIdentityDragging(false);
        if (!drag.moved) return;
        // The click that follows pointerup must not open the desktop. Capture
        // can end with lostpointercapture before that click, so both arm the
        // suppressor. pointercancel does not send a click and must not arm it.
        // A pointerup whose click never arrives clears the flag on the next turn.
        if (event.type !== 'pointercancel') {
            identitySuppressClick.current = true;
            window.setTimeout(() => {
                identitySuppressClick.current = false;
            }, 0);
        }
        const pos = identityPosRef.current;
        setIdentityPos(pos);
        try {
            if (pos) localStorage.setItem(IDENTITY_POS_KEY, JSON.stringify(pos));
        } catch { /* private mode */ }
    };
    const onIdentityClick = () => {
        if (identitySuppressClick.current) {
            identitySuppressClick.current = false;
            return;
        }
        togglePanel();
    };
    const customModelMark = creditRoute === 'custom' ? text.llmCustomMark(customModelDetailText) : '';
    return (
        <section
            ref={workspaceRef}
            className={'desktop-bot-workspace' + (listResizing ? ' is-list-resizing' : '') + (identityDragging ? ' is-identity-dragging' : '')}
            data-testid="desktop-bot-workspace"
            data-user-id={userId}
            style={{ '--desktop-bot-list-width': `${listWidth}px` } as CSSProperties}
        >
            {handoffNotice ? (
                <button
                    type="button"
                    className="desktop-bot-handoff-notice"
                    data-testid="desktop-bot-handoff-notice"
                    onClick={() => openHandoffScreen(handoffNotice)}
                >
                    <strong>{bots.find(bot => bot.id === handoffNotice.botId)?.title || text.myDesktop}</strong>
                    <span>{handoffNotice.reason || text.handoffFallback}</span>
                </button>
            ) : null}
            <aside className="desktop-bot-workspace__list-pane" aria-label={text.listTitle}>
                <header className="desktop-bot-workspace__bar">
                    <div className="desktop-bot-workspace__bar-head">
                        <h1><BotRailIcon size={20} testId="desktop-bot-title-icon" className="desktop-bot-workspace__title-icon" />{text.listTitle}</h1>
                        <div className="desktop-bot-workspace__bar-actions">
                            {customModelMark ? (
                                <span
                                    className="desktop-bot-workspace__custom-model"
                                    data-testid="desktop-bot-custom-model"
                                    role="img"
                                    title={customModelMark}
                                    aria-label={customModelMark}
                                >
                                    <CustomModelIcon />
                                </span>
                            ) : null}
                            <button type="button" className="desktop-bot-workspace__create" data-testid="desktop-bot-create" onClick={create}>{text.create}</button>
                            <button type="button" className="desktop-bot-workspace__settings" data-testid="desktop-bot-llm" aria-label={text.llmSettingsHint} title={text.llmSettingsHint} onClick={() => { void openLLMSettings(); }}>{text.llmSettings}</button>
                        </div>
                    </div>
                    <div className="desktop-bot-workspace__lede">
                        <p>{text.shared}</p>
                        <div className="desktop-bot-workspace__lede-credits">
                            {creditReadout ? (
                                <span className="desktop-bot-workspace__credit-readout" data-testid="desktop-bot-list-credits" data-notice={creditReadout.notice || 'ok'}>
                                    <CreditMark />
                                    <span className="desktop-bot-workspace__credit-label">{text.creditsLeft} </span>
                                    <span className="desktop-bot-workspace__credit-figures">
                                        {creditReadout.unlimited ? text.creditsUnlimited : `${formatDesktopCredit(creditReadout.remaining)} / ${formatDesktopCredit(creditReadout.total)}`}
                                    </span>
                                </span>
                            ) : null}
                            <button type="button" className="desktop-bot-workspace__lede-topup" data-testid="desktop-bot-list-topup" aria-label={text.creditTopUp} title={text.creditTopUp} onClick={() => openSettingsTab('redeem')}><CreditTopUpIcon /></button>
                        </div>
                    </div>
                </header>
                {listNotice ? <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-list-error" role="status">{listNotice}</p> : null}
                {bots.length === 0 && !listFailed ? (
                    <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-empty">{text.empty}</p>
                ) : (
                    <ul className="desktop-bot-workspace__list">
                        {bots.map(bot => {
                            const description = bot.description || text.defaultDescription;
                            const rowPhase = busyPhase(bot.id, messagesByBot[bot.id] || messagesForBot(userId, bot.id));
                            return (
                                <li key={bot.id} className={'desktop-bot-workspace__row' + (selectedId === bot.id ? ' is-selected' : '')} data-testid={`desktop-bot-${bot.id}`}>
                                    <button type="button" className="desktop-bot-workspace__open" aria-pressed={selectedId === bot.id} onClick={() => selectBot(bot)}>
                                        <span className="desktop-bot-workspace__avatar" style={{ background: botColor(bot.title) }} aria-hidden="true">{botInitial(bot.title)}</span>
                                        <span className="desktop-bot-workspace__who">
                                            {bot.primary ? (
                                                <span className="desktop-bot-workspace__titleline">
                                                    <strong>{bot.title}</strong>
                                                    <em className="desktop-bot-workspace__primary" data-testid={`desktop-bot-primary-${bot.id}`}>{text.primaryMark}</em>
                                                </span>
                                            ) : <strong>{bot.title}</strong>}
                                            <span>{description}</span>
                                        </span>
                                        {rowPhase ? (
                                            <span className="desktop-bot-workspace__state is-working" data-testid={`desktop-bot-row-state-${bot.id}`} aria-label={rowPhase === 'execute' ? text.workingTag : text.arrangingTag}>
                                                <i />
                                            </span>
                                        ) : null}
                                    </button>
                                    <div className="desktop-bot-workspace__actions">
                                        <button type="button" data-testid={`desktop-bot-edit-${bot.id}`} aria-label={text.editInfo} title={text.editInfo} onClick={() => { setEditBot(bot); setEditNotice(''); }}><RowActionIcon name="rename" /></button>
                                        <button type="button" data-testid={`desktop-bot-delete-${bot.id}`} aria-label={text.remove} title={text.remove} onClick={() => { void remove(bot.id); }}><RowActionIcon name="remove" /></button>
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </aside>
            <div
                className={'desktop-bot-workspace__resize' + (listResizing ? ' is-dragging' : '')}
                data-testid="desktop-bot-list-resize"
                role="separator"
                aria-orientation="vertical"
                aria-valuemin={LIST_WIDTH_MIN}
                aria-valuemax={listWidthMax}
                aria-valuenow={listWidth}
                aria-label={text.resizeList}
                title={text.resizeListHint}
                tabIndex={0}
                onPointerDown={onListResizePointerDown}
                onPointerMove={onListResizePointerMove}
                onPointerUp={finishListResize}
                onPointerCancel={finishListResize}
                onLostPointerCapture={finishListResize}
                onDoubleClick={() => persistListWidth(LIST_WIDTH_DEFAULT)}
                onKeyDown={onListResizeKeyDown}
            />
            <section ref={chatRef} className="desktop-bot-chat" data-testid="desktop-bot-chat" aria-label={selected ? selected.title : text.pick}>
                {selected ? (
                    <>
                        <header ref={identityRef} className={'desktop-bot-chat__header' + (identityDragging ? ' is-dragging' : '')} data-testid="desktop-bot-identity">
                            <button
                                type="button"
                                className={'desktop-bot-chat__identity-btn' + (showDesktop ? ' is-open' : '')}
                                data-testid="desktop-bot-open-desktop"
                                aria-pressed={showDesktop}
                                aria-expanded={showDesktop}
                                title={text.moveIdentity}
                                onPointerDown={onIdentityPointerDown}
                                onPointerMove={onIdentityPointerMove}
                                onPointerUp={finishIdentityDrag}
                                onPointerCancel={finishIdentityDrag}
                                onLostPointerCapture={finishIdentityDrag}
                                onClick={onIdentityClick}
                            >
                                <span className="desktop-bot-workspace__avatar desktop-bot-chat__avatar" style={{ background: botColor(selected.title) }} aria-hidden="true">{botInitial(selected.title)}</span>
                                <span className="desktop-bot-chat__identity">
                                    <strong>{selected.title}</strong>
                                    <span className={'desktop-bot-chat__status' + (statusPhase ? ' is-working' : '')} data-testid="desktop-bot-status"><i aria-hidden="true" />{statusText}</span>
                                </span>
                                <svg className="desktop-bot-chat__chevron" viewBox="0 0 16 16" aria-hidden="true" data-testid="desktop-bot-identity-chevron">
                                    <path d="M6 3.5 11 8l-5 4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                            </button>
                        </header>
                        <div
                            ref={logRef}
                            className="desktop-bot-chat__log"
                            data-testid="desktop-bot-log"
                            onScroll={onLogScroll}
                        >
                            <div ref={logBodyRef} className="desktop-bot-chat__log-body">
                            {messages.length === 0 ? (
                                <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pickEmpty}</p>
                            ) : null}
                            {messages.filter(message => !message.pending).map(message => {
                                const rawBody = message.failed ? (friendlyFailure(message.content, lang) || message.content) : message.content;
                                const body = message.role === 'assistant' ? stripConfirmedOrderLead(rawBody) : rawBody;
                                const structured = message.role === 'assistant' && !message.failed && botMessageIsStructured(body);
                                return (
                                <div key={message.id} className={`desktop-bot-chat__entry desktop-bot-chat__entry--${message.role}`} data-message-role={message.role}>
                                    <div className={`desktop-bot-chat__bubble desktop-bot-chat__bubble--${message.role}${message.failed ? ' is-failed' : ''}${structured ? '' : ' is-plain'}`}>
                                        {structured ? <BotMessageBody text={body} /> : body}
                                        {message.role === 'assistant' && message.noticeBots && message.noticeBots.length > 0 ? (
                                            <span className="desktop-bot-chat__notice-bots" data-testid="desktop-bot-notice-bots">
                                                {message.noticeBots.map(bot => (
                                                    <span key={bot.id} className="desktop-bot-chat__notice-avatar" title={bot.name} aria-label={bot.name}>{botInitial(bot.name)}</span>
                                                ))}
                                            </span>
                                        ) : null}
                                        {message.role === 'assistant' && !message.failed && message.shotPaths?.length ? message.shotPaths.map(path => (
                                            <BotShot key={path} path={path} alt={text.screenshot} openLabel={text.viewShot} onOpen={openShot} />
                                        )) : null}
                                        {message.role === 'assistant' && !message.failed && !message.shotPaths?.length && message.images?.length ? message.images.map((image, index) => (
                                            <InlineChatShot
                                                key={`${message.id}:${index}:${image.data.length}`}
                                                mime={image.mime}
                                                data={image.data}
                                                alt={text.screenshot}
                                                openLabel={text.viewShot}
                                                onOpen={openShot}
                                            />
                                        )) : null}
                                        {message.role === 'assistant' && !message.failed && message.localPaths?.length ? (
                                            <TaskResultArtifacts paths={message.localPaths} messageId={message.id} lang={lang} onOpen={openBotFile} />
                                        ) : null}
                                        {message.role === 'assistant' && !message.failed && !message.localPaths?.length && message.files?.length ? message.files.map(file => (
                                            <button
                                                type="button"
                                                key={file.name}
                                                className="desktop-bot-chat__file"
                                                data-testid="desktop-bot-file"
                                                onClick={() => saveDesktopFile(file)}
                                            >
                                                {text.download} {file.name}
                                            </button>
                                        )) : null}
                                    </div>
                                    {confirmMessage && confirmMessage.id === message.id ? (
                                        <button type="button" className="desktop-bot-chat__action" data-testid="desktop-bot-confirm" disabled={confirmMessage.disabled} onClick={() => {
                                            if (!selected || confirmMessage.disabled) return;
                                            const stored = messagesForBot(userId, selected.id);
                                            const plan = stored.find(item => item.id === confirmMessage.id);
                                            const order = (plan?.workOrder || plan?.content || '').trim();
                                            if (!order || order === CONFIRM_COMMAND) return;
                                            const noteId = (plan?.crossNoteId || '').trim();
                                            const botId = selected.id;
                                            const queueOrder = (claimed: boolean) => {
                                                const queued = queueRef.current.get(botId) || [];
                                                queueRef.current.set(botId, [...queued, { text: order, phase: 'execute', verbatim: true, crossNoteId: noteId || undefined, claimed }]);
                                                const current = messagesForBot(userId, botId);
                                                commit(botId, [...current, { id: messageID(), role: 'assistant', content: desktopBotQueuedSentence(lang), ack: true }]);
                                            };
                                            if (!noteId) {
                                                if (desktopRunByBotRef.current[botId]) {
                                                    queueOrder(false);
                                                    return;
                                                }
                                                void startTask(botId, order, 'execute', 'fromQueue', true);
                                                return;
                                            }
                                            void (async () => {
                                                try {
                                                    const claim = await ClaimDesktopBotCrossStart(botId, noteId);
                                                    if (!claim?.claimed) return;
                                                    rememberDesktopBotCrossRun(userId, botId, noteId, String(claim.from || ''));
                                                } catch {
                                                    return;
                                                }
                                                if (desktopRunByBotRef.current[botId]) {
                                                    queueOrder(true);
                                                    return;
                                                }
                                                try { await ArmDesktopBotCrossRun(botId, noteId); } catch { /* the run id note is best-effort */ }
                                                void startTask(botId, order, 'execute', 'fromQueue', true);
                                            })();
                                        }}>{text.confirm}</button>
                                    ) : null}
                                    {message.askUser?.options && !message.askResolved && !message.userControl ? (
                                        <div className="desktop-bot-chat__choices">
                                            {message.askUser.options.map((option, index) => (
                                                <button type="button" key={option} onClick={() => { markAskResolved(selected.id, message.id); void send(option, message.phase === 'execute' ? 'execute' : 'plan'); }}>
                                                    <span className="desktop-bot-chat__choice-key" aria-hidden="true">{String.fromCharCode(65 + (index % 26))}</span>
                                                    <span>{option}</span>
                                                    <span className="desktop-bot-chat__choice-mark" aria-hidden="true">✓</span>
                                                </button>
                                            ))}
                                        </div>
                                    ) : null}
                                    {message.noReading ? (
                                        <div className="desktop-bot-chat__choices" data-testid="desktop-bot-no-reading">
                                            {/* Timed work whose time was never named cannot be armed by a
                                                verbatim resend; only the person can say when it should run. */}
                                            {message.noReading.kind === 'unscheduled' ? null : (
                                                <button type="button" data-testid="desktop-bot-reading-verbatim" onClick={() => { consumeNoReading(selected.id, message.id); void send(message.noReading!.text, message.noReading!.phase, true); }}>
                                                    <span className="desktop-bot-chat__choice-key" aria-hidden="true">→</span>
                                                    <span>{text.readingVerbatim}</span>
                                                </button>
                                            )}
                                            {/* Reading it again only makes sense when the reading failed. A
                                                message that was routed nowhere was understood perfectly well;
                                                offering to re-read it would send the person back into the
                                                same branch that just declined it. */}
                                            {message.noReading.kind === 'unrouted' ? null : (
                                                <button type="button" data-testid="desktop-bot-reading-again" onClick={() => { consumeNoReading(selected.id, message.id); void send(message.noReading!.text, message.noReading!.phase); }}>
                                                    <span className="desktop-bot-chat__choice-key" aria-hidden="true">⟳</span>
                                                    <span>{text.readingAgain}</span>
                                                </button>
                                            )}
                                        </div>
                                    ) : null}
                                    {takeoverMessage && takeoverMessage.id === message.id && !takeover ? (
                                        <button
                                            type="button"
                                            className="desktop-bot-chat__action"
                                            data-testid="desktop-bot-take-computer"
                                            disabled={botUsingDesktop(selected.id)}
                                            title={botUsingDesktop(selected.id) ? text.takeoverBusyHint : text.takeComputerHint}
                                            onClick={() => enterTakeover(selected.id)}
                                        >{text.takeComputer}</button>
                                    ) : null}
                                    {secretForSelected && secretForSelected.messageId === message.id ? (
                                        <form className="desktop-bot-chat__secret" data-testid="desktop-bot-secret" onSubmit={event => { event.preventDefault(); void saveSecret(); }}>
                                            <p>{text.secretHint}</p>
                                            <label>
                                                {secretForSelected.name}
                                                <input type="password" autoComplete="off" data-testid="desktop-bot-secret-input" value={secretDraft} onChange={event => setSecretDraft(event.target.value)} />
                                            </label>
                                            <div className="desktop-bot-chat__secret-actions">
                                                <button type="submit" className="desktop-bot-chat__action" data-testid="desktop-bot-secret-save">{text.secretSave}</button>
                                                <button type="button" className="desktop-bot-chat__action desktop-bot-chat__action--quiet" data-testid="desktop-bot-secret-cancel" onClick={() => { setSecretPrompt(null); setSecretDraft(''); markAskResolved(selected.id, message.id); }}>{text.secretCancel}</button>
                                            </div>
                                        </form>
                                    ) : null}
                                </div>
                                );
                            })}
                            {handoffMessage && !agentOperating ? (
                                <p className="desktop-bot-chat__entry">
                                    <button type="button" className="desktop-bot-chat__action" data-testid="desktop-bot-return-control" onClick={() => resumeFromScreenHandoff(selected.id)}>{text.returnControl}</button>
                                </p>
                            ) : null}
                            </div>
                        </div>
                        {creditReadout && creditRoute !== 'custom' && creditReadout.notice ? (
                            <div className="desktop-bot-credit" role="status" data-testid="desktop-bot-credit-notice">
                                <p>{creditReadout.notice === 'empty' ? text.creditEmpty : text.creditLow}</p>
                                <div className="desktop-bot-credit__actions">
                                    <button type="button" className="desktop-bot-credit__link" data-testid="desktop-bot-credit-topup" onClick={() => openSettingsTab('redeem')}>{text.creditTopUp}</button>
                                    <button type="button" className="desktop-bot-credit__link" data-testid="desktop-bot-credit-settings" onClick={() => { void openLLMSettings(); }}>{text.creditSettings}</button>
                                </div>
                            </div>
                        ) : null}
                        {botPending(selected.id) ? (
                            <p className="desktop-bot-chat__processing" data-testid="desktop-bot-processing" role="status"><span className="desktop-bot-chat__processing-mark" aria-hidden="true" /><span className="desktop-bot-chat__processing-label">{desktopBotWorkingLabel(askingByBot[selected.id] || '', activityByBot[selected.id]?.text || '')}</span></p>
                        ) : null}
                        <DesktopBotComposer
                            userId={userId}
                            botId={selected.id}
                            lang={lang}
                            value={command}
                            onChange={setCommand}
                            onSubmit={() => { void send(); }}
                            disabled={!!secretForSelected}
                            placeholder={text.messageTo(selected.title)}
                            ariaLabel={text.messageTo(selected.title)}
                            sendLabel={text.send}
                        />
                        <p className="desktop-bot-chat__hint">{secretForSelected ? text.secretHint : text.composerHint}</p>
                    </>
                ) : (
                    <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pick}</p>
                )}
            </section>
            {selected && showDesktop ? (
                <aside className="desktop-bot-panel" data-testid="desktop-bot-panel" aria-label={selected.title}>
                    <div className="desktop-bot-panel__toolbar">
                        <button type="button" className="desktop-bot-panel__close" data-testid="desktop-bot-stage-close" aria-label={text.hideDesktop} onClick={() => collapseStage(selected.id)}>×</button>
                    </div>
                    <div className="desktop-bot-panel__who">
                        <span className="desktop-bot-panel__avatar" style={{ background: botColor(selected.title) }} aria-hidden="true">{botInitial(selected.title)}</span>
                        <h2>{selected.title}</h2>
                        <p className="desktop-bot-panel__description">{selected.description || text.defaultDescription}</p>
                    </div>
                    <div className="desktop-bot-panel__tabs" role="tablist">
                        <button type="button" role="tab" id="desktop-bot-tab-details" aria-selected={panelSection === 'details'} aria-controls="desktop-bot-details" data-testid="desktop-bot-tab-details" onClick={() => setSectionByBot(prev => prev[selected.id] === 'details' ? prev : { ...prev, [selected.id]: 'details' })}>{text.details}</button>
                        <button type="button" role="tab" id="desktop-bot-tab-schedules" aria-selected={panelSection === 'schedules'} aria-controls="desktop-bot-schedules" data-testid="desktop-bot-tab-schedules" onClick={() => setSectionByBot(prev => prev[selected.id] === 'schedules' ? prev : { ...prev, [selected.id]: 'schedules' })}>{text.schedules}</button>
                        <button type="button" role="tab" id="desktop-bot-tab-computer" aria-selected={panelSection === 'computer'} aria-controls="desktop-bot-computer" data-testid="desktop-bot-tab-computer" onClick={() => showComputer(selected.id)}>{text.computer}</button>
                    </div>
                    {panelSection === 'details' ? (
                        <div className="desktop-bot-panel__details" id="desktop-bot-details" data-testid="desktop-bot-details" role="tabpanel" aria-labelledby="desktop-bot-tab-details">
                            <dl className="desktop-bot-panel__sheet">
                                <div className="desktop-bot-panel__field">
                                    <dt>{text.status}</dt>
                                    <dd>
                                        <span className={'desktop-bot-panel__badge' + (statusPhase ? ' is-working' : '')}>
                                            <i aria-hidden="true" />
                                            {statusText}
                                        </span>
                                    </dd>
                                </div>
                                <div className="desktop-bot-panel__field">
                                    <dt>{text.created}</dt>
                                    <dd>{formatCreated(selected.createdAt) || '—'}</dd>
                                </div>
                                {creditReadout && creditRoute !== 'custom' ? (
                                    <>
                                        <div className="desktop-bot-panel__field">
                                            <dt>{text.credits}</dt>
                                            <dd data-testid="desktop-bot-credits-total">{creditReadout.unlimited ? text.creditsUnlimited : formatDesktopCredit(creditReadout.total)}</dd>
                                        </div>
                                        <div className="desktop-bot-panel__field">
                                            <dt>{text.creditsLeft}</dt>
                                            <dd data-testid="desktop-bot-credits-left">{creditReadout.unlimited ? text.creditsUnlimited : formatDesktopCredit(creditReadout.remaining)}</dd>
                                        </div>
                                    </>
                                ) : null}
                                <div className="desktop-bot-panel__field desktop-bot-panel__field--stack">
                                    <dt>{text.description}</dt>
                                    <dd>{selected.description || text.defaultDescription}</dd>
                                </div>
                                <div className="desktop-bot-panel__field desktop-bot-panel__field--stack">
                                    <dt>{text.persona}</dt>
                                    <dd data-testid="desktop-bot-persona">{personaByBot[selected.id] || text.personaEmpty}</dd>
                                </div>
                            </dl>
                            {desktopAdmin ? (
                                <label className="desktop-bot-panel__mode" data-testid="desktop-bot-isolated-toggle">
                                    <input
                                        type="checkbox"
                                        checked={screenByBot[selected.id]?.mode === 'isolated'}
                                        disabled={modeSaving}
                                        onChange={event => { void saveDesktopMode(selected.id, event.target.checked ? 'isolated' : 'shared'); }}
                                    />
                                    <span>
                                        <strong>{text.isolatedDesktop}</strong>
                                        <small>{text.isolatedDesktopHint}</small>
                                    </span>
                                </label>
                            ) : null}
                            {modeError ? <p className="desktop-screen-switch__error" data-testid="desktop-bot-mode-error" role="status">{modeError}</p> : null}
                            <button
                                type="button"
                                className="desktop-bot-panel__action"
                                data-testid="desktop-bot-set-primary"
                                disabled={selected.primary === true}
                                onClick={() => { void makePrimary(selected.id); }}
                            >{selected.primary ? text.primaryNow : text.makePrimary}</button>
                            <p className="desktop-bot-panel__note"><SharedDesktopIcon /><span>{text.shared}</span></p>
                        </div>
                    ) : panelSection === 'schedules' ? (
                        <div className="desktop-bot-panel__schedules" id="desktop-bot-schedules" data-testid="desktop-bot-schedules" role="tabpanel" aria-labelledby="desktop-bot-tab-schedules">
                            {scheduleNotice ? <p className="desktop-bot-panel__empty" data-testid="desktop-bot-schedule-notice">{scheduleNotice}</p> : null}
                            {(schedulesByBot[selected.id] || []).length === 0 && !scheduleNotice ? (
                                <p className="desktop-bot-panel__empty" data-testid="desktop-bot-schedule-empty">{text.scheduleEmpty}</p>
                            ) : (
                                <ul className="desktop-bot-schedule-list">
                                    {(schedulesByBot[selected.id] || []).map(task => {
                                        const when = scheduleWhen(task, lang);
                                        const next = scheduleNext(task.next_run_at || '', lang);
                                        const paused = task.status === 'paused';
                                        return (
                                            <li key={task.id} className="desktop-bot-schedule" data-testid="desktop-bot-schedule-row">
                                                <div className="desktop-bot-schedule__body">
                                                    <div className="desktop-bot-schedule__name">{task.name || task.action}</div>
                                                    <div className="desktop-bot-schedule__when">{when}{next ? ` · ${next}` : ''}{paused ? ` · ${text.schedulePaused}` : ''}</div>
                                                    {task.action && task.action !== task.name ? <p className="desktop-bot-schedule__action">{task.action}</p> : null}
                                                </div>
                                                <button type="button" className="desktop-bot-schedule__delete" data-testid={`desktop-bot-schedule-delete-${task.id}`} onClick={() => { void removeSchedule(selected.id, task); }}>{text.remove}</button>
                                            </li>
                                        );
                                    })}
                                </ul>
                            )}
                        </div>
                    ) : (
                        <div className="desktop-bot-panel__computer" id="desktop-bot-computer" data-testid="desktop-bot-computer" role="tabpanel" aria-labelledby="desktop-bot-tab-computer">
                            <div className="desktop-screen-switch" role="group" aria-label={text.screenSwitch} data-testid="desktop-screen-switch">
                                <button
                                    type="button"
                                    aria-pressed={screenPickNow === 'mine'}
                                    data-testid="desktop-screen-mine"
                                    className={screenPickNow === 'mine' ? 'is-selected' : ''}
                                    onClick={() => chooseScreen('mine')}
                                >{text.myDesktop}</button>
                                {bots.map(bot => {
                                    const shared = (screenByBot[bot.id]?.mode || 'shared') !== 'isolated';
                                    return (
                                        <button
                                            type="button"
                                            key={bot.id}
                                            aria-pressed={screenPickNow === bot.id}
                                            data-testid={`desktop-screen-bot-${bot.id}`}
                                            className={screenPickNow === bot.id ? 'is-selected' : ''}
                                            onClick={() => chooseScreen(bot.id)}
                                        >
                                            {bot.title}
                                            {shared ? <em>{text.sharedScreen}</em> : null}
                                        </button>
                                    );
                                })}
                            </div>
                            {handBackError ? <p className="desktop-screen-switch__error" data-testid="desktop-bot-handback-error" role="status">{handBackError}</p> : null}
                            {!ownPictureDown && shownDesktopUrl ? (
                                <div ref={stageRef} className={'desktop-bot-stage' + (stageInteractive ? ' is-user' : ' is-bot') + ((takeover || screenExpanded) ? ' is-fullscreen' : '')} data-testid="desktop-bot-stage">
                                    <div className="desktop-bot-stage__bar">
                                        <p title={stageStatusText}>{stageStatusText}</p>
                                        {takeover || showHandBack ? (
                                            <div className="desktop-bot-stage__bar-actions">
                                                {showHandBack ? (
                                                    <button type="button" data-testid="desktop-bot-handback" disabled={handingBack} onClick={() => { void handBackToBot(handBackTarget); }}>
                                                        {handingBack ? text.handBackBusy : text.handBack}
                                                    </button>
                                                ) : null}
                                                {takeover ? (
                                                    <button type="button" data-testid="desktop-bot-exit-takeover" onClick={() => releaseTakeover(selected.id)}>{text.exitTakeover}</button>
                                                ) : null}
                                            </div>
                                        ) : null}
                                    </div>
                                    <div className="desktop-bot-stage__frame">
                                        <iframe
                                            key={frameMode}
                                            ref={handoffRef}
                                            className="desktop-bot-chat__handoff"
                                            data-testid="desktop-bot-handoff"
                                            data-input={frameMode}
                                            title="desktop"
                                            src={desktopFrameSrc(shownDesktopUrl, stageInteractive)}
                                            tabIndex={screenExpanded && !stageInteractive ? -1 : undefined}
                                            onFocus={screenExpanded && !stageInteractive ? () => { restoreBtnRef.current?.focus({ preventScroll: true }); } : undefined}
                                        />
                                        {stageInteractive ? null : (
                                            <div className="desktop-bot-stage__shield" data-testid="desktop-bot-screen-shield" aria-hidden="true" />
                                        )}
                                        {takeover ? null : screenExpanded ? (
                                            <button type="button" ref={restoreBtnRef} className="desktop-bot-stage__fit is-restore" data-testid="desktop-bot-screen-restore" aria-keyshortcuts="Escape" onClick={() => restoreScreen(selected.id)}>
                                                <ScreenRestoreIcon />
                                                {text.restoreScreen}
                                            </button>
                                        ) : (
                                            <button type="button" ref={expandBtnRef} className="desktop-bot-stage__fit" data-testid="desktop-bot-screen-expand" aria-label={text.expandScreen} title={text.expandScreen} onClick={() => expandScreen(selected.id)}>
                                                <ScreenExpandIcon />
                                            </button>
                                        )}
                                    </div>
                                    <div className="desktop-bot-stage__dock">
                                        <div className="desktop-bot-stage__mode" role="group" aria-label={text.screenMode}>
                                            <button
                                                type="button"
                                                className="desktop-bot-stage__mode-btn"
                                                data-testid="desktop-bot-watch-only"
                                                aria-pressed={!stageInteractive}
                                                disabled={stageInteractive && !takeover}
                                                title={stageInteractive && !takeover ? text.watchLockedHint : text.watching}
                                                onClick={() => { if (takeover) releaseTakeover(selected.id); }}
                                            >
                                                <ScreenWatchIcon />
                                                {text.viewOnly}
                                            </button>
                                            <button
                                                type="button"
                                                className="desktop-bot-stage__mode-btn"
                                                data-testid="desktop-bot-take-control"
                                                aria-pressed={stageInteractive}
                                                disabled={stageInteractive || agentOperating || otherIsolated}
                                                title={agentOperating ? text.takeoverBusyHint : text.takeoverHint}
                                                onClick={() => enterTakeover(selected.id)}
                                            >
                                                <ScreenControlIcon />
                                                {text.takeControl}
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            ) : ownerDesktopMissing && !ownPictureDown ? (
                                <div className="desktop-bot-screen" data-testid="desktop-bot-screen">
                                    <p>{text.noSharedDesktop}</p>
                                </div>
                            ) : retryForSelected ? (
                                <div className="desktop-bot-screen" data-testid="desktop-bot-screen">
                                    <p>{text.desktopDown}</p>
                                    <button type="button" data-testid="desktop-bot-retry" onClick={() => void retryConnection(selected.id)}>{text.retryConnect}</button>
                                </div>
                            ) : (
                                <div className="desktop-bot-screen" data-testid="desktop-bot-screen">
                                    <div className="desktop-bot-stage__loading" data-testid="desktop-bot-stage-loading">
                                        <span className="desktop-bot-stage__loading-dot" aria-hidden="true" />
                                        {text.connectingDesktop}
                                    </div>
                                </div>
                            )}
                            <p className="desktop-bot-screen__caption">{text.screenOf(selected.title)}</p>
                            <div className="desktop-bot-chat__ops" data-testid="desktop-bot-ops" aria-label={text.ops}>
                                {agentOperating ? (
                                    <span className="desktop-bot-chat__ops-note" role="status">{text.takeoverBusyHint}</span>
                                ) : null}
                                <button
                                    type="button"
                                    data-testid="desktop-bot-takeover"
                                    className={'desktop-bot-chat__takeover' + (takeover ? ' is-active' : '')}
                                    disabled={agentOperating || otherIsolated}
                                    title={agentOperating ? text.takeoverBusyHint : text.takeoverHint}
                                    onClick={toggleTakeover}
                                >
                                    {takeover ? text.exitTakeover : text.takeover}
                                </button>
                            </div>
                        </div>
                    )}
                </aside>
            ) : null}
            <BotFilePreview lang={lang} botId={selectedId} />
            {visibleShot ? <BotShotPreview src={visibleShot} alt={text.screenshot} closeLabel={text.closePreview} onClose={closeShot} /> : null}
            {editBot ? (
                <BotEditDialog
                    key={editBot.id}
                    initialTitle={editBot.title}
                    initialDescription={editBot.description}
                    labels={{
                        title: text.editTitle,
                        name: text.name,
                        description: text.description,
                        save: text.save,
                        cancel: text.cancel,
                        close: text.close,
                    }}
                    notice={editNotice}
                    saving={editSaving}
                    onClose={() => setEditBot(null)}
                    onSubmit={(title, description) => { void submitEdit(title, description); }}
                />
            ) : null}
            {llmOpen ? (
                <BotLLMSettingsDialog
                    labels={{
                        title: text.llmTitle,
                        sharedHint: text.llmSharedHint,
                        useDefault: text.llmUseDefault,
                        defaultHint: text.llmDefaultHint,
                        useProvider: text.llmUseProvider,
                        providerLabel: text.llmProviderLabel,
                        modelLabel: text.llmModelLabel,
                        modelLoading: text.llmModelLoading,
                        modelEmpty: text.llmModelEmpty,
                        noProviders: text.llmNoProviders,
                        save: text.save,
                        cancel: text.cancel,
                        close: text.close,
                    }}
                    providers={llmProviders}
                    initialMode={llmMode}
                    initialName={llmName}
                    initialModel={llmModel}
                    notice={llmNotice}
                    saving={llmSaving}
                    onClose={() => setLlmOpen(false)}
                    onSubmit={payload => { void submitLLM(payload); }}
                />
            ) : null}
        </section>
    );
}


import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import {
    CreateDesktopBot,
    DeleteDesktopBot,
    FillBotSecret,
    ListDesktopBots,
    RecallBotSecret,
    ReleaseDesktopBotWatch,
    RenameDesktopBot,
    SaveBotSecret,
    SendDesktopBotTask,
    WatchDesktopBot,
} from '../../../wailsjs/go/main/App';
import {
    messagesForBot,
    notePendingBotDesktop,
    saveBotMessages,
    settlePendingBotReply,
    type BotPhase,
    type DesktopBot,
    type DesktopBotAsk,
    type DesktopBotMessage,
} from './desktopBots';
import './DesktopBotWorkspace.css';

// noVNC copies the query value straight onto viewOnly. The string "0" is
// truthy there, so an empty value is what forces the keyboard and pointer on.
function desktopFrameSrc(url: string, interactive: boolean): string {
    if (!url) return '';
    const hashAt = url.indexOf('#');
    const base = hashAt >= 0 ? url.slice(0, hashAt) : url;
    const hash = hashAt >= 0 ? url.slice(hashAt) : '';
    return base + (base.includes('?') ? '&' : '?') + (interactive ? 'view_only=' : 'view_only=1') + hash;
}

const RETURN_COMMAND = '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。';
const CONFIRM_COMMAND = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';

// Composer discussion stays a plan. Execute is only the phase the confirm
// button, the resume button, and an in-execution fill continuation pass in.
function phaseFor(content: string, requested: BotPhase): BotPhase {
    if (planSaveName(content)) return 'plan';
    return requested === 'execute' ? 'execute' : 'plan';
}

function planSaveName(content: string): string {
    const match = /^已在本机保存 ([A-Z][A-Z0-9_]{0,63})。$/.exec(content.trim());
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

type LiveDesktop = { url: string; userControl: boolean; attentionReason: string };
type SecretPrompt = { botId: string; messageId: string; name: string; fill: boolean };
type ScreenEvent = { url: string; reported: boolean; userControl: boolean; reason: string; cleared: boolean };

function screenOf(data: Record<string, unknown>): ScreenEvent {
    const url = String(data.novnc_url || data.NovncURL || data.desktop_handoff_url || data.DesktopHandoffURL || '');
    const reported = ['user_control', 'desktop_user_control', 'UserControl', 'DesktopUserControl'].some(key => Object.prototype.hasOwnProperty.call(data, key));
    const userControl = data.user_control === true || data.desktop_user_control === true || data.UserControl === true || data.DesktopUserControl === true;
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

function copy(lang: string) {
    if (lang === 'en') {
        return {
            listTitle: 'Bots',
            shared: 'Each bot is another instance of your MaClawSrv user. They share your cloud desktop.',
            empty: 'No bots yet.',
            create: 'New',
            rename: 'Rename',
            save: 'Save',
            cancel: 'Cancel',
            remove: 'Delete',
            confirmRemove: 'Confirm',
            name: 'Name',
            description: 'Description',
            defaultDescription: 'Another instance of this MaClawSrv user. Shares this user\'s cloud desktop.',
            pick: 'Select a bot to send a task.',
            placeholder: 'Message',
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
            fillMiss: 'That password field cannot be filled from here. Use View desktop and type it yourself.',
            secretStoreFailed: 'The password was not saved on this computer.',
            ops: 'Actions',
            openDesktop: 'View desktop',
            hideDesktop: 'Hide desktop',
            takeover: 'Take over',
            exitTakeover: 'Release',
            takeoverHint: 'Fullscreen the desktop and take the keyboard from the bot.',
            takeoverBusyHint: 'The bot is driving the desktop — takeover is unavailable right now.',
            takeoverActive: 'You have the desktop. Release it to hand the keyboard back.',
            watching: 'Watching the desktop. It stays open while you watch.',
            connectingDesktop: 'Starting the desktop…',
            composerHint: 'Enter to send, Shift+Enter for a new line.',
            botControl: 'The bot is using the desktop.',
            stillThere: 'The website login is still in this browser. Send another message and this bot continues there.',
            userControl: 'Finish the login or verification in this browser. The login stays here for the bot.',
            returnControl: 'Login done, continue',
            returnCommand: RETURN_COMMAND,
            emptyResult: 'The backend returned no result.',
            unavailable: 'The backend agent is unavailable.',
        };
    }
    if (lang === 'zh-Hant') {
        return {
            listTitle: 'Bot',
            shared: '每個 Bot 都是你在 MaClawSrv 上的一個實例，共用你的雲端桌面。',
            empty: '還沒有 Bot。',
            create: '新建',
            rename: '改名',
            save: '儲存',
            cancel: '取消',
            remove: '刪除',
            confirmRemove: '確認',
            name: '名稱',
            description: '描述',
            defaultDescription: '目前用戶在 MaClawSrv 上的一個實例，共用雲端桌面。',
            pick: '選擇一個 Bot 後發送任務。',
            placeholder: '發消息',
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
            fillMiss: '這個密碼框在當前頁面填不了。請點「查看桌面」自己輸入。',
            secretStoreFailed: '密碼沒有保存到本機。',
            ops: '操作',
            openDesktop: '查看桌面',
            hideDesktop: '收起桌面',
            takeover: '人類接管',
            exitTakeover: '退出接管',
            takeoverHint: '全螢幕顯示桌面，並把鍵盤交給你。',
            takeoverBusyHint: 'Bot 正在操作桌面，現在不能接管。',
            takeoverActive: '你已接管桌面。完成後退出接管，把鍵盤交還。',
            watching: '正在觀看桌面。觀看期間桌面保持開啟。',
            connectingDesktop: '正在連接桌面…',
            composerHint: 'Enter 傳送，Shift+Enter 換行。',
            botControl: 'Bot 正在操作桌面。',
            stillThere: '網站登入還在這個瀏覽器裡。再發一條訊息，這個 bot 會接著操作。',
            userControl: '請在這個桌面的瀏覽器裡完成登入或驗證。登入會留在這個瀏覽器裡，完成後交還。',
            returnControl: '登入完成，繼續',
            returnCommand: RETURN_COMMAND,
            emptyResult: '後台沒有返回結果。',
            unavailable: '後台 agent 不可用。',
        };
    }
    return {
        listTitle: 'Bot',
        shared: '每个 Bot 都是你在 MaClawSrv 上的一个实例，共用你的云端桌面。',
        empty: '还没有 Bot。',
        create: '新建',
        rename: '改名',
        save: '保存',
        cancel: '取消',
        remove: '删除',
        confirmRemove: '确认',
        name: '名称',
        description: '描述',
        defaultDescription: '当前用户在 MaClawSrv 上的一个实例，共用云端桌面。',
        pick: '选择一个 Bot 后发送任务。',
        placeholder: '发消息',
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
        fillMiss: '这个密码框在当前页面填不了。请点「查看桌面」自己输入。',
        secretStoreFailed: '密码没有保存到本机。',
        ops: '操作',
        openDesktop: '查看桌面',
        hideDesktop: '收起桌面',
        takeover: '人类接管',
        exitTakeover: '退出接管',
        takeoverHint: '全屏显示桌面，并把键盘交给你。',
        takeoverBusyHint: 'Bot 正在操作桌面，现在不能接管。',
        takeoverActive: '你已接管桌面。完成后退出接管，把键盘交回。',
        watching: '正在观看桌面。观看期间桌面保持开启。',
        connectingDesktop: '正在连接桌面…',
        composerHint: 'Enter 发送，Shift+Enter 换行。',
        botControl: 'Bot 正在操作桌面。',
        stillThere: '网站登录还在这个浏览器里。再发一条消息，这个 bot 会接着操作。',
        userControl: '请在这个桌面的浏览器里完成登录或验证。登录会留在这个浏览器里，完成后交还。',
        returnControl: '登录完成，继续',
        returnCommand: RETURN_COMMAND,
        emptyResult: '后台没有返回结果。',
        unavailable: '后台 agent 不可用。',
    };
}

// The first reply reads like a colleague who accepted the task, not like an
// agent echoing a transcript: confirm the task, promise the report. The real
// result lands later on its own bubble.
function ackReply(_content: string, lang: string, phase: BotPhase): string {
    if (phase === 'execute') {
        if (lang === 'en') return "I'll follow that arrangement. I'll tell you here when it's done or I need you.";
        if (lang === 'zh-Hant') return '我按這個安排去做。有結果或需要你時再告訴你。';
        return '我按这个安排去做。有结果或需要你时再告诉你。';
    }
    if (lang === 'en') return "I'll look at the setup first, then tell you what I plan to do.";
    if (lang === 'zh-Hant') return '我先看一下環境，再告訴你打算怎麼做。';
    return '我先看一下环境，再告诉你打算怎么做。';
}

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

// The queued variant is honest about ordering: the task was accepted, and it
// runs right after the one still in progress.
function queuedAckReply(content: string, lang: string): string {
    const flat = content.replace(/\s+/g, ' ').trim();
    const task = flat.length > 16 ? `${flat.slice(0, 15)}…` : flat;
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

// A command that never comes back must not keep the chat locked. The desktop
// reply wait is eight minutes; after that the same bot can continue on the
// browser where the website login still is.
const DESKTOP_BOT_REPLY_WAIT_MS = 9 * 60 * 1000;
const DESKTOP_BOT_WATCH_INTERVAL_MS = 4000;

function messageStartedAt(id: string): number {
    const match = /^m-([0-9a-z]+)-/i.exec(id);
    if (!match) return 0;
    const started = Number.parseInt(match[1], 36);
    return Number.isFinite(started) && started > 0 ? started : 0;
}

function pendingReplyIsStale(message: DesktopBotMessage, now = Date.now()): boolean {
    if (!message.pending) return false;
    const started = messageStartedAt(message.id);
    return started > 0 && now - started > DESKTOP_BOT_REPLY_WAIT_MS;
}

let desktopBotResultBound = false;

// Keep the instance result even if the bots page is closed when it arrives.
export function bindDesktopBotResultStore() {
    if (desktopBotResultBound) return;
    desktopBotResultBound = true;
    EventsOn('ai-assistant-response', handleDesktopBotResult);
    EventsOn('desktop-bot-view', handleDesktopBotView);
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
    settlePendingBotReply(sessionKey.slice(0, split), sessionKey.slice(split + 1), {
        content: result.content,
        failed: result.failed,
        handoffUrl: screen.userControl || screen.reason ? screen.url : '',
        userControl: screen.userControl,
        attentionReason: screen.reason,
        askUser: askFrom(data),
        requestId,
    });
}

function responseText(payload: Record<string, unknown>, emptyResult: string) {
    const text = String(payload.text || payload.Text || '').trim();
    const error = String(payload.error || payload.Error || '').trim();
    if (text) return { content: text, failed: false };
    if (error) return { content: error, failed: true };
    return { content: emptyResult, failed: true };
}

type PendingSend = { botId: string; messageId: string; settled: boolean };

// The desktop panel follows the user's intent. 'auto' is set by live events
// so the stage stops flashing away, 'user' by the 查看桌面/接管 buttons, and
// 'off' by 收起 — an auto event never reopens a panel the person collapsed.
type DesktopPanelState = 'auto' | 'user' | 'off' | undefined;

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

export function DesktopBotWorkspace({ lang, userId }: { lang: string; userId: string }) {
    const text = copy(lang);
    const [bots, setBots] = useState<DesktopBot[]>([]);
    const loadGen = useRef(0);
    const [selectedId, setSelectedId] = useState('');
    const [editingId, setEditingId] = useState('');
    const [draftTitle, setDraftTitle] = useState('');
    const [draftDescription, setDraftDescription] = useState('');
    const [pendingDeleteId, setPendingDeleteId] = useState('');
    const [messagesByBot, setMessagesByBot] = useState<Record<string, DesktopBotMessage[]>>({});
    const [command, setCommand] = useState('');
    const [listNotice, setListNotice] = useState('');
    const [listFailed, setListFailed] = useState(false);
    // The picture follows the panel, not the desktop address. 'auto' is a screen
    // handoff, 'user' is 查看桌面 / 人类接管, 'off' is 收起. Unset stays closed.
    const [desktopPanels, setDesktopPanels] = useState<Record<string, Exclude<DesktopPanelState, undefined>>>( {});
    const [liveDesktop, setLiveDesktop] = useState<Record<string, LiveDesktop | undefined>>({});
    const [takeoverByBot, setTakeoverByBot] = useState<Record<string, boolean>>({});
    const [secretPrompt, setSecretPrompt] = useState<SecretPrompt | null>(null);
    const [secretDraft, setSecretDraft] = useState('');
    const [retryByBot, setRetryByBot] = useState<Record<string, boolean>>({});
    const pendingRef = useRef<Map<string, PendingSend>>(new Map());
    const panelsRef = useRef(desktopPanels);
    panelsRef.current = desktopPanels;
    const liveRef = useRef(liveDesktop);
    liveRef.current = liveDesktop;
    // Same handoff stays latched until a poll reports the keyboard back and an
    // empty attention reason. A later true is the next handoff.
    const latchRef = useRef<Record<string, boolean>>({});
    const secretHandled = useRef<Set<string>>(new Set());
    const secretOpenRef = useRef(false);
    secretOpenRef.current = secretPrompt !== null;
    const watchMisses = useRef<Record<string, number>>({});
    const selected = bots.find(bot => bot.id === selectedId) || null;
    const messages = selected ? (messagesByBot[selected.id] || []) : [];
    const botPending = (botId: string) => {
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        return items.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message));
    };
    const busyPhase = (items: DesktopBotMessage[]): BotPhase | '' => {
        if (items.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message) && message.phase === 'execute')) return 'execute';
        if (items.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message))) return 'plan';
        return '';
    };
    const running = !!selected && botPending(selected.id);
    const handoffMessage = [...messages].reverse().find(message => message.userControl || !!message.attentionReason);
    const userHasControl = !!handoffMessage;
    const live = selected ? liveDesktop[selected.id] : undefined;
    const liveControl = live?.userControl === true;
    const liveReason = live?.attentionReason || '';
    const takeover = !!selected && !!takeoverByBot[selected.id];
    const panelState = selected ? desktopPanels[selected.id] : undefined;
    const showDesktop = panelState === 'auto' || panelState === 'user';
    const desktopUrl = showDesktop ? (live?.url || '') : '';
    // Voluntary viewing while the bot is driving stays view-only. A screen
    // handoff keeps the keyboard it already handed over.
    const agentOperating = running && !userHasControl && !liveControl && liveReason === '';
    const stageInteractive = showDesktop && (liveControl || liveReason !== '' || takeover);
    const statusPhase = busyPhase(messages);

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

    // The desktop panel follows the user's intent. 'auto' is set by live events
    // so the stage stops flashing away, 'user' by the 查看桌面/接管 buttons, and
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
    const closePanel = (botId: string) => {
        setDesktopPanels(prev => {
            if (prev[botId] === 'off') return prev;
            return { ...prev, [botId]: 'off' };
        });
        setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
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
    const applyScreen = (botId: string, screen: ScreenEvent) => {
        if (!botId) return;
        if (screen.cleared) {
            latchRef.current[botId] = false;
            setLiveDesktop(prev => prev[botId] ? { ...prev, [botId]: undefined } : prev);
            setDesktopPanels(prev => prev[botId] === 'user' ? prev : { ...prev, [botId]: 'off' });
            return;
        }
        const attention = screen.userControl || screen.reason !== '';
        const latched = latchRef.current[botId] === true;
        if (screen.reported && !screen.userControl && screen.reason === '' && latched) {
            latchRef.current[botId] = false;
            const panel = panelsRef.current[botId];
            if (panel === 'auto' || panel === 'user') {
                syncLive(botId, { url: screen.url || liveRef.current[botId]?.url || '', userControl: false, attentionReason: '' });
            }
            return;
        }
        if (!attention) return;
        const panel = panelsRef.current[botId];
        if (!latched) {
            latchRef.current[botId] = true;
            syncLive(botId, { url: screen.url || liveRef.current[botId]?.url || '', userControl: screen.userControl, attentionReason: screen.reason });
            openPanel(botId, 'auto', true);
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

    // The watch hold refreshes only while the selected stage is on screen.
    // Hiding it releases that hold. Two empty polls, or a thrown poll, take
    // the picture down and offer a connection retry that does not resend the task.
    useEffect(() => {
        if (!selectedId || !showDesktop) return;
        const botId = selectedId;
        let alive = true;
        watchMisses.current[botId] = 0;
        const fail = () => {
            if (!alive) return;
            const misses = (watchMisses.current[botId] || 0) + 1;
            watchMisses.current[botId] = misses;
            if (misses < 2) return;
            closePanel(botId);
            clearLiveFlags(botId);
            setRetryByBot(prev => ({ ...prev, [botId]: true }));
        };
        const tick = async () => {
            try {
                const view = await WatchDesktopBot(botId) as { novnc_url?: string; user_control?: boolean; attention_reason?: string } | undefined;
                if (!alive) return;
                const url = String(view?.novnc_url || '');
                if (!url) {
                    fail();
                    return;
                }
                watchMisses.current[botId] = 0;
                const panel = panelsRef.current[botId];
                if (panel !== 'auto' && panel !== 'user') return;
                const userControl = view?.user_control === true;
                const reason = String(view?.attention_reason || '');
                if ((userControl || reason) && !latchRef.current[botId]) latchRef.current[botId] = true;
                if (!userControl && !reason && latchRef.current[botId]) latchRef.current[botId] = false;
                syncLive(botId, { url, userControl, attentionReason: reason });
            } catch {
                fail();
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, DESKTOP_BOT_WATCH_INTERVAL_MS);
        return () => {
            alive = false;
            window.clearInterval(timer);
            void ReleaseDesktopBotWatch(botId);
        };
    }, [selectedId, showDesktop]);

    useEffect(() => {
        const gen = ++loadGen.current;
        setBots([]);
        setSelectedId('');
        setEditingId('');
        setDraftTitle('');
        setDraftDescription('');
        setPendingDeleteId('');
        setMessagesByBot({});
        setCommand('');
        setDesktopPanels({});
        setLiveDesktop({});
        setTakeoverByBot({});
        setSecretPrompt(null);
        setSecretDraft('');
        setRetryByBot({});
        latchRef.current = {};
        secretHandled.current = new Set();
        setListNotice('');
        setListFailed(false);
        ListDesktopBots().then(items => {
            if (gen !== loadGen.current) return;
            setListFailed(false);
            const list = (items || []).map(item => ({
                id: item.id,
                title: item.title,
                description: item.description || '',
                createdAt: Date.now(),
            }));
            setBots(list);
            // Seed every bot's history so the list can show who is working.
            // Tasks that were accepted but not dispatched yet (page closed) are
            // re-queued here; the queued marker is still on the user bubble.
            const seeded: Record<string, DesktopBotMessage[]> = {};
            const recovered: string[] = [];
            for (const bot of list) {
                const stored = messagesForBot(userId, bot.id);
                const queued = stored.filter(item => item.role === 'user' && item.queued).map(item => ({ text: item.content, phase: storedQueuePhase(item) }));
                if (queued.length > 0) {
                    queueRef.current.set(bot.id, [...(queueRef.current.get(bot.id) || []), ...queued]);
                    recovered.push(bot.id);
                }
                seeded[bot.id] = stored;
            }
            setMessagesByBot(seeded);
            for (const botId of recovered) drainIfIdle(botId);
        }).catch((error: unknown) => {
            if (gen !== loadGen.current) return;
            setBots([]);
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

    useEffect(() => {
        const handler = (payload: unknown) => {
            const data = (typeof payload === 'string' ? JSON.parse(payload) : payload) as Record<string, unknown>;
            const requestId = String(data.request_id || data.RequestID || '');
            const sessionKey = String(data.session_key || data.SessionKey || '');
            let pending = requestId ? pendingRef.current.get(requestId) : undefined;
            let pendingKey = requestId;
            if (!pending && sessionKey && !requestOwnsStoredMessage(userId, sessionKey, requestId)) {
                const split = sessionKey.lastIndexOf(':');
                const botId = split >= 0 ? sessionKey.slice(split + 1) : sessionKey;
                for (const [key, value] of pendingRef.current) {
                    if (value.botId === botId && !value.settled) {
                        pending = value;
                        pendingKey = key;
                        break;
                    }
                }
            }
            const screen = screenOf(data);
            const staleScreen = screenIsStale(sessionKey, requestId, pending);
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                handleDesktopBotResult(data);
            } else {
                pending.settled = true;
                pendingRef.current.delete(pendingKey);
                const botId = pending.botId;
                const result = responseText(data, text.emptyResult);
                const ask = askFrom(data);
                const stored = messagesForBot(userId, botId);
                const attention = screen.userControl || screen.reason !== '';
                const next = stored.map(item => item.id === pending.messageId ? {
                    ...item,
                    content: result.content,
                    pending: false,
                    failed: result.failed,
                    handoffUrl: attention ? (screen.url || item.handoffUrl || '') : '',
                    userControl: screen.userControl,
                    attentionReason: screen.reason,
                    askUser: ask || item.askUser,
                } : item);
                commit(botId, next);
                drainIfIdle(botId);
            }
            const botId = sessionBotId(sessionKey, userId);
            if (!botId) return;
            if (!staleScreen) {
                if (screen.userControl || screen.reason || screen.cleared || (screen.reported && latchRef.current[botId])) applyScreen(botId, screen);
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
            let pending = requestId ? pendingRef.current.get(requestId) : undefined;
            if (!pending && sessionKey && !requestOwnsStoredMessage(userId, sessionKey, requestId)) {
                const split = sessionKey.lastIndexOf(':');
                const botId = split >= 0 ? sessionKey.slice(split + 1) : sessionKey;
                for (const value of pendingRef.current.values()) {
                    if (value.botId === botId && !value.settled) {
                        pending = value;
                        break;
                    }
                }
            }
            const screen = screenOf(data);
            const botId = sessionBotId(sessionKey, userId);
            if (botId && !screenIsStale(sessionKey, requestId, pending)) applyScreen(botId, screen);
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

    const selectBot = (bot: DesktopBot) => {
        setSelectedId(bot.id);
        setPendingDeleteId('');
        setMessagesByBot(prev => prev[bot.id] ? prev : { ...prev, [bot.id]: messagesForBot(userId, bot.id) });
    };

    const create = async () => {
        loadGen.current += 1;
        setListNotice('');
        const title = `Bot ${bots.length + 1}`;
        try {
            const created = await CreateDesktopBot(title, text.defaultDescription);
            const bot: DesktopBot = {
                id: created.id,
                title: created.title || title,
                description: created.description || text.defaultDescription,
                createdAt: Date.now(),
            };
            setBots(prev => [...prev, bot]);
            setPendingDeleteId('');
            selectBot(bot);
        } catch (error) {
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
        }
    };

    const saveEdit = async (botId: string) => {
        const title = draftTitle.trim();
        const description = draftDescription.trim();
        if (!title) return;
        loadGen.current += 1;
        setListNotice('');
        try {
            await RenameDesktopBot(botId, title, description);
        } catch (error) {
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
            return;
        }
        setBots(prev => prev.map(bot => bot.id === botId ? { ...bot, title, description } : bot));
        setEditingId('');
        setDraftTitle('');
        setDraftDescription('');
    };

    const remove = async (botId: string) => {
        if (pendingDeleteId !== botId) {
            setPendingDeleteId(botId);
            return;
        }
        loadGen.current += 1;
        setListNotice('');
        try {
            await DeleteDesktopBot(botId);
        } catch (error) {
            setListNotice(error instanceof Error && error.message ? error.message : text.unavailable);
            return;
        }
        setBots(prev => prev.filter(bot => bot.id !== botId));
        setPendingDeleteId('');
        // Dropping a bot drops its accepted-but-unsent queue with it, and no
        // later reply for the gone instance settles into a ghost record.
        queueRef.current.delete(botId);
        for (const [key, value] of pendingRef.current) {
            if (value.botId === botId) pendingRef.current.delete(key);
        }
        setDesktopPanels(prev => {
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
        if (editingId === botId) {
            setEditingId('');
            setDraftTitle('');
            setDraftDescription('');
        }
        if (selectedId === botId) setSelectedId('');
        // The chat history is per-bot local storage; remove it with the bot.
        saveBotMessages(userId, botId, []);
        setMessagesByBot(prev => {
            const copy = { ...prev };
            delete copy[botId];
            return copy;
        });
    };

    // One bot runs one agent turn at a time on the shared cloud desktop, and
    // MaClawSrv does not serialize two turns on one session. A message sent
    // while a run is going is accepted and queued here; the queue drains in
    // order every time a reply settles.
    const queueRef = useRef<Map<string, Array<{ text: string; phase: BotPhase }>>>(new Map());

    const dropAutoPanel = (botId: string) => {
        setDesktopPanels(prev => prev[botId] === 'user' ? prev : { ...prev, [botId]: 'off' });
        setLiveDesktop(prev => prev[botId] ? { ...prev, [botId]: undefined } : prev);
        latchRef.current[botId] = false;
    };

    const startTask = (botId: string, content: string, phase: BotPhase, mode: 'now' | 'fromQueue') => {
        const turnPhase = phaseFor(content, phase);
        // The missed reply was still holding the keyboard. This dispatch is the
        // agent continuing in that same browser, so the person no longer types.
        const prior = messagesForBot(userId, botId).map(item => (
            item.role === 'assistant' && pendingReplyIsStale(item)
                ? { ...item, pending: false, userControl: false, attentionReason: '', content: item.content || text.userControl }
                : item
        ));
        const assistantMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: '', pending: true, phase: turnPhase };
        let next: DesktopBotMessage[];
        if (mode === 'now') {
            const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content, phase: turnPhase };
            const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: ackReply(content, lang, turnPhase), ack: true };
            next = [...prior, userMessage, ackMessage, assistantMessage];
        } else {
            next = [...prior, assistantMessage];
        }
        rememberMessages(botId, next);
        setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        const pending: PendingSend = { botId, messageId: assistantMessage.id, settled: false };
        const ticket = `local:${assistantMessage.id}`;
        pendingRef.current.set(ticket, pending);
        void (async () => {
            try {
                const response = await SendDesktopBotTask(botId, content, turnPhase) as { request_id?: string; RequestID?: string; deferred?: boolean; Deferred?: boolean; text?: string; Text?: string; error?: string; Error?: string };
                if (pending.settled) return;
                const requestId = String(response?.request_id || response?.RequestID || '');
                const deferred = response?.deferred === true || response?.Deferred === true;
                if (!deferred && (response?.text || response?.Text || response?.error || response?.Error)) {
                    pending.settled = true;
                    pendingRef.current.delete(ticket);
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
                    commit(botId, settled);
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
                }
            } catch (error) {
                if (pending.settled) return;
                pending.settled = true;
                pendingRef.current.delete(ticket);
                const raw = error instanceof Error && error.message ? error.message : text.unavailable;
                const stored = messagesForBot(userId, botId);
                const failed = stored.map(item => item.id === assistantMessage.id ? { ...item, content: raw, pending: false, failed: true } : item);
                commit(botId, failed);
                dropAutoPanel(botId);
                drainIfIdle(botId);
            }
        })();
    };

    const drainIfIdle = (botId: string) => {
        const queue = queueRef.current.get(botId);
        if (!queue || queue.length === 0) return;
        const busy =
            [...pendingRef.current.values()].some(value => value.botId === botId && !value.settled)
            || messagesForBot(userId, botId).some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message));
        if (busy) return;
        const nextSend = queue.shift()!;
        queueRef.current.set(botId, queue);
        const stored = messagesForBot(userId, botId);
        let cleared = false;
        const next = stored.map(item => {
            if (!cleared && item.role === 'user' && item.queued && item.content === nextSend.text) {
                cleared = true;
                return { ...item, queued: false };
            }
            return item;
        });
        commit(botId, next);
        startTask(botId, nextSend.text, nextSend.phase, 'fromQueue');
    };

    const send = async (preset?: string, phase: BotPhase = 'plan') => {
        if (!selected) return;
        if (!preset && secretOpenRef.current) return;
        const botId = selected.id;
        const content = (preset ?? command).trim();
        if (!content) return;
        const turnPhase = phaseFor(content, phase);
        if (botPending(botId)) {
            if (!preset) setCommand('');
            const prior = messagesForBot(userId, botId);
            const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content, queued: true, phase: turnPhase };
            const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: queuedAckReply(content, lang), ack: true };
            commit(botId, [...prior, userMessage, ackMessage]);
            queueRef.current.set(botId, [...(queueRef.current.get(botId) || []), { text: content, phase: turnPhase }]);
            return;
        }
        startTask(botId, content, turnPhase, 'now');
        if (!preset) setCommand('');
    };

    const markAskResolved = (botId: string, messageId: string) => {
        const stored = messagesForBot(userId, botId);
        commit(botId, stored.map(item => item.id === messageId ? { ...item, askResolved: true } : item));
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

    const onComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            void send();
        }
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
        const items = messagesForBot(userId, botId);
        const handoff = [...items].reverse().find(item => (item.userControl || item.attentionReason) && item.handoffUrl);
        const driving = botPending(botId) && !handoff;
        if (handoff && !driving) {
            latchRef.current[botId] = true;
            syncLive(botId, { url: handoff.handoffUrl, userControl: handoff.userControl === true, attentionReason: handoff.attentionReason || '' });
        }
    };

    const collapseStage = (botId: string) => {
        const handoff = userHasControl || liveControl || liveReason !== '';
        closePanel(botId);
        clearLiveFlags(botId);
        if (handoff) latchRef.current[botId] = true;
    };

    const toggleTakeover = () => {
        if (!selected || agentOperating) return;
        const botId = selected.id;
        if (takeoverByBot[botId]) {
            if (userHasControl || liveControl || liveReason !== '') resumeFromScreenHandoff(botId);
            else setTakeoverByBot(prev => ({ ...prev, [botId]: false }));
            return;
        }
        setTakeoverByBot(prev => ({ ...prev, [botId]: true }));
        openPanel(botId, 'user');
    };

    const togglePanel = () => {
        if (!selected) return;
        if (showDesktop) collapseStage(selected.id);
        else openForPerson(selected.id);
    };

    const retryConnection = async (botId: string) => {
        try {
            const view = await WatchDesktopBot(botId) as { novnc_url?: string; user_control?: boolean; attention_reason?: string } | undefined;
            const url = String(view?.novnc_url || '');
            if (!url) return;
            setRetryByBot(prev => ({ ...prev, [botId]: false }));
            syncLive(botId, { url, userControl: view?.user_control === true, attentionReason: String(view?.attention_reason || '') });
            openPanel(botId, 'user');
        } catch {
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
        let blocked = false;
        for (let i = messages.length - 1; i >= 0; i--) {
            const item = messages[i];
            if (item.role !== 'assistant' || item.ack) continue;
            if (item.pending && !pendingReplyIsStale(item)) {
                blocked = true;
                continue;
            }
            if (item.pending) continue;
            if (item.phase === 'plan' && !item.failed && !item.userControl && !item.attentionReason && !(item.askUser && !item.askResolved)) return { id: item.id, disabled: blocked || running };
            return null;
        }
        return null;
    })();

    const stageStatusText = takeover && !liveControl && liveReason === ''
        ? text.takeoverActive
        : (liveControl || liveReason !== '' || userHasControl)
            ? text.userControl
            : (statusPhase ? text.botControl : text.watching);

    const secretForSelected = secretPrompt && selected && secretPrompt.botId === selected.id ? secretPrompt : null;

    return (
        <section className="desktop-bot-workspace" data-testid="desktop-bot-workspace" data-user-id={userId}>
            <aside className="desktop-bot-workspace__list-pane" aria-label={text.listTitle}>
                <header className="desktop-bot-workspace__bar">
                    <div>
                        <h1>{text.listTitle}</h1>
                        <p>{text.shared}</p>
                    </div>
                    <button type="button" className="desktop-bot-workspace__create" data-testid="desktop-bot-create" onClick={create}>{text.create}</button>
                </header>
                {listNotice ? <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-list-error" role="status">{listNotice}</p> : null}
                {bots.length === 0 && !listFailed ? (
                    <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-empty">{text.empty}</p>
                ) : (
                    <ul className="desktop-bot-workspace__list">
                        {bots.map(bot => {
                            const editing = editingId === bot.id;
                            const description = bot.description || text.defaultDescription;
                            const rowPhase = busyPhase(messagesByBot[bot.id] || messagesForBot(userId, bot.id));
                            const busy = rowPhase !== '';
                            return (
                                <li key={bot.id} className={'desktop-bot-workspace__row' + (selectedId === bot.id ? ' is-selected' : '')} data-testid={`desktop-bot-${bot.id}`}>
                                    {editing ? (
                                        <div className="desktop-bot-workspace__edit">
                                            <input aria-label={text.name} value={draftTitle} maxLength={40} onChange={event => setDraftTitle(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') saveEdit(bot.id); if (event.key === 'Escape') setEditingId(''); }} />
                                            <input aria-label={text.description} value={draftDescription} maxLength={80} onChange={event => setDraftDescription(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') saveEdit(bot.id); if (event.key === 'Escape') setEditingId(''); }} />
                                        </div>
                                    ) : (
                                        <button type="button" className="desktop-bot-workspace__open" aria-pressed={selectedId === bot.id} onClick={() => selectBot(bot)}>
                                            <span className="desktop-bot-workspace__avatar" style={{ background: botColor(bot.title) }} aria-hidden="true">{botInitial(bot.title)}</span>
                                            <span className="desktop-bot-workspace__who">
                                                <strong>{bot.title}</strong>
                                                <span>{description}</span>
                                            </span>
                                            <span className={'desktop-bot-workspace__state' + (busy ? ' is-working' : '')} aria-hidden="true">
                                                <i />
                                                <span>{rowPhase === 'execute' ? text.workingTag : rowPhase === 'plan' ? text.arrangingTag : text.idleTag}</span>
                                            </span>
                                        </button>
                                    )}
                                    {!editing ? (
                                        <div className="desktop-bot-workspace__actions">
                                            <button type="button" onClick={() => { setEditingId(bot.id); setDraftTitle(bot.title); setDraftDescription(bot.description); setPendingDeleteId(''); }}>{text.rename}</button>
                                            <button type="button" data-testid={`desktop-bot-delete-${bot.id}`} onClick={() => remove(bot.id)}>
                                                {pendingDeleteId === bot.id ? text.confirmRemove : text.remove}
                                            </button>
                                        </div>
                                    ) : (
                                        <div className="desktop-bot-workspace__actions">
                                            <button type="button" onClick={() => saveEdit(bot.id)}>{text.save}</button>
                                            <button type="button" onClick={() => setEditingId('')}>{text.cancel}</button>
                                        </div>
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                )}
            </aside>
            <section className="desktop-bot-chat" data-testid="desktop-bot-chat" aria-label={selected ? selected.title : text.pick}>
                {selected ? (
                    <>
                        <header className="desktop-bot-chat__header">
                            <div className="desktop-bot-chat__title">
                                <span className="desktop-bot-workspace__avatar desktop-bot-chat__avatar" style={{ background: botColor(selected.title) }} aria-hidden="true">{botInitial(selected.title)}</span>
                                <div className="desktop-bot-chat__identity">
                                    <strong>{selected.title}</strong>
                                    <p className={'desktop-bot-chat__status' + (statusPhase ? ' is-working' : '')} data-testid="desktop-bot-status">
                                        <span>{selected.title} {statusPhase === 'execute' ? text.working : statusPhase === 'plan' ? text.arranging : text.idle}</span>
                                    </p>
                                </div>
                            </div>
                            <div className="desktop-bot-chat__ops" data-testid="desktop-bot-ops" aria-label={text.ops}>
                                {agentOperating ? (
                                    <span className="desktop-bot-chat__ops-note" role="status">{text.takeoverBusyHint}</span>
                                ) : null}
                                <button type="button" data-testid="desktop-bot-open-desktop" onClick={togglePanel}>
                                    {showDesktop ? text.hideDesktop : text.openDesktop}
                                </button>
                                <button
                                    type="button"
                                    data-testid="desktop-bot-takeover"
                                    className={'desktop-bot-chat__takeover' + (takeover ? ' is-active' : '')}
                                    disabled={agentOperating}
                                    title={agentOperating ? text.takeoverBusyHint : text.takeoverHint}
                                    onClick={toggleTakeover}
                                >
                                    {takeover ? text.exitTakeover : text.takeover}
                                </button>
                            </div>
                        </header>
                        <div className="desktop-bot-chat__log" data-testid="desktop-bot-log">
                            {messages.length === 0 ? (
                                <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pick}</p>
                            ) : null}
                            {messages.filter(message => !message.pending).map(message => (
                                <div key={message.id} className="desktop-bot-chat__entry">
                                    <p className={`desktop-bot-chat__bubble desktop-bot-chat__bubble--${message.role}${message.failed ? ' is-failed' : ''}`}>
                                        {message.failed ? (friendlyFailure(message.content, lang) || message.content) : message.content}
                                    </p>
                                    {confirmMessage && confirmMessage.id === message.id ? (
                                        <button type="button" data-testid="desktop-bot-confirm" disabled={confirmMessage.disabled} onClick={() => void send(CONFIRM_COMMAND, 'execute')}>{text.confirm}</button>
                                    ) : null}
                                    {message.askUser?.options && !message.askResolved && !message.userControl ? (
                                        <div className="desktop-bot-chat__choices">
                                            {message.askUser.options.map(option => (
                                                <button type="button" key={option} onClick={() => { markAskResolved(selected.id, message.id); void send(option, message.phase === 'execute' ? 'execute' : 'plan'); }}>{option}</button>
                                            ))}
                                        </div>
                                    ) : null}
                                    {secretForSelected && secretForSelected.messageId === message.id ? (
                                        <form className="desktop-bot-chat__secret" data-testid="desktop-bot-secret" onSubmit={event => { event.preventDefault(); void saveSecret(); }}>
                                            <p>{text.secretHint}</p>
                                            <label>
                                                {secretForSelected.name}
                                                <input type="password" autoComplete="off" data-testid="desktop-bot-secret-input" value={secretDraft} onChange={event => setSecretDraft(event.target.value)} />
                                            </label>
                                            <button type="submit" data-testid="desktop-bot-secret-save">{text.secretSave}</button>
                                            <button type="button" data-testid="desktop-bot-secret-cancel" onClick={() => { setSecretPrompt(null); setSecretDraft(''); markAskResolved(selected.id, message.id); }}>{text.secretCancel}</button>
                                        </form>
                                    ) : null}
                                </div>
                            ))}
                            {messages.some(message => message.pending && !pendingReplyIsStale(message)) ? (
                                <p className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-typing" aria-hidden="true">
                                    <i /><i /><i />
                                </p>
                            ) : null}
                            {handoffMessage ? (
                                <p className="desktop-bot-chat__entry">
                                    <button type="button" data-testid="desktop-bot-return-control" onClick={() => resumeFromScreenHandoff(selected.id)}>{text.returnControl}</button>
                                </p>
                            ) : null}
                            {retryByBot[selected.id] ? (
                                <p className="desktop-bot-chat__entry">
                                    {text.desktopDown}
                                    <button type="button" data-testid="desktop-bot-retry" onClick={() => void retryConnection(selected.id)}>{text.retryConnect}</button>
                                </p>
                            ) : null}
                        </div>
                        {showDesktop ? (
                            <div className={'desktop-bot-stage' + (stageInteractive ? ' is-user' : ' is-bot') + (takeover ? ' is-fullscreen' : '')} data-testid="desktop-bot-stage">
                                <div className="desktop-bot-stage__bar">
                                    <p>{stageStatusText}</p>
                                    <div className="desktop-bot-stage__bar-actions">
                                        {takeover || liveControl || liveReason !== '' ? (
                                            <button type="button" data-testid="desktop-bot-exit-takeover" onClick={() => {
                                                if (userHasControl || liveControl || liveReason !== '') resumeFromScreenHandoff(selected.id);
                                                else toggleTakeover();
                                            }}>{text.exitTakeover}</button>
                                        ) : null}
                                        <button type="button" className="desktop-bot-stage__close" data-testid="desktop-bot-stage-close" onClick={() => collapseStage(selected.id)}>{text.hideDesktop}</button>
                                    </div>
                                </div>
                                {desktopUrl ? (
                                    <iframe className="desktop-bot-chat__handoff" data-testid="desktop-bot-handoff" title="desktop" src={desktopFrameSrc(desktopUrl, stageInteractive)} />
                                ) : (
                                    <div className="desktop-bot-stage__loading" data-testid="desktop-bot-stage-loading">
                                        <span className="desktop-bot-stage__loading-dot" aria-hidden="true" />
                                        {text.connectingDesktop}
                                    </div>
                                )}
                            </div>
                        ) : null}
                        <form className="desktop-bot-chat__form" onSubmit={event => { event.preventDefault(); void send(); }}>
                            <textarea data-testid="desktop-bot-command" aria-label={text.placeholder} placeholder={`${text.placeholder} ${selected.title}`} value={command} onChange={event => setCommand(event.target.value)} onKeyDown={onComposerKeyDown} />
                            <button type="submit" data-testid="desktop-bot-send" disabled={!command.trim() || !!secretForSelected}>{text.send}</button>
                        </form>
                        <p className="desktop-bot-chat__hint">{secretForSelected ? text.secretHint : text.composerHint}</p>
                    </>
                ) : (
                    <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pick}</p>
                )}
            </section>
        </section>
    );
}


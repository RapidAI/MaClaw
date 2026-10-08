import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import {
    CreateDesktopBot,
    DeleteDesktopBot,
    ListDesktopBots,
    RenameDesktopBot,
    SendDesktopBotTask,
    WatchDesktopBot,
} from '../../../wailsjs/go/main/App';
import {
    messagesForBot,
    notePendingBotDesktop,
    saveBotMessages,
    settlePendingBotReply,
    type DesktopBot,
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
            idle: 'idle',
            workingTag: 'Working',
            idleTag: 'Idle',
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
            returnCommand: '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。',
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
            idle: '待命',
            workingTag: '工作中',
            idleTag: '待命',
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
            returnCommand: '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。',
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
        idle: '待命',
        workingTag: '工作中',
        idleTag: '待命',
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
        returnCommand: '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。',
        emptyResult: '后台没有返回结果。',
        unavailable: '后台 agent 不可用。',
    };
}

// The first reply reads like a colleague who accepted the task, not like an
// agent echoing a transcript: confirm the task, promise the report. The real
// result lands later on its own bubble.
function ackReply(content: string, lang: string): string {
    const flat = content.replace(/\s+/g, ' ').trim();
    const task = flat.length > 16 ? `${flat.slice(0, 15)}…` : flat;
    if (lang === 'en') {
        return task
            ? `Got it — I'll take care of "${task}". The result will show up right here when it's done.`
            : "Got it — the result will show up right here when it's done.";
    }
    if (lang === 'zh-Hant') {
        return task ? `收到，我來處理「${task}」，完成後我會在這裡告訴你。` : '收到，完成後我會在這裡告訴你。';
    }
    return task ? `收到，我来处理「${task}」，完成后我会在这里告诉你。` : '收到，完成后我会在这里告诉你。';
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
    const handoffUrl = String(data.novnc_url || data.desktop_handoff_url || '');
    const reported = Object.prototype.hasOwnProperty.call(data, 'user_control') || Object.prototype.hasOwnProperty.call(data, 'desktop_user_control');
    const userControl = data.user_control === true || data.desktop_user_control === true;
    notePendingBotDesktop(sessionKey.slice(0, split), sessionKey.slice(split + 1), { handoffUrl, userControl, reported, requestId });
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
    const handoffUrl = String(data.desktop_handoff_url || data.DesktopHandoffURL || '');
    const userControl = data.desktop_user_control === true || data.DesktopUserControl === true;
    settlePendingBotReply(sessionKey.slice(0, split), sessionKey.slice(split + 1), {
        content: result.content,
        failed: result.failed,
        handoffUrl,
        userControl,
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
    // Live desktop watch per bot. 'running' per bot derives from the messages.
    const [desktopPanels, setDesktopPanels] = useState<Record<string, Exclude<DesktopPanelState, undefined>>>( {});
    const [liveDesktop, setLiveDesktop] = useState<Record<string, { url: string; userControl: boolean } | undefined>>({});
    const [takeoverByBot, setTakeoverByBot] = useState<Record<string, boolean>>({});
    const pendingRef = useRef<Map<string, PendingSend>>(new Map());
    const selected = bots.find(bot => bot.id === selectedId) || null;
    const messages = selected ? (messagesByBot[selected.id] || []) : [];
    const botPending = (botId: string) => {
        const items = messagesByBot[botId] || messagesForBot(userId, botId);
        return items.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message));
    };
    const running = !!selected && botPending(selected.id);
    const pendingView = [...messages].reverse().find(message => message.role === 'assistant' && message.pending && message.handoffUrl);
    const settledView = [...messages].reverse().find(message => message.role === 'assistant' && !message.pending && !message.ack);
    const settledUrl = settledView?.handoffUrl || '';
    const settledKeepsBrowser = !running && !!settledView?.failed && !!settledUrl && !settledView?.userControl;
    const desktopUrl = pendingView?.handoffUrl
        || (running || settledView?.userControl || settledKeepsBrowser ? settledUrl : '')
        || (liveDesktop[selectedId]?.url || '');
    const pendingKeyboard = !!pendingView?.userControl && pendingView.handoffUrl === desktopUrl && !(running && pendingReplyIsStale(pendingView));
    const staleLogin = !running && !!pendingView && pendingReplyIsStale(pendingView) && !!pendingView.userControl && pendingView.handoffUrl === desktopUrl;
    const settledKeyboard = staleLogin || (!running && !!settledView?.userControl && !!settledUrl && desktopUrl === settledUrl);
    const userHasControl = pendingKeyboard || settledKeyboard;
    const takeover = !!selected && !!takeoverByBot[selected.id];
    // The keyboard is with a person either through the login handoff or an
    // explicit takeover. While the bot drives, takeover is off the table.
    const stageInteractive = userHasControl || takeover;
    const agentOperating = running && !userHasControl;
    const panelState = selected ? desktopPanels[selected.id] : undefined;
    // Once a bot was working in front of the human there is always a picture,
    // so the VNC never flashes away on settle. Only 收起 (panel 'off') hides it,
    // and 收起 keeps hiding until a keyboard handoff forces it back.
    const autoDesktop = userHasControl
        || (running && !!desktopUrl)
        || settledKeepsBrowser
        || (panelState === 'auto' && !!desktopUrl);
    const showDesktop = (panelState !== 'off' && autoDesktop) || panelState === 'user';
    const workingNow = running;

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
    const syncLive = (botId: string, view: { novnc_url?: string; NovncURL?: string; user_control?: boolean; UserControl?: boolean } | null | undefined) => {
        if (view === null || view === undefined) return;
        const url = String(view.novnc_url || view.NovncURL || '');
        if (!url) {
            // A poll that answers without a desktop means this desktop stopped.
            // Drop the stale URL so the stage shows the connecting placeholder
            // instead of a dead noVNC page.
            setLiveDesktop(prev => prev[botId] ? { ...prev, [botId]: undefined } : prev);
            return;
        }
        const userControl = view.user_control === true || view.UserControl === true;
        setLiveDesktop(prev => {
            const existing = prev[botId];
            if (existing && existing.url === url && existing.userControl === userControl) return prev;
            return { ...prev, [botId]: { url, userControl } };
        });
    };

    useEffect(() => {
        bindDesktopBotResultStore();
    }, []);

    // While a desktop panel is open the GUI polls the live view. Every call
    // refreshes the Hub hold, so the desktop stays up while a human is
    // watching or driving instead of going black after the last command.
    useEffect(() => {
        if (!selectedId) return;
        const panel = desktopPanels[selectedId];
        if (!panel || panel === 'off') return;
        let alive = true;
        const tick = async () => {
            try {
                const view = await WatchDesktopBot(selectedId);
                if (!alive || !view) return;
                syncLive(selectedId, view as Record<string, unknown>);
            } catch {
                // A failed poll just retries on the next tick.
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, DESKTOP_BOT_WATCH_INTERVAL_MS);
        return () => {
            alive = false;
            window.clearInterval(timer);
        };
    }, [selectedId, desktopPanels]);

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
                const queued = stored.filter(item => item.role === 'user' && item.queued).map(item => item.content);
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
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                handleDesktopBotResult(data);
            } else {
                pending.settled = true;
                pendingRef.current.delete(pendingKey);
                const botId = pending.botId;
                const result = responseText(data, text.emptyResult);
                const handoffUrl = String(data.desktop_handoff_url || data.DesktopHandoffURL || '');
                const userControl = data.desktop_user_control === true || data.DesktopUserControl === true;
                const stored = messagesForBot(userId, botId);
                const next = stored.map(item => item.id === pending.messageId ? { ...item, content: result.content, pending: false, failed: result.failed, handoffUrl, userControl } : item);
                commit(botId, next);
                drainIfIdle(botId);
            }
            const botId = sessionBotId(sessionKey, userId);
            if (!botId) return;
            // A keyboard-holding handoff is an attention request: even a panel
            // the person collapsed comes back for it. Pure viewing respects 收起.
            const handoffUserControl = data.desktop_user_control === true || data.DesktopUserControl === true;
            if (handoffOf(data)) openPanel(botId, 'auto', handoffUserControl);
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
            const handoffUrl = String(data.novnc_url || data.desktop_handoff_url || '');
            const reported = Object.prototype.hasOwnProperty.call(data, 'user_control') || Object.prototype.hasOwnProperty.call(data, 'desktop_user_control');
            const userControl = data.user_control === true || data.desktop_user_control === true;
            if (handoffUrl) {
                const botId = sessionBotId(sessionKey, userId);
                if (botId) {
                    syncLive(botId, data);
                    openPanel(botId, 'auto', userControl);
                }
            }
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                handleDesktopBotView(data);
                const botId = sessionBotId(sessionKey, userId);
                if (!botId) return;
                setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
                return;
            }
            if (!handoffUrl) return;
            const stored = messagesForBot(userId, pending.botId);
            const next = stored.map(item => item.id === pending.messageId && item.pending ? { ...item, handoffUrl, userControl: reported ? userControl : item.userControl } : item);
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
    const queueRef = useRef<Map<string, string[]>>(new Map());

    const startTask = (botId: string, content: string, mode: 'now' | 'fromQueue') => {
        // The missed reply was still holding the keyboard. This dispatch is the
        // agent continuing in that same browser, so the person no longer types.
        const prior = messagesForBot(userId, botId).map(item => (
            item.role === 'assistant' && pendingReplyIsStale(item)
                ? { ...item, pending: false, userControl: false, content: item.content || text.userControl }
                : item
        ));
        const assistantMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: '', pending: true };
        let next: DesktopBotMessage[];
        if (mode === 'now') {
            const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content };
            const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: ackReply(content, lang), ack: true };
            next = [...prior, userMessage, ackMessage, assistantMessage];
        } else {
            // The user bubble and the queued acceptance were already recorded
            // when the task was accepted; only the pending result is new.
            next = [...prior, assistantMessage];
        }
        rememberMessages(botId, next);
        // A dispatch hands the keyboard back to the agent: takeover ends here.
        setTakeoverByBot(prev => prev[botId] ? { ...prev, [botId]: false } : prev);
        const pending: PendingSend = { botId, messageId: assistantMessage.id, settled: false };
        const ticket = `local:${assistantMessage.id}`;
        pendingRef.current.set(ticket, pending);
        void (async () => {
            try {
                const response = await SendDesktopBotTask(botId, content) as { request_id?: string; RequestID?: string; deferred?: boolean; Deferred?: boolean; text?: string; Text?: string; error?: string; Error?: string };
                if (pending.settled) return;
                const requestId = String(response?.request_id || response?.RequestID || '');
                const deferred = response?.deferred === true || response?.Deferred === true;
                if (!deferred && (response?.text || response?.Text || response?.error || response?.Error)) {
                    pending.settled = true;
                    pendingRef.current.delete(ticket);
                    const result = responseText(response as Record<string, unknown>, text.emptyResult);
                    const inline = response as Record<string, unknown>;
                    const handoffUrl = String(inline.desktop_handoff_url || inline.DesktopHandoffURL || '');
                    const userControl = inline.desktop_user_control === true || inline.DesktopUserControl === true;
                    const stored = messagesForBot(userId, botId);
                    const settled = stored.map(item => item.id === assistantMessage.id ? { ...item, content: result.content, pending: false, failed: result.failed, handoffUrl, userControl } : item);
                    commit(botId, settled);
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
                // Storage keeps the raw error; the bubble wording maps at display
                // time through friendlyFailure.
                const raw = error instanceof Error && error.message ? error.message : text.unavailable;
                const stored = messagesForBot(userId, botId);
                const failed = stored.map(item => item.id === assistantMessage.id ? { ...item, content: raw, pending: false, failed: true } : item);
                commit(botId, failed);
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
        const content = queue.shift()!;
        queueRef.current.set(botId, queue);
        // The accepted task starts now: clear its queue marker so a page
        // reload cannot recover it a second time.
        const stored = messagesForBot(userId, botId);
        let cleared = false;
        const next = stored.map(item => {
            if (!cleared && item.role === 'user' && item.queued && item.content === content) {
                cleared = true;
                return { ...item, queued: false };
            }
            return item;
        });
        commit(botId, next);
        startTask(botId, content, 'fromQueue');
    };

    const send = async (preset?: string) => {
        if (!selected) return;
        const botId = selected.id;
        const content = (preset ?? command).trim();
        if (!content) return;
        if (botPending(botId)) {
            if (!preset) setCommand('');
            const prior = messagesForBot(userId, botId);
            const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content, queued: true };
            const ackMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: queuedAckReply(content, lang), ack: true };
            commit(botId, [...prior, userMessage, ackMessage]);
            queueRef.current.set(botId, [...(queueRef.current.get(botId) || []), content]);
            return;
        }
        startTask(botId, content, 'now');
        if (!preset) setCommand('');
    };

    const onComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            void send();
        }
    };

    const toggleTakeover = () => {
        if (!selected || agentOperating) return;
        const botId = selected.id;
        const next = !takeoverByBot[botId];
        setTakeoverByBot(prev => ({ ...prev, [botId]: next }));
        if (next) openPanel(botId, 'user');
    };

    const togglePanel = () => {
        if (!selected) return;
        const botId = selected.id;
        if (panelState === 'user' || (panelState === 'auto' && showDesktop && !takeover)) {
            closePanel(botId);
            return;
        }
        openPanel(botId, 'user');
    };

    const stageStatusText = takeover
        ? text.takeoverActive
        : userHasControl
            ? text.userControl
            : (settledKeepsBrowser ? text.stillThere : (workingNow ? text.botControl : text.watching));

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
                            const busy = botPending(bot.id);
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
                                                <span>{busy ? text.workingTag : text.idleTag}</span>
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
                                    <p className={'desktop-bot-chat__status' + (workingNow ? ' is-working' : '')} data-testid="desktop-bot-status">
                                        <span>{selected.title} {workingNow ? text.working : text.idle}</span>
                                    </p>
                                </div>
                            </div>
                            <div className="desktop-bot-chat__ops" data-testid="desktop-bot-ops" aria-label={text.ops}>
                                {agentOperating ? (
                                    <span className="desktop-bot-chat__ops-note" role="status">{text.takeoverBusyHint}</span>
                                ) : null}
                                <button type="button" data-testid="desktop-bot-open-desktop" onClick={togglePanel}>
                                    {panelState === 'off' ? text.openDesktop : text.hideDesktop}
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
                                <p key={message.id} className={`desktop-bot-chat__bubble desktop-bot-chat__bubble--${message.role}${message.failed ? ' is-failed' : ''}`}>
                                    {message.failed ? (friendlyFailure(message.content, lang) || message.content) : message.content}
                                </p>
                            ))}
                            {messages.some(message => message.pending && !pendingReplyIsStale(message)) ? (
                                <p className="desktop-bot-chat__bubble desktop-bot-chat__bubble--assistant is-typing" aria-hidden="true">
                                    <i /><i /><i />
                                </p>
                            ) : null}
                        </div>
                        {showDesktop ? (
                            <div className={'desktop-bot-stage' + (stageInteractive ? ' is-user' : ' is-bot') + (takeover ? ' is-fullscreen' : '')} data-testid="desktop-bot-stage">
                                <div className="desktop-bot-stage__bar">
                                    <p>{stageStatusText}</p>
                                    <div className="desktop-bot-stage__bar-actions">
                                        {takeover ? (
                                            <button type="button" data-testid="desktop-bot-exit-takeover" onClick={toggleTakeover}>{text.exitTakeover}</button>
                                        ) : null}
                                        {settledKeyboard ? (
                                            <button type="button" data-testid="desktop-bot-return-control" onClick={() => void send(text.returnCommand)}>{text.returnControl}</button>
                                        ) : null}
                                        <button type="button" className="desktop-bot-stage__close" data-testid="desktop-bot-stage-close" onClick={() => closePanel(selected.id)}>{text.hideDesktop}</button>
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
                            <button type="submit" data-testid="desktop-bot-send" disabled={!command.trim()}>{text.send}</button>
                        </form>
                        <p className="desktop-bot-chat__hint">{text.composerHint}</p>
                    </>
                ) : (
                    <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pick}</p>
                )}
            </section>
        </section>
    );
}

function handoffOf(data: Record<string, unknown>): string {
    return String(data.desktop_handoff_url || data.DesktopHandoffURL || '');
}

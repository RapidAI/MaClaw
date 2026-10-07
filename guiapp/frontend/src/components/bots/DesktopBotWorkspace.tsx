import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { EventsOn } from '../../../wailsjs/runtime';
import { CreateDesktopBot, DeleteDesktopBot, ListDesktopBots, RenameDesktopBot, SendDesktopBotTask } from '../../../wailsjs/go/main/App';
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
            botControl: 'Bot 正在操作桌面。',
            stillThere: '网站登录还在这个浏览器里。再发一条消息，这个 bot 会接着操作。',
            userControl: '请在这个桌面的浏览器里完成登录或验证。登录会留在这个浏览器里，完成后交还。',
        returnControl: '登录完成，继续',
        returnCommand: '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。',
        emptyResult: '后台没有返回结果。',
        unavailable: '后台 agent 不可用。',
    };
}

function messageID() {
    return `m-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

// A command that never comes back must not keep the chat locked. The desktop
// reply wait is eight minutes; after that the same bot can continue on the
// browser where the website login still is.
const DESKTOP_BOT_REPLY_WAIT_MS = 9 * 60 * 1000;

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
    const [runningBots, setRunningBots] = useState<Record<string, boolean>>({});
    const [listNotice, setListNotice] = useState('');
    const [listFailed, setListFailed] = useState(false);
    const pendingRef = useRef<Map<string, PendingSend>>(new Map());
    const selected = bots.find(bot => bot.id === selectedId) || null;
    const messages = selected ? (messagesByBot[selected.id] || []) : [];
    const sessionRunning = selected ? !!runningBots[selected.id] : false;
    const hasPending = messages.some(message => message.role === 'assistant' && message.pending && !pendingReplyIsStale(message));
    const running = sessionRunning || hasPending;
    const pendingView = [...messages].reverse().find(message => message.role === 'assistant' && message.pending && message.handoffUrl);
    const settledView = [...messages].reverse().find(message => message.role === 'assistant' && !message.pending);
    const settledUrl = settledView?.handoffUrl || '';
    const settledKeepsBrowser = !running && !!settledView?.failed && !!settledUrl && !settledView?.userControl;
    const desktopUrl = pendingView?.handoffUrl || (running || settledView?.userControl || settledKeepsBrowser ? settledUrl : '');
    const pendingKeyboard = !!pendingView?.userControl && pendingView.handoffUrl === desktopUrl && !(running && pendingReplyIsStale(pendingView));
    const staleLogin = !running && !!pendingView && pendingReplyIsStale(pendingView) && !!pendingView.userControl && pendingView.handoffUrl === desktopUrl;
    const settledKeyboard = staleLogin || (!running && !!settledView?.userControl && !!settledUrl && desktopUrl === settledUrl);
    const userHasControl = pendingKeyboard || settledKeyboard;
    const showDesktop = userHasControl || (running && !!desktopUrl) || settledKeepsBrowser;

    const rememberMessages = (botId: string, next: DesktopBotMessage[]) => {
        saveBotMessages(userId, botId, next);
        setMessagesByBot(prev => ({ ...prev, [botId]: next }));
    };

    useEffect(() => {
        bindDesktopBotResultStore();
    }, []);

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
        setRunningBots({});
        setListNotice('');
        setListFailed(false);
        ListDesktopBots().then(items => {
            if (gen !== loadGen.current) return;
            setListFailed(false);
            setBots((items || []).map(item => ({
                id: item.id,
                title: item.title,
                description: item.description || '',
                createdAt: Date.now(),
            })));
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
                const botId = sessionBotId(sessionKey, userId);
                if (!botId) return;
                setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
                setRunningBots(prev => ({ ...prev, [botId]: false }));
                return;
            }
            pending.settled = true;
            pendingRef.current.delete(pendingKey);
            const result = responseText(data, text.emptyResult);
            const handoffUrl = String(data.desktop_handoff_url || data.DesktopHandoffURL || '');
            const userControl = data.desktop_user_control === true || data.DesktopUserControl === true;
            setMessagesByBot(prev => {
                const current = prev[pending.botId] || messagesForBot(userId, pending.botId);
                const next = current.map(item => item.id === pending.messageId ? { ...item, content: result.content, pending: false, failed: result.failed, handoffUrl, userControl } : item);
                saveBotMessages(userId, pending.botId, next);
                return { ...prev, [pending.botId]: next };
            });
            setRunningBots(prev => ({ ...prev, [pending.botId]: false }));
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
            if (!pending || pending.settled) {
                if (!requestId.startsWith('desktop-bot-')) return;
                handleDesktopBotView(data);
                const botId = sessionBotId(sessionKey, userId);
                if (!botId) return;
                setMessagesByBot(prev => ({ ...prev, [botId]: messagesForBot(userId, botId) }));
                return;
            }
            const handoffUrl = String(data.novnc_url || data.desktop_handoff_url || '');
            if (!handoffUrl) return;
            const reported = Object.prototype.hasOwnProperty.call(data, 'user_control') || Object.prototype.hasOwnProperty.call(data, 'desktop_user_control');
            const userControl = data.user_control === true || data.desktop_user_control === true;
            setMessagesByBot(prev => {
                const current = prev[pending.botId] || messagesForBot(userId, pending.botId);
                const next = current.map(item => item.id === pending.messageId && item.pending ? { ...item, handoffUrl, userControl: reported ? userControl : item.userControl } : item);
                saveBotMessages(userId, pending.botId, next);
                return { ...prev, [pending.botId]: next };
            });
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
        if (editingId === botId) {
            setEditingId('');
            setDraftTitle('');
            setDraftDescription('');
        }
        if (selectedId === botId) setSelectedId('');
        setMessagesByBot(prev => {
            const copy = { ...prev };
            delete copy[botId];
            return copy;
        });
    };

    const send = async (preset?: string) => {
        if (!selected || running) return;
        const content = (preset ?? command).trim();
        if (!content) return;
        const userMessage: DesktopBotMessage = { id: messageID(), role: 'user', content };
        const assistantMessage: DesktopBotMessage = { id: messageID(), role: 'assistant', content: '', pending: true };
        const pending: PendingSend = { botId: selected.id, messageId: assistantMessage.id, settled: false };
        const ticket = `local:${assistantMessage.id}`;
        pendingRef.current.set(ticket, pending);
        // The missed reply was still holding the keyboard. This command is the
        // agent continuing in that same browser, so the person no longer types.
        const prior = (messagesByBot[selected.id] || messagesForBot(userId, selected.id)).map(item => (
            item.role === 'assistant' && pendingReplyIsStale(item)
                ? { ...item, pending: false, userControl: false, content: item.content || text.userControl }
                : item
        ));
        rememberMessages(selected.id, [...prior, userMessage, assistantMessage]);
        if (!preset) setCommand('');
        setRunningBots(prev => ({ ...prev, [selected.id]: true }));
        try {
            const response = await SendDesktopBotTask(selected.id, content) as { request_id?: string; RequestID?: string; deferred?: boolean; Deferred?: boolean; text?: string; Text?: string; error?: string; Error?: string };
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
                setMessagesByBot(prev => {
                    const current = prev[selected.id] || [];
                    const next = current.map(item => item.id === assistantMessage.id ? { ...item, content: result.content, pending: false, failed: result.failed, handoffUrl, userControl } : item);
                    saveBotMessages(userId, selected.id, next);
                    return { ...prev, [selected.id]: next };
                });
                setRunningBots(prev => ({ ...prev, [selected.id]: false }));
                return;
            }
            if (requestId) {
                pendingRef.current.delete(ticket);
                pendingRef.current.set(requestId, pending);
                setMessagesByBot(prev => {
                    const current = prev[selected.id] || messagesForBot(userId, selected.id);
                    const next = current.map(item => item.id === assistantMessage.id ? { ...item, requestId } : item);
                    saveBotMessages(userId, selected.id, next);
                    return { ...prev, [selected.id]: next };
                });
            }
        } catch (error) {
            if (pending.settled) return;
            pending.settled = true;
            pendingRef.current.delete(ticket);
            const detail = error instanceof Error ? error.message : text.unavailable;
            setMessagesByBot(prev => {
                const current = prev[selected.id] || [];
                const next = current.map(item => item.id === assistantMessage.id ? { ...item, content: detail || text.unavailable, pending: false, failed: true } : item);
                saveBotMessages(userId, selected.id, next);
                return { ...prev, [selected.id]: next };
            });
            setRunningBots(prev => ({ ...prev, [selected.id]: false }));
        }
    };

    const onComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            void send();
        }
    };

    return (
        <section className="desktop-bot-workspace" data-testid="desktop-bot-workspace" data-user-id={userId}>
            <aside className="desktop-bot-workspace__list-pane" aria-label={text.listTitle}>
                <header className="desktop-bot-workspace__bar">
                    <div>
                        <h1>{text.listTitle}</h1>
                        <p>{text.shared}</p>
                    </div>
                    <button type="button" data-testid="desktop-bot-create" onClick={create}>{text.create}</button>
                </header>
                {listNotice ? <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-list-error" role="status">{listNotice}</p> : null}
                {bots.length === 0 && !listFailed ? (
                    <p className="desktop-bot-workspace__empty" data-testid="desktop-bot-empty">{text.empty}</p>
                ) : (
                    <ul className="desktop-bot-workspace__list">
                        {bots.map(bot => {
                            const editing = editingId === bot.id;
                            const description = bot.description || text.defaultDescription;
                            return (
                                <li key={bot.id} className={'desktop-bot-workspace__row' + (selectedId === bot.id ? ' is-selected' : '')} data-testid={`desktop-bot-${bot.id}`}>
                                    {editing ? (
                                        <div className="desktop-bot-workspace__edit">
                                            <input aria-label={text.name} value={draftTitle} maxLength={40} onChange={event => setDraftTitle(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') saveEdit(bot.id); if (event.key === 'Escape') setEditingId(''); }} />
                                            <input aria-label={text.description} value={draftDescription} maxLength={80} onChange={event => setDraftDescription(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') saveEdit(bot.id); if (event.key === 'Escape') setEditingId(''); }} />
                                        </div>
                                    ) : (
                                        <button type="button" className="desktop-bot-workspace__open" aria-pressed={selectedId === bot.id} onClick={() => selectBot(bot)}>
                                            <strong>{bot.title}</strong>
                                            <span>{description}</span>
                                        </button>
                                    )}
                                    <div className="desktop-bot-workspace__actions">
                                        {editing ? (
                                            <>
                                                <button type="button" onClick={() => saveEdit(bot.id)}>{text.save}</button>
                                                <button type="button" onClick={() => setEditingId('')}>{text.cancel}</button>
                                            </>
                                        ) : (
                                            <button type="button" onClick={() => { setEditingId(bot.id); setDraftTitle(bot.title); setDraftDescription(bot.description); setPendingDeleteId(''); }}>{text.rename}</button>
                                        )}
                                        <button type="button" data-testid={`desktop-bot-delete-${bot.id}`} onClick={() => remove(bot.id)}>
                                            {pendingDeleteId === bot.id ? text.confirmRemove : text.remove}
                                        </button>
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </aside>
            <section className="desktop-bot-chat" data-testid="desktop-bot-chat" aria-label={selected ? selected.title : text.pick}>
                {selected ? (
                    <>
                        <div className="desktop-bot-chat__log" data-testid="desktop-bot-log">
                            {messages.filter(message => !message.pending).map(message => (
                                <p key={message.id} className={`desktop-bot-chat__bubble desktop-bot-chat__bubble--${message.role}${message.failed ? ' is-failed' : ''}`}>
                                    {message.content}
                                </p>
                            ))}
                        </div>
                        {showDesktop ? (
                            <div className={'desktop-bot-stage' + (userHasControl ? ' is-user' : ' is-bot')} data-testid="desktop-bot-stage">
                                <div className="desktop-bot-stage__bar">
                                    <p>{userHasControl ? text.userControl : (settledKeepsBrowser ? text.stillThere : text.botControl)}</p>
                                    {settledKeyboard ? (
                                        <button type="button" data-testid="desktop-bot-return-control" onClick={() => void send(text.returnCommand)}>{text.returnControl}</button>
                                    ) : null}
                                </div>
                                <iframe className="desktop-bot-chat__handoff" data-testid="desktop-bot-handoff" title="desktop" src={desktopFrameSrc(desktopUrl, userHasControl)} />
                            </div>
                        ) : null}
                        <p className={'desktop-bot-chat__status' + (running ? ' is-working' : '')} data-testid="desktop-bot-status">
                            <span>{selected.title} {running ? text.working : text.idle}</span>
                        </p>
                        <form className="desktop-bot-chat__form" onSubmit={event => { event.preventDefault(); void send(); }}>
                            <textarea data-testid="desktop-bot-command" aria-label={text.placeholder} placeholder={`${text.placeholder} ${selected.title}`} value={command} onChange={event => setCommand(event.target.value)} onKeyDown={onComposerKeyDown} />
                            <button type="submit" data-testid="desktop-bot-send" disabled={running || !command.trim()}>{text.send}</button>
                        </form>
                    </>
                ) : (
                    <p className="desktop-bot-chat__pick" data-testid="desktop-bot-pick">{text.pick}</p>
                )}
            </section>
        </section>
    );
}

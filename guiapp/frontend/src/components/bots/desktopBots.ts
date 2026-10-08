export interface DesktopBot {
    id: string;
    title: string;
    description: string;
    createdAt: number;
}

export type BotPhase = 'plan' | 'execute';

// Ask details copied from the turn. secretName is a placeholder such as
// SITE_PASSWORD. The secret value is never stored on the message.
export interface DesktopBotAsk {
    inputType?: string;
    secretName?: string;
    question?: string;
    options?: string[];
}

export interface DesktopBotMessage {
    id: string;
    role: 'user' | 'assistant';
    content: string;
    pending?: boolean;
    failed?: boolean;
    handoffUrl?: string;
    userControl?: boolean;
    attentionReason?: string;
    phase?: BotPhase;
    askUser?: DesktopBotAsk;
    askResolved?: boolean;
    requestId?: string;
    // Instant "colleague accepted the task" reply. It never carries a handoff
    // or a result, so result lookups skip it.
    ack?: boolean;
    // A user message accepted while that bot was still working. The dispatch
    // waits for the current run on the shared desktop; the marker is cleared
    // the moment the dispatch starts, so a page reload can recover the queue
    // without double-sending.
    queued?: boolean;
}

const STORAGE_KEY = 'maclaw.desktopBots.v1';
const MESSAGE_KEY = 'maclaw.desktopBotMessages.v1';

type StoredBots = Record<string, DesktopBot[]>;

function storage(): Storage | null {
    try {
        return window.localStorage;
    } catch {
        return null;
    }
}

function readAll(): StoredBots {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(STORAGE_KEY) || '{}') as StoredBots;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

function writeAll(value: StoredBots) {
    storage()?.setItem(STORAGE_KEY, JSON.stringify(value));
}

export function desktopUserKey(userId: string): string {
    const key = userId.trim();
    return key || 'local';
}

export function botsForUser(userId: string): DesktopBot[] {
    const items = readAll()[desktopUserKey(userId)];
    if (!Array.isArray(items)) return [];
    return items.filter(item => item && item.id && item.title).map(item => ({
        ...item,
        description: String(item.description || ''),
    }));
}

export function addDesktopBot(userId: string, now = Date.now(), description = ''): DesktopBot {
    const key = desktopUserKey(userId);
    const all = readAll();
    const current = Array.isArray(all[key]) ? all[key] : [];
    const bot: DesktopBot = {
        id: `bot-${now.toString(36)}-${current.length + 1}`,
        title: `Bot ${current.length + 1}`,
        description: description.trim().slice(0, 80),
        createdAt: now,
    };
    all[key] = [...current, bot];
    writeAll(all);
    return bot;
}

export function renameDesktopBot(userId: string, botId: string, title: string, description?: string): DesktopBot[] {
    const nextTitle = title.trim();
    if (!nextTitle) return botsForUser(userId);
    const key = desktopUserKey(userId);
    const all = readAll();
    const current = Array.isArray(all[key]) ? all[key] : [];
    all[key] = current.map(bot => bot.id === botId ? {
        ...bot,
        title: nextTitle.slice(0, 40),
        description: description === undefined ? String(bot.description || '') : description.trim().slice(0, 80),
    } : bot);
    writeAll(all);
    return botsForUser(userId);
}

export function deleteDesktopBot(userId: string, botId: string): DesktopBot[] {
    const key = desktopUserKey(userId);
    const all = readAll();
    const current = Array.isArray(all[key]) ? all[key] : [];
    all[key] = current.filter(bot => bot.id !== botId);
    writeAll(all);
    clearBotMessages(userId, botId);
    return all[key];
}

type StoredMessages = Record<string, Record<string, DesktopBotMessage[]>>;

function readMessages(): StoredMessages {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(MESSAGE_KEY) || '{}') as StoredMessages;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

export function messagesForBot(userId: string, botId: string): DesktopBotMessage[] {
    const items = readMessages()[desktopUserKey(userId)]?.[botId];
    if (!Array.isArray(items)) return [];
    return items.filter(item => item && item.id && (item.role === 'user' || item.role === 'assistant'));
}

export function saveBotMessages(userId: string, botId: string, messages: DesktopBotMessage[]) {
    const store = storage();
    if (!store) return;
    const all = readMessages();
    const key = desktopUserKey(userId);
    all[key] = { ...(all[key] || {}), [botId]: messages.slice(-80) };
    store.setItem(MESSAGE_KEY, JSON.stringify(all));
}

// notePendingBotDesktop keeps the live desktop on the pending reply.
// Coming back to this bot during login still shows that same browser.
function pendingBotIndex(current: DesktopBotMessage[], requestId: string): number {
    if (requestId) {
        for (let i = current.length - 1; i >= 0; i--) {
            const item = current[i];
            if (item.role === 'assistant' && item.pending && item.requestId === requestId) return i;
        }
        // This request already belongs to an earlier message in the same browser.
        if (current.some(item => item.requestId === requestId)) return -1;
    }
    for (let i = current.length - 1; i >= 0; i--) {
        const item = current[i];
        if (item.role === 'assistant' && item.pending && !item.requestId) return i;
    }
    return -1;
}

export function notePendingBotDesktop(userId: string, botId: string, patch: { handoffUrl: string; userControl?: boolean; attentionReason?: string; reported: boolean; requestId?: string }) {
    const handoffUrl = patch.handoffUrl.trim();
    const reason = (patch.attentionReason || '').trim();
    const control = patch.userControl === true;
    // A desktop address on its own is not a screen handoff. Leave the pending
    // reply alone so a later visit does not treat the URL as someone waiting.
    if (!control && reason === '') return;
    const current = messagesForBot(userId, botId);
    const index = pendingBotIndex(current, patch.requestId || '');
    if (index < 0) return;
    const next = current.slice();
    next[index] = {
        ...next[index],
        handoffUrl: handoffUrl || next[index].handoffUrl,
        userControl: patch.reported ? control : (control || next[index].userControl),
        attentionReason: reason || next[index].attentionReason,
    };
    saveBotMessages(userId, botId, next);
}

// settlePendingBotReply records the instance result on the pending reply.
// The bots page may already be closed when MaClawSrv finishes.
export function settlePendingBotReply(userId: string, botId: string, patch: { content: string; failed: boolean; handoffUrl?: string; userControl?: boolean; attentionReason?: string; askUser?: DesktopBotAsk; requestId?: string }) {
    const current = messagesForBot(userId, botId);
    const index = pendingBotIndex(current, patch.requestId || '');
    if (index < 0) return;
    const next = current.slice();
    const previous = next[index];
    const reason = (patch.attentionReason || '').trim();
    next[index] = {
        ...previous,
        content: patch.content,
        pending: false,
        failed: patch.failed,
        handoffUrl: patch.handoffUrl || '',
        userControl: patch.userControl === true,
        attentionReason: reason,
        askUser: patch.askUser || previous.askUser,
    };
    saveBotMessages(userId, botId, next);
}

function clearBotMessages(userId: string, botId: string) {
    const store = storage();
    if (!store) return;
    const all = readMessages();
    const key = desktopUserKey(userId);
    if (!all[key]) return;
    delete all[key][botId];
    store.setItem(MESSAGE_KEY, JSON.stringify(all));
}

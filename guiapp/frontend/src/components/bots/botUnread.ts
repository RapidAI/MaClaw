// Unread bot replies for the Windows taskbar badge. A reply counts once it
// has landed in the transcript. The person is looking when this bot's
// conversation is on screen and the window is in front; those replies are
// already seen. Opening that conversation later subtracts only its replies.
import {
    BOT_TRANSCRIPT_EVENT,
    desktopUserKey,
    eachDesktopBotTranscript,
    type DesktopBotMessage,
} from './desktopBots';

const UNREAD_KEY = 'maclaw.desktopBotUnread.v1';

type SeenBots = Record<string, string[]>;

type UnreadFile = {
    adopted?: boolean;
    seen?: Record<string, SeenBots>;
};

type Watch = { userKey: string; botId: string };

let watch: Watch | null = null;
const listeners = new Set<() => void>();

type TranscriptRow = { userKey: string; botId: string; messages: DesktopBotMessage[] };

// One read of the transcript, shared by the mark and the count that the
// same write wakes up. Screenshot saves parse a multi-megabyte store; a
// second walk in that handler was the same bytes again.
let scanRows: TranscriptRow[] | null = null;

function transcriptRows(): TranscriptRow[] {
    if (scanRows) return scanRows;
    const rows: TranscriptRow[] = [];
    eachDesktopBotTranscript((userKey, botId, messages) => {
        rows.push({ userKey, botId, messages });
    });
    return rows;
}

function withTranscript<T>(run: () => T): T {
    if (scanRows) return run();
    const rows: TranscriptRow[] = [];
    eachDesktopBotTranscript((userKey, botId, messages) => {
        rows.push({ userKey, botId, messages });
    });
    scanRows = rows;
    try {
        return run();
    } finally {
        scanRows = null;
    }
}

// Owned by window focus and blur, not by document.hasFocus(). A noVNC frame
// is a different document, so hasFocus() is false while that frame has the
// keyboard even though this window is still the one in front. The page can
// raise the flag when hasFocus() is true; it must not lower the flag that way.
let windowForeground = typeof document !== 'undefined' && typeof document.hasFocus === 'function' && document.hasFocus();

export function setBotWindowForeground(focused: boolean) {
    windowForeground = focused;
}

export function botWindowIsForeground(): boolean {
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return false;
    return windowForeground;
}

function browserStorage(): Storage | null {
    try {
        return window.localStorage;
    } catch {
        return null;
    }
}

// The file in memory is what this session counts. A failed setItem keeps
// that copy and retries it on the next transcript write, so a reply the
// person is looking at stays seen. Tests that wipe localStorage call
// discardBotUnreadMemory; a missing key alone is also how the first save fails.
let memory: UnreadFile | null = null;
let memoryStamp = '';
let memoryDirty = false;

function storedUnreadText(): string {
    const store = browserStorage();
    if (!store) return '';
    try {
        return store.getItem(UNREAD_KEY) || '';
    } catch {
        return '';
    }
}

function loadUnread(): UnreadFile {
    if (memoryDirty && memory) return memory;
    const text = storedUnreadText();
    if (memory && text === memoryStamp) return memory;
    if (!text) {
        memory = {};
        memoryStamp = '';
        return memory;
    }
    try {
        const parsed = JSON.parse(text) as UnreadFile;
        memory = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
    } catch {
        memory = {};
    }
    memoryStamp = text;
    return memory;
}

function saveUnread(file: UnreadFile) {
    memory = file;
    const store = browserStorage();
    if (!store) {
        memoryDirty = true;
        return;
    }
    const text = JSON.stringify(file);
    try {
        store.setItem(UNREAD_KEY, text);
        memoryStamp = text;
        memoryDirty = false;
    } catch {
        memoryDirty = true;
    }
}

export function discardBotUnreadMemory() {
    memory = null;
    memoryStamp = '';
    memoryDirty = false;
}

// A settled assistant reply. A pending bubble and the reading that the
// arrangement replaces are not replies yet. The settled bubble keeps its id,
// so it is counted once.
function isBotReply(message: DesktopBotMessage): boolean {
    return message.role === 'assistant' && !message.pending && !message.understood;
}

function replyIds(messages: DesktopBotMessage[]): string[] {
    const ids: string[] = [];
    for (const message of messages) {
        if (isBotReply(message)) ids.push(message.id);
    }
    return ids;
}

function seenIdList(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const ids: string[] = [];
    for (const id of value) {
        if (typeof id === 'string') ids.push(id);
    }
    return ids;
}

function sameIds(left: unknown, right: string[]): boolean {
    const list = seenIdList(left);
    if (list.length !== right.length) return false;
    for (let i = 0; i < right.length; i++) {
        if (list[i] !== right[i]) return false;
    }
    return true;
}

// adoptExistingBotReplies marks every reply already in the transcript as
// seen. Later replies are the ones the badge counts. A second call keeps
// the replies that arrived after the first.
export function adoptExistingBotReplies() {
    const file = loadUnread();
    if (file.adopted) return;
    const seen: Record<string, SeenBots> = {};
    for (const row of transcriptRows()) {
        if (!seen[row.userKey]) seen[row.userKey] = {};
        seen[row.userKey][row.botId] = replyIds(row.messages);
    }
    saveUnread({ adopted: true, seen });
}

function currentReplyIds(userId: string, botId: string): string[] {
    const key = desktopUserKey(userId);
    for (const row of transcriptRows()) {
        if (row.userKey === key && row.botId === botId) return replyIds(row.messages);
    }
    return [];
}

function markBotSeen(userId: string, botId: string): boolean {
    const key = desktopUserKey(userId);
    const ids = currentReplyIds(userId, botId);
    const file = loadUnread();
    const seen = file.seen || {};
    const bots = seen[key] || {};
    if (sameIds(bots[botId], ids)) return false;
    bots[botId] = ids;
    seen[key] = bots;
    saveUnread({ ...file, seen });
    return true;
}

function notifyUnread() {
    for (const listener of listeners) listener();
}

function onTranscript(event: Event) {
    withTranscript(() => {
        if (memoryDirty && memory) saveUnread(memory);
        const detail = (event as CustomEvent<{ userId?: string; botId?: string }>).detail;
        const userId = detail?.userId || '';
        const botId = detail?.botId || '';
        if (watch && userId && botId && desktopUserKey(userId) === watch.userKey && botId === watch.botId) {
            markBotSeen(userId, botId);
        }
        notifyUnread();
    });
}

// watchBotReplies is the conversation on screen. A bot id marks that bot's
// current replies seen. Null means the person is on another page, another
// bot, or the window is in the background.
export function watchBotReplies(userId: string, botId: string | null) {
    if (!botId) {
        watch = null;
        return;
    }
    withTranscript(() => {
        watch = { userKey: desktopUserKey(userId), botId };
        if (markBotSeen(userId, botId)) notifyUnread();
    });
}

export function unreadBotReplyCount(userId: string): number {
    const key = desktopUserKey(userId);
    const seen = loadUnread().seen?.[key] || {};
    let total = 0;
    for (const row of transcriptRows()) {
        if (row.userKey !== key) continue;
        const known = new Set(seenIdList(seen[row.botId]));
        for (const message of row.messages) {
            if (isBotReply(message) && !known.has(message.id)) total += 1;
        }
    }
    return total;
}

export function subscribeBotUnread(listener: () => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

adoptExistingBotReplies();
if (typeof window !== 'undefined') {
    window.addEventListener(BOT_TRANSCRIPT_EVENT, onTranscript);
}

import { desktopUserKey } from './desktopBots';

// Typed commands for the bot composer. This store is not the assistant
// prompt history (`ai-assistant-prompt-history`) and not a bot transcript.
export const DESKTOP_BOT_INPUT_HISTORY_KEY = 'maclaw.desktopBotInputHistory.v1';
export const DESKTOP_BOT_INPUT_HISTORY_MAX = 100;
// One pasted document must not crowd out the localStorage quota shared with transcripts.
export const DESKTOP_BOT_INPUT_ENTRY_MAX = 12_000;

type StoredHistory = Record<string, string[]>;

function storage(): Storage | null {
    try {
        return window.localStorage;
    } catch {
        return null;
    }
}

function normalize(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const out: string[] = [];
    for (const item of value) {
        if (typeof item !== 'string') continue;
        const text = item.trim();
        if (!text || text.length > DESKTOP_BOT_INPUT_ENTRY_MAX) continue;
        out.push(text);
    }
    return out.length > DESKTOP_BOT_INPUT_HISTORY_MAX ? out.slice(-DESKTOP_BOT_INPUT_HISTORY_MAX) : out;
}

function readAll(): StoredHistory {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(DESKTOP_BOT_INPUT_HISTORY_KEY) || '{}') as StoredHistory;
        return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
    } catch {
        return {};
    }
}

function writeAll(value: StoredHistory) {
    try {
        storage()?.setItem(DESKTOP_BOT_INPUT_HISTORY_KEY, JSON.stringify(value));
    } catch {
        // localStorage full or unavailable. The composer still sends.
    }
}

export function botInputHistory(userId: string): string[] {
    return normalize(readAll()[desktopUserKey(userId)]);
}

// Newest entry is last. A repeat of the latest line is kept once, so ↑ walks
// the sequence the person actually sent.
export function rememberBotInput(userId: string, text: string): string[] {
    const trimmed = text.trim();
    const key = desktopUserKey(userId);
    const all = readAll();
    const current = normalize(all[key]);
    if (!trimmed || trimmed.length > DESKTOP_BOT_INPUT_ENTRY_MAX || current[current.length - 1] === trimmed) return current;
    const next = [...current, trimmed].slice(-DESKTOP_BOT_INPUT_HISTORY_MAX);
    all[key] = next;
    writeAll(all);
    return next;
}

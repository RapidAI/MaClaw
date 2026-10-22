// Per-bot badge look (color + shape). Hub stores it per bot (color/shape on the
// bot list, PUT /api/v1/bots/{id}/appearance). Local storage is the fallback
// for an older Hub that does not echo the fields, and the source of a one-time
// move of looks saved before Hub kept them.

export const BOT_COLORS = [
    '#16a34a', '#2563eb', '#7c3aed', '#db2777', '#ea580c',
    '#ca8a04', '#0891b2', '#dc2626', '#4f46e5', '#475569',
] as const;

export const BOT_SHAPES = ['blob', 'cloud', 'leaf', 'square', 'pill', 'circle'] as const;
export type BotShape = typeof BOT_SHAPES[number];

export type BotAppearance = { color: string; shape: BotShape };

const KEY = 'maclaw.desktopBotAppearance.v1';
type Stored = Record<string, Record<string, BotAppearance>>;

function storage(): Storage | null {
    try {
        return typeof window !== 'undefined' && window.localStorage ? window.localStorage : null;
    } catch {
        return null;
    }
}

function userKey(userId: string): string {
    return String(userId || '').trim() || 'local';
}

function readAll(): Stored {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(KEY) || '{}') as Stored;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

export function isBotColor(value: string): boolean {
    return /^#[0-9a-fA-F]{6}$/.test(String(value || '').trim());
}

export function isBotShape(value: string): value is BotShape {
    return (BOT_SHAPES as readonly string[]).includes(value);
}

export function appearancesForUser(userId: string): Record<string, BotAppearance> {
    const mine = readAll()[userKey(userId)];
    if (!mine || typeof mine !== 'object') return {};
    const out: Record<string, BotAppearance> = {};
    for (const [id, look] of Object.entries(mine)) {
        if (look && isBotColor(look.color) && isBotShape(look.shape)) out[id] = { color: look.color, shape: look.shape };
    }
    return out;
}

export function saveBotAppearance(userId: string, botId: string, look: BotAppearance | null) {
    const store = storage();
    if (!store || !botId) return;
    const all = readAll();
    const key = userKey(userId);
    const mine = { ...(all[key] || {}) };
    if (!look || !isBotColor(look.color) || !isBotShape(look.shape)) delete mine[botId];
    else mine[botId] = { color: look.color, shape: look.shape };
    all[key] = mine;
    store.setItem(KEY, JSON.stringify(all));
}

/** Fallback for a bot without a saved look: stable hash of its title. */
export function defaultAppearance(seed: string): BotAppearance {
    let hash = 0;
    for (const ch of seed) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
    return { color: BOT_COLORS[hash % BOT_COLORS.length], shape: 'circle' };
}

/** The next color/shape for a new bot: the first color not already used, else round-robin. */
export function nextAppearance(used: BotAppearance[]): BotAppearance {
    const colors = new Set(used.map(look => look.color.toLowerCase()));
    const color = BOT_COLORS.find(c => !colors.has(c)) || BOT_COLORS[used.length % BOT_COLORS.length];
    return { color, shape: BOT_SHAPES[used.length % (BOT_SHAPES.length - 1)] };
}

/** True when another bot (not exceptId) already has this name, ignoring case and spaces. */
export function botNameTaken(name: string, bots: { id: string; title: string }[], exceptId = ''): boolean {
    const norm = (s: string) => s.trim().replace(/\s+/g, ' ').toLowerCase();
    const want = norm(name);
    if (!want) return false;
    return bots.some(bot => bot.id !== exceptId && norm(bot.title) === want);
}

/** The look Hub echoed for a bot, or null when Hub sent none (or an unknown shape). */
export function hubAppearance(item: { color?: string; shape?: string } | null | undefined): BotAppearance | null {
    const color = String(item?.color || '').trim();
    const shape = String(item?.shape || '').trim();
    return isBotColor(color) && isBotShape(shape) ? { color, shape } : null;
}

/** Hub look wins; local storage fills bots Hub has no look for. */
export function mergeLooks(local: Record<string, BotAppearance>, items: { id: string; color?: string; shape?: string }[]): Record<string, BotAppearance> {
    const out: Record<string, BotAppearance> = { ...local };
    for (const item of items) {
        const look = hubAppearance(item);
        if (look) out[item.id] = look;
    }
    return out;
}

const MIGRATED_KEY = 'maclaw.desktopBotAppearance.v1.migrated';

function readMigrated(): Record<string, string[]> {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(MIGRATED_KEY) || '{}');
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

/** Bots listed without a Hub look that have a local look not yet moved to Hub. */
export function looksToMigrate(userId: string, items: { id: string; color?: string; shape?: string }[]): { id: string; look: BotAppearance }[] {
    const local = appearancesForUser(userId);
    const done = new Set(readMigrated()[userKey(userId)] || []);
    return items
        .filter(item => !hubAppearance(item) && local[item.id] && !done.has(item.id))
        .map(item => ({ id: item.id, look: local[item.id] }));
}

/** Record that a bot's local look was sent to Hub, so it is moved only once. */
export function markLookMigrated(userId: string, botId: string) {
    const store = storage();
    if (!store || !botId) return;
    const all = readMigrated();
    const key = userKey(userId);
    const mine = new Set(all[key] || []);
    mine.add(botId);
    all[key] = [...mine];
    store.setItem(MIGRATED_KEY, JSON.stringify(all));
}

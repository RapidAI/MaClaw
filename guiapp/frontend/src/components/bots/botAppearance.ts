// Per-bot badge look (color + shape). Hub stores it per bot (color/shape on the
// bot list, PUT /api/v1/bots/{id}/appearance). The on-disk copy lives in
// SQLite (desktop_bot_appearance.db) for an older Hub that does not echo the
// fields. Browser localStorage is not written: that origin quota is what
// failed creating a bot. A document left under the old keys is imported once,
// then those keys are removed.

import {
    ClearDesktopBotLook,
    LoadDesktopBotLooks,
    MarkDesktopBotLookMigrated,
    SaveDesktopBotLook,
    UnmarkDesktopBotLookMigrated,
} from '../../../wailsjs/go/main/App';

export const BOT_COLORS = [
    '#16a34a', '#2563eb', '#7c3aed', '#db2777', '#ea580c',
    '#ca8a04', '#0891b2', '#dc2626', '#4f46e5', '#475569',
] as const;

export const BOT_SHAPES = ['blob', 'cloud', 'leaf', 'square', 'pill', 'circle'] as const;
export type BotShape = typeof BOT_SHAPES[number];

export type BotAppearance = { color: string; shape: BotShape };

const LEGACY_KEY = 'maclaw.desktopBotAppearance.v1';
const LEGACY_MIGRATED_KEY = 'maclaw.desktopBotAppearance.v1.migrated';

type Stored = Record<string, Record<string, BotAppearance>>;
type LookFile = { looks: Record<string, BotAppearance>; migrated: string[] };

const cache: Stored = {};
const migratedCache: Record<string, string[]> = {};
// A look cleared while a load is in flight. Absence in the cache also means
// "not loaded yet", so a late SQLite or WebView row would put the cleared
// badge back. A later valid save removes the mark.
const dropped = new Set<string>();
// A copied mark cleared because the stored look changed. A load that started
// earlier still returns the old mark; without this it would put that mark
// back and the new look would never be sent.
const unmarked = new Set<string>();
// Bumped when a bot's stored look changes. A Hub list is a snapshot from
// when it was requested; a save after that is the newer look.
const lookRev = new Map<string, number>();
// Mark and Unmark are separate bridge calls. One chain per bot so a copy
// that finishes as the user picks a new look cannot insert the old mark
// after that look has already cleared it.
const markWrite = new Map<string, Promise<void>>();
let legacySent = false;
let testLoad: ((userId: string, legacyLooks: string, legacyMigrated: string) => Promise<{ looks?: Record<string, { color?: string; shape?: string }> | null; migrated?: string[] | null }>) | null = null;

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

function tombKey(user: string, botId: string): string {
    return user + '\0' + botId;
}

function safeGet(key: string): string {
    const store = storage();
    if (!store) return '';
    try {
        return store.getItem(key) || '';
    } catch {
        return '';
    }
}

function safeRemove(key: string) {
    const store = storage();
    if (!store) return;
    try {
        store.removeItem(key);
    } catch {
        /* private mode */
    }
}

function readableLegacy(key: string): string {
    const raw = safeGet(key);
    if (!raw) return '';
    try {
        JSON.parse(raw);
        return raw;
    } catch {
        safeRemove(key);
        return '';
    }
}

export function isBotColor(value: string): boolean {
    return /^#[0-9a-fA-F]{6}$/.test(String(value || '').trim());
}

export function isBotShape(value: string): value is BotShape {
    return (BOT_SHAPES as readonly string[]).includes(value);
}

function validLook(look: { color?: string; shape?: string } | null | undefined): BotAppearance | null {
    const color = String(look?.color || '').trim();
    const shape = String(look?.shape || '').trim();
    return isBotColor(color) && isBotShape(shape) ? { color, shape } : null;
}

function rememberCache(userId: string, file: LookFile) {
    const key = userKey(userId);
    const mine = { ...(cache[key] || {}) };
    for (const [id, look] of Object.entries(file.looks)) {
        if (dropped.has(tombKey(key, id))) continue;
        if (!mine[id]) mine[id] = look;
    }
    cache[key] = mine;
    rememberMigrated(key, file.migrated);
}

function rememberMigrated(key: string, ids: string[]) {
    const merged = new Set(migratedCache[key] || []);
    for (const id of ids) {
        if (!id || unmarked.has(tombKey(key, id))) continue;
        merged.add(id);
    }
    migratedCache[key] = [...merged];
}

function enqueueMarkWrite(user: string, id: string, run: () => unknown) {
    const revKey = tombKey(user, id);
    const prev = markWrite.get(revKey) || Promise.resolve();
    const next = prev.then(() => {
        try {
            return Promise.resolve(run()).then(() => undefined, () => undefined);
        } catch {
            return undefined;
        }
    });
    markWrite.set(revKey, next);
}

function applyLegacyDocuments(looksJSON: string, migratedJSON: string) {
    if (looksJSON) {
        try {
            const parsed = JSON.parse(looksJSON) as Stored;
            if (parsed && typeof parsed === 'object') {
                for (const [user, mine] of Object.entries(parsed)) {
                    if (!mine || typeof mine !== 'object') continue;
                    const key = userKey(user);
                    const next = { ...(cache[key] || {}) };
                    for (const [id, look] of Object.entries(mine)) {
                        if (dropped.has(tombKey(key, id))) continue;
                        const valid = validLook(look);
                        if (valid && !next[id]) next[id] = valid;
                    }
                    cache[key] = next;
                }
            }
        } catch {
            /* leave the cache as it is */
        }
    }
    if (migratedJSON) {
        try {
            const parsed = JSON.parse(migratedJSON) as Record<string, string[]>;
            if (parsed && typeof parsed === 'object') {
                for (const [user, ids] of Object.entries(parsed)) {
                    const key = userKey(user);
                    const idsToKeep: string[] = [];
                    for (const id of Array.isArray(ids) ? ids : []) {
                        if (id) idsToKeep.push(String(id));
                    }
                    rememberMigrated(key, idsToKeep);
                }
            }
        } catch {
            /* leave the cache as it is */
        }
    }
}

function normalizeFile(file: { looks?: Record<string, { color?: string; shape?: string }> | null; migrated?: string[] | null } | null | undefined): LookFile {
    const looks: Record<string, BotAppearance> = {};
    for (const [id, look] of Object.entries(file?.looks || {})) {
        const valid = validLook(look);
        if (valid) looks[id] = valid;
    }
    const migrated = Array.isArray(file?.migrated) ? file.migrated.map(id => String(id || '').trim()).filter(Boolean) : [];
    return { looks, migrated };
}

async function loadFromSQLite(userId: string, legacyLooks: string, legacyMigrated: string): Promise<LookFile | null> {
    try {
        if (testLoad) return normalizeFile(await testLoad(userId, legacyLooks, legacyMigrated));
        if (typeof LoadDesktopBotLooks !== 'function') return null;
        return normalizeFile(await LoadDesktopBotLooks(userId, legacyLooks, legacyMigrated));
    } catch {
        return null;
    }
}

/** Read SQLite into the session cache. Old WebView documents are imported once, then removed. */
export async function hydrateBotLooks(userId: string): Promise<Record<string, BotAppearance>> {
    const legacyLooks = legacySent ? '' : readableLegacy(LEGACY_KEY);
    const legacyMigrated = legacySent ? '' : readableLegacy(LEGACY_MIGRATED_KEY);
    const sentLegacy = legacyLooks !== '' || legacyMigrated !== '';
    const loaded = await loadFromSQLite(userId, legacyLooks, legacyMigrated);
    if (loaded) {
        legacySent = true;
        safeRemove(LEGACY_KEY);
        safeRemove(LEGACY_MIGRATED_KEY);
        rememberCache(userId, loaded);
        return appearancesForUser(userId);
    }
    // The import call failed, so the bridge returned no rows. Read what SQLite
    // already has, and leave the WebView keys for the next launch.
    if (sentLegacy) {
        const stored = await loadFromSQLite(userId, '', '');
        if (stored) rememberCache(userId, stored);
    }
    if (!legacySent) applyLegacyDocuments(legacyLooks, legacyMigrated);
    return appearancesForUser(userId);
}

export function appearancesForUser(userId: string): Record<string, BotAppearance> {
    const mine = cache[userKey(userId)];
    if (!mine) return {};
    const out: Record<string, BotAppearance> = {};
    for (const [id, look] of Object.entries(mine)) {
        const valid = validLook(look);
        if (valid) out[id] = valid;
    }
    return out;
}

export function saveBotAppearance(
    userId: string,
    botId: string,
    look: BotAppearance | null,
    options?: { keepRevision?: boolean; keepMark?: boolean },
) {
    const id = String(botId || '').trim();
    if (!id) return;
    const key = userKey(userId);
    const mine = { ...(cache[key] || {}) };
    const prev = mine[id];
    const valid = look ? validLook(look) : null;
    if (!valid) {
        delete mine[id];
        dropped.add(tombKey(key, id));
    } else {
        mine[id] = valid;
        dropped.delete(tombKey(key, id));
    }
    // Copying a Hub list into the cache is not a user edit. Bumping here would
    // make that same list look newer than itself and skip its own color.
    // keepMark is Hub's own echo of a look it just accepted. That echo is
    // still the copied look, so it must not clear the mark either.
    const lookChanged = !!valid && (!prev || prev.color !== valid.color || prev.shape !== valid.shape);
    const cleared = !valid && !!prev;
    if ((lookChanged || cleared) && !options?.keepRevision) {
        const revKey = tombKey(key, id);
        lookRev.set(revKey, (lookRev.get(revKey) || 0) + 1);
    }
    const dropMark = lookChanged && !options?.keepRevision && !options?.keepMark;
    if (dropMark) {
        unmarked.add(tombKey(key, id));
        const marks = migratedCache[key];
        if (marks?.includes(id)) migratedCache[key] = marks.filter(item => item !== id);
    }
    cache[key] = mine;
    if (dropMark) {
        try {
            enqueueMarkWrite(key, id, () => (typeof UnmarkDesktopBotLookMigrated === 'function' ? UnmarkDesktopBotLookMigrated(userId, id) : null));
        } catch {
            /* The session mark is already gone. */
        }
    }
    try {
        const pending = !valid
            ? (typeof ClearDesktopBotLook === 'function' ? ClearDesktopBotLook(userId, id) : null)
            : (typeof SaveDesktopBotLook === 'function' ? SaveDesktopBotLook(userId, id, valid.color, valid.shape) : null);
        void Promise.resolve(pending).catch(() => { /* Hub keeps the look; this session still has the cache */ });
    } catch {
        /* The WebView bridge is missing. The session cache still has the look. */
    }
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
    return validLook(item);
}

export function botLookRevision(userId: string, botId: string): number {
    const id = String(botId || '').trim();
    if (!id) return 0;
    return lookRev.get(tombKey(userKey(userId), id)) || 0;
}

/** Revisions at this moment. A later save does not change the returned map. */
export function captureBotLookRevisions(userId: string): Record<string, number> {
    const prefix = userKey(userId) + '\0';
    const out: Record<string, number> = {};
    for (const [key, rev] of lookRev) {
        if (!key.startsWith(prefix)) continue;
        out[key.slice(prefix.length)] = rev;
    }
    return out;
}

/** Write a Hub list's colors into the session copy.
 *  `since` is the revision map from when that list was requested.
 *  A look saved after that is left alone. The next launch then starts from
 *  the color Hub already showed, instead of an older SQLite row.
 *  This is Hub's color, not a new local look, so the copied mark stays. */
export function storeHubLooks(
    userId: string,
    items: { id: string; color?: string; shape?: string }[],
    since?: { userId: string; revision: Record<string, number> },
): void {
    for (const item of items) {
        const look = hubAppearance(item);
        if (!look) continue;
        const id = String(item.id || '').trim();
        if (!id) continue;
        if (since && botLookRevision(since.userId, id) !== (since.revision[id] || 0)) continue;
        const local = appearancesForUser(userId)[id];
        if (local && local.color === look.color && local.shape === look.shape) continue;
        saveBotAppearance(userId, id, look, { keepRevision: true });
    }
}

/** Hub look wins; the session cache fills bots Hub has no look for.
 *  `since` is the revision map from when that Hub list was requested.
 *  A look saved after that stays. */
export function mergeLooks(
    local: Record<string, BotAppearance>,
    items: { id: string; color?: string; shape?: string }[],
    since?: { userId: string; revision: Record<string, number> },
): Record<string, BotAppearance> {
    const out: Record<string, BotAppearance> = { ...local };
    for (const item of items) {
        const look = hubAppearance(item);
        if (!look) continue;
        const id = String(item.id || '').trim();
        if (since && botLookRevision(since.userId, id) !== (since.revision[id] || 0)) continue;
        out[item.id] = look;
    }
    return out;
}

/** Bots listed without a Hub look that have a local look not yet moved to Hub. */
export function looksToMigrate(userId: string, items: { id: string; color?: string; shape?: string }[]): { id: string; look: BotAppearance }[] {
    const local = appearancesForUser(userId);
    const done = new Set(migratedCache[userKey(userId)] || []);
    return items
        .filter(item => !hubAppearance(item) && local[item.id] && !done.has(item.id))
        .map(item => ({ id: item.id, look: local[item.id] }));
}

/** Record that this bot's current look was copied to Hub. A later different look clears it. */
export function markLookMigrated(userId: string, botId: string) {
    const id = String(botId || '').trim();
    if (!id) return;
    const key = userKey(userId);
    unmarked.delete(tombKey(key, id));
    const mine = new Set(migratedCache[key] || []);
    mine.add(id);
    migratedCache[key] = [...mine];
    try {
        enqueueMarkWrite(key, id, () => (typeof MarkDesktopBotLookMigrated === 'function' ? MarkDesktopBotLookMigrated(userId, id) : null));
    } catch {
        /* bridge missing; the session set still skips a second send */
    }
}

/** Test hook. Production code does not call this. */
export function setBotLookLoaderForTest(load: typeof testLoad) {
    testLoad = load;
}

/** Test hook. Production code does not call this. */
export function resetBotLookCacheForTest() {
    for (const key of Object.keys(cache)) delete cache[key];
    for (const key of Object.keys(migratedCache)) delete migratedCache[key];
    dropped.clear();
    unmarked.clear();
    lookRev.clear();
    markWrite.clear();
    legacySent = false;
    testLoad = null;
}

import { FetchDesktopBotMailFile } from '../../../wailsjs/go/main/App';
import { botFeedLineOf, type BotFeedMeta } from './botFeedLine';
import { messageIdTime } from './collab/botCollab';

// Same rule as the chat: a picture is shown inline, so it is not a document.
function isDirectPreview(file: { name?: string; mime?: string }): boolean {
    const mime = String(file.mime || '').toLowerCase().split(';')[0].trim();
    const normalized = mime === 'image/jpg' ? 'image/jpeg' : mime;
    if (normalized) return normalized === 'image/png' || normalized === 'image/jpeg' || normalized === 'image/gif' || normalized === 'image/webp';
    return /\.(png|jpe?g|gif|webp)$/i.test(String(file.name || ''));
}

// Documents a desktop bot handed the person: saved paths on a reply, files
// still held as bytes, and files a said note (or a feed line) carried.
// Pictures the chat already shows inline are not documents.

export type BotArtifactSource =
    | { kind: 'path'; path: string; botId: string }
    | { kind: 'said'; noteId: string; botId: string }
    | { kind: 'inline'; botId: string; messageId: string };

export type BotArtifactRow = {
    key: string;
    name: string;
    size?: number;
    whenMs: number;
    source: BotArtifactSource;
};

export type ArtifactMessage = {
    id: string;
    role: string;
    failed?: boolean;
    pending?: boolean;
    content?: string;
    chat?: boolean;
    requestId?: string;
    conversationAt?: number;
    localPaths?: string[];
    files?: Array<{ name: string; mime?: string; data?: string }>;
    feed?: BotFeedMeta;
};

export type ArtifactSaid = {
    id: string;
    from?: string;
    created_at?: string;
    files?: Array<{ name?: string; mime?: string; MIME?: string; size?: number }>;
};

type SaveBinding = (botID: string, name: string, mime: string, data: string) => Promise<string>;

function saveBinding(): SaveBinding | undefined {
    const save = (window as unknown as { go?: { main?: { App?: { SaveDesktopBotDeliveredFile?: SaveBinding } } } }).go?.main?.App?.SaveDesktopBotDeliveredFile;
    return typeof save === 'function' ? save : undefined;
}

const savedBytes = new WeakMap<object, Promise<string>>();

// saveDesktopDeliveredFile asks the app to write these bytes once and returns
// the path. The same file object reuses that promise, so a second click does
// not keep another copy of the bytes or start a second write. The app also
// reuses the file already on disk.
export function saveDesktopDeliveredFile(botId: string, file: { name: string; mime?: string; data: string }): Promise<string> {
    const pending = savedBytes.get(file);
    if (pending) return pending;
    const next = (async () => {
        const save = saveBinding();
        if (!save) throw new Error('文件没有写入本机');
        const path = String(await save(botId, file.name.trim(), file.mime || 'application/octet-stream', compactBase64(file.data)) || '').trim();
        if (!path) throw new Error('文件没有写入本机');
        return path;
    })().catch(err => {
        savedBytes.delete(file);
        throw err;
    });
    savedBytes.set(file, next);
    return next;
}

const saidPaths = new Map<string, Promise<string>>();

// ensureSaidFilePath is the local copy of one file a Hub note carried.
// A large file is already saved; a small one is written on the first ask.
export function ensureSaidFilePath(botId: string, noteId: string, name: string): Promise<string> {
    const key = `${botId}\n${noteId}\n${name}`;
    const pending = saidPaths.get(key);
    if (pending) return pending;
    const next = (async () => {
        const file = await FetchDesktopBotMailFile(noteId, name) as { name?: string; mime?: string; data?: string; local_path?: string; LocalPath?: string } | undefined;
        const path = String(file?.local_path || file?.LocalPath || '').trim();
        if (path) return path;
        const data = compactBase64(String(file?.data || ''));
        if (!data) throw new Error('Hub 上没有这个文件');
        return saveDesktopDeliveredFile(botId, { name: String(file?.name || name), mime: file?.mime, data });
    })().catch(err => {
        saidPaths.delete(key);
        throw err;
    });
    saidPaths.set(key, next);
    return next;
}

export function artifactBaseName(path: string): string {
    const parts = path.split(/[/\\]/).filter(Boolean);
    return parts[parts.length - 1] || path;
}

function pathKey(path: string): string {
    return path.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
}

function whenOf(value: string | number | undefined): number {
    if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value;
    if (typeof value === 'string' && value.trim()) {
        const ms = Date.parse(value);
        if (Number.isFinite(ms) && ms > 0) return ms;
    }
    return 0;
}

// compactBase64 drops whitespace only when there is some, so a clean
// document is not copied again on the way to disk.
function compactBase64(data: string): string {
    return /\s/.test(data) ? data.replace(/\s+/g, '') : data;
}

function base64Bytes(data: string): number | undefined {
    // Count in place. Replacing whitespace would copy a multi-megabyte
    // document just to learn how big it is.
    let n = 0;
    let pad = 0;
    for (let i = 0; i < data.length; i++) {
        const c = data.charCodeAt(i);
        if (c <= 32) continue;
        n++;
        if (c === 61) pad++;
    }
    const body = n - pad;
    if (body <= 0) return undefined;
    return Math.floor(body * 3 / 4);
}

type SizeBinding = (paths: string[]) => Promise<number[]>;

// artifactFileSizes asks the app for the size of saved copies. null means the
// lookup itself failed, so the caller retries later. -1 means this path was
// not checked. 0 means it was checked and is missing or empty.
export function artifactFileSizes(paths: string[]): Promise<number[] | null> {
    const stat = (window as unknown as { go?: { main?: { App?: { DesktopBotArtifactSizes?: SizeBinding } } } }).go?.main?.App?.DesktopBotArtifactSizes;
    if (typeof stat !== 'function' || paths.length === 0) return Promise.resolve(null);
    return Promise.resolve(stat(paths)).then(sizes => {
        const list = Array.isArray(sizes) ? sizes : [];
        return paths.map((_, index) => {
            const n = Number(list[index]);
            return Number.isFinite(n) ? n : -1;
        });
    }).catch(() => null);
}

// collectBotArtifacts is every document this chat can open, newest first.
// A picture the bubble already shows is left out. The same Hub file is one row
// whether it also rode along on a feed line.
export function collectBotArtifacts(input: {
    botId: string;
    selfName?: string;
    messages: ArtifactMessage[];
    said: ArtifactSaid[];
}): BotArtifactRow[] {
    const rows: BotArtifactRow[] = [];
    const seen = new Set<string>();
    const add = (row: BotArtifactRow) => {
        if (!row.name || seen.has(row.key)) return;
        seen.add(row.key);
        rows.push(row);
    };
    for (const item of input.said) {
        const whenMs = whenOf(item.created_at);
        const botId = (item.from || '').trim() || input.botId;
        for (const file of item.files || []) {
            const name = String(file.name || '').trim();
            const mime = String(file.mime || file.MIME || '');
            if (!name || isDirectPreview({ name, mime })) continue;
            add({
                key: `said:${item.id}:${name}`,
                name,
                size: file.size && file.size > 0 ? file.size : undefined,
                whenMs,
                source: { kind: 'said', noteId: item.id, botId },
            });
        }
    }
    for (const message of input.messages) {
        if (message.role !== 'assistant' || message.failed || message.pending) continue;
        // The id is minted with the clock (m-<base36 ms>-…). conversationAt is
        // only a fallback for an older bubble whose id has no clock: a follow-up
        // rewrites conversationAt, and that must not jump the file to the top.
        const whenMs = messageIdTime(message.id) || whenOf(message.conversationAt);
        for (const path of message.localPaths || []) {
            const trimmed = String(path || '').trim();
            const name = artifactBaseName(trimmed);
            if (!trimmed || !name || isDirectPreview({ name })) continue;
            add({
                key: `path:${pathKey(trimmed)}`,
                name,
                whenMs,
                source: { kind: 'path', path: trimmed, botId: input.botId },
            });
        }
        for (const file of message.files || []) {
            const name = String(file?.name || '').trim();
            if (!name || !file.data || isDirectPreview({ name, mime: file.mime })) continue;
            add({
                key: `inline:${message.id}:${name}`,
                name,
                size: base64Bytes(file.data),
                whenMs,
                source: { kind: 'inline', botId: input.botId, messageId: message.id },
            });
        }
        if ((message.localPaths && message.localPaths.length) || (message.files && message.files.length) || !message.feed) continue;
        const line = botFeedLineOf(message.content || '', input.selfName || '', { feed: message.feed, failed: message.failed, requestId: message.requestId });
        if (!line?.noteId) continue;
        for (const name of line.files) {
            if (!name || isDirectPreview({ name }) || (line.stored && !line.stored.includes(name))) continue;
            add({
                key: `said:${line.noteId}:${name}`,
                name,
                whenMs,
                source: { kind: 'said', noteId: line.noteId, botId: input.botId },
            });
        }
    }
    const order = new Map(rows.map((row, index) => [row.key, index]));
    rows.sort((a, b) => (b.whenMs - a.whenMs) || ((order.get(b.key) || 0) - (order.get(a.key) || 0)));
    return rows;
}

export function formatArtifactSize(size: number | undefined): string {
    if (size == null || !Number.isFinite(size) || size < 0) return '';
    if (size < 1024) return `${Math.round(size)} B`;
    if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} KB`;
    const mb = size / (1024 * 1024);
    return `${mb >= 10 ? Math.round(mb) : mb.toFixed(1)} MB`;
}

export function formatArtifactWhen(ms: number): string {
    if (!ms || !Number.isFinite(ms)) return '';
    const date = new Date(ms);
    if (Number.isNaN(date.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

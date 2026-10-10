export interface DesktopBot {
    id: string;
    title: string;
    description: string;
    createdAt: number;
}

export type BotPhase = 'plan' | 'execute';

// What the latest plan reply still needs from the person.
// done: the request is already fulfilled, so the reply is the result.
// arrange: work remains, and the confirm control is the approval.
// ask: one missing fact, asked in the reply itself.
// assist: a person has to finish a login, captcha, or consent step.
export type DesktopTaskStep = 'done' | 'arrange' | 'ask' | 'assist';

const REMAINING_WORK = /确认后|按这个安排|我将|我会|下一步|请确认|安排[:：]/;
const FINISHED_REPORT = /已截屏|截图已|图片已附|已附在|可直接查看|已经完成|任务已完成/;
const OPEN_QUESTION = /[?？]/;
const ASKS_HOW = /要我|是否|怎么完成|哪个/;
const SCREEN_REQUEST = /截屏|截图|screenshot|发我屏|看一下桌面|看看桌面|当前桌面/i;
const FURTHER_WORK = /安装|打开|点击|输入|生成|文档|docx|登录|下载|install|open\b|click|navigate/i;

// A side note is answered in this chat and is not a desktop turn. An
// arrangement or a login handoff behind it still stands. A failure or a
// question that belongs to a turn carries a phase and is not a side note.
export function sideNote(message: Pick<DesktopBotMessage, 'ack' | 'chat' | 'failed' | 'askUser' | 'phase'>): boolean {
    if (message.ack || message.chat) return true;
    return !message.phase && (!!message.failed || !!message.askUser);
}

export function userRequestBefore(messages: DesktopBotMessage[], index: number): string {
    for (let i = index - 1; i >= 0; i--) {
        const item = messages[i];
        if (item.role === 'user') return item.content || '';
        // A side note is not the desktop turn. Keep walking back to the request.
        if (item.role === 'assistant' && !sideNote(item)) return '';
    }
    return '';
}

// A pending bubble whose process no longer holds the turn must not keep the
// chat locked. This clock only covers a reload: the Go turn is gone, and the
// stored bubble is all that is left. A turn the bots page admitted stays busy
// until it reports, however long the desktop work takes.
export const BOT_PENDING_REPLY_WAIT_MS = 31 * 60 * 1000;

export function botMessageStartedAt(id: string): number {
    const match = /^m-([0-9a-z]+)-/i.exec(id);
    if (!match) return 0;
    const started = Number.parseInt(match[1], 36);
    return Number.isFinite(started) && started > 0 ? started : 0;
}

export function pendingBotReplyIsStale(message: Pick<DesktopBotMessage, 'id' | 'pending'>, now = Date.now()): boolean {
    if (!message.pending) return false;
    const started = botMessageStartedAt(message.id);
    return started > 0 && now - started > BOT_PENDING_REPLY_WAIT_MS;
}

// running: the bot is in a turn. waiting: that turn handed the desktop to the
// person. queued: the person already sent the next task and it has not started.
export type BotTaskProgress = 'running' | 'waiting' | 'queued';

export interface InProgressBotTask {
    id: string;
    botId: string;
    text: string;
    phase?: BotPhase;
    status: BotTaskProgress;
    startedAt: number;
}

// Turns this process admitted. The bots page can unmount while Go is still
// polling, and the 31-minute clock must not hide that turn. A reload starts
// with an empty set, so a leftover pending bubble still ages out.
const liveBotTurns = new Set<string>();

function liveBotTurnKey(userId: string, botId: string, messageId: string): string {
    return `${desktopUserKey(userId)}\0${botId}\0${messageId}`;
}

export function noteLiveBotTurn(userId: string, botId: string, messageId: string) {
    const bot = botId.trim();
    const id = messageId.trim();
    if (!bot || !id) return;
    liveBotTurns.add(liveBotTurnKey(userId, bot, id));
}

export function clearLiveBotTurn(userId: string, botId: string, messageId: string) {
    liveBotTurns.delete(liveBotTurnKey(userId, botId.trim(), messageId.trim()));
}

function botTurnIsLive(userId: string, botId: string, messageId: string): boolean {
    return liveBotTurns.has(liveBotTurnKey(userId, botId, messageId));
}

// Successful transcript writes. The monitor compares this instead of copying
// the stored JSON on every poll. A failed setItem leaves storage unchanged
// and does not advance the counter.
let transcriptGeneration = 0;

export function botTranscriptGeneration(): number {
    return transcriptGeneration;
}

function noteTranscriptWrite() {
    transcriptGeneration += 1;
}

function transcriptMessages(list: unknown): DesktopBotMessage[] {
    if (!Array.isArray(list)) return [];
    const messages: DesktopBotMessage[] = [];
    for (const item of list) {
        if (!item || typeof item !== 'object') continue;
        const message = item as DesktopBotMessage;
        if (!message.id || (message.role !== 'user' && message.role !== 'assistant')) continue;
        messages.push(message);
    }
    return messages;
}

function personHasBotTurn(message: DesktopBotMessage): boolean {
    return message.userControl === true || (message.attentionReason || '').trim() !== '';
}

// inProgressBotTasks is the task monitor's Bot tab. One read of the transcript
// covers every bot. Finished replies and a pending bubble left behind by a
// reload are omitted. A handoff whose HTTP turn already settled stays, because
// the person still has the desktop.
export function inProgressBotTasks(userId: string, now = Date.now()): InProgressBotTask[] {
    const stored = readMessages()[desktopUserKey(userId)];
    if (!stored) return [];
    const tasks: InProgressBotTask[] = [];
    for (const botId of Object.keys(stored)) {
        const messages = transcriptMessages(stored[botId]);
        let handoff: InProgressBotTask | null = null;
        for (let index = 0; index < messages.length; index++) {
            const message = messages[index];
            if (message.role === 'assistant' && message.pending && !message.ack) {
                const live = botTurnIsLive(userId, botId, message.id);
                if (!live && pendingBotReplyIsStale(message, now)) {
                    handoff = null;
                    continue;
                }
                tasks.push({
                    id: message.id,
                    botId,
                    text: userRequestBefore(messages, index).trim(),
                    phase: message.phase,
                    status: personHasBotTurn(message) ? 'waiting' : 'running',
                    startedAt: botMessageStartedAt(message.id),
                });
                handoff = null;
                continue;
            }
            if (message.role === 'user' && message.queued && (message.content || '').trim()) {
                tasks.push({
                    id: message.id,
                    botId,
                    text: message.content.trim(),
                    phase: message.phase,
                    status: 'queued',
                    startedAt: botMessageStartedAt(message.id),
                });
                continue;
            }
            if (message.role !== 'assistant' || message.pending || sideNote(message)) continue;
            if (personHasBotTurn(message)) {
                handoff = {
                    id: message.id,
                    botId,
                    text: userRequestBefore(messages, index).trim(),
                    phase: message.phase,
                    status: 'waiting',
                    startedAt: botMessageStartedAt(message.id),
                };
            } else {
                handoff = null;
            }
        }
        if (handoff) tasks.push(handoff);
    }
    tasks.sort((left, right) => left.startedAt - right.startedAt || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
    return tasks;
}

const BOT_TASK_SNAPSHOT_MS = 30_000;

let botTaskSnapshot: { userId: string; generation: number; at: number; tasks: InProgressBotTask[] } | null = null;

// One parse serves the task monitor and the rail badge. A new write advances
// the generation and refreshes on the next read. An empty result stays until
// that write: nothing in it can age out. A non-empty result is recomputed
// after 30s so a pending bubble can age out without a write.
export function currentInProgressBotTasks(userId: string, now = Date.now()): InProgressBotTask[] {
    const generation = botTranscriptGeneration();
    const snapshot = botTaskSnapshot;
    if (snapshot && snapshot.userId === userId && snapshot.generation === generation) {
        const fresh = now >= snapshot.at && now - snapshot.at < BOT_TASK_SNAPSHOT_MS;
        if (fresh || snapshot.tasks.length === 0 || now < snapshot.at) return snapshot.tasks;
    }
    const tasks = inProgressBotTasks(userId, now);
    botTaskSnapshot = { userId, generation, at: now, tasks };
    return tasks;
}

export function desktopBotAccountId(config: { remote_user_id?: unknown; remote_email?: unknown } | null | undefined): string {
    return String(config?.remote_user_id || config?.remote_email || 'local');
}

// desktopTaskStep decides the next step from the request and this reply.
// A screenshot that is already attached finished a look. An arrangement that
// still names later work keeps the confirm control. A login handoff does not.
export function desktopTaskStep(userText: string, reply: Pick<DesktopBotMessage, 'content' | 'images' | 'files' | 'localPaths' | 'userControl' | 'attentionReason' | 'askUser' | 'askResolved'>): DesktopTaskStep {
    if (reply.userControl || (reply.attentionReason || '').trim()) return 'assist';
    const ask = reply.askUser;
    if (ask && !reply.askResolved && (ask.question || ask.secretName || (ask.options && ask.options.length > 0))) return 'ask';
    const content = reply.content || '';
    if ((reply.localPaths && reply.localPaths.length > 0) || (reply.files && reply.files.length > 0)) return 'done';
    const remaining = REMAINING_WORK.test(content);
    // A picture of the desktop does not finish a request to build and run a program.
    if (!remaining && FINISHED_REPORT.test(content) && !isProgramTask(userText)) return 'done';
    if (!remaining && reply.images && reply.images.length > 0 && SCREEN_REQUEST.test(userText) && !FURTHER_WORK.test(userText) && !isProgramTask(userText)) return 'done';
    if (!remaining && OPEN_QUESTION.test(content) && ASKS_HOW.test(content)) return 'ask';
    return 'arrange';
}

// foldPlanConfirmation drops the foreground reading once the worker has stated
// the same arrangement. One request then keeps one confirmation. The worker
// sentence stays, and it is not an ack, so the confirm control still attaches.
// A delivered result, a question, a handoff, or a failure keeps the reading.
// The result often has no file and no screenshot, so the step alone is not
// enough: only a sentence that still names later work is the second confirmation.
export function foldPlanConfirmation(messages: DesktopBotMessage[], settledIndex: number): DesktopBotMessage[] {
    if (settledIndex <= 0 || settledIndex >= messages.length) return messages;
    const settled = messages[settledIndex];
    if (settled.role !== 'assistant' || settled.ack || settled.pending || settled.failed || settled.phase !== 'plan') return messages;
    const content = (settled.content || '').trim();
    if (!content || !REMAINING_WORK.test(content)) return messages;
    const previous = messages[settledIndex - 1];
    if (previous.role !== 'assistant' || !previous.ack || !previous.understood) return messages;
    if (desktopTaskStep(userRequestBefore(messages, settledIndex), settled) !== 'arrange') return messages;
    return [...messages.slice(0, settledIndex - 1), ...messages.slice(settledIndex)];
}

// isProgramTask keeps the confirm control up when a reply attaches a picture
// of a program the person asked to build and run. The sentence the person
// sees, and the order the desktop worker receives, come from UnderstandDesktopBotTask.
function isProgramTask(text: string): boolean {
    if (/c\+\+/i.test(text) && /hello\s*world|程序|代码|代碼|运行|運行|编译|編譯|开发|编写|編寫/i.test(text)) return true;
    if (/hello\s*world/i.test(text) && /程序|代码|代碼|运行|運行|编译|編譯|开发|编写|編寫|写|寫/.test(text)) return true;
    return /开发|编写|編寫|编译|編譯|(?:写|寫|做)(?:个|個|一个|一個)/.test(text)
        && /程序|代码|代碼|脚本|腳本|c\+\+|python|java|hello/i.test(text);
}

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
    // The foreground reading of this turn, shown while the worker writes the
    // arrangement. A queue note and a side note are acks too, and they stay.
    // foldPlanConfirmation drops only this reading once that arrangement arrives.
    understood?: boolean;
    // A short answer given here, with no desktop worker. It is not an
    // arrangement, so the confirm control does not attach to it.
    chat?: boolean;
    // A user message accepted while that bot was still working. The dispatch
    // waits for the current run on the shared desktop; the marker is cleared
    // the moment the dispatch starts, so a page reload can recover the queue
    // without double-sending.
    queued?: boolean;
    // One cloud-desktop screenshot kept only until it is written to a file.
    // Data is standard base64. A saved shot is shotPaths, and these bytes are
    // not written to the transcript.
    images?: DesktopBotImage[];
    // Screenshots written under the maclaw data directory. The bubble reads
    // the file. The transcript keeps the path.
    shotPaths?: string[];
    // One document that could not be written on this machine. Data is standard base64.
    files?: DesktopBotFile[];
    // Documents written under the maclaw data directory. The result card opens these.
    localPaths?: string[];
}

export interface DesktopBotImage {
    mime: 'image/png' | 'image/jpeg';
    data: string;
}

export interface DesktopBotFile {
    name: string;
    mime: string;
    data: string;
}

const SHOT_MAX = 1_200_000;
const FILE_MAX = 200_000;

// desktopBotFiles keeps the latest file. The chat does not choose a type.
// A path, a hostile mime, or a payload that is not base64 never becomes a download.
export function desktopBotFiles(value: unknown): DesktopBotFile[] | undefined {
    const list = Array.isArray(value) ? value : [];
    for (let i = list.length - 1; i >= 0; i--) {
        const item = list[i];
        if (!item || typeof item !== 'object') continue;
        const mime = desktopFileMIME(String((item as { mime?: unknown }).mime || ''));
        const name = String((item as { name?: unknown }).name || '').trim();
        const data = String((item as { data?: unknown }).data || '').replace(/\s+/g, '');
        if (!mime || !desktopFileName(name) || !desktopFileData(data)) continue;
        return [{ name, mime, data }];
    }
    return undefined;
}

function desktopFileName(name: string): boolean {
    if (!name || name.length > 80 || /[\\/]/.test(name) || name.includes('..') || name.startsWith('.') || /[\u0000-\u001f\u007f]/.test(name)) return false;
    return true;
}

function desktopFileMIME(raw: string): string | undefined {
    const bare = raw.split(';')[0].trim().toLowerCase();
    if (!bare) return 'application/octet-stream';
    if (!/^[a-z0-9][a-z0-9.+_-]{0,126}\/[a-z0-9][a-z0-9.+_-]{0,126}$/.test(bare)) return undefined;
    return bare;
}

function desktopFileData(data: string): boolean {
    return !!data && data.length <= FILE_MAX && data.length % 4 === 0 && /^[A-Za-z0-9+/]+={0,2}$/.test(data);
}

// desktopBotLocalPaths keeps absolute paths for the result card. A relative
// path, a .. segment, or a broken path never becomes a file the card can open.
export function desktopBotLocalPaths(value: unknown): string[] | undefined {
    const list = Array.isArray(value) ? value : (typeof value === 'string' ? [value] : []);
    const kept: string[] = [];
    for (const item of list) {
        const path = String(item ?? '').trim();
        if (!desktopLocalPath(path) || kept.includes(path)) continue;
        kept.push(path);
        if (kept.length >= 8) break;
    }
    return kept.length ? kept : undefined;
}

function desktopLocalPath(path: string): boolean {
    if (!path || path.length > 400 || path.includes('\0') || /[\r\n]/.test(path)) return false;
    if (path.split(/[\\/]/).some(part => part === '..')) return false;
    if (/^[A-Za-z]:[\\/]/.test(path)) return true;
    if (path.startsWith('\\\\')) return true;
    return path.startsWith('/') && !path.startsWith('//');
}

// desktopBotImages keeps the latest PNG or JPEG. A bad mime or a payload
// that is not base64 never becomes an image URL.
export function desktopBotImages(value: unknown): DesktopBotImage[] | undefined {
    const list = Array.isArray(value) ? value : [];
    for (let i = list.length - 1; i >= 0; i--) {
        const item = list[i];
        if (!item || typeof item !== 'object') continue;
        const mime = String((item as { mime?: unknown }).mime || '').trim().toLowerCase();
        const data = String((item as { data?: unknown }).data || '').replace(/\s+/g, '');
        if (mime !== 'image/png' && mime !== 'image/jpeg') continue;
        if (!data || data.length > SHOT_MAX || data.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(data)) continue;
        return [{ mime, data }];
    }
    return undefined;
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

const PERSONA_KEY = 'maclaw.desktopBotPersona.v1';

type StoredPersonas = Record<string, Record<string, string>>;

function readPersonas(): StoredPersonas {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(PERSONA_KEY) || '{}') as StoredPersonas;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

function writePersonas(value: StoredPersonas) {
    storage()?.setItem(PERSONA_KEY, JSON.stringify(value));
}

// personasForUser is the identity each bot keeps for this account. The
// understand model writes it. Empty means this bot has no persona yet.
export function personasForUser(userId: string): Record<string, string> {
    const mine = readPersonas()[desktopUserKey(userId)];
    if (!mine || typeof mine !== 'object') return {};
    const out: Record<string, string> = {};
    for (const [botId, persona] of Object.entries(mine)) {
        const text = String(persona || '').trim();
        if (botId && text) out[botId] = text;
    }
    return out;
}

export function personaForBot(userId: string, botId: string): string {
    return personasForUser(userId)[botId] || '';
}

export function saveBotPersona(userId: string, botId: string, persona: string) {
    const id = botId.trim();
    if (!id) return;
    const key = desktopUserKey(userId);
    const all = readPersonas();
    const mine = { ...(all[key] && typeof all[key] === 'object' ? all[key] : {}) };
    const text = persona.trim();
    if (!text) delete mine[id];
    else mine[id] = text;
    all[key] = mine;
    writePersonas(all);
}

export function forgetBotPersona(userId: string, botId: string) {
    saveBotPersona(userId, botId, '');
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
    forgetBotPersona(userId, botId);
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
    return items.filter(item => item && item.id && (item.role === 'user' || item.role === 'assistant')).map(item => {
        const shotPaths = desktopBotLocalPaths(item.shotPaths);
        const images = shotPaths ? undefined : desktopBotImages(item.images);
        const localPaths = desktopBotLocalPaths(item.localPaths);
        const files = localPaths ? undefined : desktopBotFiles(item.files);
        const next = { ...item };
        if (shotPaths) next.shotPaths = shotPaths;
        else delete next.shotPaths;
        if (images) next.images = images;
        else delete next.images;
        if (localPaths) next.localPaths = localPaths;
        else delete next.localPaths;
        if (files) next.files = files;
        else delete next.files;
        return next;
    });
}

const MESSAGE_BUDGET = 3_500_000;

function withoutShot(message: DesktopBotMessage): DesktopBotMessage {
    const next = { ...message };
    delete next.images;
    return next;
}

// withoutImageBytes drops screenshot bytes once the file path is known.
// The caller's message is left alone, so a bubble can still paint from memory
// until the transcript is read back.
function withoutImageBytes(message: DesktopBotMessage): DesktopBotMessage {
    const shotPaths = desktopBotLocalPaths(message.shotPaths);
    if (!shotPaths) return message;
    const next = { ...message, shotPaths };
    delete next.images;
    return next;
}

// dropOldestShot removes one stored screenshot, oldest first. The newest
// screenshot on the bot being saved is kept until every other screenshot is
// gone. The budget is the whole account, so an older bot cannot crowd out
// the picture that just arrived.
function dropOldestShot(all: StoredMessages, userKey: string, botId: string): boolean {
    const bots = all[userKey];
    if (!bots) return false;
    const current = bots[botId];
    let newest = -1;
    if (Array.isArray(current)) {
        for (let i = current.length - 1; i >= 0; i--) {
            if (current[i]?.images?.length) {
                newest = i;
                break;
            }
        }
        for (let i = 0; i < newest; i++) {
            if (!current[i]?.images?.length) continue;
            current[i] = withoutShot(current[i]);
            return true;
        }
    }
    for (const id of Object.keys(bots)) {
        if (id === botId || !Array.isArray(bots[id])) continue;
        for (let i = 0; i < bots[id].length; i++) {
            if (!bots[id][i]?.images?.length) continue;
            bots[id][i] = withoutShot(bots[id][i]);
            return true;
        }
    }
    return false;
}

function dropEveryShot(bots: Record<string, DesktopBotMessage[]> | undefined) {
    if (!bots) return;
    for (const id of Object.keys(bots)) {
        const list = bots[id];
        if (!Array.isArray(list)) continue;
        for (let i = 0; i < list.length; i++) {
            if (list[i]?.images?.length) list[i] = withoutShot(list[i]);
        }
    }
}

export function saveBotMessages(userId: string, botId: string, messages: DesktopBotMessage[]) {
    const store = storage();
    if (!store) return;
    const all = readMessages();
    const key = desktopUserKey(userId);
    const kept = messages.slice(-80).map(withoutImageBytes);
    all[key] = { ...(all[key] || {}), [botId]: kept };
    let payload = JSON.stringify(all);
    while (payload.length > MESSAGE_BUDGET && dropOldestShot(all, key, botId)) {
        payload = JSON.stringify(all);
    }
    try {
        store.setItem(MESSAGE_KEY, payload);
        noteTranscriptWrite();
    } catch {
        dropEveryShot(all[key]);
        try {
            store.setItem(MESSAGE_KEY, JSON.stringify(all));
            noteTranscriptWrite();
        } catch {
            // The previous transcript stays. A screenshot that does not fit
            // is better lost than the whole conversation.
        }
    }
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
export function settlePendingBotReply(userId: string, botId: string, patch: { content: string; failed: boolean; handoffUrl?: string; userControl?: boolean; attentionReason?: string; askUser?: DesktopBotAsk; requestId?: string; images?: DesktopBotImage[]; shotPaths?: string[]; files?: DesktopBotFile[]; localPaths?: string[] }) {
    const current = messagesForBot(userId, botId);
    const index = pendingBotIndex(current, patch.requestId || '');
    if (index < 0) return;
    const next = current.slice();
    const previous = next[index];
    clearLiveBotTurn(userId, botId, previous.id);
    const reason = (patch.attentionReason || '').trim();
    const localPaths = patch.failed ? undefined : desktopBotLocalPaths(patch.localPaths);
    const shotPaths = patch.failed ? undefined : desktopBotLocalPaths(patch.shotPaths);
    next[index] = {
        ...previous,
        content: patch.content,
        pending: false,
        failed: patch.failed,
        handoffUrl: patch.handoffUrl || '',
        userControl: patch.userControl === true,
        attentionReason: reason,
        askUser: patch.askUser || previous.askUser,
        images: patch.failed || shotPaths ? undefined : desktopBotImages(patch.images),
        shotPaths,
        localPaths,
        files: patch.failed || localPaths ? undefined : desktopBotFiles(patch.files),
    };
    saveBotMessages(userId, botId, foldPlanConfirmation(next, index));
}

// appendDesktopBotReport adds one scheduled result. The same request id is
// written once, so the open page and the store can both hear the event.
// The bubble has no phase, so it does not take the confirm control.
export function appendDesktopBotReport(userId: string, botId: string, patch: { content: string; failed: boolean; handoffUrl?: string; userControl?: boolean; attentionReason?: string; askUser?: DesktopBotAsk; requestId?: string; images?: DesktopBotImage[]; shotPaths?: string[]; files?: DesktopBotFile[]; localPaths?: string[] }) {
    const requestId = (patch.requestId || '').trim();
    const current = messagesForBot(userId, botId);
    if (requestId && current.some(item => item.requestId === requestId)) return;
    const reason = (patch.attentionReason || '').trim();
    const localPaths = patch.failed ? undefined : desktopBotLocalPaths(patch.localPaths);
    const shotPaths = patch.failed ? undefined : desktopBotLocalPaths(patch.shotPaths);
    const message: DesktopBotMessage = {
        id: `m-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
        role: 'assistant',
        content: patch.content,
        failed: patch.failed,
        requestId: requestId || undefined,
        handoffUrl: patch.handoffUrl || '',
        userControl: patch.userControl === true,
        attentionReason: reason,
        askUser: patch.askUser,
        images: patch.failed || shotPaths ? undefined : desktopBotImages(patch.images),
        shotPaths,
        localPaths,
        files: patch.failed || localPaths ? undefined : desktopBotFiles(patch.files),
    };
    saveBotMessages(userId, botId, [...current, message]);
}

function clearBotMessages(userId: string, botId: string) {
    const store = storage();
    if (!store) return;
    const all = readMessages();
    const key = desktopUserKey(userId);
    if (!all[key]) return;
    delete all[key][botId];
    store.setItem(MESSAGE_KEY, JSON.stringify(all));
    noteTranscriptWrite();
}

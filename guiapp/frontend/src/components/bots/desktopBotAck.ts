import { WriteDesktopBotAck } from '../../../wailsjs/go/main/App';
import { normalizeLang } from '../../i18n/langSelect';

export type DesktopBotAckKind = 'accept' | 'supplement' | 'change' | 'parallel' | 'files' | 'stopping' | 'stopped' | 'idle' | 'pause' | 'queued' | 'stop_failed' | 'continuing' | 'status_notice' | 'error' | 'interrupted';

// The structured state behind a status line. Facts only; the composer in
// guiapp (desktop_bot_ack.go) writes the sentence from it and the chat.
export interface DesktopBotStatusPayload {
    active_run?: string;
    current_step?: string;
    queued?: number;
    takeover?: string;
    stop_reason?: string;
    last_task?: string;
}

export interface DesktopBotAckContext {
    kind: DesktopBotAckKind;
    message: string;
    lang: string;
    persona?: string;
    goal?: string;
    instruction?: string;
    unclear?: string;
    assumption?: string;
    options?: string[];
    files?: string[];
    running_task?: string;
    fallback?: boolean;
    status?: DesktopBotStatusPayload;
    recent?: string[];
    // What is left of the first reply's end-to-end budget (see
    // DESKTOP_BOT_FIRST_ACK_BUDGET_MS); the Go writer stops there too.
    budget_ms?: number;
    // late: this line upgrades a stock receipt already on screen, so the
    // backend gives it a longer budget than a first reply.
    late?: boolean;
}

export type DesktopBotAckWriter = (ctx: DesktopBotAckContext) => Promise<string>;

// The one line shown only when every model attempt failed. No other stock
// sentence exists: everything else is written by the model.
export function desktopBotNeutralAck(ctx: Pick<DesktopBotAckContext, 'kind' | 'lang' | 'files'>): string {
    const code = normalizeLang(ctx.lang);
    const files = ctx.kind !== 'accept' || (ctx.files?.length ?? 0) > 0;
    if (code === 'en') return files ? 'Got it.' : "OK, I'm on it.";
    if (code === 'mn') return files ? 'Ойлголоо.' : 'За, би хийж эхэллээ.';
    return files ? '收到。' : '好，我先做着。';
}

// DESKTOP_BOT_FIRST_ACK_BUDGET_MS is how long, from the moment a command
// arrives, the person may wait for a first reply. The reading that the work
// needs anyway usually brings the model's own understanding inside it (no
// extra call). When it does not, one neutral receipt goes out at this mark
// so the person hears back at once; a later understanding line still
// replaces that receipt in place.
export const DESKTOP_BOT_FIRST_ACK_BUDGET_MS = 4000;

// A late line that replaces the stock receipt may take this long: the person
// already heard back, and a slow model could not fit the first 4 s.
export const DESKTOP_BOT_LATE_ACK_BUDGET_MS = 45000;

// desktopBotUnderstandingLabel is the status-row phrase shown while the
// first reply is still being written past DESKTOP_BOT_FIRST_ACK_BUDGET_MS.
// No chat line stands in for it (Daniel 11-12, rule 1): a stock receipt such
// as 「收到，正在看。」 in the chat read as a template reply, so the wait is
// shown where live status belongs and the model's own line follows.
export function desktopBotUnderstandingLabel(lang: string): string {
    const code = normalizeLang(lang);
    if (code === 'en') return 'Reading your message';
    if (code === 'mn') return 'Таны мессежийг уншиж байна';
    if (code === 'zh-Hant') return '正在理解你的話';
    return '正在理解你的话';
}

// desktopBotFoldHead mirrors guiapp desktopBotFoldHead: the fixed start of a
// reply to a message sent while a job runs, saying how it was folded in.
export function desktopBotFoldHead(kind: string, lang: string): string {
    const en = normalizeLang(lang) === 'en';
    switch (kind) {
        case 'supplement': return en ? 'Added: ' : '加进去了：';
        case 'change': return en ? 'OK, changing it: ' : '好，改一下：';
        case 'parallel': return en ? "That's a separate job, starting it as its own task: " : '这是另一件事，我另开一个任务：';
        case 'queued': return en ? "That's a separate job, next after this one: " : '这是另一件事，等手上这件做完就开始：';
        default: return '';
    }
}

// desktopBotFoldFallback is said only when no model wrote the fold line in
// time. It is not a stock sentence: it names the decision and repeats what
// was folded in, so the person still sees exactly what happened to their
// words. A model line arriving later replaces it in place.
export function desktopBotFoldFallback(kind: string, lang: string, words: string): string {
    const head = desktopBotFoldHead(kind, lang) || desktopBotFoldHead('supplement', lang);
    const body = String(words || '').replace(/^【改变做法】/, '').replace(/\s+/g, ' ').trim();
    const clipped = Array.from(body).length > 40 ? Array.from(body).slice(0, 40).join('') + '…' : body;
    return (head + clipped).trim();
}

// The receipt said when the reading is still running at the budget. It
// claims nothing about the task: the reading may yet turn it into a question
// or an answer, which then takes this line's place.
export function desktopBotReceiptAck(lang: string): string {
    const code = normalizeLang(lang);
    if (code === 'en') return 'Got it, reading it now.';
    if (code === 'mn') return 'Ойлголоо, одоо уншиж байна.';
    if (code === 'zh-Hant') return '收到，正在看。';
    return '收到，正在看。';
}

function plainAckLine(text: string): string {
    return String(text || '').replace(/\s+/g, '').replace(/[。.!！?？]+$/, '');
}

// desktopBotIsPlaceholderAck is true for the stock receipt or the one
// neutral line used when every model attempt failed. An understanding
// sentence from the model must never be replaced by one of these.
export function desktopBotIsPlaceholderAck(line: string, lang?: string): boolean {
    const got = plainAckLine(line);
    if (!got) return false;
    const langs = lang ? [lang] : ['zh-Hans', 'zh-Hant', 'en', 'mn'];
    for (const item of langs) {
        if (plainAckLine(desktopBotReceiptAck(item)) === got) return true;
        if (plainAckLine(desktopBotNeutralAck({ kind: 'accept', lang: item })) === got) return true;
        if (plainAckLine(desktopBotNeutralAck({ kind: 'accept', lang: item, files: ['x'] })) === got) return true;
        if (plainAckLine(desktopBotNeutralAck({ kind: 'supplement', lang: item })) === got) return true;
    }
    return false;
}

// desktopBotAck never rejects. The work has already started; this only
// decides which sentence follows it. budgetMs, when given, is the most it
// may wait: past it the neutral line is returned and a late answer dropped.
export async function desktopBotAck(ctx: DesktopBotAckContext, write: DesktopBotAckWriter = WriteDesktopBotAck, budgetMs?: number): Promise<string> {
    if (budgetMs !== undefined && budgetMs <= 0) return desktopBotNeutralAck(ctx);
    const attempt = (async () => {
        try {
            const line = String(await write(budgetMs !== undefined ? { ...ctx, budget_ms: Math.ceil(budgetMs) } : ctx) || '').trim();
            if (line) return line;
        } catch {
            // falls through to the neutral line
        }
        return '';
    })();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const line = budgetMs === undefined ? await attempt : await Promise.race([
        attempt,
        new Promise<string>(resolve => { timer = setTimeout(() => resolve(''), budgetMs); }),
    ]);
    if (timer !== undefined) clearTimeout(timer);
    return line || desktopBotNeutralAck(ctx);
}

// The minimal line used only when every model attempt for a still-stopping
// update failed. It is said only after the person asked for the stop.
export function desktopBotStoppingFallback(lang: string): string {
    const code = normalizeLang(lang);
    if (code === 'en') return 'Still stopping.';
    if (code === 'mn') return 'Одоо ч зогсоож байна.';
    if (code === 'zh-Hant') return '還在停。';
    return '还在停。';
}

// desktopBotStoppingLine writes the "still stopping" update in the bot's own
// voice. It never rejects.
export async function desktopBotStoppingLine(ctx: { lang: string; message?: string; persona?: string; running_task?: string; recent?: string[] }, write: DesktopBotAckWriter = WriteDesktopBotAck): Promise<string> {
    try {
        const line = String(await write({ kind: 'stopping', message: ctx.message || '', lang: ctx.lang, persona: ctx.persona, running_task: ctx.running_task, recent: ctx.recent }) || '').trim();
        if (line) return line;
    } catch {
        // falls through to the minimal line
    }
    return desktopBotVariedFallback('stopping', ctx.lang, desktopBotStoppingFallback(ctx.lang), ctx.recent);
}

// desktopBotLine writes a stop-path line (stopped, idle, pause) in the bot's
// voice; fallback is used only when every model attempt failed.
export async function desktopBotLine(kind: Exclude<DesktopBotAckKind, 'accept' | 'supplement' | 'files' | 'stopping'>, ctx: { lang: string; message?: string; persona?: string; running_task?: string; instruction?: string; status?: DesktopBotStatusPayload; recent?: string[] }, fallback: string, write: DesktopBotAckWriter = WriteDesktopBotAck): Promise<string> {
    try {
        const line = String(await write({ kind, message: ctx.message || '', lang: ctx.lang, persona: ctx.persona, running_task: ctx.running_task, instruction: ctx.instruction, status: ctx.status, recent: ctx.recent }) || '').trim();
        if (line) return line;
    } catch {
        // falls through
    }
    return desktopBotVariedFallback(kind, ctx.lang, fallback, ctx.recent);
}

// Plain alternates per state, used only when the model wrote nothing. The
// state-specific fallback comes first; another is picked when that one was
// just said, so a failing model never makes the bot repeat itself.
type FallbackVoice = 'hans' | 'hant' | 'en' | 'mn';

const FALLBACK_VARIANTS: Record<string, Record<FallbackVoice, string[]>> = {
    queued: {
        hans: ['手上这件做完，马上接这个。', '好，这件弄完就做，做完告诉你。', '收到，等这件收尾就开始。'],
        hant: ['手上這件做完，馬上接這個。', '記下了，排在現在這件後面。', '收到，等這件收尾就開始。'],
        en: ['Noted. That comes right after the current job.', "Got it, it's next in line.", "I'll start that as soon as this one wraps up."],
        mn: ['Одоогийн ажил дуусмагц үүнийг авна.', 'Тэмдэглэв. Одоогийн ажлын ард байна.', 'Ойлголоо. Энэ ажил дуусмагц эхэлнэ.'],
    },
    continuing: {
        hans: ['还在做，没停。', '在接着推进。', '没断，继续着呢。'],
        hant: ['還在做，沒停。', '在接著推進。', '沒斷，繼續著呢。'],
        en: ['Still going.', 'Carrying on with it.', "Haven't stopped, still on it."],
        mn: ['Хийсээр байна, зогсоогүй.', 'Үргэлжлүүлэн хийж байна.', 'Тасараагүй, үргэлжилсээр байна.'],
    },
    idle: {
        hans: ['眼下没有在跑的事。', '这会儿是空着的，没什么要停。'],
        hant: ['眼下沒有在跑的事。', '這會兒是空著的，沒什麼要停。'],
        en: ["There's nothing running right now.", "I'm idle at the moment, nothing to stop."],
        mn: ['Одоогоор хийж байгаа ажил алга.', 'Одоогоор сул байна, зогсоох зүйл алга.'],
    },
    stopped: {
        hans: ['好，已经停下了。', '停住了。'],
        hant: ['好，已經停下了。', '停住了。'],
        en: ['Okay, it has stopped.', 'Stopped.'],
        mn: ['За, зогссон.', 'Зогссон.'],
    },
    error: {
        hans: ['这一步没成，稍后我再试，或者你换个说法。', '没办成，我这边出了点状况，等一下再来。'],
        hant: ['這一步沒成，稍後我再試，或者你換個說法。', '沒辦成，我這邊出了點狀況，等一下再來。'],
        en: ["That didn't go through. I'll try again shortly, or you can rephrase.", "Something went wrong on my side. Give me a moment and try again."],
        mn: ['Энэ алхам бүтсэнгүй. Удахгүй дахин оролдъё, эсвэл өөрөөр хэлнэ үү.', 'Болсонгүй. Миний талд асуудал гарлаа. Түр хүлээгээд дахин ирнэ үү.'],
    },
    interrupted: {
        hans: ['刚才那件中途断了，要我接着做吗？', '那件没做完就停了，需要的话我重新接上。'],
        hant: ['剛才那件中途斷了，要我接著做嗎？', '那件沒做完就停了，需要的話我重新接上。'],
        en: ['That job got cut off partway. Want me to pick it up again?', 'It stopped before finishing. I can resume it if you like.'],
        mn: ['Саяны ажил дундуур тасарлаа. Үргэлжлүүлэх үү?', 'Дуусаагүй зогссон. Хүсвэл дахин залгана.'],
    },
};
FALLBACK_VARIANTS.status_notice = FALLBACK_VARIANTS.continuing;
FALLBACK_VARIANTS.stopping = {
    hans: ['在收尾了，马上停。', '正在停，稍等一下。'],
    hant: ['在收尾了，馬上停。', '正在停，稍等一下。'],
    en: ['Wrapping up, stopping in a moment.', 'Stopping, one moment.'],
    mn: ['Дуусгаж байна, удахгүй зогсоно.', 'Зогсоож байна, түр хүлээнэ үү.'],
};

function fallbackVoice(lang: string): FallbackVoice {
    const code = normalizeLang(lang);
    if (code === 'mn') return 'mn';
    if (code === 'zh-Hant') return 'hant';
    if (code === 'en') return 'en';
    return 'hans';
}

function plainLine(text: string): string {
    return String(text || '').replace(/^(你|我|you|me)\s*[:：]\s*/i, '').replace(/\s+/g, '').replace(/[。.!！?？]+$/, '');
}

export function desktopBotVariedFallback(kind: string, lang: string, fallback: string, recent: string[] = []): string {
    const said = new Set(recent.map(plainLine));
    const pool = [fallback, ...((FALLBACK_VARIANTS[kind] || { hans: [], hant: [], en: [], mn: [] })[fallbackVoice(lang)])].filter(Boolean);
    return pool.find(line => !said.has(plainLine(line))) || fallback;
}


// One first reply per command. A stock line is posted only when no reply is
// up yet, and a later model line replaces that same bubble. A stock line
// never lands beside a line the model already wrote for that command.
export interface FirstAckMessage {
    id: string;
    role: string;
    content: string;
    ack?: boolean;
    understood?: boolean;
    pending?: boolean;
    requestId?: string;
    ackFor?: string;
    options?: string[];
}

// bindAckCommand joins every id that names one command (the pending bubble,
// the local ticket, the server request id) into one set. A later call that
// shares any id returns the whole set, so the intake event and the local
// timer land on the same ack even when they learn the ids in either order.
// The map is keyed by bot as well as id: two bots started together do not
// share a command.
// One command's ack keys stay until that command's pending bubble is gone.
// The map is also capped so a long-lived window cannot grow it without bound.
export const FIRST_ACK_REGISTRY_MAX = 256;

export function releaseSettledAckCommands(
    registry: Map<string, Set<string>>,
    botId: string,
    messages: Array<{ id?: string; role?: string; pending?: boolean; requestId?: string }>,
) {
    const owner = String(botId || '').trim();
    if (!owner) return;
    const live = new Set<string>();
    for (const item of messages) {
        if (item.role === 'assistant' && item.pending) {
            if (item.id) live.add(item.id);
            if (item.requestId) live.add(item.requestId);
        }
    }
    const seen = new Set<Set<string>>();
    for (const [mapKey, set] of registry) {
        if (!mapKey.startsWith(`${owner}\0`)) continue;
        if (seen.has(set)) continue;
        seen.add(set);
        let pending = false;
        for (const key of set) {
            if (live.has(key)) pending = true;
        }
        if (pending) continue;
        for (const key of set) registry.delete(`${owner}\0${key}`);
    }
    while (registry.size > FIRST_ACK_REGISTRY_MAX) {
        const oldest = registry.keys().next().value;
        if (oldest === undefined) break;
        const set = registry.get(oldest);
        registry.delete(oldest);
        if (!set) continue;
        for (const key of set) {
            const prefix = oldest.slice(0, oldest.indexOf('\0'));
            registry.delete(`${prefix}\0${key}`);
        }
    }
}

export function bindAckCommand(registry: Map<string, Set<string>>, botId: string, keys: Array<string | undefined | null>): string[] {
    const fresh = uniqueKeys(keys);
    const owner = String(botId || '').trim();
    if (!owner || fresh.length === 0) return fresh;
    let set: Set<string> | undefined;
    for (const key of fresh) {
        const found = registry.get(`${owner}\0${key}`);
        if (found) {
            set = found;
            break;
        }
    }
    if (!set) set = new Set();
    for (const key of fresh) {
        set.add(key);
        registry.set(`${owner}\0${key}`, set);
    }
    return [...set];
}

function uniqueKeys(keys: Array<string | undefined | null>): string[] {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const key of keys) {
        const text = String(key || '').trim();
        if (!text || seen.has(text)) continue;
        seen.add(text);
        out.push(text);
    }
    return out;
}

// firstAckCommandKeys names the command an intake ack belongs to. The request
// id is the stable name. The pending bubble's own id is included when that
// request id is not on the transcript yet and this bot has one open command,
// so the ack is inserted on that command rather than appended after it.
export function firstAckCommandKeys(
    messages: Array<{ id: string; role: string; pending?: boolean; requestId?: string; ack?: boolean }>,
    requestId: string,
): string[] {
    const id = String(requestId || '').trim();
    const keys: string[] = [];
    if (id) keys.push(id);
    const anchor = id ? messages.find(item => item.id === id || item.requestId === id) : undefined;
    if (anchor) {
        keys.push(anchor.id);
        if (anchor.requestId) keys.push(anchor.requestId);
        return uniqueKeys(keys);
    }
    const open = messages.filter((item, index) => {
        if (item.role !== 'assistant' || !item.pending) return false;
        const before = index > 0 ? messages[index - 1] : undefined;
        return !(before && before.ack);
    });
    if (open.length === 1) {
        keys.push(open[0].id);
        if (open[0].requestId) keys.push(open[0].requestId);
    }
    return uniqueKeys(keys);
}

// landFirstAck inserts or replaces the single ack bubble for a command.
// The returned array is the input when nothing changes.
export function landFirstAck<T extends FirstAckMessage>(
    messages: T[],
    commandKeys: Array<string | undefined | null>,
    line: string,
    lang: string,
    newId: () => string,
    options?: string[],
): T[] {
    const keys = uniqueKeys(commandKeys);
    const text = String(line || '').trim();
    if (!text || keys.length === 0) return messages;
    const keySet = new Set(keys);
    const incomingPlaceholder = desktopBotIsPlaceholderAck(text, lang);
    const anchorAt = messages.findIndex(item => keySet.has(item.id) || (!!item.requestId && keySet.has(item.requestId)));
    // A queue note is also ack:true. It is not this command's first reply,
    // so it must not swallow the model line. A placeholder counts only when
    // it sits on this command.
    const onThisCommand = (item: T | undefined) => {
        if (!item || !item.ack) return false;
        if (item.ackFor && keySet.has(item.ackFor)) return true;
        if (desktopBotIsPlaceholderAck(String(item.content || ''), lang)) return true;
        return item.understood === true && !item.ackFor;
    };
    let ackAt = messages.findIndex(item => !!item.ack && !!item.ackFor && keySet.has(item.ackFor));
    if (ackAt < 0 && anchorAt > 0 && onThisCommand(messages[anchorAt - 1])) ackAt = anchorAt - 1;
    if (ackAt < 0 && anchorAt >= 0 && anchorAt + 1 < messages.length && onThisCommand(messages[anchorAt + 1])) ackAt = anchorAt + 1;
    const ackFor = keys[0];
    if (ackAt >= 0) {
        const current = messages[ackAt];
        const currentPlaceholder = desktopBotIsPlaceholderAck(String(current.content || ''), lang);
        if (!currentPlaceholder || incomingPlaceholder) return messages;
        const next = messages.slice();
        next[ackAt] = {
            ...current,
            content: text,
            ack: true,
            understood: true,
            ackFor: current.ackFor || ackFor,
            ...(options && options.length ? { options } : {}),
        };
        return next;
    }
    const ack = {
        id: newId(),
        role: 'assistant',
        content: text,
        ack: true,
        understood: true,
        ackFor,
        ...(options && options.length ? { options } : {}),
    } as T;
    if (anchorAt >= 0) return [...messages.slice(0, anchorAt), ack, ...messages.slice(anchorAt)];
    return [...messages, ack];
}

// desktopBotWithFoldHead puts the fold head in front of a line the model
// already wrote (for example the reading's sentence), unless it starts so.
export function desktopBotWithFoldHead(kind: string, lang: string, line: string): string {
    const head = desktopBotFoldHead(kind, lang);
    const text = String(line || '').trim();
    if (!head || !text) return text;
    const bare = head.replace(/[\s：:]+$/, '');
    return text.startsWith(bare) ? text : head + text;
}

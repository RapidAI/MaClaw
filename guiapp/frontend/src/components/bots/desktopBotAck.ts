import { WriteDesktopBotAck } from '../../../wailsjs/go/main/App';
import { normalizeLang } from '../../i18n/langSelect';

export type DesktopBotAckKind = 'accept' | 'supplement' | 'files' | 'stopping' | 'stopped' | 'idle' | 'pause' | 'queued' | 'stop_failed' | 'continuing' | 'status_notice' | 'error' | 'interrupted';

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

// desktopBotAck never rejects. The work has already started; this only
// decides which sentence follows it.
export async function desktopBotAck(ctx: DesktopBotAckContext, write: DesktopBotAckWriter = WriteDesktopBotAck): Promise<string> {
    try {
        const line = String(await write(ctx) || '').trim();
        if (line) return line;
    } catch {
        // falls through to the neutral line
    }
    return desktopBotNeutralAck(ctx);
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


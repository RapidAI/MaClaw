import type { BotFeedMeta } from './botFeedLine';
import { visibleActivityPhrase } from './visibleDesktopTurn';

export interface DesktopBot {
    id: string;
    title: string;
    description: string;
    createdAt: number;
    // Copied from the hub list. This client does not recompute it from CreatedAt.
    primary?: boolean;
}

export type BotPhase = 'plan' | 'execute';

// Why a turn has no reading. Not good or bad, just what to tell the person.
// unrouted differs from the other three: the reading succeeded and the message
// was understood, but no running task took it.
//
// It lives here rather than beside the reader because the parked turn on the
// bubble has to be able to hold all four; declaring the union twice let the
// message type fall behind the reader and fail to compile.
export type DesktopBotNoReading = 'unread' | 'unscheduled' | 'unreachable' | 'unrouted';

// What the latest plan reply still needs from the person.
// done: the request is already fulfilled, so the reply is the result.
// arrange: work remains, and the confirm control is the approval.
// ask: one missing fact, asked in the reply itself.
// assist: a person has to finish a login, captcha, or consent step.
export type DesktopTaskStep = 'done' | 'arrange' | 'ask' | 'assist';

// An explicit "still to confirm" mark. 「下一步」「我会」are not that mark:
// planning prose does not open a confirm button by default.
const REMAINING_WORK = /确认后|按这个安排|请确认|安排[:：]/;
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

// The chat controls. The bot workspace sends these, and UnderstandDesktopBotTask
// treats them as protocol. The monitor must not present them as the task.
export const BOT_CONFIRM_TASK = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';

// The arrangement often still says the work waits, or that this round will not
// do it. This lead says the approval already happened. A side mention of some
// other waiting plan is not part of this job.
const CONFIRMED_ORDER_LEAD = '现在就执行下面这项安排。同意已经给出：文中的「下一轮」「确认后」「等你点头」以及「不实际」「先不要」都是确认前的说法，现在按其中的动作做完，不要再陈述，也不要再征求确认。顺带提到的其他还在等待确认的事不要做。';

// A clear repeat is armed without this button. The button only sees a time
// when a large change was also timed, and that approval must set the timer
// rather than do the change on this turn. This is a gate, not the reading.
const LATER_TIME = /每(?:分钟|分鐘|小时|小時|天|周|週|月|年|个工作日|個工作日|隔)|定时|定時|凌晨|午夜|点半|點半|\d{1,2}\s*[:：]\s*\d{2}|\d{1,2}\s*[点點]|every\s+(?:day|hour|minute|week)|daily|weekly|hourly/i;

export function namesLaterTime(text: string): boolean {
    return LATER_TIME.test(text || '');
}

// confirmedWorkOrder is the arrangement itself. The old worker lead is not
// part of the desktop send: the button hands the stored work order over.
export function confirmedWorkOrder(arrangement: string): string {
    return stripConfirmedOrderLead(arrangement);
}

export function stripConfirmedOrderLead(text: string): string {
    const trimmed = text.trim();
    if (!trimmed.startsWith(CONFIRMED_ORDER_LEAD)) return trimmed;
    return trimmed.slice(CONFIRMED_ORDER_LEAD.length).trim();
}
export const BOT_RETURN_TASK = '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。';
export const BOT_SAVED_TASK = /^已在本机保存 ([A-Z][A-Z0-9_]{0,63})。$/;
const BOT_FILLED_TASK = /^[A-Z][A-Z0-9_]{0,63} 已在本地填入当前密码框。/;

export function protocolControl(text: string): 'confirm' | 'resume' | '' {
    const trimmed = text.trim();
    if (trimmed === BOT_CONFIRM_TASK) return 'confirm';
    if (trimmed === BOT_RETURN_TASK || BOT_SAVED_TASK.test(trimmed) || BOT_FILLED_TASK.test(trimmed)) return 'resume';
    return '';
}

// The instant path. A short line made only of stop words cancels before any
// model call. Other languages and phrasings are classified while work is
// active; this list is not the only way a stop is recognized. A phrase that
// names a task, including 「别装了」 and 「取消任务」, still goes to the reading.
export type DesktopBotLocalControl = 'status' | 'stop' | '';

const DESKTOP_BOT_STOP_PARTS = [
    'stopeverything', 'canceleverything', 'nevermind', 'forgetit', 'stopall', 'stopit', 'cancelall',
    '全部停止', '全部停下', '停止所有', '取消所有', '别继续了', '不要继续了', '都停下', '都取消',
    '别做了', '不要了', '不干了', '不做了', '别干了', '别跑了', '不用了', '停一下', '停止吧', '取消吧',
    '先停', '算了', '停下', '停止', '停掉', '都停', '全停', '取消', 'abort', 'stop', 'cancel', '停',
];

// Same courtesy words Hub strips in desktopTaskHint. A leftover piece is a
// name, so the question is not answered from the generic live-run sentence.
const DESKTOP_BOT_QUESTION_PHRASES = [
    '查一下', '看一下', '怎么样', '怎样了', '如何了', '什么情况', '什么进度',
    '完成了吗', '好了吗', '好了没', '做完了吗', '状态如何', '进度如何',
    'what about', "how's", 'how is', 'status of', 'progress of', 'it going',
];
const DESKTOP_BOT_EDGE_PREFIXES = ['那个', '这个'];
const DESKTOP_BOT_EDGE_SUFFIXES = ['状态', '进度', 'status', 'progress'];
const DESKTOP_BOT_PARTICLES = new Set(['了', '的', '吗', '嗎', '呢', '啊', '呀', '吧']);
const DESKTOP_BOT_GENERIC = [
    '任务', '结果', '情况', '状态', '进度', '那个', '这个', '现在', '查询', '看看', '一下',
    '后台', '在运行', '在跑', '跑着', '运行',
    '後台', '在運行', '跑著', '運行',
    'task', 'about', 'status', 'progress', 'the', 'a', 'an', 'it', 'is', 'my', 'this', 'that', 'going',
    'of', 'and', 'or', 'to', 'for', 'in', 'on', 'at', 'by',
];

function desktopBotUtf8Size(value: string): number {
    let size = 0;
    for (const ch of value) {
        const code = ch.codePointAt(0) || 0;
        if (code <= 0x7f) size += 1;
        else if (code <= 0x7ff) size += 2;
        else if (code <= 0xffff) size += 3;
        else size += 4;
    }
    return size;
}

const DESKTOP_BOT_GENERIC_ORDER = [...DESKTOP_BOT_GENERIC].sort((a, b) => desktopBotUtf8Size(b) - desktopBotUtf8Size(a));

function desktopBotTrimParticles(field: string): string {
    const runes = Array.from(field);
    while (runes.length > 0 && DESKTOP_BOT_PARTICLES.has(runes[runes.length - 1])) runes.pop();
    return runes.join('');
}

function desktopBotTrimPrefix(field: string): string {
    for (const word of DESKTOP_BOT_EDGE_PREFIXES) {
        if (!word || Array.from(field).length <= Array.from(word).length) continue;
        if (field.startsWith(word)) return field.slice(word.length).trim();
    }
    return field;
}

function desktopBotTrimStatusSuffix(field: string): string {
    for (const word of DESKTOP_BOT_EDGE_SUFFIXES) {
        if (!field.endsWith(word) || field.length <= word.length) continue;
        const head = Array.from(field.slice(0, field.length - word.length));
        if (head.length === 0 || !DESKTOP_BOT_PARTICLES.has(head[head.length - 1])) continue;
        return desktopBotTrimParticles(head.join(''));
    }
    return field;
}

function desktopBotDropLinker(field: string): string {
    if (!field.includes('的')) return field;
    const parts = field.split('的');
    if (parts.some(part => Array.from(part).length < 2)) return field;
    return parts.join('');
}

function desktopBotTrimEdges(field: string): string {
    let current = field;
    for (;;) {
        let next = desktopBotTrimPrefix(current);
        next = desktopBotTrimStatusSuffix(next);
        next = desktopBotDropLinker(next);
        next = desktopBotTrimParticles(next);
        if (next === current) return current;
        current = next;
    }
}

function desktopBotGenericField(field: string): boolean {
    if (!field) return false;
    if (DESKTOP_BOT_GENERIC.includes(field)) return true;
    let rest = field;
    while (rest) {
        const word = DESKTOP_BOT_GENERIC_ORDER.find(item => item && rest.startsWith(item));
        if (!word) return false;
        rest = rest.slice(word.length);
    }
    return true;
}

// desktopBotTaskHint is the name left after the "how is it going" words are
// gone. Empty means the person did not name a task.
export function desktopBotTaskHint(hint: string): string {
    let text = hint.trim().toLowerCase();
    if (!text) return '';
    for (const phrase of DESKTOP_BOT_QUESTION_PHRASES) text = text.split(phrase).join(' ');
    const cleaned = Array.from(text).map(ch => (
        '，。？！、,.?!:：;；"\'“”（）()'.includes(ch) ? ' ' : ch
    )).join('');
    const kept: string[] = [];
    for (const field of cleaned.split(/\s+/)) {
        const trimmed = desktopBotTrimEdges(field);
        if (!trimmed || desktopBotGenericField(trimmed)) continue;
        kept.push(trimmed);
    }
    return kept.join(' ');
}

function desktopBotAsksAboutRunning(text: string): boolean {
    const trimmed = text.trim();
    if (!trimmed) return false;
    const last = Array.from(trimmed).pop() || '';
    if (last === '吗' || last === '嗎' || last === '呢' || last === '？' || last === '?') return true;
    return ['怎么样', '怎麼樣', '进度', '進度', '状态', '狀態', '在运行', '在運行', '在跑', '做完了'].some(word => trimmed.includes(word));
}

function desktopBotStopCompact(text: string): string {
    return text.trim().toLowerCase().replace(/[\s.。!！?？,，、~～…:：;；'"“”]+/g, '');
}

// A short message made only of stop words. 「停下，不干了。」 is one stop.
// 「取消任务」 keeps the task name, so it is not.
export function desktopBotStopIntent(text: string): boolean {
    const compact = desktopBotStopCompact(text);
    if (!compact || Array.from(compact).length > 16) return false;
    let rest = compact;
    let used = false;
    while (rest) {
        const part = DESKTOP_BOT_STOP_PARTS.find(item => item && rest.startsWith(item));
        if (!part) return false;
        rest = rest.slice(part.length);
        used = true;
    }
    return used;
}

export function desktopBotLocalControl(text: string): DesktopBotLocalControl {
    const trimmed = text.trim();
    if (!trimmed) return '';
    if (desktopBotStopIntent(trimmed)) return 'stop';
    if (!desktopBotTaskHint(trimmed) && desktopBotAsksAboutRunning(trimmed)) return 'status';
    return '';
}

function desktopBotVoice(lang: string): 'en' | 'hant' | 'hans' {
    const value = String(lang || '').trim();
    if (value.toLowerCase().startsWith('en')) return 'en';
    if (value.includes('Hant')) return 'hant';
    return 'hans';
}

// The same sentences Hub speaks. A spoken status is never the working
// row, and never the four characters 「正在处理」. The row says
// 「正在一点点推进」 only when this turn has not reported a real step.
export function desktopBotRunningSentence(titles: string[], lang: string): string {
    const names = titles.map(item => item.trim()).filter(Boolean);
    const voice = desktopBotVoice(lang);
    if (names.length === 1) {
        if (voice === 'en') return `Working on "${names[0]}".`;
        if (voice === 'hant') return `在做。這件是「${names[0]}」。`;
        return `在做。这件是「${names[0]}」。`;
    }
    const head = voice === 'en' ? 'Working on these:' : voice === 'hant' ? '在做這幾件：' : '在做这几件：';
    return [head, ...names].join('\n');
}

// A new job that arrived while this bot's desktop is already in use.
// The older read-ahead lines stay in queuedAckReply.
export function desktopBotQueuedSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return "I'll do this once the one in progress is finished.";
    if (voice === 'hant') return '好，這個等手上的做完我就做。';
    return '好，这个等手上的做完我就做。';
}

export function desktopBotAdmittingSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return 'Just accepted. Still handing it to the background.';
    if (voice === 'hant') return '剛接下，還在交給後台。';
    return '刚接下，还在交给后台。';
}

export function desktopBotWaitingSentence(reason: string, lang: string): string {
    const voice = desktopBotVoice(lang);
    const lead = voice === 'en' ? 'Waiting on you.' : voice === 'hant' ? '停在你這兒。' : '停在你这儿。';
    const extra = reason.trim();
    if (!extra || extra === lead || extra.startsWith(lead)) return extra || lead;
    return `${lead}${extra}`;
}

export function desktopBotStatusUnreachableSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return 'I could not reach the background. Say it again.';
    if (voice === 'hant') return '剛才沒問到後台，你再說一次。';
    return '刚才没问到后台，你再说一次。';
}

export function desktopBotStillHandingReason(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return 'still handing it to the background';
    if (voice === 'hant') return '還在交給後台';
    return '还在交给后台';
}

// Typing the old approval sentence is not an approval. The button under the
// arrangement is the only way that sentence used to start work.
export function desktopBotTypedConfirmSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return 'Use the button under the arrangement. Typing this sentence does not start it.';
    if (voice === 'hant') return '點安排下面那個按鈕就行，打這句不會開始。';
    return '点安排下面那个按钮就行，打这句不会开始。';
}

export function desktopBotStoppedSentence(titles: string[], hadNext: boolean, lang: string): string {
    const voice = desktopBotVoice(lang);
    if (hadNext) {
        if (voice === 'en') return 'Stopped. One more is written down, and I can do it when you ask.';
        if (voice === 'hant') return '停了。後面還有一件記下的，你要的話我再做。';
        return '停了。后面还有一件记下的，你要的话我再做。';
    }
    const names = titles.map(item => item.trim()).filter(Boolean);
    if (voice === 'en') {
        if (names.length > 1) return `Stopped.\n${names.map(name => `${name} will not continue.`).join('\n')}`;
        return names[0] ? `Stopped. ${names[0]} will not continue.` : 'Stopped.';
    }
    if (voice === 'hant') {
        if (names.length > 1) return `停了。\n${names.map(name => `${name} 不再繼續。`).join('\n')}`;
        return names[0] ? `停了。${names[0]} 不再繼續。` : '停了。';
    }
    if (names.length > 1) return `停了。\n${names.map(name => `${name} 不再继续。`).join('\n')}`;
    return names[0] ? `停了。${names[0]} 不再继续。` : '停了。';
}

export function desktopBotStopIdleSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return 'Nothing is running, so there is nothing to stop.';
    if (voice === 'hant') return '現在沒有在做的事，不用停。';
    return '现在没有在做的事，不用停。';
}

export function desktopBotPauseAskSentence(lang: string): string {
    const voice = desktopBotVoice(lang);
    if (voice === 'en') return "I've paused it. Should I stop the whole thing, or only change what's in it?";
    if (voice === 'hant') return '我先停一下。是整個停下，還是只改正在做的內容？';
    return '我先停一下。是整个停下，还是只改正在做的内容？';
}

// A reply that claims the desktop work already stopped. Content edits such as
// "stop using bold" are not that claim. STOP_WORK is the reading's cancel
// token and is handled by the caller, not here.
export function desktopReplyClaimsStopped(text: string): boolean {
    const raw = (text || '').trim();
    if (!raw || raw === 'STOP_WORK' || Array.from(raw).length > 180) return false;
    const lower = raw.toLowerCase();
    if (['stop using', "don't use", 'do not use', 'stop the music', 'the music', 'bold', '别用', '不要用', '粗体', '音乐', '别停', '不要停', "don't stop"].some(skip => lower.includes(skip))) {
        return false;
    }
    return ['已停下', '已经停', '不再继续', '停了', "i've stopped", 'i have stopped', 'i stopped', 'work is stopped', 'work has stopped', 'cancelled the', 'canceled the'].some(hit => lower.includes(hit));
}

export function desktopFalseStopClaim(classified: string, told: string): boolean {
    if (!classified || classified === 'skip') return false;
    if ((told || '').trim() === 'STOP_WORK') return classified !== 'continue_other';
    return desktopReplyClaimsStopped(told);
}

export function desktopBotStopFailedSentence(reason: string, lang: string): string {
    const clean = reason.trim().replace(/[。．.]+$/u, '');
    const voice = desktopBotVoice(lang);
    if (!clean) {
        if (voice === 'en') return 'It did not stop.';
        if (voice === 'hant') return '沒停下來。';
        return '没停下来。';
    }
    if (voice === 'en') return `It did not stop: ${clean}.`;
    if (voice === 'hant') return `沒停下來：${clean}。`;
    return `没停下来：${clean}。`;
}

// A chat, a phaseless failure, or a phaseless question is answered beside the
// task. end is the turn being described, so a later turn cannot reclassify it.
function asideUser(messages: DesktopBotMessage[], userIndex: number, end: number): boolean {
    let sawAside = false;
    const stop = Math.min(end, messages.length);
    for (let j = userIndex + 1; j < stop; j++) {
        const next = messages[j];
        if (next.role === 'user') return sawAside;
        if (next.pending || next.ack || next.understood) return false;
        if (next.role === 'assistant' && !sideNote(next)) return false;
        if (next.chat || next.failed || next.askUser) sawAside = true;
    }
    return sawAside;
}

function taskAnchor(text: string): string {
    const content = text.trim();
    if (!content || protocolControl(content)) return '';
    return content;
}

// botTaskText is the task column. Confirm approves the plan reply that sits
// in front of it. A finished report, an execute result, and an older plan are
// not that approval. A login return or a saved secret resumes the request.
// Several requests with no later plan stay together.
export function botTaskText(messages: DesktopBotMessage[], index: number): string {
    const current = messages[index];
    let confirmed = false;
    if (current?.role === 'user') {
        const content = taskAnchor(current.content || '');
        if (content) return content;
        confirmed = protocolControl(current.content || '') === 'confirm';
    }
    const lines: string[] = [];
    let passedReply = false;
    for (let i = index - 1; i >= 0; i--) {
        const item = messages[i];
        if (item.role === 'assistant') {
            if (item.pending || item.failed || sideNote(item) || personHasBotTurn(item)) continue;
            if (item.askUser && !item.askResolved) continue;
            const arrangement = (item.content || '').trim();
            if (!arrangement || protocolControl(arrangement)) continue;
            if (lines.length > 0) break;
            if (!passedReply && confirmed && item.phase === 'plan' && !FINISHED_REPORT.test(arrangement)) return arrangement;
            passedReply = true;
            continue;
        }
        if (item.role !== 'user' || item.queued) continue;
        const kind = protocolControl(item.content || '');
        if (kind === 'confirm') {
            confirmed = true;
            continue;
        }
        if (kind === 'resume') continue;
        const content = (item.content || '').trim();
        if (!content || asideUser(messages, i, index)) continue;
        if (!confirmed) return content;
        lines.push(content);
    }
    return lines.reverse().join('；');
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

// orphanedPendingReply is a pending bubble from an earlier app session that
// was still being read when the app went away: its order never reached
// MaClawSrv, so no reply will ever settle it. liveIds are the bubbles this
// page is still working on.
export function orphanedPendingReply(message: Pick<DesktopBotMessage, 'id' | 'role' | 'pending' | 'dispatched' | 'requestId'>, liveIds: ReadonlySet<string>): boolean {
    if (message.role !== 'assistant' || !message.pending || message.dispatched) return false;
    if (liveIds.has(message.id)) return false;
    return String(message.requestId || '').startsWith('local:');
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
// and does not advance the counter. The event lets bot-task and seen-reply
// listeners see the same write, including one that lands while this page is closed.
export const BOT_TRANSCRIPT_EVENT = 'maclaw-bot-transcript';

let transcriptGeneration = 0;

export function botTranscriptGeneration(): number {
    return transcriptGeneration;
}

function noteTranscriptWrite(userId: string, botId: string) {
    transcriptGeneration += 1;
    if (typeof window === 'undefined') return;
    window.dispatchEvent(new CustomEvent(BOT_TRANSCRIPT_EVENT, { detail: { userId, botId } }));
}

// eachDesktopBotTranscript visits every stored conversation from one read.
// userKey is the same key desktopUserKey writes, so a caller can match one
// account. The messages are the stored transcript, already limited to rows
// with an id and a user or assistant role.
export function eachDesktopBotTranscript(visit: (userKey: string, botId: string, messages: DesktopBotMessage[]) => void) {
    const all = readMessages();
    for (const userKey of Object.keys(all)) {
        const bots = all[userKey];
        if (!bots || typeof bots !== 'object') continue;
        for (const botId of Object.keys(bots)) {
            visit(userKey, botId, transcriptMessages(bots[botId]));
        }
    }
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

const COMPUTER_TAKEOVER_REASON = /^(login_wall|captcha_widget|mfa|payment_confirm|consent_dialog|computer_takeover)$/;

// Warn only once fewer than 100 credits are left. A larger remainder still
// finishes ordinary tasks. The hub heartbeat withdraws a token-bank share
// while this balance is under the same floor and the bank can pay.
const DESKTOP_CREDIT_LOW_REMAINING = 100;

export type DesktopCreditWindow = { limit: number; used: number; held?: number; group?: string };

// Desktop bots spend the reserved system-free route. A limit on another
// service group does not stop that route, so it must not raise this warning.
const DESKTOP_CREDIT_GROUP = 'system-free';

function desktopRouteWindows(windows: DesktopCreditWindow[]): DesktopCreditWindow[] {
    const named = windows.filter(window => (window.group || '').trim());
    if (named.length === 0) return windows;
    return named.filter(window => (window.group || '').trim().toLowerCase() === DESKTOP_CREDIT_GROUP);
}

export type DesktopCreditInput = {
    mode?: string;
    unlimited?: boolean;
    authorized?: boolean;
    total?: number;
    remaining?: number;
    available?: number;
    showPeriodAvailable?: boolean;
    status?: string;
    windows?: DesktopCreditWindow[];
};

export type DesktopCreditReadout = {
    total: number;
    remaining: number;
    notice: '' | 'low' | 'empty';
    unlimited?: boolean;
};

// desktopCreditReadout is the pair the detail sheet shows, plus whether the
// chat should speak up. A custom model does not spend this account. An
// unlimited route has nothing to run out of. Among several windows, the
// tightest fraction is the one that will stop the task. The chat speaks up
// only when that remainder is under 100 credits.
export function desktopCreditReadout(input: DesktopCreditInput | null | undefined): DesktopCreditReadout | null {
    if (!input || input.mode === 'custom' || input.authorized === false) return null;
    const windows = desktopRouteWindows((input.windows || []).filter(window => window.limit > 0));
    let total = Number(input.total) || 0;
    let remaining = input.showPeriodAvailable
        ? Number(input.available) || 0
        : Number(input.remaining ?? input.available) || 0;
    if (windows.length > 0) {
        let tightRemaining = Number.POSITIVE_INFINITY;
        let tightLimit = 0;
        for (const window of windows) {
            const left = Math.max(0, window.limit - window.used - (window.held || 0));
            const fraction = left / window.limit;
            const tightFraction = tightLimit > 0 ? tightRemaining / tightLimit : Number.POSITIVE_INFINITY;
            if (fraction < tightFraction) {
                tightRemaining = left;
                tightLimit = window.limit;
            }
        }
        remaining = tightRemaining;
        total = tightLimit;
    }
    // A balance with no wallet total looks unlimited. Windows passed in by
    // the caller are a separate cap and are what that caller should show.
    if (windows.length === 0 && input.unlimited) return null;
    if (!(total > 0) && !(remaining > 0) && input.status !== 'period_limited') return null;
    let notice: DesktopCreditReadout['notice'] = '';
    if (!(remaining > 0) || input.status === 'period_limited') {
        notice = 'empty';
        // The server is already refusing this route. A leftover window total
        // would show credits that cannot be spent.
        if (input.status === 'period_limited') remaining = 0;
    } else if (remaining < DESKTOP_CREDIT_LOW_REMAINING) notice = 'low';
    if (!Number.isFinite(remaining)) remaining = 0;
    return { total: Math.max(0, total), remaining: Math.max(0, remaining), notice };
}

export function formatDesktopCredit(value: number): string {
    if (!Number.isFinite(value)) return '0';
    const rounded = Math.round(value * 100) / 100;
    return String(rounded);
}

// messageNeedsComputerTakeover is a step only the person can finish on the
// shared computer: a QR login, a captcha, a payment, or an explicit handoff.
// A report that the account is already signed in is not one of those steps.
export function messageNeedsComputerTakeover(message: Pick<DesktopBotMessage, 'content' | 'failed' | 'userControl' | 'attentionReason'>): boolean {
    if (message.failed) return false;
    if (message.userControl) return true;
    const reason = (message.attentionReason || '').trim();
    if (COMPUTER_TAKEOVER_REASON.test(reason)) return true;
    // Only an explicit request in the reply counts. A sentence that merely
    // mentions 登录 or 需要你 (a question, a plan) is not a takeover.
    const text = message.content || '';
    if (!text.trim()) return false;
    if (/已登录|登录成功|扫码成功/.test(text) && !/尚未登录|未登录/.test(text) && !/接管电脑|请接管|需要你接管/.test(text)) return false;
    if (/接管电脑|请接管|需要你接管/.test(text)) return true;
    return /(请|需要你|缺少)[^。！？\n]{0,12}(扫码|扫描二维码)/.test(text);
}

// inProgressBotTasks is the task monitor's Bot tab. One read of the transcript
// covers every bot. Finished replies and a pending bubble left behind by a
// reload are omitted. A handoff whose HTTP turn already settled stays, because
// the person still has the desktop. The task column is the arrangement or the
// request, never the confirm, return, or secret control that started the turn.
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
                    text: botTaskText(messages, index),
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
                    text: botTaskText(messages, index),
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
                    text: botTaskText(messages, index),
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
    const id = String(config?.remote_user_id || '').trim();
    if (id) return id;
    const email = String(config?.remote_email || '').trim();
    return email || 'local';
}

// desktopTaskStep decides the next step from the request and this reply.
// A screenshot that is already attached finished a look. An arrangement that
// still names later work keeps the confirm control. A login handoff does not.
export function desktopTaskStep(userText: string, reply: Pick<DesktopBotMessage, 'content' | 'images' | 'files' | 'localPaths' | 'userControl' | 'attentionReason' | 'askUser' | 'askResolved' | 'workOrder'>): DesktopTaskStep {
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
    if (isProgramTask(userText) && ((reply.images && reply.images.length > 0) || FINISHED_REPORT.test(content))) return 'arrange';
    // A destructive order still needs the one button. The pace machine already
    // turned that reading into a plan. This is that control, not a prose default.
    const order = reply.workOrder || '';
    if (desktopBotDestructive(userText) || desktopBotDestructive(order) || desktopBotDestructive(content)) return 'arrange';
    if (remaining) return 'arrange';
    // 「先打开网站，再告诉你结果。」 and any other planning prose are not a button.
    return 'done';
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
    // Set just before the order goes to SendDesktopBotTask. A pending bubble
    // without it never left this app: after a restart nothing will answer it.
    dispatched?: boolean;
    // The worker order for a confirm arrangement. The visible content is the
    // short plan; this is what the accept button sends. It is not a user bubble.
    workOrder?: string;
    // Instant "colleague accepted the task" reply. It never carries a handoff
    // or a result, so result lookups skip it.
    ack?: boolean;
    // Readings of an ambiguous order offered as one-tap corrections under the
    // acceptance line. The first is what the run already does. They never
    // hold the run; optionsUsed hides them once one is picked.
    options?: string[];
    optionsUsed?: boolean;
    // The foreground reading of this turn, shown while the worker writes the
    // arrangement. A queue note and a side note are acks too, and they stay.
    // foldPlanConfirmation drops only this reading once that arrangement arrives.
    understood?: boolean;
    // A short answer given here, with no desktop worker. It is not an
    // arrangement, so the confirm control does not attach to it.
    chat?: boolean;
    // A user message accepted while that bot was still working. The dispatch
    // waits for this bot's current run; the marker is cleared
    // the moment the dispatch starts, so a page reload can recover the queue
    // without double-sending.
    queued?: boolean;
    // This bubble is a supplement receipt: the words were folded into a
    // running turn. supplementRunId is the run they were aimed at, and
    // supplementOutcome is the terminal answer (invariant I4: applied or
    // missed, nothing else). Only these ids are persisted — the supplement
    // text itself is never stored here.
    supplementId?: string;
    supplementRunId?: string;
    supplementOutcome?: 'applied' | 'missed';
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
    // A delegate that still needs the button on this thread. Claiming happens
    // on that click, not when the note is shown.
    crossNoteId?: string;
    // Bot-to-bot traffic received from another bot: shown as one compact line.
    feed?: BotFeedMeta;
    // Small faces beside 「已发消息给 N 个 Bot」.
    noticeBots?: DesktopBotNoticeBot[];
    // This turn has no reading, so the person's own words are parked on the
    // bubble: one control sends exactly those words, the other reads again.
    // Nothing has gone to the desktop yet. The words are kept because the
    // reading is a helper for the turn, not the turn itself.
    noReading?: { text: string; phase: BotPhase; kind: DesktopBotNoReading };
}

export interface DesktopBotNoticeBot {
    id: string;
    name: string;
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

// /clear wipes this bot's chat on screen and in the local transcript. It is
// never sent to the bot and touches no Hub task or run. Bubbles a live turn
// still needs (a pending reply, a queued order) stay so the running work can
// land; everything else is gone for good, including after a reload.
export function isDesktopBotClearCommand(text: string): boolean {
    return text.trim().toLowerCase() === '/clear';
}

export function clearedBotMessages(messages: DesktopBotMessage[]): DesktopBotMessage[] {
    return messages.filter(message => message.pending || message.queued);
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
        noteTranscriptWrite(userId, botId);
    } catch {
        dropEveryShot(all[key]);
        try {
            store.setItem(MESSAGE_KEY, JSON.stringify(all));
            noteTranscriptWrite(userId, botId);
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
export function settlePendingBotReply(userId: string, botId: string, patch: { content: string; failed: boolean; handoffUrl?: string; userControl?: boolean; attentionReason?: string; askUser?: DesktopBotAsk; requestId?: string; images?: DesktopBotImage[]; shotPaths?: string[]; files?: DesktopBotFile[]; localPaths?: string[] }): string {
    const current = messagesForBot(userId, botId);
    const index = pendingBotIndex(current, patch.requestId || '');
    if (index < 0) return '';
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
    return previous.id;
}

// dropPendingBotReply removes a turn that ended without send_to_user.
// An empty bubble, or 「后台没有返回结果。」, is not that ending.
export function dropPendingBotReply(userId: string, botId: string, requestId?: string) {
    const current = messagesForBot(userId, botId);
    const index = pendingBotIndex(current, requestId || '');
    if (index < 0) return;
    const previous = current[index];
    clearLiveBotTurn(userId, botId, previous.id);
    const next = current.slice();
    next.splice(index, 1);
    saveBotMessages(userId, botId, next);
}

// applySupplementOutcomes turns a supplement receipt from "queued" into its
// terminal answer, closing invariant I4 on the UI side. A receipt that already
// has an outcome is never rewritten, so a repeated event or a stale pull
// cannot flip a settled bubble back.
//
// missed keeps the original words parked on the bubble (kind 'unrouted': the
// verbatim control shows, "read again" does not — reading these words again
// was never the failure mode). The words come from the server's in-memory
// ticket; they are shown and held for one resend, not persisted beyond the
// transcript this bubble already lives in.
export function applySupplementOutcomes(userId: string, botId: string, runId: string, outcomes: Array<{ id?: string; state?: string; content?: string }>) {
    userId = String(userId || '').trim();
    botId = String(botId || '').trim();
    runId = String(runId || '').trim();
    if (!userId || !botId || !runId || !Array.isArray(outcomes) || outcomes.length === 0) return;
    const byId = new Map(outcomes.filter(item => item && String(item.id || '').trim() !== '').map(item => [String(item.id), item]));
    if (byId.size === 0) return;
    const current = messagesForBot(userId, botId);
    let changed = false;
    const next = current.map(item => {
        if (!item.supplementId || item.supplementOutcome) return item;
        const hit = byId.get(item.supplementId);
        if (!hit) return item;
        changed = true;
        if (hit.state === 'applied') {
            return { ...item, content: '补充已生效。', supplementOutcome: 'applied' as const };
        }
        if (hit.state !== 'missed') return item;
        const words = String(hit.content || item.noReading?.text || '').trim();
        return {
            ...item,
            content: words !== '' ? '任务已结束，这条补充没能生效。' : '任务已结束，这条补充没能生效，原文已无法找回。',
            failed: true,
            supplementOutcome: 'missed' as const,
            noReading: words !== '' ? { text: words, phase: 'plan' as BotPhase, kind: 'unrouted' as DesktopBotNoReading } : undefined,
        };
    });
    if (changed) saveBotMessages(userId, botId, next);
}

// A plan-turn order that installs, removes, pays, sends on someone's behalf,
// or wipes still waits for the one button. An execute turn is already that button.
const DESKTOP_BOT_INSTALL_WORD = /(?:^|[^a-z])install(?:[^a-z]|$)/i;

export const DESKTOP_BOT_NOT_PRIMARY = '我不能去叫别的 Bot。你跟主 Bot 说，或者直接打开那个 Bot。';

// The line a target bot's chat shows when Hub (not this window) started a
// delegated order: it runs on the server and finishes with MaClaw closed.
export function desktopBotHubStartedText(runId: string): string {
    const run = (runId || '').trim();
    return run
        ? `已在服务器上开始做这件（${run}），关掉 MaClaw 也会继续，做完结果会交回主 Bot。`
        : '已在服务器上开始做这件，关掉 MaClaw 也会继续，做完结果会交回主 Bot。';
}

export function desktopBotDestructive(text: string): boolean {
    const value = text || '';
    if (/安装|卸载|刪除|删除|付款|支付|代发|代發|清空/.test(value)) return true;
    const lower = value.toLowerCase();
    if (lower.includes('uninstall') || lower.includes('wipe') || lower.includes('delete')) return true;
    return DESKTOP_BOT_INSTALL_WORD.test(lower);
}

// A working-row phrase is 正在 plus two to six Han characters. Typed text,
// a tool name, and the spoken word 正在处理 are not phrases. 正在一点点推进
// is the row's own fallback, and 正在询问 is composed with the other bot's name.
export function desktopBotActivityText(raw: string): string {
    return visibleActivityPhrase(raw);
}

// Asking another bot wins over the step. With no reported step the row
// keeps the fallback sentence.
export function desktopBotWorkingLabel(asking: string, activity: string): string {
    const who = (asking || '').trim();
    if (who) return `正在询问 ${who}`;
    return desktopBotActivityText(activity) || '正在一点点推进';
}

export function desktopBotAskingLabel(target: string, bots: Array<{ id: string; title: string }>): string {
    const raw = (target || '').trim();
    const lower = raw.toLowerCase();
    if (!raw || lower === 'all' || raw === '*' || raw === '所有' || lower === '所有bot' || raw === '所有的bot') return '这些 Bot';
    const named = bots.find(bot => bot.id === raw);
    return (named?.title || raw).trim() || '这些 Bot';
}

export function desktopBotCrossTargetText(kind: string, text: string): string {
    const body = (text || '').trim();
    if (kind === 'notify') return `主 Bot 说：${body}`;
    return `主 Bot 交代：${body}`;
}

const NOTICE_KEY = 'maclaw.desktopBotNotices.v1';
const CROSS_RUN_KEY = 'maclaw.desktopBotCrossRuns.v1';

function readNoticeMap(): Record<string, string[]> {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(NOTICE_KEY) || '{}') as Record<string, string[]>;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

export function desktopBotNoticeSeen(userId: string, key: string): boolean {
    if (!userId || !key) return false;
    const list = readNoticeMap()[userId];
    return Array.isArray(list) && list.includes(key);
}

export function markDesktopBotNotice(userId: string, key: string): void {
    const store = storage();
    if (!store || !userId || !key) return;
    const all = readNoticeMap();
    const list = Array.isArray(all[userId]) ? all[userId].slice() : [];
    if (list.includes(key)) return;
    list.push(key);
    all[userId] = list.slice(-400);
    try { store.setItem(NOTICE_KEY, JSON.stringify(all)); } catch { /* private mode */ }
}

type DesktopBotCrossRun = { noteId: string; from: string };

function readCrossRuns(): Record<string, DesktopBotCrossRun> {
    const store = storage();
    if (!store) return {};
    try {
        const parsed = JSON.parse(store.getItem(CROSS_RUN_KEY) || '{}') as Record<string, DesktopBotCrossRun>;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

export function rememberDesktopBotCrossRun(userId: string, botId: string, noteId: string, from: string): void {
    const store = storage();
    const id = (noteId || '').trim();
    if (!store || !userId || !botId || !id) return;
    const all = readCrossRuns();
    all[`${userId}\0${botId}`] = { noteId: id, from: (from || '').trim() };
    try { store.setItem(CROSS_RUN_KEY, JSON.stringify(all)); } catch { /* private mode */ }
}

export function takeDesktopBotCrossRun(userId: string, botId: string): DesktopBotCrossRun | undefined {
    const store = storage();
    if (!store || !userId || !botId) return undefined;
    const all = readCrossRuns();
    const key = `${userId}\0${botId}`;
    const run = all[key];
    if (!run?.noteId) return undefined;
    delete all[key];
    try { store.setItem(CROSS_RUN_KEY, JSON.stringify(all)); } catch { /* private mode */ }
    return run;
}

// appendDesktopBotReport adds one scheduled result, or one cross-note line.
// The same request id is written once, so the open page and the store can
// both hear the event. A scheduled report passes no phase, so it does not
// take the confirm control. A destructive delegate passes phase plan.
export function appendDesktopBotReport(userId: string, botId: string, patch: { content: string; failed: boolean; handoffUrl?: string; userControl?: boolean; attentionReason?: string; askUser?: DesktopBotAsk; requestId?: string; images?: DesktopBotImage[]; shotPaths?: string[]; files?: DesktopBotFile[]; localPaths?: string[]; phase?: BotPhase; workOrder?: string; crossNoteId?: string; noticeBots?: DesktopBotNoticeBot[]; chat?: boolean; feed?: BotFeedMeta }) {
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
        phase: patch.phase,
        workOrder: patch.workOrder || undefined,
        crossNoteId: (patch.crossNoteId || '').trim() || undefined,
        noticeBots: patch.noticeBots && patch.noticeBots.length > 0 ? patch.noticeBots : undefined,
        chat: patch.chat === true ? true : undefined,
        feed: patch.feed,
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
    noteTranscriptWrite(userId, botId);
}

const reportTopicWords: Record<string, string> = {
    done: '完成了', blocked: '卡住了', progress: '进展', question: '有个问题', artifact: '交来文件',
    info: '说', reply: '回复', task: '交来任务',
};

// A message one bot sent another, as the person sees it in each chat.
export function desktopBotReportFeedText(note: Record<string, unknown>, bots: Array<{ id: string; title?: string }>): { received: string; sent: string; feed: BotFeedMeta } {
    const name = (id: string) => bots.find(bot => bot.id === id)?.title || id;
    const from = String(note.from || '').trim();
    const target = String(note.target || '').trim();
    const topic = String(note.topic || '').trim().toLowerCase();
    const text = String(note.text || '').trim();
    const files = Array.isArray(note.files) ? (note.files as Array<Record<string, unknown>>) : [];
    const fileLine = files.length > 0
        ? `\n附件：${files.map(f => String(f.name || f.path || f.url || '').split(/[\\/]/).pop()).filter(Boolean).join('、')}`
        : '';
    const word = reportTopicWords[topic] || '说';
    const woken = String(note.wake_run_id || '').trim() ? '\n（MaClaw 没开时服务器已把这条交给我处理）' : '';
    return {
        received: `${name(from)} ${word}：${text}${fileLine}${woken}`,
        sent: `发给 ${name(target)}（${word}）：${text}${fileLine}`,
        feed: { from: name(from), word },
    };
}

// The end of a run Hub started on the server (a delegate or a wake).
export function desktopBotServerRunText(kind: 'wake' | 'delegate', runId: string, text: string, error: string, status: string): string {
    const head = kind === 'wake' ? `服务器上处理队友消息的这一轮（${runId}）` : `服务器上做的这件（${runId}）`;
    const body = (text || '').trim();
    if (body) return `${head}结果：\n${body}`;
    if ((error || '').trim()) return `${head}没做成：${error.trim()}`;
    return `${head}已结束（${status || 'done'}）。`;
}

// desktopAckWorthSaying decides whether a direct job gets a first line at all.
// "I'll go check that now" is filler: the status row already shows the work.
// A line is worth it only when it tells the person something they can act on.
export function desktopAckWorthSaying(r: { unclear?: string; assumption?: string; options?: number; files?: number; fallback?: boolean }): boolean {
    return !!((r.unclear || '').trim() || (r.assumption || '').trim() || (r.options || 0) >= 2 || (r.files || 0) > 0 || r.fallback);
}

import { useState } from 'react';
import { BotMessageBody, botMessageIsStructured } from './botMessageBody';

// botFeedLine: bot-to-bot traffic (Hub feed items) as one short, muted line
// for the person: who → who, what kind, a short summary. Run ids and raw
// prompts stay behind a click; files show as chips; help/errors stand out.

export type BotFeedKind = '交代任务' | '开始执行' | '交来文件' | '完成' | '求助' | '出错' | '进展' | '消息';

export interface BotFeedMeta {
    // The other bot's name, as shown in the message head.
    from?: string;
    to?: string;
    // The topic word desktopBotReportFeedText used (完成了, 交来文件, ...).
    word?: string;
    // The Hub note that carried the files, and which of them Hub stored
    // (only those can be fetched and opened on any client).
    noteId?: string;
    stored?: string[];
}

export interface BotFeedLineData {
    from: string;
    to: string;
    kind: BotFeedKind;
    summary: string;
    full: string;
    files: string[];
    alert: boolean;
    // Set when the files can be fetched from Hub: the note that carried them.
    noteId?: string;
    // Names Hub stored; undefined means unknown (an older message), try it.
    stored?: string[];
}

const SUMMARY_MAX = 60;

const WORD_KIND: Record<string, BotFeedKind> = {
    完成了: '完成', 卡住了: '求助', 有个问题: '求助', 交来文件: '交来文件', 交来任务: '交代任务',
    交代: '交代任务', 进展: '进展', 说: '消息', 回复: '消息',
};

// Internal bot↔bot briefs (peer review, accept/return) keep their full text
 // for the receiving bot, but the person only sees a short human label.
const REVIEW_ASK = /请审核|please\s*review|action\s*=\s*accept_result|assignment\s*=/i;
const REVIEW_OK = /审核\s*\/?\s*review\s+(accepted|通过)|review\s+accepted/i;
const REVIEW_BACK = /审核\s*\/?\s*review\s+(returned|退回)|return_result|review\s+returned/i;
const INTERNAL_BRIEF = /任务\s*\/?\s*task\s*:|结果\s*\/?\s*result\s*:|action\s*=\s*(accept_result|return_result|delegate)/i;

const KIND_ICON: Record<BotFeedKind, string> = {
    交代任务: '📋', 开始执行: '▶', 交来文件: '📎', 完成: '✓', 求助: '✋', 出错: '⚠', 进展: '…', 消息: '💬',
};

const RUN_ID = /[（(]\s*run_[^）)]*[）)]/g;

function cjk(text: string): boolean {
    return /[\u3400-\u9fff]/.test(text);
}

// One short line: first non-empty line, run ids out, at most ~60 chars.
// A long prompt with no Chinese in it is a raw order: it stays behind the click.
// Peer-review / accept_result briefs never leak assignment ids or bilingual
 // machine instructions into the summary the person sees.
export function botFeedSummary(text: string): string {
    const clean = (text || '').replace(RUN_ID, '').trim();
    if (!clean) return '';
    if (REVIEW_OK.test(clean)) return '审核通过';
    if (REVIEW_BACK.test(clean)) return '退回修改';
    if (REVIEW_ASK.test(clean)) return '请审核';
    if (INTERNAL_BRIEF.test(clean)) return '内部协作说明';
    const first = (clean.split(/\r?\n/).find(line => line.trim() !== '') || '').replace(/\s+/g, ' ').trim();
    if (!first) return '';
    // Drop trailing machine crumbs that still ride on an otherwise human line.
    const human = first
        .replace(/\s*assignment\s*=\S+/ig, '')
        .replace(/\s*action\s*=\S+/ig, '')
        .replace(/\s*\/?\s*please\s*review\b/ig, '')
        .replace(/\s+/g, ' ')
        .trim();
    // The summary is one plain line: Markdown marks are not shown raw.
    const line = (human || first)
        .replace(/^(?:#{1,6}\s+|[-*+]\s+|>\s*)/, '')
        .replace(/\*\*([^*]+)\*\*/g, '$1')
        .replace(/`([^`]+)`/g, '$1')
        .trim() || human || first;
    if (!cjk(line) && line.length > SUMMARY_MAX) return '一段英文指令（点开看原文）';
    const chars = [...line];
    return chars.length > SUMMARY_MAX ? chars.slice(0, SUMMARY_MAX - 1).join('') + '…' : line;
}

function splitFiles(body: string): { text: string; files: string[] } {
    const m = body.match(/\n附件：([^\n]*)/);
    if (!m) return { text: body, files: [] };
    return { text: body.replace(m[0], ''), files: m[1].split('、').map(s => s.trim()).filter(Boolean) };
}

function stripWoken(body: string): string {
    return body.replace(/\n（MaClaw 没开时服务器已把这条交给我处理）/, '');
}

// botFeedLineOf reads a stored chat message. It returns null for anything
// that is not bot-to-bot traffic, so normal chat keeps its bubble.
export function botFeedLineOf(content: string, self: string, opts: { feed?: BotFeedMeta; failed?: boolean; fileNames?: string[]; requestId?: string } = {}): BotFeedLineData | null {
    const raw = (content || '').trim();
    if (!raw) return null;
    const me = (self || '').trim() || '我';
    const extra = opts.fileNames || [];
    const done = (from: string, to: string, kind: BotFeedKind, body: string, files: string[] = []): BotFeedLineData => {
        let k: BotFeedKind = opts.failed ? '出错' : kind;
        const summary = botFeedSummary(body) || k;
        if (!opts.failed && (summary === '请审核' || summary === '退回修改')) k = '求助';
        if (!opts.failed && summary === '审核通过') k = '完成';
        const all = [...files, ...extra];
        const noteId = all.length ? botFeedNoteId(opts.feed, opts.requestId) : undefined;
        return { from, to, kind: k, summary, full: raw, files: all, alert: k === '求助' || k === '出错', noteId, stored: noteId ? opts.feed?.stored : undefined };
    };
    if (opts.feed?.from) {
        const word = opts.feed.word || '说';
        const head = `${opts.feed.from} ${word}：`;
        const body = raw.startsWith(head) ? raw.slice(head.length) : raw;
        const parts = splitFiles(stripWoken(body));
        return done(opts.feed.from, opts.feed.to || me, WORD_KIND[word] || '消息', parts.text, parts.files);
    }
    let m = raw.match(/^主 Bot (交代|说)：([\s\S]*)$/);
    if (m) return done('主 Bot', me, m[1] === '交代' ? '交代任务' : '消息', m[2]);
    if (raw.startsWith('已在服务器上开始做这件')) return done(me, '服务器', '开始执行', '在服务器上执行，关掉 MaClaw 也会继续');
    m = raw.match(/^发给 (.+?)（(.+?)）：([\s\S]*)$/);
    if (m) {
        const parts = splitFiles(m[3]);
        return done(me, m[1], WORD_KIND[m[2]] || '消息', parts.text, parts.files);
    }
    m = raw.match(/^服务器上(?:处理队友消息的这一轮|做的这件)[（(][^）)]*[）)](结果：|没做成：|已结束)([\s\S]*)$/);
    if (m) {
        if (m[1] === '没做成：') return { ...done('服务器', me, '出错', m[2]), kind: '出错', alert: true };
        return done('服务器', me, '完成', m[1] === '结果：' ? m[2] : '服务器上的这一轮已结束');
    }
    return null;
}

// botFeedLineInternal: a teammate's order or message to this bot is an
// internal brief (Daniel 10-31: 主bot让各bot跟用户说话，直接以它自己的口吻说).
// The person sees what this bot then says in its own voice, not the relay
// line "主 Bot → Bot 6 消息 对用户友好点。".
export function botFeedLineInternal(line: BotFeedLineData, self: string): boolean {
    const me = (self || '').trim() || '我';
    return line.to === me && line.from !== me && (line.kind === '消息' || line.kind === '交代任务') && !line.files.length;
}

// botFeedNoteId: the Hub note a feed line's files came with. A report is
// stored with request id "<note id>:from" / "<note id>:target".
export function botFeedNoteId(feed?: BotFeedMeta, requestId?: string): string | undefined {
    const direct = (feed?.noteId || '').trim();
    if (direct) return direct;
    const m = (requestId || '').trim().match(/^(note_[A-Za-z0-9]+):(?:from|target)$/);
    return m ? m[1] : undefined;
}

// botFeedFileOpenable: a chip opens only when Hub holds the bytes.
export function botFeedFileOpenable(line: BotFeedLineData, name: string): boolean {
    if (!line.noteId) return false;
    return !line.stored || line.stored.includes(name);
}

export const BOT_FEED_FILE_MISSING = '这个文件只在机器人的桌面上，没有传到 Hub，这里打不开';

function BotFeedFile({ line, name, onOpenFile }: { line: BotFeedLineData; name: string; onOpenFile?: (noteId: string, name: string) => Promise<void> }) {
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    if (!onOpenFile || !botFeedFileOpenable(line, name)) {
        return (
            <span className="desktop-bot-feed__file is-disabled" data-testid="desktop-bot-feed-file" aria-disabled="true" title={`${name}：${BOT_FEED_FILE_MISSING}`}>📄 {name}</span>
        );
    }
    const open = async (event: { preventDefault: () => void; stopPropagation: () => void }) => {
        event.preventDefault();
        event.stopPropagation();
        if (busy) return;
        setBusy(true);
        setError('');
        try {
            await onOpenFile(line.noteId as string, name);
        } catch (err) {
            setError(`打不开：${String((err as Error)?.message || err || '下载失败')}`);
        } finally {
            setBusy(false);
        }
    };
    return (
        <button
            type="button"
            className={'desktop-bot-feed__file is-openable' + (error ? ' is-error' : '')}
            data-testid="desktop-bot-feed-file"
            title={error || `下载 ${name}`}
            disabled={busy}
            onClick={open}
        >📄 {name}{error ? <span className="desktop-bot-feed__file-error" role="alert">{error}</span> : null}</button>
    );
}

export function BotFeedLine({ line, onOpenFile }: { line: BotFeedLineData; onOpenFile?: (noteId: string, name: string) => Promise<void> }) {
    return (
        <details className={'desktop-bot-feed' + (line.alert ? ' is-alert' : '')} data-testid="desktop-bot-feed" data-kind={line.kind}>
            <summary className="desktop-bot-feed__row">
                <span className="desktop-bot-feed__icon" aria-hidden="true">{KIND_ICON[line.kind]}</span>
                <span className="desktop-bot-feed__dir">{line.from} → {line.to}</span>
                <span className="desktop-bot-feed__kind">{line.kind}</span>
                <span className="desktop-bot-feed__summary" data-testid="desktop-bot-feed-summary">{line.summary}</span>
                {line.files.map(name => <BotFeedFile key={name} line={line} name={name} onOpenFile={onOpenFile} />)}
            </summary>
            <div className="desktop-bot-feed__full" data-testid="desktop-bot-feed-full">{botMessageIsStructured(line.full) ? <BotMessageBody text={line.full} /> : line.full}</div>
        </details>
    );
}

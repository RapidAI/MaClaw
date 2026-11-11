import type {
    DeleteBotRoom, GetBotCollabFeatures, ListBotAssignments, ListBotRooms, ListBotSaid, PostBotRoomMessage, SaveBotRoom,
} from '../../../../wailsjs/go/main/App';
import { isChineseLang } from '../../../i18n/langSelect';
import { tailText } from '../../../i18n/mnTail';

export type CollabFeatures = Awaited<ReturnType<typeof GetBotCollabFeatures>>;
export type SaidItem = Awaited<ReturnType<typeof ListBotSaid>>[number];
export type Assignment = Awaited<ReturnType<typeof ListBotAssignments>>[number];
export type Room = Awaited<ReturnType<typeof ListBotRooms>>[number];
export type Attention = 'all' | 'mentions' | 'off';

export type CollabApi = {
    features: typeof GetBotCollabFeatures;
    said: typeof ListBotSaid;
    assignments: typeof ListBotAssignments;
    rooms: typeof ListBotRooms;
    saveRoom: typeof SaveBotRoom;
    deleteRoom: typeof DeleteBotRoom;
    postRoom: typeof PostBotRoomMessage;
};

// Loaded on use, so screens and tests that never open these need no binding.
const bindings = () => import('../../../../wailsjs/go/main/App');
export const defaultCollabApi: CollabApi = {
    features: async () => (await bindings()).GetBotCollabFeatures(),
    said: async room => (await bindings()).ListBotSaid(room),
    assignments: async () => (await bindings()).ListBotAssignments(),
    rooms: async () => (await bindings()).ListBotRooms(),
    saveRoom: async (id, name, members, attention) => (await bindings()).SaveBotRoom(id, name, members, attention),
    deleteRoom: async id => (await bindings()).DeleteBotRoom(id),
    postRoom: async (id, text) => (await bindings()).PostBotRoomMessage(id, text),
};

export const FEATURES_OFF: CollabFeatures = { assignments: false, peer_review: false, say: false, rooms: false };

export type BotRef = { id: string; title: string; primary?: boolean };

export function botName(bots: BotRef[], id: string, lang = 'zh-Hans'): string {
    if (id === 'human') return tailText(lang, 'Me', '我');
    return bots.find(b => b.id === id)?.title || id;
}

export const STATE_LABEL: Record<string, string> = {
    queued: '排队中',
    running: '进行中',
    progress: '进行中',
    held: '等你确认',
    waiting_human: '等你回答',
    needs_login: '需要你登录',
    blocked: '卡住了',
    failed: '失败',
    completed: '已完成',
    awaiting_review: '等待审核',
    accepted: '已验收',
    returned: '已退回重做',
};

const STATE_EN: Record<string, string> = {
    queued: 'Queued',
    running: 'Running',
    progress: 'Running',
    held: 'Waiting for your confirmation',
    waiting_human: 'Waiting for your answer',
    needs_login: 'Needs you to sign in',
    blocked: 'Stuck',
    failed: 'Failed',
    completed: 'Completed',
    awaiting_review: 'Waiting for review',
    accepted: 'Accepted',
    returned: 'Returned for rework',
};

export function stateLabel(state: string, lang = 'zh-Hans'): string {
    const zh = STATE_LABEL[state];
    const en = STATE_EN[state];
    if (!zh || !en) return state;
    return tailText(lang, en, zh);
}

/** States where the person has to do something. */
export function needsHuman(a: Assignment): boolean {
    return a.state === 'waiting_human' || a.state === 'needs_login' || a.state === 'held';
}

const ACTIVE = new Set(['queued', 'running', 'progress']);

/**
 * The one-line status: who is working and on what stage, at most three
 * bots, then "等 N 个". Empty when nobody is busy.
 */
// BotBusy is a bot's own run (a job the person gave it directly), which is
// not an assignment. The row has to count it too, or it says no bot is busy
// while that bot shows 工作中 (2026-11-03 21:08).
export interface BotBusy { bot: string; phase: 'execute' | 'plan'; task?: string }

export function statusRowText(assignments: Assignment[], bots: BotRef[], max = 3, lang = 'zh-Hans', busy: BotBusy[] = []): string {
    const seen = new Set<string>();
    const parts: string[] = [];
    const zh = isChineseLang(lang);
    for (const a of assignments) {
        if (!ACTIVE.has(a.state) || seen.has(a.target_bot)) continue;
        seen.add(a.target_bot);
        if (parts.length >= max) continue;
        const name = botName(bots, a.target_bot, lang);
        const stage = shorten(a.last_report || a.task, 18);
        const verbZh = a.state === 'queued' ? '排队中' : a.last_report ? '正在做' : '在想';
        const verbEn = a.state === 'queued' ? 'is queued' : a.last_report ? 'is working' : 'is thinking';
        parts.push(tailText(lang, `${name} ${verbEn}: ${stage}`, `${name} ${verbZh}：${stage}`));
    }
    for (const b of busy) {
        if (!b || !b.bot || seen.has(b.bot)) continue;
        seen.add(b.bot);
        if (parts.length >= max) continue;
        const name = botName(bots, b.bot, lang);
        const stage = shorten(b.task || '', 18);
        const verbZh = b.phase === 'plan' ? '在安排' : '正在做';
        const verbEn = b.phase === 'plan' ? 'is planning' : 'is working';
        // A live step (正在登录 CSDN) is the row itself: 「Bot 1 正在登录 CSDN」.
        if (zh && /^正在/.test(stage)) { parts.push(`${name} ${stage}`); continue; }
        parts.push(stage ? tailText(lang, `${name} ${verbEn}: ${stage}`, `${name} ${verbZh}：${stage}`) : tailText(lang, `${name} ${verbEn}`, `${name} ${verbZh}`));
    }
    if (!parts.length) return '';
    const more = seen.size - parts.length;
    const sep = zh ? '；' : '; ';
    const joined = parts.join(sep);
    if (more <= 0) return joined;
    return `${joined}${sep}${tailText(lang, `${more} more bots are busy`, `还有 ${more} 个机器人在忙`)}`;
}

export function shorten(text: string, n: number): string {
    const chars = Array.from((text || '').replace(/\s+/g, ' ').trim());
    return chars.length > n ? chars.slice(0, n).join('') + '…' : chars.join('');
}

/** What the person must do for an assignment that waits on them. */
export function humanPrompt(a: Assignment, bots: BotRef[], lang = 'zh-Hans'): { title: string; body: string } | null {
    const who = botName(bots, a.target_bot, lang);
    switch (a.state) {
        case 'waiting_human': {
            const en = (a.last_report ? `Question: ${a.last_report}\n` : '') + `Reply to ${who} in the chat and it will continue.`;
            const zh = (a.last_report ? `问题：${a.last_report}\n` : '') + `直接在和 ${who} 的对话里回复就行，它收到后会接着做。`;
            return { title: tailText(lang, `${who} is waiting for your answer`, `${who} 在等你回答`), body: tailText(lang, en, zh) };
        }
        case 'needs_login': {
            const detailEn = a.last_report ? ` (${a.last_report})` : '';
            const detailZh = a.last_report ? `（${a.last_report}）` : '';
            return {
                title: tailText(lang, `${who} needs you to sign in`, `${who} 需要你登录`),
                body: tailText(
                    lang,
                    `The site asks you to sign in${detailEn}. Open ${who}'s desktop, sign in yourself, then say you are signed in. The bot will not type your password.`,
                    `网站要求登录${detailZh}。请打开 ${who} 的桌面，自己完成登录，然后在对话里告诉它“登好了”。机器人不会替你输入密码。`,
                ),
            };
        }
        case 'held':
            return {
                title: tailText(lang, `${who} is waiting for you to confirm`, `${who} 的任务等你确认`),
                body: tailText(lang, 'Confirm this in the chat before it starts. It may install, delete, or pay.', '这件事涉及安装、删除或付款，需要你在对话里确认后才会开始。'),
            };
    }
    return null;
}

/**
 * Messages to show in one bot's chat: its own direct messages. The main bot's
 * chat also shows what a bot without a chat of its own said; a bot that has
 * its own chat (ownChats) is heard only there (11-11 20:36: Bot 2's progress
 * note showed in Bot 2's chat and again in Bot 1's, the main bot).
 */
export function saidForChat(items: SaidItem[], botId: string, isPrimary: boolean, sinceMs = 0, ownChats?: string[]): SaidItem[] {
    // The inline block renders below the transcript, so only what was said
    // after the chat's latest message belongs there. 10-31: a 10-28 15:26
    // 「今天适合喝乌龙茶…」 say stayed pinned under every newer reply.
    const own = new Set(ownChats || []);
    return items.filter(i => i.kind === 'say' && i.target === 'human' && (i.from === botId || (isPrimary && !own.has(i.from)))
        && (!sinceMs || Date.parse(String(i.created_at || '')) > sinceMs));
}

/** Time a GUI message id (m-<base36 ms>-…) was made, or 0. */
export function messageIdTime(id: string | undefined): number {
    const m = /^m-([0-9a-z]+)-/.exec(String(id || ''));
    const t = m ? parseInt(m[1], 36) : 0;
    return Number.isFinite(t) && t > 1e12 && t < 1e13 ? t : 0;
}

/** Lower bound for inline says: the latest message, and never older than 12 h. */
export function saidSince(messageIds: string[], now = Date.now()): number {
    return Math.max(now - 12 * 3600 * 1000, ...messageIds.map(messageIdTime));
}

/** Bots that spoke in a room after the person's last message. */
export function answeredSinceLast(items: SaidItem[]): string[] {
    const out: string[] = [];
    for (let i = items.length - 1; i >= 0; i--) {
        const it = items[i];
        if (it.kind === 'room') break;
        if (it.kind === 'say' && !out.includes(it.from)) out.unshift(it.from);
    }
    return out;
}

/** The @word being typed at the caret, or null. */
export function mentionQuery(text: string, caret: number): { start: number; query: string } | null {
    const before = text.slice(0, caret);
    const m = /[@＠]([^\s@＠]*)$/.exec(before);
    if (!m) return null;
    return { start: before.length - m[0].length, query: m[1] };
}

export function mentionCandidates(members: BotRef[], query: string): BotRef[] {
    const q = query.toLowerCase();
    return members.filter(b => b.title.toLowerCase().includes(q) || b.id.toLowerCase().startsWith(q)).slice(0, 6);
}

export function applyMention(text: string, start: number, caret: number, bot: BotRef): { text: string; caret: number } {
    const insert = '@' + bot.title + ' ';
    return { text: text.slice(0, start) + insert + text.slice(caret), caret: start + insert.length };
}

export function attentionOf(room: Room, bot: BotRef): Attention {
    const a = room.attention?.[bot.id];
    if (a === 'all' || a === 'mentions' || a === 'off') return a;
    return bot.primary ? 'all' : 'mentions';
}

export const ATTENTION_LABEL: Record<Attention, string> = { all: '每条都听', mentions: '被@才听', off: '不听' };

/**
 * applyBotStatusLine keeps one live status line per bot. An empty line from
 * a run clears only that run's line, so a finished run cannot wipe a newer
 * run's status.
 */
export function applyBotStatusLine(prev: Record<string, { text: string; runId: string }>, botId: string, runId: string, line: string): Record<string, { text: string; runId: string }> {
    const current = prev[botId];
    if (!line) {
        if (!current) return prev;
        if (runId && current.runId && current.runId !== runId) return prev;
        const next = { ...prev };
        delete next[botId];
        return next;
    }
    if (current && current.text === line && current.runId === runId) return prev;
    return { ...prev, [botId]: { text: line, runId } };
}

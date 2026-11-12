// Routing of a follow-up to the srv conversation of a finished (or still
// visible) task. The Hub also keeps a finished thread sticky for a short
// window; the page is the primary signal when it knows which task the
// person is looking at or replying in.

export const DESKTOP_RECENT_THREAD_MS = 30 * 60 * 1000;

export type DesktopContinueMessage = {
    role: string;
    conversation?: string;
    conversationAt?: number;
    pending?: boolean;
    ack?: boolean;
    chat?: boolean;
};

// desktopThreadOf returns conversation when it is a per-run thread of this
// bot, not the bot's main conversation.
export function desktopThreadOf(botId: string, conversation?: string | null): string {
    const bot = String(botId || '').trim();
    const session = String(conversation || '').trim();
    if (!bot || !session) return '';
    if (session === bot) return '';
    return session.startsWith(`${bot}#`) ? session : '';
}

export function lastSettledConversation(
    botId: string,
    messages: readonly DesktopContinueMessage[],
): string {
    const bot = String(botId || '').trim();
    if (!bot || !Array.isArray(messages)) return '';
    for (let i = messages.length - 1; i >= 0; i--) {
        const item = messages[i];
        if (!item || item.pending || item.ack || item.chat) continue;
        if (item.role !== 'assistant' && item.role !== 'user') continue;
        const session = String(item.conversation || '').trim();
        if (!session) continue;
        if (session === bot || session.startsWith(`${bot}#`)) return session;
    }
    return '';
}

export type DesktopContinueForce = { session?: string; main?: boolean } | null | undefined;

// continueSessionForSend is the continue_session value to post with a new
// message: a named thread the person picked or is answering in, the last
// recent thread in this chat, the bot id for a clearly new message, or
// empty when the page should leave the choice to Hub.
export function continueSessionForSend(
    botId: string,
    messages: readonly DesktopContinueMessage[],
    now = Date.now(),
    forced?: DesktopContinueForce,
    stickyMs = DESKTOP_RECENT_THREAD_MS,
): string {
    const bot = String(botId || '').trim();
    if (!bot) return '';
    if (forced?.main) return bot;
    const named = desktopThreadOf(bot, forced?.session);
    if (named) return named;
    const last = lastSettledConversation(bot, messages);
    const thread = desktopThreadOf(bot, last);
    if (thread) {
        const at = lastConversationAt(messages, thread);
        if (at > 0 && now - at > stickyMs) return bot;
        return thread;
    }
    if (last === bot) return bot;
    return '';
}

function lastConversationAt(messages: readonly DesktopContinueMessage[], session: string): number {
    for (let i = messages.length - 1; i >= 0; i--) {
        const item = messages[i];
        if (!item || String(item.conversation || '').trim() !== session) continue;
        const at = Number(item.conversationAt || 0);
        if (Number.isFinite(at) && at > 0) return at;
        return 0;
    }
    return 0;
}

// Content routing beyond the sticky window: the bot's recent finished
// threads are offered to a cheap classifier (ClassifyDesktopFollowUp) with
// their title, goal and result. Only a confident pick routes the message.
export const DESKTOP_FOLLOWUP_MAX_AGE_MS = 24 * 60 * 60 * 1000;
export const DESKTOP_FOLLOWUP_MAX_CANDIDATES = 5;
export const DESKTOP_FOLLOWUP_BUDGET_MS = 2000;

export type DesktopFollowUpMessage = DesktopContinueMessage & {
    content?: string;
    workOrder?: string;
};

export type DesktopFollowUpCandidate = {
    session: string;
    title: string;
    goal?: string;
    result?: string;
};

// recentThreadCandidates lists the threads of this bot seen in this chat in
// the last day, newest first. A thread with a reply still pending is live,
// not a candidate (a live run is handled by the supplement path).
export function recentThreadCandidates(
    botId: string,
    messages: readonly DesktopFollowUpMessage[],
    now = Date.now(),
    maxAgeMs = DESKTOP_FOLLOWUP_MAX_AGE_MS,
    max = DESKTOP_FOLLOWUP_MAX_CANDIDATES,
): DesktopFollowUpCandidate[] {
    const bot = String(botId || '').trim();
    if (!bot || !Array.isArray(messages)) return [];
    type Acc = DesktopFollowUpCandidate & { at: number; live: boolean };
    const bySession = new Map<string, Acc>();
    for (let i = 0; i < messages.length; i++) {
        const item = messages[i];
        if (!item) continue;
        const session = desktopThreadOf(bot, item.conversation);
        if (!session) continue;
        let acc = bySession.get(session);
        if (!acc) {
            // The thread's title is the person's line that started it.
            let title = '';
            for (let j = i - 1; j >= 0; j--) {
                const prev = messages[j];
                if (prev && prev.role === 'user' && String(prev.content || '').trim()) {
                    title = String(prev.content || '').trim();
                    break;
                }
            }
            acc = { session, title, at: 0, live: false };
            bySession.set(session, acc);
        }
        if (item.pending) acc.live = true;
        const order = String(item.workOrder || '').trim();
        if (order && !acc.goal) acc.goal = order;
        if (item.role === 'assistant' && !item.pending && !item.ack && String(item.content || '').trim()) {
            acc.result = String(item.content || '').trim();
        }
        const at = Number(item.conversationAt || 0);
        if (Number.isFinite(at) && at > acc.at) acc.at = at;
    }
    return [...bySession.values()]
        .filter(item => !item.live && item.at > 0 && now - item.at <= maxAgeMs && (item.title || item.goal))
        .sort((a, b) => b.at - a.at)
        .slice(0, max)
        .map(({ session, title, goal, result }) => ({
            session,
            title: clip(title || goal || '', 120),
            goal: goal ? clip(goal, 200) : undefined,
            result: result ? clip(result, 240) : undefined,
        }));
}

// contentRoutingApplies: the page asks the classifier only when nothing
// explicit decided the conversation: no button or chip choice, and no
// recent thread still sticky (continueSessionForSend gave '' or main).
export function contentRoutingApplies(botId: string, continueSession: string, forced?: DesktopContinueForce): boolean {
    const bot = String(botId || '').trim();
    if (!bot || forced?.main || forced?.session) return false;
    return continueSession === '' || continueSession === bot;
}

function clip(s: string, n: number): string {
    const chars = Array.from(s.replace(/\s+/g, ' ').trim());
    return chars.length > n ? chars.slice(0, n).join('') + '…' : chars.join('');
}

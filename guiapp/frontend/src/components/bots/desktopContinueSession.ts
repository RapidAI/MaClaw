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

// The page does not decide which conversation a message belongs to. Hub
// and MaClawSrv do (explicit hint, then the sticky window, then content
// routing over the bot's persisted threads). The page only forwards what
// the person explicitly chose: a task they picked (继续这个任务) or 改为新的事.

export type DesktopContinueForce = { session?: string; main?: boolean } | null | undefined;

// desktopThreadOf returns conversation when it is a per-run thread of this
// bot, not the bot's main conversation.
export function desktopThreadOf(botId: string, conversation?: string | null): string {
    const bot = String(botId || '').trim();
    const session = String(conversation || '').trim();
    if (!bot || !session) return '';
    if (session === bot) return '';
    return session.startsWith(`${bot}#`) ? session : '';
}

// continueSessionForSend is the continue_session to post: only an explicit
// choice of the person. Empty leaves the decision to the server.
export function continueSessionForSend(botId: string, forced?: DesktopContinueForce): string {
    const bot = String(botId || '').trim();
    if (!bot) return '';
    if (forced?.main) return bot;
    return desktopThreadOf(bot, forced?.session);
}

// routedLine is the visible line for the server's routing decision.
export function routedLine(lang: string, title?: string): string {
    const name = String(title || '').trim();
    if (String(lang || '').startsWith('zh')) return name ? `接着「${name}」这件事` : '接着之前的任务';
    return name ? `Continuing “${name}”` : 'Continuing an earlier task';
}

export const BOT_CHAT_NEAR_BOTTOM_PX = 80;

export function botChatMaxScroll(el: Pick<HTMLElement, 'scrollHeight' | 'clientHeight'>): number {
    return Math.max(0, el.scrollHeight - el.clientHeight);
}

export function botChatDistanceFromBottom(el: Pick<HTMLElement, 'scrollHeight' | 'scrollTop' | 'clientHeight'>): number {
    return el.scrollHeight - el.scrollTop - el.clientHeight;
}

export function botChatIsNearBottom(
    el: Pick<HTMLElement, 'scrollHeight' | 'scrollTop' | 'clientHeight'>,
    slack = BOT_CHAT_NEAR_BOTTOM_PX,
): boolean {
    return botChatDistanceFromBottom(el) <= slack;
}

/** Move the log to the latest line. Already-there is a no-op so a pin does not retrigger scroll. */
export function pinBotChatToBottom(el: HTMLElement | null): boolean {
    if (!el) return false;
    const top = botChatMaxScroll(el);
    if (Math.abs(el.scrollTop - top) <= 1) return false;
    el.scrollTop = top;
    return true;
}

export type BotChatStampMessage = {
    id: string;
    content?: string;
    pending?: boolean;
    failed?: boolean;
    phase?: string;
    shotPaths?: readonly unknown[];
    images?: readonly unknown[];
    files?: readonly unknown[];
    localPaths?: readonly unknown[];
    askResolved?: boolean;
    userControl?: boolean;
    attentionReason?: string;
    askUser?: { options?: readonly string[]; secretName?: string };
};

function textHash(value: string): string {
    let hash = 2166136261;
    for (let i = 0; i < value.length; i++) hash = Math.imul(hash ^ value.charCodeAt(i), 16777619);
    return (hash >>> 0).toString(36);
}

// Message objects are replaced, not edited. The cache skips rehashing the
// bubbles a commit left untouched.
const messageStampCache = new WeakMap<BotChatStampMessage, string>();

function messageStamp(message: BotChatStampMessage): string {
    const cached = messageStampCache.get(message);
    if (cached !== undefined) return cached;
    const content = message.content || '';
    const options = message.askUser?.options?.join('\u0002') || '';
    const stamp = [
        message.id,
        message.pending ? 1 : 0,
        message.failed ? 1 : 0,
        message.phase || '',
        content.length,
        textHash(content),
        message.shotPaths?.length || 0,
        message.images?.length || 0,
        message.files?.length || 0,
        message.localPaths?.length || 0,
        message.askResolved ? 1 : 0,
        message.userControl ? 1 : 0,
        message.attentionReason || '',
        options ? textHash(options) : '',
        message.askUser?.secretName || '',
    ].join('\u0001');
    messageStampCache.set(message, stamp);
    return stamp;
}

/** Identity of the visible transcript. Same text and attachments keep the same stamp. */
export function botChatLogStamp(messages: readonly BotChatStampMessage[]): string {
    let stamp = '';
    for (const message of messages) stamp += messageStamp(message) + '\n';
    return stamp;
}

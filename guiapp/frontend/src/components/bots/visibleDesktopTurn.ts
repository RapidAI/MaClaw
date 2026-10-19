// What one desktop-bot run shows a person.
// The sentence is send_to_user. Assistant prose and the thinking channel are
// not a bubble and not a thinking panel. A missing call does not invent one.

const NAMED_FILLERS = new Set([
    '后台没有返回结果。',
    '後台沒有返回結果。',
    'The backend returned no result.',
    'MaClawSrv 没有返回结果',
    'run ended without a result',
    '无变化',
    '(无输出)',
    '（无输出）',
]);

const EMPTY_RUN_FILLER = /^run \S+ ended as \S+ without a result$/;

// Same working-row rule the desktop page already uses. Not a new verb list.
const ACTIVITY_PHRASE = /^正在[\u4e00-\u9fff]{2,6}$/;
const ACTIVITY_BLOCKED = new Set(['正在处理', '正在一点点推进', '正在询问']);

export type VisibleDesktopInput = {
    prose?: string;
    thinking?: string;
    sendToUser?: string;
    error?: string;
    waiting?: boolean;
    activity?: string;
};

export type VisibleDesktopView = {
    sentence: string;
    bubble: boolean;
    failed: boolean;
    arrange: boolean;
    activity: string;
    notify: boolean;
};

export function isDesktopBotFiller(text: string): boolean {
    const value = (text || '').trim();
    if (!value) return false;
    if (NAMED_FILLERS.has(value)) return true;
    if (value.toLowerCase() === 'no change') return true;
    return EMPTY_RUN_FILLER.test(value);
}

// A closed 「正在…」 phrase for the working row. Model thinking is not one.
export function visibleActivityPhrase(raw: string): string {
    const text = (raw || '').trim();
    if (!ACTIVITY_PHRASE.test(text) || ACTIVITY_BLOCKED.has(text)) return '';
    return text;
}

// visibleDesktopTurn is the render decision for a finished desktop-bot run.
// prose, thinking, and waiting do not become the sentence. arrange is false:
// planning prose is not a confirm button. notify is false: the working row
// is not a phone notification.
export function visibleDesktopTurn(input: VisibleDesktopInput): VisibleDesktopView {
    const send = (input.sendToUser || '').trim();
    const sentence = send && !isDesktopBotFiller(send) ? send : '';
    const error = (input.error || '').trim();
    const failed = sentence === '' && error !== '' && !isDesktopBotFiller(error);
    return {
        sentence: failed ? error : sentence,
        bubble: sentence !== '' || failed,
        failed,
        arrange: false,
        activity: visibleActivityPhrase(input.activity || ''),
        notify: false,
    };
}

// takeAssistantStreamToken drops a desktop-bot token before it can become a
// chat bubble or a thinking panel. Other chats keep their stream.
export function takeAssistantStreamToken(requestId: string, delta: string): { text: string; thinking: boolean } | null {
    if ((requestId || '').startsWith('desktop-bot-')) return null;
    const text = delta || '';
    return { text, thinking: text.startsWith('\x01') };
}

export function desktopTurnFromPayload(payload: Record<string, unknown>): VisibleDesktopView {
    return visibleDesktopTurn({
        prose: String(payload.text || payload.Text || ''),
        thinking: String(payload.reasoning || payload.Reasoning || ''),
        sendToUser: String(payload.send_to_user ?? payload.SendToUser ?? ''),
        error: String(payload.error || payload.Error || ''),
        waiting: payload.waiting === true || payload.Waiting === true,
        activity: String(payload.activity || payload.Activity || ''),
    });
}

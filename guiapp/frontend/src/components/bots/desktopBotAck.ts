import { WriteDesktopBotAck } from '../../../wailsjs/go/main/App';

export type DesktopBotAckKind = 'accept' | 'supplement' | 'files' | 'stopping' | 'stopped' | 'idle' | 'pause';

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
}

export type DesktopBotAckWriter = (ctx: DesktopBotAckContext) => Promise<string>;

// The one line shown only when every model attempt failed. No other stock
// sentence exists: everything else is written by the model.
export function desktopBotNeutralAck(ctx: Pick<DesktopBotAckContext, 'kind' | 'lang' | 'files'>): string {
    const en = String(ctx.lang || '').toLowerCase().startsWith('en');
    const files = ctx.kind !== 'accept' || (ctx.files?.length ?? 0) > 0;
    if (en) return files ? 'Got it.' : "OK, I'm on it.";
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
    const value = String(lang || '');
    if (value.toLowerCase().startsWith('en')) return 'Still stopping.';
    if (value.includes('Hant')) return '還在停。';
    return '还在停。';
}

// desktopBotStoppingLine writes the "still stopping" update in the bot's own
// voice. It never rejects.
export async function desktopBotStoppingLine(ctx: { lang: string; message?: string; persona?: string; running_task?: string }, write: DesktopBotAckWriter = WriteDesktopBotAck): Promise<string> {
    try {
        const line = String(await write({ kind: 'stopping', message: ctx.message || '', lang: ctx.lang, persona: ctx.persona, running_task: ctx.running_task }) || '').trim();
        if (line) return line;
    } catch {
        // falls through to the minimal line
    }
    return desktopBotStoppingFallback(ctx.lang);
}

// desktopBotLine writes a stop-path line (stopped, idle, pause) in the bot's
// voice; fallback is used only when every model attempt failed.
export async function desktopBotLine(kind: 'stopped' | 'idle' | 'pause', ctx: { lang: string; message?: string; persona?: string; running_task?: string }, fallback: string, write: DesktopBotAckWriter = WriteDesktopBotAck): Promise<string> {
    try {
        const line = String(await write({ kind, message: ctx.message || '', lang: ctx.lang, persona: ctx.persona, running_task: ctx.running_task }) || '').trim();
        if (line) return line;
    } catch {
        // falls through
    }
    return fallback;
}

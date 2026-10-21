import { WriteDesktopBotAck } from '../../../wailsjs/go/main/App';

export type DesktopBotAckKind = 'accept' | 'supplement' | 'files';

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

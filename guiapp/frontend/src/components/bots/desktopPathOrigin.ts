export type BotPathKind = 'local' | 'desktop' | 'text';

const DESKTOP_HOME = '/home/desktop';

// resolveDesktopPath is the same resolution as the desktop ContainerPath:
// ~/ and a relative path start at the desktop home. A backslash, a colon,
// or a newline is not a container path.
export function resolveDesktopPath(raw: string): string | null {
    let path = String(raw || '').trim();
    if (!path || /[\r\n\0\\:]/.test(path)) return null;
    if (path === '~') path = DESKTOP_HOME;
    else if (path.startsWith('~/')) path = `${DESKTOP_HOME}/${path.slice(2)}`;
    else if (!path.startsWith('/')) path = `${DESKTOP_HOME}/${path}`;
    const parts: string[] = [];
    for (const part of path.split('/')) {
        if (part === '' || part === '.') continue;
        if (part === '..') parts.pop();
        else parts.push(part);
    }
    const cleaned = `/${parts.join('/')}`;
    if (cleaned.length > 4096) return null;
    return cleaned;
}

// classifyBotPath decides what a path chip may do. A desktop-run message
// marks the origin. A path that resolves inside that desktop's home is
// fetched from the desktop. Anything else on that message stays a local
// file. A path that is not a path at all is plain text.
export function classifyBotPath(raw: string, fromDesktopRun: boolean): BotPathKind {
    const resolved = resolveDesktopPath(raw);
    if (!resolved) {
        if (/^[A-Za-z]:[\\/]/.test(raw) || raw.startsWith('\\\\')) return 'local';
        return 'text';
    }
    const onDesktop = resolved === DESKTOP_HOME || resolved.startsWith(`${DESKTOP_HOME}/`);
    if (fromDesktopRun && onDesktop) return 'desktop';
    return 'local';
}

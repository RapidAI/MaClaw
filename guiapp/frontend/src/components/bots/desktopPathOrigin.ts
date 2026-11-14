export type BotPathKind = 'local' | 'desktop' | 'text';

const DESKTOP_HOME = '/home/desktop';

export type BotPathSource = {
    // guiPaths are files this GUI wrote. Only those open on this machine.
    guiPaths?: readonly string[];
};

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

function guiWrote(raw: string, source?: BotPathSource): boolean {
    const want = String(raw || '').trim();
    return (source?.guiPaths || []).some(item => String(item || '').trim() === want);
}

// A fetchable desktop file is under the run work tree or the Desktop.
// A hidden directory is not fetched.
function fetchableDesktopPath(resolved: string): boolean {
    const roots = [`${DESKTOP_HOME}/work`, `${DESKTOP_HOME}/Desktop`, `${DESKTOP_HOME}/桌面`];
    const under = roots.some(root => resolved === root || resolved.startsWith(`${root}/`));
    if (!under) return false;
    return !resolved.split('/').some(part => part.startsWith('.') && part !== '.' && part !== '..');
}

// classifyBotPath decides what a path chip may do. A path this GUI wrote
// opens on this machine. Anything else that is a deliverable desktop path
// is fetched through Hub. Every other path is plain text.
export function classifyBotPath(raw: string, source?: BotPathSource): BotPathKind {
    if (guiWrote(raw, source)) return 'local';
    const resolved = resolveDesktopPath(raw);
    if (resolved && fetchableDesktopPath(resolved)) return 'desktop';
    return 'text';
}

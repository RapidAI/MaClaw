export type DesktopMode = 'shared' | 'isolated' | 'none';

export type BotScreen = {
    url: string;
    mode: DesktopMode;
};

export function desktopModeOf(mode: string | undefined): DesktopMode {
    if (mode === 'isolated') return 'isolated';
    if (mode === 'none') return 'none';
    return 'shared';
}

// The bot list arrives before any screen watch. Until a watch names the
// display, the saved mode is that bot's screen. A watch that has already
// named one stays: it is the newer read.
export function withSavedScreenMode<T extends { url?: string; mode?: DesktopMode }>(
    bots: Array<{ id: string; desktopMode?: string }>,
    screens: Record<string, T | undefined>,
): Record<string, T> {
    const out: Record<string, T> = {};
    for (const id of Object.keys(screens)) {
        const screen = screens[id];
        if (screen) out[id] = screen;
    }
    for (const bot of bots) {
        if (!bot.desktopMode) continue;
        const saved = desktopModeOf(bot.desktopMode);
        const known = out[bot.id];
        if (known?.mode === saved) continue;
        // A named watch is the newer read. A bot with no computer is not
        // that shared display, even if an older watch said it was.
        if (known?.mode && saved !== 'none') continue;
        out[bot.id] = { ...(known || { url: '' }), mode: saved } as T;
    }
    return out;
}

// Shared bots use the owner's one display. Switching between them must keep
// the keyboard. An isolated bot, or a bot with no desktop, is a different screen.
export function sameDesktopScreen(a: string, b: string, screens: Record<string, { mode?: DesktopMode } | undefined>): boolean {
    if (!a || !b) return false;
    if (a === b) return true;
    const mode = (id: string) => screens[id]?.mode;
    if (mode(a) === 'isolated' || mode(b) === 'isolated' || mode(a) === 'none' || mode(b) === 'none') return false;
    return true;
}

// A shared bot's picture is the owner's desktop. The first one with a URL
// is enough: every shared bot is that same display.
export function mineScreenURL(bots: Array<{ id: string }>, screens: Record<string, BotScreen | undefined>): string {
    for (const bot of bots) {
        const screen = screens[bot.id];
        if (screen && screen.mode === 'shared' && screen.url) return screen.url;
    }
    return '';
}

// The open picture of another shared bot keeps the iframe mounted. An
// isolated bot has its own display, so that picture is the wrong screen.
export function screenPictureFallback(mode: DesktopMode | undefined, ownLive: string, borrowed: string): string {
    if (mode === 'isolated') return ownLive || '';
    return ownLive || borrowed || '';
}

// mine and the chat bot fall back to the picture the watch hold already
// opened. Another bot never borrows that picture. An isolated chat bot's
// picture is not the owner's desktop, so "my desktop" stays empty until a
// shared bot has one.
export function shownScreenURL(
    pick: string,
    selectedId: string,
    bots: Array<{ id: string }>,
    screens: Record<string, BotScreen | undefined>,
    fallbackURL: string,
): string {
    if (pick === 'mine') {
        const mine = mineScreenURL(bots, screens);
        if (mine) return mine;
        if (screens[selectedId]?.mode === 'isolated') return '';
        return fallbackURL;
    }
    const own = screens[pick]?.url || '';
    if (pick === selectedId) return own || fallbackURL;
    return own;
}

// An isolated bot's computer opens on its own display until the person
// picks a different screen.
export function effectiveScreenPick(touched: boolean, pick: string, selectedId: string, mode: DesktopMode | undefined): string {
    if (!touched && selectedId && mode === 'isolated') return selectedId;
    return pick || 'mine';
}

// Hub keeps one watch generation per display. The chat bot's poll already
// holds its display, and every shared bot is that same owner display, so a
// second epoch there drops the first. A different display still needs its
// own hold: GET does not start noVNC.
export function screenNeedsOwnHold(
    pick: string,
    selectedId: string,
    screens: Record<string, BotScreen | undefined>,
    bots: Array<{ id: string }>,
): string {
    if (!selectedId || !pick || pick === selectedId) return '';
    const selectedIsolated = screens[selectedId]?.mode === 'isolated';
    if (pick === 'mine') {
        if (!selectedIsolated) return '';
        for (const bot of bots) {
            if (screens[bot.id]?.mode === 'shared') return bot.id;
        }
        return '';
    }
    if (screens[pick]?.mode !== 'isolated' && !selectedIsolated) return '';
    return pick;
}

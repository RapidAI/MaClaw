export type DesktopMode = 'shared' | 'isolated';

export type BotScreen = {
    url: string;
    mode: DesktopMode;
};

export function desktopModeOf(mode: string | undefined): DesktopMode {
    return mode === 'isolated' ? 'isolated' : 'shared';
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

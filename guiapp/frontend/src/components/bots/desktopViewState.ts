// Typed desktop picture. The viewer shows a frame only for state running
// with a page. It does not invent that state from a leftover noVNC URL.
// asleep: the container is stopped. display_stopped: the supervisor
// idle-stopped the display. unavailable: the state read failed (inspect
// or transport); that is not a stopped desktop.

export type DesktopRunState = 'running' | 'asleep' | 'display_stopped' | 'unavailable';

export const DESKTOP_ASLEEP = 'DESKTOP_ASLEEP';

export type DesktopWatchView = {
    desktop_state?: string;
    novnc_url?: string;
    code?: string;
};

export function pictureFromWatch(view?: DesktopWatchView | null): { state: DesktopRunState | ''; url: string } {
    // The code is a field on the watch result. The message text is not a signal.
    if (String(view?.code || '').trim() === DESKTOP_ASLEEP) return { state: 'asleep', url: '' };
    const url = String(view?.novnc_url || '').trim();
    switch (String(view?.desktop_state || '').trim()) {
        case 'running':
            return { state: 'running', url };
        case 'asleep':
            return { state: 'asleep', url: '' };
        case 'display_stopped':
            return { state: 'display_stopped', url: '' };
        case 'unavailable':
            return { state: 'unavailable', url: '' };
        default:
            return { state: '', url: '' };
    }
}

export function showsLiveFrame(state: DesktopRunState | '', url: string): boolean {
    return state === 'running' && url !== '';
}

// A poll carries the picture generation it started with. A wake bumps the
// generation, so a poll that started earlier must not unmount the frame the
// wake just mounted. display_stopped on the wake's own generation is the
// display still starting, not a stopped screen.
export function applyWatchResult(pollSeq: number, currentSeq: number, state: DesktopRunState | '', wakeArm: number | undefined): 'ignore' | 'starting' | 'apply' {
    if (pollSeq !== currentSeq) return 'ignore';
    if (state === 'display_stopped' && wakeArm !== undefined && wakeArm === currentSeq) return 'starting';
    return 'apply';
}

// Only the H.264 player posts this, and only from its own iframe. noVNC does not.
export function isH264Disconnect(data: unknown): boolean {
    if (!data || typeof data !== 'object') return false;
    const event = data as { source?: unknown; event?: unknown };
    return event.source === 'maclaw-desktop' && event.event === 'disconnect';
}

// The bot id of the frame, when this message came from that frame. Anything
// else is ignored so a foreign postMessage cannot kick the watch.
export function disconnectBot(frame: Window | null, source: MessageEventSource | null, data: unknown, frameBotId: string): string {
    if (!frame || source !== frame || !frameBotId) return '';
    if (!isH264Disconnect(data)) return '';
    return frameBotId;
}

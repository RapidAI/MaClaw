// Typed desktop picture. running is the only state that may show a frame.
// asleep: the container is stopped. display_stopped: the supervisor
// idle-stopped the display. A remembered noVNC URL is not a picture in
// either case.

export type DesktopRunState = 'running' | 'asleep' | 'display_stopped';

export const DESKTOP_ASLEEP = 'DESKTOP_ASLEEP';

export function pictureFromWatch(view?: { desktop_state?: string; novnc_url?: string } | null): { state: DesktopRunState; url: string } {
    const url = String(view?.novnc_url || '').trim();
    const raw = String(view?.desktop_state || '').trim();
    if (raw === 'asleep' || raw === 'display_stopped') return { state: raw, url: '' };
    if (raw === 'running') return url ? { state: 'running', url } : { state: 'asleep', url: '' };
    if (!url) return { state: 'asleep', url: '' };
    return { state: 'running', url };
}

export function showsLiveFrame(state: DesktopRunState, url: string): boolean {
    return state === 'running' && url !== '';
}

// A 409 DESKTOP_ASLEEP arrives as that code. Match the code, not a sentence.
export function stateFromFailure(error: unknown): DesktopRunState | '' {
    if (error && typeof error === 'object' && 'code' in error && (error as { code?: unknown }).code === DESKTOP_ASLEEP) {
        return 'asleep';
    }
    const text = error instanceof Error ? error.message : String(error ?? '');
    const token = text.trim();
    if (token === DESKTOP_ASLEEP || token.split(/[\s:]+/).includes(DESKTOP_ASLEEP)) return 'asleep';
    return '';
}

// The H.264 player posts this when its WebSocket closes. noVNC can post the
// same shape. A disconnect is display_stopped until the next watch says
// the screen is running again.
export function stateFromStreamEvent(data: unknown): DesktopRunState | '' {
    if (!data || typeof data !== 'object') return '';
    const event = data as { source?: unknown; event?: unknown };
    if (event.source !== 'maclaw-desktop') return '';
    if (event.event === 'disconnect' || event.event === 'stream-end') return 'display_stopped';
    return '';
}

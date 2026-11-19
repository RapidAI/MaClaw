// Panel switch for the viewer contract. Unit tests leave this false so the
// existing raw-event listeners stay the only writers. App startup sets it
// from App.ViewerReducerEnabled, which is on unless MACLAW_VIEWER_REDUCER=0.

let enabled = false;

export function isViewerReducerEnabled(): boolean {
    return enabled;
}

export function setViewerReducerEnabled(value: boolean): void {
    enabled = !!value;
}

export function viewerAdapted(payload: unknown): boolean {
    return !!payload && typeof payload === 'object' && (payload as { __viewerAdapted?: boolean }).__viewerAdapted === true;
}

/** Raw ai-assistant-* events are ignored once the panel owns viewer-event. */
export function viewerOwnsRaw(payload: unknown): boolean {
    return isViewerReducerEnabled() && !viewerAdapted(payload);
}

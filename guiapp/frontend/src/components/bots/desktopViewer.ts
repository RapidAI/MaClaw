// Which player the desktop stage frames: the H.264 player served by the
// desktop's gate (h264.html, WebCodecs, takes input during takeover) or the
// classic noVNC page. H.264 is the default; the gate itself sends the frame
// back to noVNC when the desktop has the stream off, and Hub does the same
// for a desktop too old to know h264.html.

export type DesktopViewer = 'h264' | 'vnc';

export const DESKTOP_VIEWER_KEY = 'maclaw.desktop.viewer';

export function loadDesktopViewer(): DesktopViewer {
    try {
        return globalThis.localStorage?.getItem(DESKTOP_VIEWER_KEY) === 'vnc' ? 'vnc' : 'h264';
    } catch {
        return 'h264';
    }
}

export function saveDesktopViewer(viewer: DesktopViewer): void {
    try {
        globalThis.localStorage?.setItem(DESKTOP_VIEWER_KEY, viewer);
    } catch {
        // storage blocked: the choice lasts for this page only
    }
}

// h264FrameSrc turns a noVNC page URL (already carrying view_only) into the
// sibling h264.html with the same view_only, and the noVNC page as the
// player's fallback in ?vnc=. A URL that is not a vnc.html page is kept.
export function h264FrameSrc(vncSrc: string): string {
    if (!vncSrc) return '';
    const hashAt = vncSrc.indexOf('#');
    const base = hashAt >= 0 ? vncSrc.slice(0, hashAt) : vncSrc;
    const queryAt = base.indexOf('?');
    const path = queryAt >= 0 ? base.slice(0, queryAt) : base;
    const query = queryAt >= 0 ? base.slice(queryAt + 1) : '';
    const slash = path.lastIndexOf('/');
    if (path.slice(slash + 1) !== 'vnc.html') return vncSrc;
    const viewOnly = query.split('&').find(part => part.startsWith('view_only=')) || 'view_only=1';
    const fallback = 'vnc.html' + (query ? '?' + query : '');
    return `${path.slice(0, slash + 1)}h264.html?${viewOnly}&vnc=${encodeURIComponent(fallback)}`;
}

export function desktopViewerSrc(vncSrc: string, viewer: DesktopViewer): string {
    return viewer === 'h264' ? h264FrameSrc(vncSrc) : vncSrc;
}

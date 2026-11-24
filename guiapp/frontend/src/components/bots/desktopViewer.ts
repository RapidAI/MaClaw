// Which player the desktop stage frames: the H.264 player served by the
// desktop's gate (h264.html, WebCodecs, takes input during takeover) or the
// classic noVNC page. H.264 is the default; the gate itself sends the frame
// back to noVNC when the desktop has the stream off, and Hub does the same
// for a desktop too old to know h264.html.

export type DesktopViewer = 'h264' | 'vnc';

// w/fps ride the h264.html query straight onto the /h264 socket. Encoding at
// the stage's pixel size instead of the remote's native resolution cuts the
// decoded frame + canvas cost in the iframe's own renderer — a 1440p-class
// feed grew that renderer ~900MB in minutes and it died to the OOM page.
// The gate only lets fps go below its env default, never above.
export type DesktopStreamSpec = { w?: number; fps?: number };

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
export function h264FrameSrc(vncSrc: string, stream?: DesktopStreamSpec): string {
    if (!vncSrc) return '';
    const hashAt = vncSrc.indexOf('#');
    const base = hashAt >= 0 ? vncSrc.slice(0, hashAt) : vncSrc;
    const hash = hashAt >= 0 ? vncSrc.slice(hashAt) : '';
    const queryAt = base.indexOf('?');
    const path = queryAt >= 0 ? base.slice(0, queryAt) : base;
    const query = queryAt >= 0 ? base.slice(queryAt + 1) : '';
    const slash = path.lastIndexOf('/');
    const page = path.slice(slash + 1);
    if (page === 'h264.html') {
        // Already the player: a spec the caller still wants is appended to
        // the query, before any #hash. A w/fps already present wins (the
        // page reads the first match).
        let extra = '';
        let sep = query ? '&' : '?';
        if (stream?.w && stream.w > 0) { extra += `${sep}w=${Math.round(stream.w)}`; sep = '&'; }
        if (stream?.fps && stream.fps > 0) { extra += `${sep}fps=${Math.round(stream.fps)}`; }
        return base + extra + hash;
    }
    if (page !== 'vnc.html') return vncSrc;
    const viewOnly = query.split('&').find(part => part.startsWith('view_only=')) || 'view_only=1';
    const fallback = 'vnc.html' + (query ? '?' + query : '');
    let out = `${path.slice(0, slash + 1)}h264.html?${viewOnly}&vnc=${encodeURIComponent(fallback)}`;
    if (stream?.w && stream.w > 0) out += `&w=${Math.round(stream.w)}`;
    if (stream?.fps && stream.fps > 0) out += `&fps=${Math.round(stream.fps)}`;
    return out;
}

export function desktopViewerSrc(vncSrc: string, viewer: DesktopViewer, stream?: DesktopStreamSpec): string {
    return viewer === 'h264' ? h264FrameSrc(vncSrc, stream) : vncSrc;
}

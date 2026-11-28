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

// desktopStreamWidthForSurface converts a physical surface width to the
// encoder width. H.264 dimensions need to be even; the cap avoids restarting
// into a needlessly expensive encode when the stage is maximized on a large
// monitor. Zero means the caller has no trustworthy surface width.
export function desktopStreamWidthForSurface(width: number): number {
    const rounded = Math.round(width);
    if (!Number.isFinite(rounded) || rounded <= 0) return 0;
    const capped = Math.max(640, Math.min(1920, rounded));
    return capped - (capped % 2);
}

// desktopStreamWidthOverride replaces the outer h264.html w= parameter in
// place. The first w wins inside the player, so appending another one would
// leave the narrow embedded-panel encode active in the native window. URLs
// without an existing w (notably noVNC) are unchanged.
export function desktopStreamWidthOverride(src: string, width: number): string {
    const w = Math.round(width);
    if (!src || !Number.isFinite(w) || w <= 0) return src;
    const hashAt = src.indexOf('#');
    const base = hashAt >= 0 ? src.slice(0, hashAt) : src;
    const hash = hashAt >= 0 ? src.slice(hashAt) : '';
    const queryAt = base.indexOf('?');
    if (queryAt < 0) return src;
    const path = base.slice(0, queryAt);
    const parts = base.slice(queryAt + 1).split('&');
    let replaced = false;
    const next = parts.map(part => {
        if (!replaced && (part === 'w' || part.startsWith('w='))) {
            replaced = true;
            return `w=${w}`;
        }
        return part;
    });
    if (!replaced) return src;
    return `${path}?${next.join('&')}${hash}`;
}

export function desktopViewerSrc(vncSrc: string, viewer: DesktopViewer, stream?: DesktopStreamSpec): string {
    return viewer === 'h264' ? h264FrameSrc(vncSrc, stream) : vncSrc;
}

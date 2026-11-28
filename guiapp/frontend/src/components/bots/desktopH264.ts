// Pure helpers for the in-page H.264 desktop player (DesktopBotH264Player).
// These mirror the wire protocol of the gate's h264.html page
// (desktopd/image/desktop_supervisor.py H264_PLAYER) so the player logic can
// run inside the app document: under WebView2 composition hosting the
// cross-origin iframe's OOPIF dies on layout transitions and never
// respawns, so the player cannot live in an iframe at all.

export type H264VideoHeader = { key?: boolean; codec?: string; ts?: number; seq?: number };

export type H264PageParams = {
    wantInput: boolean;
    vnc: string;
    mse: boolean;
    w: number;
    fps: number;
};

// h264PageParams reads the player's own query off an h264.html URL. A URL
// that cannot be parsed behaves like a plain watch page.
export function h264PageParams(pageURL: string): H264PageParams {
    try {
        const q = new URL(pageURL).searchParams;
        return {
            wantInput: q.get('view_only') === '0',
            vnc: q.get('vnc') || '',
            mse: q.get('mse') === '1',
            w: Math.max(0, parseInt(q.get('w') || '0', 10) || 0),
            fps: Math.max(0, parseInt(q.get('fps') || '0', 10) || 0),
        };
    } catch {
        return { wantInput: false, vnc: '', mse: false, w: 0, fps: 0 };
    }
}

// h264SocketURL derives the video WebSocket URL from the player page URL:
// same directory, /h264.html -> /h264, http(s) -> ws(s). Only the stream
// parameters ride the socket (mse/input/w/fps); view_only and the noVNC
// fallback belong to the page, matching the iframe player's behaviour.
export function h264SocketURL(pageURL: string, useMSE: boolean, wantInput: boolean, widthOverride?: number): string {
    const u = new URL(pageURL);
    u.protocol = u.protocol === 'https:' || u.protocol === 'wss:' ? 'wss:' : 'ws:';
    u.pathname = /h264\.html$/.test(u.pathname)
        ? u.pathname.replace(/h264\.html$/, 'h264')
        : u.pathname.replace(/[^/]*$/, 'h264');
    u.hash = '';
    const q = u.searchParams;
    const out = new URLSearchParams();
    if (useMSE) out.set('mse', '1');
    if (wantInput) out.set('input', '1');
    const w = widthOverride && widthOverride > 0 ? Math.round(widthOverride) : parseInt(q.get('w') || '0', 10) || 0;
    if (w > 0) out.set('w', String(w - (w % 2)));
    const fps = parseInt(q.get('fps') || '0', 10) || 0;
    if (fps > 0) out.set('fps', String(fps));
    u.search = out.toString();
    return u.toString();
}

// h264VideoMessage splits the binary frame envelope: uint32 header length,
// uint32 payload length, JSON header, Annex B payload.
export function h264VideoMessage(buf: ArrayBuffer): { h: H264VideoHeader; data: Uint8Array } {
    const dv = new DataView(buf);
    const hl = dv.getUint32(0);
    const pl = dv.getUint32(4);
    const h = JSON.parse(new TextDecoder().decode(new Uint8Array(buf, 8, hl))) as H264VideoHeader;
    return { h, data: new Uint8Array(buf, 8 + hl, pl) };
}

// h264SurfacePoint maps a client coordinate onto normalized video space for
// an object-fit:contain surface (letterboxing included).
export function h264SurfacePoint(
    rect: { left: number; top: number; width: number; height: number },
    videoW: number,
    videoH: number,
    clientX: number,
    clientY: number,
): { x: number; y: number } {
    const w = videoW || 1;
    const h = videoH || 1;
    const s = Math.min(rect.width / w, rect.height / h) || 1;
    const ox = (rect.width - w * s) / 2;
    const oy = (rect.height - h * s) / 2;
    return {
        x: Math.min(1, Math.max(0, (clientX - rect.left - ox) / (w * s))),
        y: Math.min(1, Math.max(0, (clientY - rect.top - oy) / (h * s))),
    };
}

export const H264_BUTTONS: Record<number, number> = { 0: 1, 1: 2, 2: 3, 3: 8, 4: 9 };

export const H264_IS_MAC = /Mac|iPhone|iPad/.test(typeof navigator !== 'undefined' ? navigator.platform || '' : '');

export function h264MapCode(code: string, isMac: boolean): string {
    if (isMac && code === 'MetaLeft') return 'ControlLeft';
    if (isMac && code === 'MetaRight') return 'ControlRight';
    return code;
}

import { useEffect, useMemo, useRef, useState } from 'react';
import { LogFrontendDiagnostic } from '../../../wailsjs/go/main/App';
import {
    H264_BUTTONS,
    H264_IS_MAC,
    h264MapCode,
    h264PageParams,
    h264SocketURL,
    h264SurfacePoint,
    h264VideoMessage,
} from './desktopH264';

// DesktopBotH264Player renders the desktop H.264 stream inside the app
// document — same wire protocol as the gate's h264.html page, but without
// an iframe. Under WebView2 composition hosting (RDP) the cross-origin
// player OOPIF dies on layout transitions (fullscreen/takeover) and
// subsequent OOPIFs never run script again; an in-document player keeps
// its browsing context, its decoder and its compositing layer alive
// through every stage transition.
type Props = {
    // The h264.html URL — the handoff token rides its path and the stream
    // parameters (view_only/w/fps/mse/vnc) ride its query, exactly like the
    // iframe src did.
    url: string;
    // Encoder width target for live renegotiation. Changing it reconnects
    // the video socket only — no remount, no layout transition.
    streamWidth: number;
    // Every video-socket close (like the iframe player's "disconnect"
    // postMessage); the parent re-checks the watch on it.
    onDisconnect?: () => void;
    // The gate reports the display is not running (hello/display_state
    // stopped): the parent can swap the stage for its asleep view at once
    // instead of leaving the player's "not running" text up until the
    // next watch poll lands.
    onStopped?: () => void;
    // No WebCodecs and no MSE: the caller swaps to the noVNC fallback page.
    onUnsupported?: (vncURL: string) => void;
};

type Badge = { text: string; cls: string };

export function DesktopBotH264Player({ url, streamWidth, onDisconnect, onStopped, onUnsupported }: Props) {
    const surfaceRef = useRef<HTMLCanvasElement | HTMLVideoElement | null>(null);
    const [statusText, setStatusText] = useState('连接中…');
    const [badge, setBadge] = useState<Badge>({ text: '连接中…', cls: '' });
    const renegotiateRef = useRef<(w: number) => void>(() => undefined);
    const disconnectRef = useRef(onDisconnect);
    disconnectRef.current = onDisconnect;
    const unsupportedRef = useRef(onUnsupported);
    unsupportedRef.current = onUnsupported;
    const stoppedCbRef = useRef(onStopped);
    stoppedCbRef.current = onStopped;

    const params = useMemo(() => h264PageParams(url), [url]);
    const hasWC = typeof VideoDecoder !== 'undefined' && typeof EncodedVideoChunk !== 'undefined';
    const hasMSE = typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported('video/mp4; codecs="avc1.42E029"');
    // ?mse=1 opts out of the canvas/WebCodecs path: on surfaces where
    // drawImage(VideoFrame) dithers, the <video> element composites in
    // hardware. Same rule as the iframe player.
    const useMSE = hasMSE && (params.mse || !hasWC);

    useEffect(() => {
        if (!hasWC && !hasMSE) {
            try {
                void Promise.resolve(LogFrontendDiagnostic({ tag: 'desktop-h264', ev: 'fallback', reason: 'no-decoder' })).catch(() => undefined);
            } catch { /* diagnostics never block playback */ }
            if (unsupportedRef.current) unsupportedRef.current(params.vnc);
            return;
        }
        const wantInput = params.wantInput;
        const surface = surfaceRef.current as HTMLElement | null;
        if (!surface) return;

        let ws: WebSocket | null = null;
        let decoder: VideoDecoder | null = null;
        let needKey = true;
        let lastKeyReq = 0;
        let fails = 0;
        let gotFrame = false;
        let lastRender = 0;
        let connectAt = performance.now();
        let streamStopped = false;
        let renegotiating = false;
        let closed = false;
        let streamW = params.w;
        let seq = Math.floor(Math.random() * 1e6);
        let pending: Array<Record<string, unknown>> = [];
        let flushQueued = false;
        let inputOn = false;
        let lease = '';
        const held = new Set<string>();
        let pasteHold: { code: string; timer: number } | null = null;
        const wheelAcc = [0, 0];
        let ms: MediaSource | null = null;
        let sb: SourceBuffer | null = null;
        let mq: ArrayBuffer[] = [];
        let msReady = false;

        // A black panel tells us nothing; these reach maclaw.log so a dead
        // stream shows WHICH leg failed — connect refused, handshake closed,
        // or frames simply never arrived on an open socket.
        const diag = (ev: string, fields?: Record<string, unknown>) => {
            try {
                void Promise.resolve(LogFrontendDiagnostic(Object.assign({ tag: 'desktop-h264', ev }, fields))).catch(() => undefined);
            } catch { /* diagnostics never block playback */ }
        };
        const status = (text: string) => setStatusText(text);
        const markRender = () => {
            if (!gotFrame) diag('frame', { mse: useMSE });
            gotFrame = true;
            fails = 0;
            lastRender = performance.now();
            status('');
        };
        const send = (type: string, payload?: Record<string, unknown>) => {
            if (ws && ws.readyState === 1) ws.send(JSON.stringify(Object.assign({ type }, payload)));
        };
        const requestKey = (reason: string) => {
            const n = performance.now();
            if (n - lastKeyReq < 500) return;
            lastKeyReq = n;
            send('request_keyframe', { reason });
        };
        const show = () => {
            if (!ws || ws.readyState !== 1) setBadge({ text: '连接中…', cls: '' });
            else if (lease === 'you') setBadge({ text: '你在控制', cls: 'you' });
            else if (lease === 'bot') setBadge({ text: 'Bot 正在操作', cls: 'bot' });
            else if (lease === 'human') setBadge({ text: '其他人在控制', cls: '' });
            else setBadge({ text: inputOn ? '可以操作' : '仅观看', cls: '' });
        };

        const video = surface as HTMLVideoElement;
        const canvas = surface as HTMLCanvasElement;
        const ctx = useMSE ? null : canvas.getContext('2d');

        const resetDecoder = () => {
            try { if (decoder && decoder.state !== 'closed') decoder.close(); } catch { /* decoder already gone */ }
            decoder = null;
            needKey = true;
        };
        const onVideo = (buf: ArrayBuffer) => {
            const { h, data } = h264VideoMessage(buf);
            if (!decoder || decoder.state === 'closed') {
                if (!h.key) { requestKey('missing_key'); return; }
                decoder = new VideoDecoder({
                    output: (f) => {
                        try {
                            if (canvas.width !== f.displayWidth) canvas.width = f.displayWidth;
                            if (canvas.height !== f.displayHeight) canvas.height = f.displayHeight;
                            ctx?.drawImage(f, 0, 0);
                            markRender();
                        } finally { f.close(); }
                    },
                    error: () => { resetDecoder(); requestKey('decoder_error'); },
                });
                decoder.configure({ codec: h.codec || 'avc1.42C029', optimizeForLatency: true });
                needKey = true;
            }
            if (needKey && !h.key) { requestKey('missing_key'); return; }
            if (decoder.decodeQueueSize > 4 && !h.key) { needKey = true; requestKey('decoder_busy'); return; }
            if (h.key) needKey = false;
            try {
                decoder.decode(new EncodedVideoChunk({ type: h.key ? 'key' : 'delta', timestamp: h.ts || 0, data }));
            } catch { resetDecoder(); requestKey('decoder_error'); return; }
            send('frame_ack', { seq: h.seq });
        };
        const pump = () => {
            if (!sb || sb.updating || !mq.length) return;
            sb.appendBuffer(mq.shift() as BufferSource);
            const b = sb.buffered;
            if (b.length && video.currentTime < b.end(0) - 0.3) video.currentTime = b.end(0) - 0.05;
            if (b.length && b.end(0) - b.start(0) > 30 && !sb.updating) {
                try { sb.remove(b.start(0), b.end(0) - 10); } catch { /* range may still be appending */ }
            }
        };
        const setupMSE = () => {
            ms = new MediaSource();
            video.src = URL.createObjectURL(ms);
            ms.addEventListener('sourceopen', () => {
                try {
                    sb = ms!.addSourceBuffer('video/mp4; codecs="avc1.42E029"');
                    sb.mode = 'sequence';
                    sb.addEventListener('updateend', pump);
                    msReady = true;
                } catch { /* unsupported buffer type on this runtime */ }
            });
        };

        const releaseLocal = () => { held.clear(); if (pasteHold) { clearTimeout(pasteHold.timer); pasteHold = null; } };
        const queue = (ev: Record<string, unknown>) => {
            if (!inputOn) return;
            const last = pending[pending.length - 1];
            if (ev.t === 'move' && last && last.t === 'move') pending[pending.length - 1] = ev;
            else pending.push(ev);
            if (!flushQueued) { flushQueued = true; queueMicrotask(() => { flushQueued = false; if (pending.length) { send('input', { seq, events: pending }); seq += 1; pending = []; } }); }
        };
        const point = (e: { clientX: number; clientY: number }) => {
            const r = surface.getBoundingClientRect();
            const vw = useMSE ? video.videoWidth : canvas.width || 1;
            const vh = useMSE ? video.videoHeight : canvas.height || 1;
            return h264SurfacePoint(r, vw, vh, e.clientX, e.clientY);
        };

        const connect = () => {
            if (closed) return;
            connectAt = performance.now();
            gotFrame = false;
            streamStopped = false;
            // A page URL the parser rejects leaves connect() throwing inside
            // the effect; surface it as a player failure instead.
            let sockURL: string;
            try {
                sockURL = h264SocketURL(url, useMSE, wantInput, streamW || undefined);
            } catch {
                diag('fallback', { reason: 'bad-url' });
                if (unsupportedRef.current && params.vnc) { unsupportedRef.current(params.vnc); return; }
                status('桌面视频流异常，正在重连\n桌面本身可能仍在运行');
                return;
            }
            diag('connect', { mse: useMSE, input: wantInput, w: streamW, path: (() => { try { return new URL(sockURL).pathname.replace(/[0-9a-f]{16,}/g, '<t>'); } catch { return ''; } })() });
            const sock = new WebSocket(sockURL);
            ws = sock;
            sock.binaryType = 'arraybuffer';
            sock.onopen = () => diag('open');
            show();
            sock.onmessage = (e) => {
                if (typeof e.data === 'string') { control(JSON.parse(e.data)); return; }
                if (!useMSE) { onVideo(e.data as ArrayBuffer); return; }
                if (mq.length >= 8) { mq = []; try { sock.close(); } catch { /* already closing */ } return; }
                mq.push(e.data as ArrayBuffer);
                if (msReady) pump();
            };
            sock.onclose = (e?: CloseEvent) => {
                if (ws === sock) ws = null;
                mq = [];
                // code 1006 = the upgrade never completed (proxy/auth
                // rejection); 1000/1001 = the server accepted then hung up.
                diag('close', { fails, gotFrame, stopped: streamStopped, reneg: renegotiating, closed, code: e ? e.code : 0, clean: e ? e.wasClean : false });
                disconnectRef.current?.();
                resetDecoder();
                releaseLocal();
                show();
                if (closed) return;
                if (streamStopped) { status('桌面画面未运行（可能正在休眠）\n桌面任务本身可能仍在继续'); window.setTimeout(connect, 5000); return; }
                if (renegotiating) { renegotiating = false; window.setTimeout(connect, 80); return; }
                fails += 1;
                // Give transient rejections room: under RDP the vnc fallback
                // is a guaranteed-black surface, so trading away h264 after
                // only three fast closes is strictly worse than retrying.
                if (!gotFrame && fails >= 8) {
                    diag('fallback', { reason: 'fails', fails });
                    if (unsupportedRef.current && params.vnc) { unsupportedRef.current(params.vnc); return; }
                    status('桌面视频流异常，正在重连\n桌面本身可能仍在运行');
                } else {
                    status(gotFrame ? '视频连接中断，正在重连…' : '正在等待桌面画面…');
                }
                window.setTimeout(connect, Math.min(5000, 500 * fails));
            };
        };
        const noteStopped = () => {
            if (!streamStopped) stoppedCbRef.current?.();
            streamStopped = true;
            status('桌面画面未运行（可能正在休眠）\n桌面任务本身可能仍在继续');
            try { ws?.close(); } catch { /* already closing */ }
        };
        const control = (m: { type?: string; input?: boolean; display_state?: string; state?: string; holder?: string; text?: string; code?: string; expected?: number }) => {
            if (m.type === 'hello') {
                inputOn = wantInput && !!m.input;
                if (m.display_state && m.display_state !== 'running') noteStopped();
                show();
            } else if (m.type === 'display_state') {
                if (m.state && m.state !== 'running') noteStopped();
            } else if (m.type === 'lease') {
                const was = lease;
                lease = m.holder || '';
                inputOn = wantInput && !!m.input;
                if (was === 'you' && lease !== 'you') releaseLocal();
                show();
            } else if (m.type === 'clipboard' && typeof m.text === 'string') {
                if (navigator.clipboard?.writeText) navigator.clipboard.writeText(m.text).catch(() => undefined);
            } else if (m.type === 'input_error' && m.code === 'sequence_gap' && typeof m.expected === 'number') {
                seq = m.expected;
            }
        };

        renegotiateRef.current = (w: number) => {
            const width = Math.round(w || 0);
            if (width < 320 || width > 3840 || streamStopped || closed) return;
            const next = width - (width % 2);
            if (streamW === next) return;
            streamW = next;
            if (!ws) { connect(); return; }
            // readyState >= CLOSING: the onclose reconnect already scheduled
            // picks up the new width — closing again would be a no-op anyway.
            if (ws.readyState >= 2) return;
            renegotiating = true;
            try { ws.close(); } catch { /* already closing */ }
        };

        // ---- input
        const onPointerDown = (e: PointerEvent) => {
            if (!inputOn) return;
            surface.focus();
            try { surface.setPointerCapture(e.pointerId); } catch { /* capture may be unavailable */ }
            const b = H264_BUTTONS[e.button];
            if (b) queue(Object.assign({ t: 'down', b }, point(e)));
            e.preventDefault();
        };
        const onPointerMove = (e: PointerEvent) => {
            if (inputOn) queue(Object.assign({ t: 'move' }, point(e)));
        };
        const onPointerUp = (e: PointerEvent) => {
            if (!inputOn) return;
            const b = H264_BUTTONS[e.button];
            if (b) queue(Object.assign({ t: 'up', b }, point(e)));
            e.preventDefault();
        };
        const onContextMenu = (e: Event) => e.preventDefault();
        const onWheel = (e: WheelEvent) => {
            if (!inputOn) return;
            e.preventDefault();
            const k = e.deltaMode === 1 ? 33 : e.deltaMode === 2 ? 400 : 1;
            wheelAcc[0] += e.deltaX * k;
            wheelAcc[1] += e.deltaY * k;
            const dx = Math.trunc(wheelAcc[0] / 100);
            const dy = Math.trunc(wheelAcc[1] / 100);
            if (dx || dy) {
                wheelAcc[0] -= dx * 100;
                wheelAcc[1] -= dy * 100;
                queue(Object.assign({ t: 'wheel', dx, dy }, point(e)));
            }
        };
        const onKeyDown = (e: KeyboardEvent) => {
            if (!inputOn) return;
            const code = h264MapCode(e.code, H264_IS_MAC);
            if (!code) return;
            if ((e.ctrlKey || e.metaKey) && e.code === 'KeyV') {
                held.add(code);
                pasteHold = { code, timer: window.setTimeout(() => { if (pasteHold) { pasteHold = null; queue({ t: 'key', code, down: true }); } }, 300) };
                return;
            }
            e.preventDefault();
            held.add(code);
            queue({ t: 'key', code, down: true });
        };
        const onKeyUp = (e: KeyboardEvent) => {
            if (!inputOn) return;
            const code = h264MapCode(e.code, H264_IS_MAC);
            if (!code) return;
            e.preventDefault();
            if (pasteHold && pasteHold.code === code) { clearTimeout(pasteHold.timer); pasteHold = null; queue({ t: 'key', code, down: true }); }
            held.delete(code);
            queue({ t: 'key', code, down: false });
            if ((e.ctrlKey || e.metaKey || code.startsWith('Control')) && (e.code === 'KeyC' || e.code === 'KeyX')) window.setTimeout(() => send('clipboard_get'), 250);
        };
        const onPaste = (e: ClipboardEvent) => {
            if (!inputOn) return;
            const text = (e.clipboardData && e.clipboardData.getData('text/plain')) || '';
            e.preventDefault();
            if (text) send('clipboard_set', { text });
            if (pasteHold) { clearTimeout(pasteHold.timer); const code = pasteHold.code; pasteHold = null; queue({ t: 'key', code, down: true }); }
        };
        const onBlur = () => { if (held.size || pasteHold) { releaseLocal(); queue({ t: 'release' }); } };

        surface.addEventListener('pointerdown', onPointerDown);
        surface.addEventListener('pointermove', onPointerMove);
        surface.addEventListener('pointerup', onPointerUp);
        surface.addEventListener('contextmenu', onContextMenu);
        surface.addEventListener('wheel', onWheel, { passive: false });
        // Keys live on the focused surface, not window — inside the app
        // document a window-level handler would eat the app's own shortcuts.
        surface.addEventListener('keydown', onKeyDown);
        surface.addEventListener('keyup', onKeyUp);
        surface.addEventListener('paste', onPaste);
        window.addEventListener('blur', onBlur);

        const watchTimer = window.setInterval(() => {
            if (!ws || ws.readyState !== 1) return;
            const idle = performance.now() - (gotFrame ? lastRender : connectAt);
            if (idle > 1800) requestKey('stalled');
            if (idle > 6000) { try { ws.close(); } catch { /* already closing */ } }
        }, 1000);

        if (useMSE) {
            setupMSE();
            video.addEventListener('playing', markRender);
            video.addEventListener('timeupdate', markRender);
        }
        if (wantInput) surface.focus();
        connect();

        return () => {
            closed = true;
            renegotiateRef.current = () => undefined;
            window.clearInterval(watchTimer);
            surface.removeEventListener('pointerdown', onPointerDown);
            surface.removeEventListener('pointermove', onPointerMove);
            surface.removeEventListener('pointerup', onPointerUp);
            surface.removeEventListener('contextmenu', onContextMenu);
            surface.removeEventListener('wheel', onWheel);
            surface.removeEventListener('keydown', onKeyDown);
            surface.removeEventListener('keyup', onKeyUp);
            surface.removeEventListener('paste', onPaste);
            window.removeEventListener('blur', onBlur);
            video.removeEventListener('playing', markRender);
            video.removeEventListener('timeupdate', markRender);
            try { ws?.close(); } catch { /* already closed */ }
            ws = null;
            resetDecoder();
            if (ms) { try { if (ms.readyState === 'open') ms.endOfStream(); } catch { /* already ended */ } ms = null; }
            if (video.src) { try { URL.revokeObjectURL(video.src); } catch { /* ignore */ } }
        };
    }, [url, useMSE, hasWC, hasMSE, params.wantInput, params.vnc, params.w]);

    useEffect(() => {
        renegotiateRef.current(streamWidth);
    }, [streamWidth]);

    const surfaceClass = 'desktop-h264__surface';
    return (
        // The src/data-* attributes mirror the iframe element they replace so
        // the stage's existing diagnostics and tests keep describing the
        // player URL, not the container element.
        <div ref={(el) => { if (el && el.getAttribute('src') !== url) el.setAttribute('src', url); }} className="desktop-h264" data-testid="desktop-bot-handoff" data-input={params.wantInput ? 'live' : 'watch'} data-viewer="h264">
            {useMSE ? (
                <video ref={surfaceRef as React.RefObject<HTMLVideoElement>} className={surfaceClass} autoPlay muted playsInline tabIndex={0} />
            ) : (
                <canvas ref={surfaceRef as React.RefObject<HTMLCanvasElement>} className={surfaceClass} tabIndex={0} />
            )}
            {statusText ? <div className="desktop-h264__status">{statusText}</div> : null}
            <div className={'desktop-h264__badge' + (badge.cls ? ' is-' + badge.cls : '')}>{badge.text}</div>
        </div>
    );
}

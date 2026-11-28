import { describe, expect, it } from 'vitest';
import { h264MapCode, h264PageParams, h264SocketURL, h264SurfacePoint, h264VideoMessage } from './desktopH264';

describe('h264PageParams', () => {
    it('reads the player query off the page URL', () => {
        const p = h264PageParams('https://hub.example/api/v1/desktop-handoff/tok/h264.html?view_only=0&vnc=vnc.html&w=1920&fps=10&mse=1');
        expect(p).toEqual({ wantInput: true, vnc: 'vnc.html', mse: true, w: 1920, fps: 10 });
    });

    it('treats a watch page as no-input and a bad URL as empty', () => {
        expect(h264PageParams('https://hub.example/x/h264.html?view_only=1&w=640').wantInput).toBe(false);
        const bad = h264PageParams('not a url');
        expect(bad).toEqual({ wantInput: false, vnc: '', mse: false, w: 0, fps: 0 });
    });
});

describe('h264SocketURL', () => {
    const page = 'https://hub.example/api/v1/desktop-handoff/tok/h264.html?view_only=1&vnc=vnc.html&w=640&fps=10#frag';

    it('swaps the page path and scheme and forwards only stream params', () => {
        const ws = h264SocketURL(page, false, false);
        expect(ws).toBe('wss://hub.example/api/v1/desktop-handoff/tok/h264?w=640&fps=10');
    });

    it('adds mse/input flags and applies the width override', () => {
        const ws = h264SocketURL(page, true, true, 1800);
        const u = new URL(ws);
        expect(u.searchParams.get('mse')).toBe('1');
        expect(u.searchParams.get('input')).toBe('1');
        expect(u.searchParams.get('w')).toBe('1800');
        expect(u.searchParams.get('fps')).toBe('10');
        expect(u.searchParams.get('view_only')).toBeNull();
        expect(u.searchParams.get('vnc')).toBeNull();
    });

    it('keeps H.264 widths even', () => {
        expect(new URL(h264SocketURL(page, false, false, 801)).searchParams.get('w')).toBe('800');
    });
});

describe('h264VideoMessage', () => {
    it('splits the header/payload envelope', () => {
        const header = new TextEncoder().encode(JSON.stringify({ key: true, codec: 'avc1.42C029', seq: 7 }));
        const payload = new Uint8Array([1, 2, 3, 4]);
        const buf = new ArrayBuffer(8 + header.length + payload.length);
        const dv = new DataView(buf);
        dv.setUint32(0, header.length);
        dv.setUint32(4, payload.length);
        new Uint8Array(buf, 8, header.length).set(header);
        new Uint8Array(buf, 8 + header.length, payload.length).set(payload);
        const { h, data } = h264VideoMessage(buf);
        expect(h.key).toBe(true);
        expect(h.seq).toBe(7);
        expect(Array.from(data)).toEqual([1, 2, 3, 4]);
    });
});

describe('h264SurfacePoint', () => {
    const rect = { left: 10, top: 20, width: 800, height: 600 };

    it('maps the video centre to 0.5/0.5 inside a letterboxed surface', () => {
        // 400x200 video in an 800x600 surface scales to 800x400, centered
        // vertically with a 100px gutter.
        const p = h264SurfacePoint(rect, 400, 200, 10 + 400, 20 + 300);
        expect(p.x).toBeCloseTo(0.5);
        expect(p.y).toBeCloseTo(0.5);
    });

    it('clamps points inside the letterbox gutter to the edge', () => {
        const p = h264SurfacePoint(rect, 400, 200, 10 + 400, 20 + 10);
        expect(p.x).toBeCloseTo(0.5);
        expect(p.y).toBe(0);
    });
});

describe('h264MapCode', () => {
    it('maps Meta to Control on macOS only', () => {
        expect(h264MapCode('MetaLeft', true)).toBe('ControlLeft');
        expect(h264MapCode('MetaRight', true)).toBe('ControlRight');
        expect(h264MapCode('MetaLeft', false)).toBe('MetaLeft');
        expect(h264MapCode('KeyA', true)).toBe('KeyA');
    });
});

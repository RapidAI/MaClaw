import { afterEach, describe, expect, it } from 'vitest';
import { DESKTOP_VIEWER_KEY, desktopStreamWidthForSurface, desktopStreamWidthOverride, desktopViewerSrc, h264FrameSrc, loadDesktopViewer, saveDesktopViewer } from './desktopViewer';

describe('desktop viewer', () => {
    afterEach(() => localStorage.removeItem(DESKTOP_VIEWER_KEY));

    it('defaults to H.264 and remembers noVNC', () => {
        expect(loadDesktopViewer()).toBe('h264');
        saveDesktopViewer('vnc');
        expect(loadDesktopViewer()).toBe('vnc');
    });

    it('frames the sibling h264.html with view_only and the noVNC fallback', () => {
        const vnc = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1&path=api%2Fws&view_only=0';
        const src = h264FrameSrc(vnc);
        const url = new URL(src);
        expect(url.pathname).toBe('/api/v1/desktop-handoff/abc/h264.html');
        expect(url.searchParams.get('view_only')).toBe('0');
        expect(url.searchParams.get('vnc')).toBe('vnc.html?autoconnect=1&path=api%2Fws&view_only=0');
    });

    it('keeps URLs that are not a noVNC page, and noVNC when chosen', () => {
        expect(h264FrameSrc('http://x/other.html?a=1')).toBe('http://x/other.html?a=1');
        expect(h264FrameSrc('')).toBe('');
        expect(desktopViewerSrc('http://x/vnc.html?view_only=1', 'vnc')).toBe('http://x/vnc.html?view_only=1');
        expect(desktopViewerSrc('http://x/vnc.html?view_only=1', 'h264')).toBe('http://x/h264.html?view_only=1&vnc=vnc.html%3Fview_only%3D1');
    });

    it('appends the stream spec to the h264 page only', () => {
        const vnc = 'http://x/vnc.html?view_only=1&autoconnect=1';
        const src = desktopViewerSrc(vnc, 'h264', { w: 1440.6, fps: 10 });
        const url = new URL(src);
        expect(url.pathname).toBe('/h264.html');
        expect(url.searchParams.get('w')).toBe('1441');
        expect(url.searchParams.get('fps')).toBe('10');
        expect(url.searchParams.get('vnc')).toBe('vnc.html?view_only=1&autoconnect=1');
        // noVNC ignores the spec, and a non-vnc page never gains params.
        expect(desktopViewerSrc(vnc, 'vnc', { w: 800, fps: 5 })).toBe(vnc);
        expect(desktopViewerSrc('http://x/other.html?a=1', 'h264', { w: 800 })).toBe('http://x/other.html?a=1');
    });

    it('normalizes the native surface width to an even bounded encoder width', () => {
        expect(desktopStreamWidthForSurface(1501)).toBe(1500);
        expect(desktopStreamWidthForSurface(500)).toBe(640);
        expect(desktopStreamWidthForSurface(2560)).toBe(1920);
        expect(desktopStreamWidthForSurface(Number.NaN)).toBe(0);
    });

    it('replaces only the outer stream width in place', () => {
        const src = 'http://x/h264.html?view_only=1&vnc=vnc.html%3Fview_only%3D1&w=640&fps=10#frag';
        expect(desktopStreamWidthOverride(src, 1280))
            .toBe('http://x/h264.html?view_only=1&vnc=vnc.html%3Fview_only%3D1&w=1280&fps=10#frag');
        expect(desktopStreamWidthOverride('http://x/vnc.html?view_only=1', 1280))
            .toBe('http://x/vnc.html?view_only=1');
        expect(desktopStreamWidthOverride(src, 0)).toBe(src);
    });

    it('appends the spec to a direct h264 page before the hash', () => {
        expect(desktopViewerSrc('http://x/h264.html?view_only=1#frag', 'h264', { w: 1280, fps: 10 }))
            .toBe('http://x/h264.html?view_only=1&w=1280&fps=10#frag');
        expect(desktopViewerSrc('http://x/h264.html', 'h264', { w: 1280 }))
            .toBe('http://x/h264.html?w=1280');
        expect(desktopViewerSrc('http://x/h264.html?view_only=1', 'h264'))
            .toBe('http://x/h264.html?view_only=1');
    });
});

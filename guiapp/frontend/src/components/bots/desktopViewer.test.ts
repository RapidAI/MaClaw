import { afterEach, describe, expect, it } from 'vitest';
import { DESKTOP_VIEWER_KEY, desktopViewerSrc, h264FrameSrc, loadDesktopViewer, saveDesktopViewer } from './desktopViewer';

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
});

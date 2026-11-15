import { describe, expect, it } from 'vitest';
import { desktopPictureURL, preferDirectDesktop } from './directDesktop';

function answer(ok: boolean, delay = 0): typeof fetch {
    return ((_url: string, init?: RequestInit) => new Promise((resolve, reject) => {
        const timer = setTimeout(() => resolve({ ok } as Response), delay);
        init?.signal?.addEventListener('abort', () => {
            clearTimeout(timer);
            reject(new DOMException('aborted', 'AbortError'));
        });
    })) as typeof fetch;
}

describe('preferDirectDesktop', () => {
    it('uses a direct picture that answers', async () => {
        const url = 'http://10.1.0.8:18081/v1/desktops/viewer/t/vnc.html';
        const got = await preferDirectDesktop(url, '/api/v1/desktop-handoff/abc/vnc.html', answer(true));
        expect(got).toBe(url);
    });

    it('keeps the hub address when the direct picture does not answer', async () => {
        const hub = '/api/v1/desktop-handoff/abc/vnc.html';
        const got = await preferDirectDesktop('http://10.9.9.9:18081/v1/desktops/viewer/slow/vnc.html', hub, answer(false, 1500));
        expect(got).toBe(hub);
    });

    it('keeps the hub address when no direct picture was offered', async () => {
        const view = { novnc_url: '/api/v1/desktop-handoff/abc/vnc.html' };
        expect(await desktopPictureURL(view, answer(true))).toBe(view.novnc_url);
    });
});

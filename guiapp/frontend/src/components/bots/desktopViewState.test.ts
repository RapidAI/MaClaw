import { describe, expect, it } from 'vitest';
import { applyWatchResult, disconnectBot, isH264Disconnect, pictureFromWatch, showsLiveFrame } from './desktopViewState';

describe('desktop picture state', () => {
    it('uses the typed state and ignores a leftover page', () => {
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'display_stopped' })).toEqual({ state: 'display_stopped', url: '' });
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'asleep' })).toEqual({ state: 'asleep', url: '' });
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'unavailable' })).toEqual({ state: 'unavailable', url: '' });
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html' })).toEqual({ state: '', url: '' });
        expect(pictureFromWatch({ novnc_url: '' })).toEqual({ state: '', url: '' });
        expect(pictureFromWatch(null)).toEqual({ state: '', url: '' });
        expect(showsLiveFrame('display_stopped', 'http://hub/stale/vnc.html')).toBe(false);
        expect(showsLiveFrame('', 'http://hub/stale/vnc.html')).toBe(false);
    });

    it('shows a frame only for a running state that has a page', () => {
        expect(pictureFromWatch({ novnc_url: 'http://hub/live/vnc.html', desktop_state: 'running' })).toEqual({ state: 'running', url: 'http://hub/live/vnc.html' });
        expect(pictureFromWatch({ novnc_url: '', desktop_state: 'running' })).toEqual({ state: 'running', url: '' });
        expect(showsLiveFrame('running', 'http://hub/live/vnc.html')).toBe(true);
        expect(showsLiveFrame('running', '')).toBe(false);
    });

    it('reads DESKTOP_ASLEEP from the code field', () => {
        expect(pictureFromWatch({ code: 'DESKTOP_ASLEEP', novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'running' })).toEqual({ state: 'asleep', url: '' });
        expect(pictureFromWatch({ code: 'OTHER', novnc_url: 'http://hub/live/vnc.html', desktop_state: 'running' }).state).toBe('running');
    });

    it('drops a poll that started before a wake, and treats display_stopped on that wake as starting', () => {
        expect(applyWatchResult(1, 2, 'display_stopped', 2)).toBe('ignore');
        expect(applyWatchResult(2, 2, 'display_stopped', 2)).toBe('starting');
        expect(applyWatchResult(2, 2, 'asleep', 2)).toBe('apply');
        expect(applyWatchResult(2, 2, 'running', undefined)).toBe('apply');
    });

    it('accepts a disconnect only from the frame that shows that bot', () => {
        const frame = {} as Window;
        const other = {} as Window;
        const data = { source: 'maclaw-desktop', event: 'disconnect' };
        expect(isH264Disconnect(data)).toBe(true);
        expect(isH264Disconnect({ source: 'maclaw-desktop', event: 'connect' })).toBe(false);
        expect(isH264Disconnect({ type: 'maclaw-desktop-lease' })).toBe(false);
        expect(disconnectBot(frame, frame, data, 'bot_1')).toBe('bot_1');
        expect(disconnectBot(frame, other, data, 'bot_1')).toBe('');
        expect(disconnectBot(frame, frame, data, '')).toBe('');
        expect(disconnectBot(null, frame, data, 'bot_1')).toBe('');
    });
});

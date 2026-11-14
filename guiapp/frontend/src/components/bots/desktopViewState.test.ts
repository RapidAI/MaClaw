import { pictureFromWatch, showsLiveFrame, stateFromFailure, stateFromStreamEvent } from './desktopViewState';

describe('desktop picture state', () => {
    it('drops a remembered page when the display or the container is stopped', () => {
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'display_stopped' })).toEqual({ state: 'display_stopped', url: '' });
        expect(pictureFromWatch({ novnc_url: 'http://hub/stale/vnc.html', desktop_state: 'asleep' })).toEqual({ state: 'asleep', url: '' });
        expect(showsLiveFrame('display_stopped', 'http://hub/stale/vnc.html')).toBe(false);
        expect(showsLiveFrame('asleep', 'http://hub/stale/vnc.html')).toBe(false);
    });

    it('shows a frame only while the screen is running and has a page', () => {
        expect(pictureFromWatch({ novnc_url: 'http://hub/live/vnc.html', desktop_state: 'running' })).toEqual({ state: 'running', url: 'http://hub/live/vnc.html' });
        expect(pictureFromWatch({ novnc_url: '', desktop_state: 'running' })).toEqual({ state: 'asleep', url: '' });
        expect(showsLiveFrame('running', 'http://hub/live/vnc.html')).toBe(true);
        expect(showsLiveFrame('running', '')).toBe(false);
    });

    it('treats an older watch with no state the same way: a page is running, none is asleep', () => {
        expect(pictureFromWatch({ novnc_url: 'http://hub/live/vnc.html' }).state).toBe('running');
        expect(pictureFromWatch({ novnc_url: '' }).state).toBe('asleep');
        expect(pictureFromWatch(null).state).toBe('asleep');
    });

    it('reads the DESKTOP_ASLEEP code and a stream disconnect', () => {
        expect(stateFromFailure(new Error('DESKTOP_ASLEEP'))).toBe('asleep');
        expect(stateFromFailure(new Error('bot service: DESKTOP_ASLEEP'))).toBe('asleep');
        expect(stateFromFailure({ code: 'DESKTOP_ASLEEP' })).toBe('asleep');
        expect(stateFromFailure(new Error('bot service is unavailable'))).toBe('');
        expect(stateFromStreamEvent({ source: 'maclaw-desktop', event: 'disconnect' })).toBe('display_stopped');
        expect(stateFromStreamEvent({ source: 'maclaw-desktop', event: 'stream-end' })).toBe('display_stopped');
        expect(stateFromStreamEvent({ source: 'maclaw-desktop', event: 'connect' })).toBe('');
        expect(stateFromStreamEvent({ type: 'maclaw-desktop-lease', holder: '' })).toBe('');
    });
});

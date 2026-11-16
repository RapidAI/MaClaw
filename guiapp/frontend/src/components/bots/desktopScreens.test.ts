import { describe, expect, it } from 'vitest';
import { desktopModeOf, effectiveScreenPick, mineScreenURL, sameDesktopScreen, screenNeedsOwnHold, screenPictureFallback, shownScreenURL, withSavedScreenMode } from './desktopScreens';

const bots = [{ id: 'a' }, { id: 'b' }];

describe('desktop screens', () => {
    it('treats a missing mode as the shared owner desktop', () => {
        expect(desktopModeOf(undefined)).toBe('shared');
        expect(desktopModeOf('shared')).toBe('shared');
        expect(desktopModeOf('isolated')).toBe('isolated');
        expect(desktopModeOf('none')).toBe('none');
    });

    it('uses a shared bot URL for my desktop and keeps an isolated bot on its own URL', () => {
        const screens = {
            a: { url: 'http://hub/mine', mode: 'shared' as const },
            b: { url: 'http://hub/bot-b', mode: 'isolated' as const },
        };
        expect(mineScreenURL(bots, screens)).toBe('http://hub/mine');
        expect(shownScreenURL('mine', 'b', bots, screens, 'http://hub/fallback')).toBe('http://hub/mine');
        expect(shownScreenURL('mine', 'b', bots, { b: { url: 'http://hub/bot-b', mode: 'isolated' } }, 'http://hub/fallback')).toBe('');
        expect(shownScreenURL('b', 'a', bots, screens, 'http://hub/fallback')).toBe('http://hub/bot-b');
        expect(shownScreenURL('a', 'a', bots, { a: { url: '', mode: 'shared' } }, 'http://hub/fallback')).toBe('http://hub/fallback');
        expect(shownScreenURL('b', 'a', bots, { b: { url: '', mode: 'isolated' } }, 'http://hub/fallback')).toBe('');
    });

    it('keeps an isolated bot off the shared desktop before its watch returns', () => {
        const known = withSavedScreenMode(
            [{ id: 'a', desktopMode: 'shared' }, { id: 'b', desktopMode: 'isolated' }],
            {},
        );
        expect(known.b.mode).toBe('isolated');
        expect(effectiveScreenPick(false, 'mine', 'b', known.b.mode)).toBe('b');
        expect(sameDesktopScreen('a', 'b', known)).toBe(false);
        const watched = withSavedScreenMode(
            [{ id: 'b', desktopMode: 'isolated' }],
            { b: { url: 'http://hub/bot-b', mode: 'shared' as const } },
        );
        expect(watched.b.mode).toBe('shared');
        expect(watched.b.url).toBe('http://hub/bot-b');
        const cleared = withSavedScreenMode(
            [{ id: 'a', desktopMode: 'none' }],
            { a: { url: 'http://hub/old', mode: 'shared' as const } },
        );
        expect(cleared.a.mode).toBe('none');
        expect(cleared.a.url).toBe('http://hub/old');
        expect(screenPictureFallback('isolated', '', 'http://hub/shared')).toBe('');
        expect(screenPictureFallback('isolated', 'http://hub/own', 'http://hub/shared')).toBe('http://hub/own');
        expect(screenPictureFallback('shared', '', 'http://hub/shared')).toBe('http://hub/shared');
    });

    it('opens an isolated bot on its own screen until the person picks', () => {
        expect(sameDesktopScreen('a', 'b', { a: { mode: 'shared' }, b: { mode: 'shared' } })).toBe(true);
        expect(sameDesktopScreen('a', 'c', { a: { mode: 'shared' }, c: { mode: 'isolated' } })).toBe(false);
        expect(sameDesktopScreen('a', 'e', {})).toBe(true);
        expect(effectiveScreenPick(false, 'mine', 'b', 'isolated')).toBe('b');
        expect(effectiveScreenPick(true, 'mine', 'b', 'isolated')).toBe('mine');
        expect(effectiveScreenPick(false, 'mine', 'a', 'shared')).toBe('mine');
    });

    it('holds a different display and leaves the shared owner watch alone', () => {
        const screens = {
            a: { url: 'http://hub/mine', mode: 'shared' as const },
            b: { url: 'http://hub/bot-b', mode: 'isolated' as const },
        };
        expect(screenNeedsOwnHold('b', 'a', screens, bots)).toBe('b');
        expect(screenNeedsOwnHold('a', 'b', screens, bots)).toBe('a');
        expect(screenNeedsOwnHold('mine', 'b', screens, bots)).toBe('a');
        expect(screenNeedsOwnHold('mine', 'a', screens, bots)).toBe('');
        expect(screenNeedsOwnHold('a', 'a', screens, bots)).toBe('');
        expect(screenNeedsOwnHold('b', 'a', { a: { url: '', mode: 'shared' } }, bots)).toBe('');
    });
});

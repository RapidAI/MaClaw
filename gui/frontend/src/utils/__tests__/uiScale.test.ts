import { afterEach, describe, expect, it, vi } from 'vitest';
import {
    applySavedUIZoomFactor,
    clampUIScale,
    isUIScaleAuto,
    quantizeUIScale,
    recommendUIScale,
    resolveUIScale,
    subscribeDisplayScaleChanges,
    uiScaleEquals,
    uiScaleToPercent,
    UI_SCALE_AUTO,
} from '../uiScale';

describe('clampUIScale', () => {
    it('clamps into [0.5, 2] and quantizes to 5% steps', () => {
        expect(clampUIScale(0.1)).toBe(0.5);
        expect(clampUIScale(3)).toBe(2);
        expect(clampUIScale(1.24)).toBe(1.25);
        expect(clampUIScale(Number.NaN)).toBe(1);
    });
});

describe('quantizeUIScale', () => {
    it('snaps to 0.05 steps', () => {
        expect(quantizeUIScale(1.02)).toBe(1);
        expect(quantizeUIScale(1.03)).toBe(1.05);
        expect(quantizeUIScale(1.149)).toBe(1.15);
    });
});

describe('isUIScaleAuto', () => {
    it('treats 0 / null / non-finite as Auto', () => {
        expect(isUIScaleAuto(0)).toBe(true);
        expect(isUIScaleAuto(UI_SCALE_AUTO)).toBe(true);
        expect(isUIScaleAuto(null)).toBe(true);
        expect(isUIScaleAuto(undefined)).toBe(true);
        expect(isUIScaleAuto(Number.NaN)).toBe(true);
        expect(isUIScaleAuto(1)).toBe(false);
        expect(isUIScaleAuto(0.9)).toBe(false);
    });
});

describe('recommendUIScale', () => {
    it('bumps scale on low-DPI 1080p so dense rem chrome stays readable', () => {
        expect(
            recommendUIScale({ screenWidth: 1920, screenHeight: 1080, devicePixelRatio: 1 }),
        ).toBe(1.05);
    });

    it('enlarges more on small low-DPI laptops (1366×768)', () => {
        expect(
            recommendUIScale({ screenWidth: 1366, screenHeight: 768, devicePixelRatio: 1 }),
        ).toBe(1.15);
    });

    it('avoids double-scaling on common HiDPI 150% 4K logical sizes', () => {
        expect(
            recommendUIScale({ screenWidth: 2560, screenHeight: 1440, devicePixelRatio: 1.5 }),
        ).toBe(1.05);
    });

    it('eases off when OS scale is high but logical space is tight', () => {
        expect(
            recommendUIScale({ screenWidth: 1280, screenHeight: 800, devicePixelRatio: 2 }),
        ).toBe(0.95);
    });

    it('stays near 1.0 on standard 1080p-equivalent logical @ 200%', () => {
        expect(
            recommendUIScale({ screenWidth: 1920, screenHeight: 1080, devicePixelRatio: 2 }),
        ).toBe(1.0);
    });
});

describe('resolveUIScale', () => {
    it('uses recommendation when saved factor is Auto (0)', () => {
        const auto = resolveUIScale(0, {
            screenWidth: 1366,
            screenHeight: 768,
            devicePixelRatio: 1,
        });
        expect(auto).toBe(1.15);
    });

    it('honors manual override and quantizes', () => {
        expect(
            resolveUIScale(1.24, {
                screenWidth: 1366,
                screenHeight: 768,
                devicePixelRatio: 1,
            }),
        ).toBe(1.25);
    });
});

describe('uiScaleEquals', () => {
    it('compares at two-decimal precision', () => {
        expect(uiScaleEquals(1.05, 1.05)).toBe(true);
        expect(uiScaleEquals(1.05, 1.049)).toBe(true);
        expect(uiScaleEquals(1.05, 1.1)).toBe(false);
        expect(uiScaleEquals(Number.NaN, 1)).toBe(false);
    });
});

describe('uiScaleToPercent', () => {
    it('formats clamped scale as integer percent', () => {
        expect(uiScaleToPercent(1.05)).toBe(105);
        expect(uiScaleToPercent(0.1)).toBe(50);
        expect(uiScaleToPercent(3)).toBe(200);
    });
});

describe('applySavedUIZoomFactor', () => {
    it('applies Auto recommendation and skips redundant scale updates', () => {
        const setAuto = vi.fn((updater: boolean | ((prev: boolean) => boolean)) => {
            if (typeof updater === 'function') {
                expect(updater(true)).toBe(true); // already auto → unchanged
            }
        });
        const setZoom = vi.fn((updater: number | ((prev: number) => number)) => {
            if (typeof updater === 'function') {
                expect(updater(1.15)).toBe(1.15); // same as prev → unchanged
            }
        });
        const result = applySavedUIZoomFactor(
            0,
            setAuto,
            setZoom,
            { screenWidth: 1366, screenHeight: 768, devicePixelRatio: 1 },
        );
        expect(result).toEqual({ auto: true, scale: 1.15 });
        expect(setAuto).toHaveBeenCalledTimes(1);
        expect(setZoom).toHaveBeenCalledTimes(1);
    });

    it('applies manual scale and flips auto off', () => {
        const setAuto = vi.fn((updater: boolean | ((prev: boolean) => boolean)) => {
            if (typeof updater === 'function') {
                expect(updater(true)).toBe(false);
            }
        });
        const setZoom = vi.fn((updater: number | ((prev: number) => number)) => {
            if (typeof updater === 'function') {
                expect(updater(1)).toBe(1.25);
            }
        });
        const result = applySavedUIZoomFactor(1.25, setAuto, setZoom);
        expect(result).toEqual({ auto: false, scale: 1.25 });
        expect(setAuto).toHaveBeenCalledTimes(1);
    });
});

describe('subscribeDisplayScaleChanges', () => {
    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    it('debounces resize and unsubscribes cleanly', () => {
        vi.useFakeTimers();
        const onChange = vi.fn();
        const listeners = new Map<string, Set<EventListener>>();
        const originalAdd = window.addEventListener.bind(window);
        const originalRemove = window.removeEventListener.bind(window);
        const addEventListener = vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
            if (type === 'resize') {
                const set = listeners.get(type) ?? new Set();
                set.add(listener as EventListener);
                listeners.set(type, set);
                return;
            }
            return originalAdd(type, listener);
        });
        const removeEventListener = vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
            if (type === 'resize') {
                listeners.get(type)?.delete(listener as EventListener);
                return;
            }
            return originalRemove(type, listener);
        });
        window.addEventListener = addEventListener as typeof window.addEventListener;
        window.removeEventListener = removeEventListener as typeof window.removeEventListener;

        const mql = {
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
        };
        const originalMatchMedia = window.matchMedia;
        window.matchMedia = vi.fn().mockReturnValue(mql) as unknown as typeof window.matchMedia;

        try {
            const unsubscribe = subscribeDisplayScaleChanges(onChange, { resizeDebounceMs: 100 });
            expect(addEventListener).toHaveBeenCalledWith('resize', expect.any(Function));
            expect(mql.addEventListener).toHaveBeenCalledWith('change', expect.any(Function));

            const resizeHandler = [...(listeners.get('resize') ?? [])][0] as EventListener;
            resizeHandler(new Event('resize'));
            resizeHandler(new Event('resize'));
            expect(onChange).not.toHaveBeenCalled();
            vi.advanceTimersByTime(100);
            expect(onChange).toHaveBeenCalledTimes(1);

            unsubscribe();
            expect(removeEventListener).toHaveBeenCalledWith('resize', resizeHandler);
            expect(mql.removeEventListener).toHaveBeenCalled();
        } finally {
            window.addEventListener = originalAdd;
            window.removeEventListener = originalRemove;
            window.matchMedia = originalMatchMedia;
        }
    });
});

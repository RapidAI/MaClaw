import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { startVisibleInterval } from '../visibleInterval';

describe('startVisibleInterval', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    });

    afterEach(() => {
        vi.useRealTimers();
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    });

    it('ticks on the interval, pauses while hidden, and stops after cleanup', () => {
        const tick = vi.fn();
        const stop = startVisibleInterval(tick, 5000);

        vi.advanceTimersByTime(5000);
        expect(tick).toHaveBeenCalledTimes(1);

        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
        document.dispatchEvent(new Event('visibilitychange'));
        vi.advanceTimersByTime(10000);
        expect(tick).toHaveBeenCalledTimes(1);

        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        document.dispatchEvent(new Event('visibilitychange'));
        expect(tick).toHaveBeenCalledTimes(2);

        stop();
        vi.advanceTimersByTime(5000);
        expect(tick).toHaveBeenCalledTimes(2);
    });

    it('does not start ticking when the document is already hidden', () => {
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
        const tick = vi.fn();
        const stop = startVisibleInterval(tick, 5000);
        vi.advanceTimersByTime(10000);
        expect(tick).not.toHaveBeenCalled();
        stop();
    });
});

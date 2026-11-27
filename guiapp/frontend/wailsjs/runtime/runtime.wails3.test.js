import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { eventsOnMultiple } = vi.hoisted(() => ({
    eventsOnMultiple: vi.fn(),
}));

vi.mock('./v3bridge.js', () => ({
    eventsOnMultiple,
    eventsOff: vi.fn(async () => {}),
    eventsEmit: vi.fn(async () => {}),
    callWindow: vi.fn(async () => {}),
    browserOpenURL: vi.fn(async () => {}),
    clipboardText: vi.fn(async () => ''),
    clipboardSetText: vi.fn(async () => {}),
    getV3: vi.fn(async () => ({})),
}));

import { EventsOn } from './runtime.js';

describe('wails3 EventsOn', () => {
    let v2Runtime;

    beforeEach(() => {
        v2Runtime = window.runtime;
        delete window.runtime;
        eventsOnMultiple.mockReset();
    });

    afterEach(() => {
        window.runtime = v2Runtime;
    });

    it('returns a function while the wails3 subscribe is still pending', async () => {
        let resolveOff;
        const off = vi.fn();
        eventsOnMultiple.mockReturnValue(new Promise((resolve) => {
            resolveOff = resolve;
        }));

        const cancel = EventsOn('env-check-done', () => {});

        expect(typeof cancel).toBe('function');
        cancel();
        resolveOff(off);
        await vi.waitFor(() => expect(off).toHaveBeenCalledTimes(1));
    });

    it('unsubscribes once after the listener is registered', async () => {
        const off = vi.fn();
        eventsOnMultiple.mockResolvedValue(off);

        const cancel = EventsOn('knowledge:import-progress', () => {});
        await vi.waitFor(() => expect(eventsOnMultiple).toHaveBeenCalledTimes(1));
        await Promise.resolve();

        cancel();
        cancel();
        expect(off).toHaveBeenCalledTimes(1);
    });

    it('does not drop a listener registered after an earlier cancel', async () => {
        let resolveFirst;
        const offFirst = vi.fn();
        const offSecond = vi.fn();
        eventsOnMultiple
            .mockReturnValueOnce(new Promise((resolve) => {
                resolveFirst = resolve;
            }))
            .mockResolvedValueOnce(offSecond);

        const cancelFirst = EventsOn('im-startmenu-task-created', () => {});
        cancelFirst();
        const cancelSecond = EventsOn('im-startmenu-task-created', () => {});

        resolveFirst(offFirst);
        await vi.waitFor(() => expect(offFirst).toHaveBeenCalledTimes(1));
        expect(offSecond).not.toHaveBeenCalled();

        cancelSecond();
        expect(offSecond).toHaveBeenCalledTimes(1);
    });

    it('keeps the synchronous v2 unsubscribe when window.runtime is present', () => {
        const off = vi.fn();
        window.runtime = { EventsOnMultiple: vi.fn(() => off) };

        const cancel = EventsOn('config-changed', () => {});

        expect(cancel).toBe(off);
        expect(eventsOnMultiple).not.toHaveBeenCalled();
    });
});

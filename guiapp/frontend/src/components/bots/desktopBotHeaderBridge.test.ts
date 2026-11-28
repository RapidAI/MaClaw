import { describe, expect, it, vi, beforeEach } from 'vitest';
import {
    noteDesktopBotRooms,
    openDesktopBotRoomsFromHeader,
    resetDesktopBotHeaderBridgeForTests,
    subscribeDesktopBotRooms,
} from './desktopBotHeaderBridge';

describe('desktopBotHeaderBridge', () => {
    beforeEach(() => {
        resetDesktopBotHeaderBridgeForTests();
    });

    it('notifies once on a visibility flip and passes the registered open callback through', () => {
        const listener = vi.fn();
        const unsubscribe = subscribeDesktopBotRooms(listener);
        const openRooms = vi.fn();

        noteDesktopBotRooms({ visible: true, open: openRooms });
        expect(listener).toHaveBeenCalledTimes(1);

        openDesktopBotRoomsFromHeader();
        expect(openRooms).toHaveBeenCalledTimes(1);
        unsubscribe();
    });

    it('re-registration with the same visibility does not notify (fresh open closure per bots refresh)', () => {
        const listener = vi.fn();
        const unsubscribe = subscribeDesktopBotRooms(listener);

        noteDesktopBotRooms({ visible: true, open: vi.fn() });
        noteDesktopBotRooms({ visible: true, open: vi.fn() });
        noteDesktopBotRooms({ visible: true, open: vi.fn() });
        expect(listener).toHaveBeenCalledTimes(1);
        unsubscribe();
    });

    it('a revoked registration notifies only when rooms were visible', () => {
        const listener = vi.fn();
        const unsubscribe = subscribeDesktopBotRooms(listener);

        noteDesktopBotRooms(null);
        expect(listener).not.toHaveBeenCalled();

        noteDesktopBotRooms({ visible: true, open: vi.fn() });
        noteDesktopBotRooms(null);
        expect(listener).toHaveBeenCalledTimes(2);
        unsubscribe();
    });

    it('an unregistered header click is a no-op, not a crash', () => {
        expect(() => openDesktopBotRoomsFromHeader()).not.toThrow();
    });
});

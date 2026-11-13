// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { publishSharedUnreadCount, subscribeSharedUnreadCount } from '../useNotifications';

describe('shared unread-count mirror', () => {
    it('fires immediately with the current value', () => {
        publishSharedUnreadCount(7);
        const listener = vi.fn();
        const unsubscribe = subscribeSharedUnreadCount(listener);
        expect(listener).toHaveBeenCalledWith(7);
        unsubscribe();
    });

    it('notifies on change and skips unchanged publishes', () => {
        publishSharedUnreadCount(9);
        const listener = vi.fn();
        const unsubscribe = subscribeSharedUnreadCount(listener);
        listener.mockClear();

        publishSharedUnreadCount(2);
        expect(listener).toHaveBeenCalledTimes(1);
        expect(listener).toHaveBeenCalledWith(2);

        // Same value: no redundant notification.
        publishSharedUnreadCount(2);
        expect(listener).toHaveBeenCalledTimes(1);
        unsubscribe();
    });

    it('stops delivering after unsubscribe', () => {
        publishSharedUnreadCount(1);
        const listener = vi.fn();
        const unsubscribe = subscribeSharedUnreadCount(listener);
        unsubscribe();
        listener.mockClear();

        publishSharedUnreadCount(5);
        expect(listener).not.toHaveBeenCalled();
    });

    it('clamps negative and fractional inputs', () => {
        publishSharedUnreadCount(4);
        const listener = vi.fn();
        const unsubscribe = subscribeSharedUnreadCount(listener);
        listener.mockClear();

        publishSharedUnreadCount(-3);
        expect(listener).toHaveBeenCalledWith(0);
        publishSharedUnreadCount(2.6);
        expect(listener).toHaveBeenCalledWith(3);
        unsubscribe();
    });
});

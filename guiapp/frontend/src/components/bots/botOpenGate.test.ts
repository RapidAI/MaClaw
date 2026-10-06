import { describe, expect, it } from 'vitest';
import { beginBotAccessRead, botOpenDecision, isCurrentBotAccessRead, navigationEpoch, publishBotAccess, subscribeBotAccess } from './botOpenGate';

describe('botOpenDecision', () => {
    it('opens Bot when this request is still the latest and Hub granted access', () => {
        expect(botOpenDecision(1, 1, true, 'ai')).toEqual({ open: true, leave: false, toast: false });
    });

    it('drops a late grant after the user already left for another page', () => {
        expect(botOpenDecision(2, 1, true, 'ai')).toEqual({ open: false, leave: false, toast: false });
    });

    it('toasts and leaves when access is denied while the Bot page is open', () => {
        expect(botOpenDecision(1, 1, false, 'bots')).toEqual({ open: false, leave: true, toast: true });
    });

    it('toasts without moving the user when access is denied from another page', () => {
        expect(botOpenDecision(1, 1, false, 'settings')).toEqual({ open: false, leave: false, toast: true });
    });
});

describe('navigationEpoch', () => {
    it('cancels a pending Bot open when another page is selected', () => {
        expect(navigationEpoch(1, 'remote', 'ai')).toBe(2);
        expect(botOpenDecision(2, 1, true, 'remote')).toEqual({ open: false, leave: false, toast: false });
    });

    it('keeps the Bot open in flight while the page itself is being shown', () => {
        expect(navigationEpoch(1, 'bots', 'ai')).toBe(1);
    });

    it('leaves the epoch alone when the selected page does not change', () => {
        expect(navigationEpoch(1, 'ai', 'ai')).toBe(1);
    });
});

describe('bot access reads', () => {
    it('retires an earlier read when a newer one starts or access is published', () => {
        const first = beginBotAccessRead();
        const second = beginBotAccessRead();
        expect(isCurrentBotAccessRead(first)).toBe(false);
        expect(isCurrentBotAccessRead(second)).toBe(true);
        publishBotAccess(false);
        expect(isCurrentBotAccessRead(second)).toBe(false);
    });
});

describe('publishBotAccess', () => {
    it('tells current listeners and drops them after unsubscribe', () => {
        const seen: boolean[] = [];
        const stop = subscribeBotAccess(enabled => { seen.push(enabled); });
        publishBotAccess(false);
        stop();
        publishBotAccess(true);
        expect(seen).toEqual([false]);
    });
});

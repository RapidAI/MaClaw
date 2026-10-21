import { describe, expect, it } from 'vitest';
import { orphanedPendingReply } from './desktopBots';

describe('orphanedPendingReply', () => {
    const none = new Set<string>();
    it('flags a pending bubble from an earlier session that never went out', () => {
        expect(orphanedPendingReply({ id: 'a', role: 'assistant', pending: true, requestId: 'local:a' }, none)).toBe(true);
    });
    it('keeps bubbles that were dispatched, settled, live on this page, or have a server id', () => {
        expect(orphanedPendingReply({ id: 'a', role: 'assistant', pending: true, requestId: 'local:a', dispatched: true }, none)).toBe(false);
        expect(orphanedPendingReply({ id: 'a', role: 'assistant', pending: false, requestId: 'local:a' }, none)).toBe(false);
        expect(orphanedPendingReply({ id: 'a', role: 'assistant', pending: true, requestId: 'local:a' }, new Set(['a']))).toBe(false);
        expect(orphanedPendingReply({ id: 'a', role: 'assistant', pending: true, requestId: 'desktop-bot-123' }, none)).toBe(false);
        expect(orphanedPendingReply({ id: 'a', role: 'user', pending: true, requestId: 'local:a' }, none)).toBe(false);
    });
});

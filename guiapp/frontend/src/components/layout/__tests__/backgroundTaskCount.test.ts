import { describe, expect, it } from 'vitest';
import { countActiveBackgroundLoops } from '../backgroundTaskCount';

describe('countActiveBackgroundLoops', () => {
    it('counts active loops the same way as the background monitor badge', () => {
        expect(countActiveBackgroundLoops([
            { slot_kind: 'ssh', status: 'running' },
            { slotKind: 'ssh', status: 'paused' },
            { slot_kind: ' SSH ', status: ' Running ' },
            { SlotKind: 'ssh', Status: 'completed' },
            { slot_kind: 'coding', status: 'running' },
            { slot_kind: 'browser', status: 'paused' },
            null,
        ])).toBe(5);
    });

    it('treats non-array background loop payloads as empty', () => {
        expect(countActiveBackgroundLoops(null)).toBe(0);
        expect(countActiveBackgroundLoops({ slot_kind: 'ssh', status: 'running' })).toBe(0);
    });
});

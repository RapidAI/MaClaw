import { describe, expect, it } from 'vitest';
import { countActiveSshBackgroundTasks } from '../backgroundTaskCount';

describe('countActiveSshBackgroundTasks', () => {
    it('counts only active manageable ssh background loops', () => {
        expect(countActiveSshBackgroundTasks([
            { slot_kind: 'ssh', status: 'running' },
            { slotKind: 'ssh', status: 'paused' },
            { slot_kind: ' SSH ', status: ' Running ' },
            { SlotKind: 'ssh', Status: 'completed' },
            { slot_kind: 'coding', status: 'running' },
            { slot_kind: 'browser', status: 'paused' },
            null,
        ])).toBe(3);
    });

    it('treats non-array background loop payloads as empty', () => {
        expect(countActiveSshBackgroundTasks(null)).toBe(0);
        expect(countActiveSshBackgroundTasks({ slot_kind: 'ssh', status: 'running' })).toBe(0);
    });
});

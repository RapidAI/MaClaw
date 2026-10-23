import { describe, expect, it } from 'vitest';
import { restoredProjectHistoryResultStatus } from '../assistantPreviewState';

describe('restored project history outcome', () => {
    it('keeps a stored incomplete status', () => {
        expect(restoredProjectHistoryResultStatus({ result_status: 'incomplete' })).toBe('incomplete');
        expect(restoredProjectHistoryResultStatus({ resultStatus: 'completed' })).toBe('completed');
    });

    it('reads a terminal class as incomplete', () => {
        expect(restoredProjectHistoryResultStatus({ terminal_class: 'no_progress' })).toBe('incomplete');
    });

    it('does not treat a failure sentence as an outcome', () => {
        expect(restoredProjectHistoryResultStatus({ terminal_class: '写入日志失败', result_status: '写入日志失败' })).toBeUndefined();
        expect(restoredProjectHistoryResultStatus(undefined)).toBeUndefined();
    });
});

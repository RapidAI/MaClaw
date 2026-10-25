import { describe, expect, it, vi } from 'vitest';
import { loadRestoredProjectConversationHistory, restoredProjectHistoryResultStatus } from '../assistantPreviewState';

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

    it('keeps rich recovery payloads during backend history restore', async () => {
        const load = vi.fn().mockResolvedValue([{
            role: 'assistant', content: '', unfinished_slot: { slot_id: 'shutdown-1' },
            attachments: [{ fileName: 'report.pptx' }], result_status: 'incomplete',
        }]);
        (window as any).go = { main: { App: { LoadProjectConversationHistory: load } } };
        const messages = await loadRestoredProjectConversationHistory('D:/work/deck');
        expect(messages).toHaveLength(1);
        expect(messages[0].unfinishedSlot).toEqual({ slot_id: 'shutdown-1' });
        expect(messages[0].attachments).toEqual([{ fileName: 'report.pptx' }]);
        expect(messages[0].resultStatus).toBe('incomplete');
        delete (window as any).go;
    });
});

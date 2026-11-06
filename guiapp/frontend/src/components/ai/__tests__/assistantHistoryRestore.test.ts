import { describe, expect, it, vi } from 'vitest';
import { commitHeldProjectTranscript, loadRestoredProjectConversationHistory, projectTranscriptBase, restoredProjectHistoryResultStatus, retainStoredTranscript } from '../assistantPreviewState';

describe('retain stored transcript', () => {
    it('keeps stored turns when the view is still empty', () => {
        expect(retainStoredTranscript(
            [{ id: 'ctx', role: 'system', content: 'card', isProjectContext: true }],
            [{ id: 'u', role: 'user', content: 'stored question' }, { id: 'a', role: 'assistant', content: 'stored answer' }],
        ).map(message => message.id)).toEqual(['u', 'a', 'ctx']);
    });

    it('keeps stored turns under a view that only has the new live row', () => {
        const kept = retainStoredTranscript(
            [{ id: 'live', role: 'user', content: 'new question' }],
            [
                { id: 'restored-old', role: 'user', content: 'placeholder' },
                { id: 'u', role: 'user', content: 'stored question' },
                { id: 'a', role: 'assistant', content: 'stored answer' },
            ],
        );
        expect(kept.map(message => message.content)).toEqual(['stored question', 'stored answer', 'new question']);
    });

    it('keeps the visible snapshot when the cache has no conversation', () => {
        expect(retainStoredTranscript(
            [{ id: 'live', role: 'user', content: 'only live' }],
            [],
        )).toEqual([{ id: 'live', role: 'user', content: 'only live' }]);
    });

    it('keeps project-memory turns under a view that only has the new live row', () => {
        expect(retainStoredTranscript(
            [{ id: 'live', role: 'user', content: 'new question' }],
            [
                { id: 'restored-1', role: 'user', content: 'stored question' },
                { id: 'restored-2', role: 'assistant', content: 'stored answer' },
            ],
        ).map(message => message.content)).toEqual(['stored question', 'stored answer', 'new question']);
    });

    it('drops a painted placeholder once the session file replaces it', () => {
        expect(retainStoredTranscript(
            [{ id: 'restored-old', role: 'user', content: 'placeholder' }, { id: 'live', role: 'user', content: 'new question' }],
            [{ id: 'u', role: 'user', content: 'stored question' }, { id: 'a', role: 'assistant', content: 'stored answer' }],
        ).map(message => message.id)).toEqual(['u', 'a', 'live']);
    });
});

describe('project transcript base', () => {
    it('shows the saved transcript when the held stream has no turn', () => {
        expect(projectTranscriptBase(
            [{ id: 'u', role: 'user', content: 'saved', timestamp: 0 }],
            [{ id: 'ctx', role: 'system', content: 'card', isProjectContext: true, timestamp: 0 }],
        ).map(message => message.id)).toEqual(['u']);
    });

    it('keeps saved turns under a stream that only has the live row', () => {
        expect(projectTranscriptBase(
            [{ id: 'u', role: 'user', content: 'saved', timestamp: 0 }, { id: 'a', role: 'assistant', content: 'answer', timestamp: 0 }],
            [{ id: 'live', role: 'user', content: 'new', timestamp: 0 }],
        ).map(message => message.id)).toEqual(['u', 'a', 'live']);
    });
});

describe('commit held project transcript', () => {
    it('keeps the restored transcript when the finished stream only has the live row', () => {
        expect(commitHeldProjectTranscript(
            [{ id: 'live', role: 'user', content: 'new question' }],
            [{ id: 'u', role: 'user', content: 'stored question' }, { id: 'a', role: 'assistant', content: 'stored answer' }],
            [{ id: 'live', role: 'user', content: 'new question' }],
        ).map(message => message.content)).toEqual(['stored question', 'stored answer', 'new question']);
    });
});

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

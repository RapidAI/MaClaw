import { describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { usePastedImageAttachments } from '../usePastedImageAttachments';
import { forgetAIAssistantSessionRounds } from '../useAIAssistant';

describe('usePastedImageAttachments', () => {
    it('keeps pending pasted attachments scoped to the active session key', () => {
        const { result, rerender } = renderHook(
            ({ sessionKey }) => usePastedImageAttachments(sessionKey),
            { initialProps: { sessionKey: 'desktop-user:D:/tasks/pasted-project' } },
        );

        act(() => {
            result.current.setPendingAttachments([{ filePath: '/tmp/project.png', isImage: true, fileName: 'project.png', extension: '.png' }]);
        });
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['/tmp/project.png']);

        rerender({ sessionKey: 'desktop-user' });
        expect(result.current.pendingAttachments).toEqual([]);

        act(() => {
            result.current.setPendingAttachments([{ filePath: '/tmp/local.txt', isImage: false, fileName: 'local.txt', extension: '.txt' }]);
        });
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['/tmp/local.txt']);

        rerender({ sessionKey: 'desktop-user:D:/tasks/pasted-project' });
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['/tmp/project.png']);
    });

    it('drops pending attachments when a project session is forgotten', () => {
        const { result, rerender } = renderHook(
            ({ sessionKey }) => usePastedImageAttachments(sessionKey),
            { initialProps: { sessionKey: 'desktop-user:D:/tasks/pasted-project' } },
        );

        act(() => {
            result.current.setPendingAttachments([{ filePath: '/tmp/project.png', isImage: true, fileName: 'project.png', extension: '.png' }]);
        });
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['/tmp/project.png']);

        act(() => forgetAIAssistantSessionRounds('desktop-user:D:/tasks/pasted-project'));
        expect(result.current.pendingAttachments).toEqual([]);

        rerender({ sessionKey: 'desktop-user' });
        rerender({ sessionKey: 'desktop-user:D:/tasks/pasted-project' });
        expect(result.current.pendingAttachments).toEqual([]);
    });
});

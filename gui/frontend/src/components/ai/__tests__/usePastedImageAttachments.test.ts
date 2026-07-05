import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { usePastedImageAttachments } from '../usePastedImageAttachments';
import { forgetAIAssistantSessionRounds } from '../useAIAssistant';

describe('usePastedImageAttachments', () => {
    afterEach(() => {
        delete (window as any).go;
        vi.restoreAllMocks();
    });

    async function waitForCondition(condition: () => boolean) {
        for (let i = 0; i < 20; i += 1) {
            if (condition()) return;
            await new Promise(resolve => setTimeout(resolve, 0));
        }
        expect(condition()).toBe(true);
    }

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

    it('attaches dropped files that expose a native file path', async () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['hello'], 'report.txt', { type: 'text/plain', lastModified: 123 });
        Object.defineProperty(file, 'path', { value: 'D:\\work\\report.txt' });
        const preventDefault = vi.fn();

        await act(async () => {
            result.current.handleDrop({
                preventDefault,
                dataTransfer: {
                    items: null,
                    files: [file],
                },
            } as any);
            await Promise.resolve();
        });

        expect(preventDefault).toHaveBeenCalled();
        expect(result.current.pendingAttachments).toEqual([{
            filePath: 'D:\\work\\report.txt',
            isImage: false,
            fileName: 'report.txt',
            extension: '.txt',
        }]);
    });

    it('keeps dropped files with identical metadata when their native paths differ', async () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const first = new File(['same'], 'report.txt', { type: 'text/plain', lastModified: 123 });
        const second = new File(['same'], 'report.txt', { type: 'text/plain', lastModified: 123 });
        Object.defineProperty(first, 'path', { value: 'D:\\work\\a\\report.txt' });
        Object.defineProperty(second, 'path', { value: 'D:\\work\\b\\report.txt' });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: {
                    items: null,
                    files: [first, second],
                },
            } as any);
            await Promise.resolve();
        });

        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual([
            'D:\\work\\a\\report.txt',
            'D:\\work\\b\\report.txt',
        ]);
    });

    it('allows file drops without navigating away', () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const preventDefault = vi.fn();
        const stopPropagation = vi.fn();
        const event = {
            preventDefault,
            stopPropagation,
            dataTransfer: {
                types: ['Files'],
                dropEffect: 'none',
            },
        } as any;

        act(() => result.current.handleDragOver(event));

        expect(preventDefault).toHaveBeenCalled();
        expect(stopPropagation).toHaveBeenCalled();
        expect(event.dataTransfer.dropEffect).toBe('copy');
    });

    it('stops file drops from bubbling into parent drop zones', async () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['hello'], 'report.txt', { type: 'text/plain', lastModified: 123 });
        Object.defineProperty(file, 'path', { value: 'D:\\\\work\\\\report.txt' });
        const stopPropagation = vi.fn();

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                stopPropagation,
                dataTransfer: {
                    types: ['Files'],
                    items: null,
                    files: [file],
                },
            } as any);
            await Promise.resolve();
        });

        expect(stopPropagation).toHaveBeenCalled();
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['D:\\\\work\\\\report.txt']);
    });

    it('prevents default file drops even when no file object is available', () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const preventDefault = vi.fn();
        const stopPropagation = vi.fn();

        act(() => result.current.handleDrop({
            preventDefault,
            stopPropagation,
            dataTransfer: {
                types: ['Files'],
                items: null,
                files: [],
            },
        } as any));

        expect(preventDefault).toHaveBeenCalled();
        expect(stopPropagation).toHaveBeenCalled();
        expect(result.current.pendingAttachments).toEqual([]);
    });

    it('blocks attachments while disabled but still prevents file-drop navigation', async () => {
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user', { disabled: true }));
        const file = new File(['hello'], 'blocked.txt', { type: 'text/plain', lastModified: 123 });
        Object.defineProperty(file, 'path', { value: 'D:\\work\\blocked.txt' });
        const preventDefault = vi.fn();
        const dragEvent = {
            preventDefault: vi.fn(),
            dataTransfer: {
                types: ['Files'],
                dropEffect: 'copy',
            },
        } as any;

        act(() => result.current.handleDragOver(dragEvent));
        expect(dragEvent.preventDefault).toHaveBeenCalled();
        expect(dragEvent.dataTransfer.dropEffect).toBe('none');

        await act(async () => {
            result.current.handleDrop({
                preventDefault,
                dataTransfer: {
                    types: ['Files'],
                    items: null,
                    files: [file],
                },
            } as any);
            await Promise.resolve();
        });

        expect(preventDefault).toHaveBeenCalled();
        expect(result.current.pendingAttachments).toEqual([]);
    });

    it('keeps async pathless attachments scoped to the session where the drop started', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedFile = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        (window as any).go = {
            main: {
                App: {
                    SavePastedFile: savePastedFile,
                },
            },
        };
        const { result, rerender } = renderHook(
            ({ sessionKey }) => usePastedImageAttachments(sessionKey),
            { initialProps: { sessionKey: 'desktop-user:D:/project-a' } },
        );
        const file = new File(['slow'], 'slow.txt', { type: 'text/plain', lastModified: 456 });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: {
                    items: null,
                    files: [file],
                },
            } as any);
            await Promise.resolve();
        });

        await waitForCondition(() => savePastedFile.mock.calls.length === 1);
        rerender({ sessionKey: 'desktop-user:D:/project-b' });
        expect(result.current.pendingAttachments).toEqual([]);

        await act(async () => {
            resolveSave('D:\\tmp\\slow.txt');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(result.current.pendingAttachments).toEqual([]);
        rerender({ sessionKey: 'desktop-user:D:/project-a' });
        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['D:\\tmp\\slow.txt']);
    });

    it('does not resurrect async attachments after their session is forgotten', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedFile = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        (window as any).go = { main: { App: { SavePastedFile: savePastedFile } } };
        const sessionKey = 'desktop-user:D:/forgotten-project';
        const { result, rerender } = renderHook(
            ({ sessionKey }) => usePastedImageAttachments(sessionKey),
            { initialProps: { sessionKey } },
        );
        const file = new File(['slow'], 'forgotten.txt', { type: 'text/plain', lastModified: 789 });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: { items: null, files: [file] },
            } as any);
            await Promise.resolve();
        });
        await waitForCondition(() => savePastedFile.mock.calls.length === 1);

        act(() => forgetAIAssistantSessionRounds(sessionKey));
        await act(async () => {
            resolveSave('D:\\tmp\\forgotten.txt');
            await Promise.resolve();
            await Promise.resolve();
        });

        rerender({ sessionKey });
        expect(result.current.pendingAttachments).toEqual([]);
    });

    it('does not resurrect async attachments after the user clears the session attachments', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedFile = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        (window as any).go = { main: { App: { SavePastedFile: savePastedFile } } };
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['slow'], 'cleared.txt', { type: 'text/plain', lastModified: 790 });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: { items: null, files: [file] },
            } as any);
            await Promise.resolve();
        });
        await waitForCondition(() => savePastedFile.mock.calls.length === 1);

        act(() => result.current.setPendingAttachments([]));
        await act(async () => {
            resolveSave('D:\\tmp\\cleared.txt');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(result.current.pendingAttachments).toEqual([]);
    });

    it('does not append async attachments after the user replaces the session attachments', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedFile = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        (window as any).go = { main: { App: { SavePastedFile: savePastedFile } } };
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['slow'], 'old.txt', { type: 'text/plain', lastModified: 791 });
        const replacement = { filePath: 'D:\\tmp\\replacement.txt', fileName: 'replacement.txt', extension: '.txt', isImage: false };

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: { items: null, files: [file] },
            } as any);
            await Promise.resolve();
        });
        await waitForCondition(() => savePastedFile.mock.calls.length === 1);

        act(() => result.current.setPendingAttachments([replacement]));
        await act(async () => {
            resolveSave('D:\\tmp\\old.txt');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(result.current.pendingAttachments.map(att => att.filePath)).toEqual(['D:\\tmp\\replacement.txt']);
    });

    it('ignores async attachments that finish after the hook unmounts', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedFile = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
        (window as any).go = { main: { App: { SavePastedFile: savePastedFile } } };
        const { result, unmount } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['slow'], 'unmounted.txt', { type: 'text/plain', lastModified: 792 });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: { items: null, files: [file] },
            } as any);
            await Promise.resolve();
        });
        await waitForCondition(() => savePastedFile.mock.calls.length === 1);

        unmount();
        await act(async () => {
            resolveSave('D:\\tmp\\unmounted.txt');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(consoleError.mock.calls.some(call => String(call[0] || '').includes('unmounted component'))).toBe(false);
    });

    it('revokes image preview URLs when an async image attachment is rejected by a reset', async () => {
        let resolveSave!: (path: string) => void;
        const savePastedImage = vi.fn(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:rejected-image');
        const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
        (window as any).go = { main: { App: { SavePastedImage: savePastedImage } } };
        const { result } = renderHook(() => usePastedImageAttachments('desktop-user'));
        const file = new File(['png'], 'rejected.png', { type: 'image/png', lastModified: 793 });

        await act(async () => {
            result.current.handleDrop({
                preventDefault: vi.fn(),
                dataTransfer: { items: null, files: [file] },
            } as any);
            await Promise.resolve();
        });
        await waitForCondition(() => savePastedImage.mock.calls.length === 1);

        act(() => result.current.setPendingAttachments([]));
        await act(async () => {
            resolveSave('D:\\tmp\\rejected.png');
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(createObjectURL).toHaveBeenCalledWith(file);
        expect(revokeObjectURL).toHaveBeenCalledWith('blob:rejected-image');
        expect(result.current.pendingAttachments).toEqual([]);
    });

    it('keeps blob preview URLs alive across session switches and revokes them when removed', () => {
        const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
        const { result, rerender } = renderHook(
            ({ sessionKey }) => usePastedImageAttachments(sessionKey),
            { initialProps: { sessionKey: 'desktop-user:D:/project-a' } },
        );

        act(() => {
            result.current.setPendingAttachments([{
                filePath: 'D:\\work\\image.png',
                fileName: 'image.png',
                extension: '.png',
                isImage: true,
                thumbnailDataUrl: 'blob:preview-a',
            }]);
        });

        rerender({ sessionKey: 'desktop-user:D:/project-b' });
        expect(result.current.pendingAttachments).toEqual([]);
        expect(revokeObjectURL).not.toHaveBeenCalledWith('blob:preview-a');

        rerender({ sessionKey: 'desktop-user:D:/project-a' });
        expect(result.current.pendingAttachments.map(att => att.thumbnailDataUrl)).toEqual(['blob:preview-a']);

        act(() => result.current.setPendingAttachments([]));
        expect(revokeObjectURL).toHaveBeenCalledWith('blob:preview-a');
    });
});

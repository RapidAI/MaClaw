/**
 * Agent writes must surface the edited file in the preview pane.
 *
 * useCodePreviewState notifies the host panel (scope.onAgentFileWrite) for
 * create/modify events that belong to the tab's preview project, and for read
 * events the backend explicitly surfaced (force_open / auto_open_preview).
 * The panel bumps its file-focus nonce so the pane shows the file body —
 * content plus its +N -M modification status — instead of leaving the
 * directory tree selected. Events that would not actually land stay silent so
 * they cannot steal the view: unflagged reads (a read fills a background tab
 * and must never open the pane or change the selection), foreign-project
 * writes (an owned expert result write is the one exception), writes blocked
 * by the active-session guard (except force-open takeovers), identical
 * re-reads of the selected file, and identical redeliveries.
 */
import { describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

const eventHandlers = new Map<string, (data: any) => void>();

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (name: string, cb: (data: any) => void) => {
        eventHandlers.set(name, cb);
        return () => { eventHandlers.delete(name); };
    },
    EventsOff: vi.fn(),
}));

import { applyFileUpdate, fileEventSurfacesPreview, useCodePreviewState } from '../useCodePreviewState';

describe('fileEventSurfacesPreview', () => {
    it('surfaces create/modify always, and reads only when explicitly flagged', () => {
        expect(fileEventSurfacesPreview({ opType: 'modify' })).toBe(true);
        expect(fileEventSurfacesPreview({ opType: 'create' })).toBe(true);
        expect(fileEventSurfacesPreview({ opType: 'read', forceOpen: false, autoOpenPreview: false })).toBe(false);
        expect(fileEventSurfacesPreview({ opType: 'read', forceOpen: true, autoOpenPreview: false })).toBe(true);
        expect(fileEventSurfacesPreview({ opType: 'read', forceOpen: false, autoOpenPreview: true })).toBe(true);
    });
});

function emitFileUpdate(data: any) {
    const handler = eventHandlers.get('code:file_update');
    expect(handler).toBeTruthy();
    act(() => { handler?.(data); });
}

function modifyEvent(overrides: Record<string, unknown> = {}) {
    return {
        file_path: 'src/main.cpp',
        content: 'int main() { return 0; }',
        original: '// old',
        op_type: 'modify',
        project_path: 'D:/tasks/linux-sysinfo',
        session_id: 'session-1',
        ...overrides,
    };
}

describe('useCodePreviewState onAgentFileWrite', () => {
    it('notifies for a modify event belonging to the tab project', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent());

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'modify' });
    });

    it('notifies for create events too', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ op_type: 'create', original: undefined }));

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'create' });
    });

    it('stays silent for unflagged read events (snapshot restores must not steal the view)', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined }));

        expect(onWrite).not.toHaveBeenCalled();
    });

    it('a plain read never opens the pane nor selects, even with nothing selected yet', () => {
        const onWrite = vi.fn();
        const { result } = renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, auto_open_preview: false, force_open: false }));

        expect(onWrite).not.toHaveBeenCalled();
        expect(result.current.state.active).toBe(false);
        expect(result.current.state.activeFilePath).toBe('');
        expect(result.current.state.files.has('src/main.cpp')).toBe(true);

        // A coding turn starting is not a file change, so the pane stays closed.
        act(() => { eventHandlers.get('code:session_start')?.({ session_id: 'session-1', project_path: 'D:/tasks/linux-sysinfo', auto_open_preview: true }); });
        expect(result.current.state.active).toBe(false);

        // Same while the pane is already open on the directory tree.
        act(() => { result.current.reopenPanel(); });
        emitFileUpdate(modifyEvent({
            file_path: 'src/other.cpp',
            content: '// other',
            op_type: 'read',
            original: undefined,
        }));

        expect(result.current.state.active).toBe(true);
        expect(result.current.state.activeFilePath).toBe('');
        expect(result.current.state.files.has('src/other.cpp')).toBe(true);
    });

    it('opens the pane on a create or modify after a turn that only read', () => {
        const onWrite = vi.fn();
        const { result } = renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        act(() => { eventHandlers.get('code:session_start')?.({ session_id: 'session-1', project_path: 'D:/tasks/linux-sysinfo', auto_open_preview: true }); });
        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, force_open: false, auto_open_preview: false }));
        expect(result.current.state.active).toBe(false);
        expect(onWrite).not.toHaveBeenCalled();

        emitFileUpdate(modifyEvent({ force_open: true }));
        expect(result.current.state.active).toBe(true);
        expect(result.current.state.activeFilePath).toBe('src/main.cpp');
        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'modify' });
    });

    it('notifies for a force-open read so exploration shows the file body', () => {
        const onWrite = vi.fn();
        const { result } = renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, force_open: true }));

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'read' });
        expect(result.current.state.activeFilePath).toBe('src/main.cpp');
        expect(result.current.state.active).toBe(true);
    });

    it('notifies for an auto-open read even without force_open', () => {
        const onWrite = vi.fn();
        const { result } = renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, auto_open_preview: true }));

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(result.current.state.activeFilePath).toBe('src/main.cpp');
    });

    it('a force-open read selects over another open file and notifies once per change', () => {
        const onWrite = vi.fn();
        const { result } = renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({
            file_path: 'src/other.cpp',
            content: '// other',
            op_type: 'read',
            original: undefined,
            force_open: true,
        }));
        expect(result.current.state.activeFilePath).toBe('src/other.cpp');

        // First read of snake.cpp changes the selection → notify.
        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, force_open: true }));
        expect(onWrite).toHaveBeenCalledTimes(2);
        expect(result.current.state.activeFilePath).toBe('src/main.cpp');

        // Identical re-read of the selected file is a no-op → stays silent.
        emitFileUpdate(modifyEvent({ op_type: 'read', original: undefined, force_open: true }));
        expect(onWrite).toHaveBeenCalledTimes(2);
        expect(result.current.state.activeFilePath).toBe('src/main.cpp');
    });

    it('stays silent for writes stamped with another task', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        emitFileUpdate(modifyEvent({ project_path: 'D:/tasks/other-task' }));

        expect(onWrite).not.toHaveBeenCalled();
    });

    it('stays silent for a write blocked by the active-session guard', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        act(() => { eventHandlers.get('code:session_start')?.({ session_id: 'session-1', project_path: 'D:/tasks/linux-sysinfo' }); });
        emitFileUpdate(modifyEvent({ session_id: 'session-2' }));

        expect(onWrite).not.toHaveBeenCalled();

        // Control: the owning session's write still notifies.
        emitFileUpdate(modifyEvent({ session_id: 'session-1' }));
        expect(onWrite).toHaveBeenCalledTimes(1);
    });

    it('notifies once for an identical redelivery, not twice', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        const event = modifyEvent();
        emitFileUpdate(event);
        emitFileUpdate({ ...event });

        expect(onWrite).toHaveBeenCalledTimes(1);
    });

    it('notifies for a force-open takeover from another session', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, { onAgentFileWrite: onWrite }));

        act(() => { eventHandlers.get('code:session_start')?.({ session_id: 'session-1', project_path: 'D:/tasks/linux-sysinfo' }); });
        emitFileUpdate(modifyEvent({ session_id: 'session-2', force_open: true }));

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'modify' });
    });

    it('notifies for an owned expert write even when the file is stamped with another project', () => {
        // Expert result sessions own their writes regardless of the stamped
        // project: the expert tab writes into the tool workspace while the tab
        // is bound to the task directory. The expertWrite bypass — not the
        // project-belonging check — is what lets this notify.
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, {
            taskResultTab: true,
            expertId: 'paper-1',
            onAgentFileWrite: onWrite,
        }));

        emitFileUpdate(modifyEvent({
            project_path: 'D:/tasks/other-task',
            session_id: 'local-tools:desktop-user:expert:paper-1',
            force_open: true,
        }));

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'src/main.cpp', opType: 'modify' });
    });

    it('notifies for a latex result write on a latex result tab', () => {
        const onWrite = vi.fn();
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true, {
            latexResultTab: true,
            onAgentFileWrite: onWrite,
        }));

        emitFileUpdate({
            file_path: 'workspace/paper.tex',
            content: '\\documentclass{article}',
            op_type: 'modify',
            project_path: 'D:/tasks/linux-sysinfo',
            session_id: 'session-1',
        });

        expect(onWrite).toHaveBeenCalledTimes(1);
        expect(onWrite.mock.calls[0][0]).toMatchObject({ filePath: 'workspace/paper.tex', latexWorkbench: true, language: 'latex' });
    });

    it('keeps working when no callback is provided', () => {
        renderHook(() => useCodePreviewState('D:/tasks/linux-sysinfo', true));

        expect(() => emitFileUpdate(modifyEvent())).not.toThrow();
    });
});

describe('applyFileUpdate purity guard', () => {
    // The write-focus probe runs applyFileUpdate against the last rendered
    // state BEFORE the setState updater runs it again. If applyFileUpdate ever
    // mutates its inputs, the probe corrupts the very state the updater starts
    // from — silently. Frozen inputs make any mutation throw in strict mode.
    function frozenState() {
        const state = {
            sessionID: '',
            sessionActive: false,
            active: true,
            userClosed: false,
            activeFilePath: 'src/other.cpp',
            files: new Map([[
                'src/other.cpp',
                { filePath: 'src/other.cpp', fileName: 'other.cpp', content: 'x', opType: 'read', language: 'cpp', updatedAt: 1 },
            ]]),
            mruOrder: ['src/other.cpp'],
            pinnedPaths: [] as string[],
        };
        Object.freeze(state);
        Object.freeze(state.mruOrder);
        Object.freeze(state.pinnedPaths);
        for (const f of state.files.values()) Object.freeze(f);
        return state;
    }

    function frozenWriteFile() {
        const file = {
            sessionID: 'session-1',
            filePath: 'src/main.cpp',
            fileName: 'main.cpp',
            content: 'int main() { return 0; }',
            original: '// old',
            opType: 'modify',
            language: 'cpp',
            updatedAt: Date.now(),
            forceOpen: false,
            autoOpenPreview: false,
            previewTruncated: false,
            projectPath: 'D:/tasks/linux-sysinfo',
        };
        return Object.freeze(file);
    }

    it('never mutates its inputs when applying a write', () => {
        const state = frozenState();
        const file = frozenWriteFile();

        const next = applyFileUpdate(state as any, file as any);

        expect(next).not.toBe(state); // the write actually applied
        expect(state.activeFilePath).toBe('src/other.cpp');
        expect(state.mruOrder).toEqual(['src/other.cpp']);
        expect(state.files.get('src/main.cpp')).toBeUndefined();
        expect(file.opType).toBe('modify');
    });

    it('is deterministic across repeated runs from the same state (probe + updater)', () => {
        const state = frozenState();
        const file = frozenWriteFile();

        const first = applyFileUpdate(state as any, file as any);
        const second = applyFileUpdate(state as any, file as any);

        expect(second).toEqual(first);
    });
});

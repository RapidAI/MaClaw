// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, renderHook } from '@testing-library/react';
import { useProjectTaskActivateEvent } from '../useProjectTaskActivateEvent';
import type { AITab } from '../AITabTypes';

const { eventsOnMock, eventsOffMock } = vi.hoisted(() => ({
    eventsOnMock: vi.fn(),
    eventsOffMock: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: eventsOnMock,
    EventsOff: eventsOffMock,
}));

afterEach(() => {
    cleanup();
    eventsOnMock.mockReset();
    eventsOffMock.mockReset();
});

const tabs: AITab[] = [
    { id: 'local', type: 'local', title: 'Local', closable: false },
    { id: 'proj-a', type: 'project', title: 'Task A', projectPath: 'D:/tasks/a', closable: true },
    { id: 'cws-1', type: 'project', title: 'Cloud task', projectPath: 'C:/cache/cws_x', cloudWorkspaceId: 'cws_x', closable: true },
    { id: 'expert-1', type: 'expert', title: 'Paper expert', expertId: 'expert-paper', closable: true },
];

function setup() {
    const activateTab = vi.fn();
    const { unmount } = renderHook(() => useProjectTaskActivateEvent({ activateTab, getTabs: () => tabs }));
    const handler = eventsOnMock.mock.calls.find(([name]) => name === 'project-task:activate')?.[1];
    return { activateTab, handler, unmount };
}

describe('useProjectTaskActivateEvent', () => {
    it('activates the project tab matching by path, tolerating separators', () => {
        const { activateTab, handler } = setup();
        expect(handler).toBeTypeOf('function');

        handler({ projectPath: 'D:\\tasks\\a' });

        expect(activateTab).toHaveBeenCalledWith('proj-a');
    });

    it('prefers the cloud workspace identity over a stale cache path', () => {
        const { activateTab, handler } = setup();

        handler({ projectPath: 'D:/old/local/mount', cloudWorkspaceId: 'cws_x' });

        expect(activateTab).toHaveBeenCalledWith('cws-1');
    });

    it('activates the expert tab matching by expert id', () => {
        const { activateTab, handler } = setup();

        handler({ projectPath: 'D:/tasks/expert-workspace', expertId: 'expert-paper' });

        expect(activateTab).toHaveBeenCalledWith('expert-1');
    });

    it('supports a legacy string payload and ignores unknown targets', () => {
        const { activateTab, handler } = setup();

        handler('D:/tasks/a');
        expect(activateTab).toHaveBeenCalledWith('proj-a');

        activateTab.mockClear();
        handler({ projectPath: 'D:/tasks/unknown' });
        handler({});
        handler(null);
        expect(activateTab).not.toHaveBeenCalled();
    });

    it('uses the unsubscribe function returned by EventsOn on unmount', () => {
        const off = vi.fn();
        eventsOnMock.mockReturnValue(off);
        const { unmount } = renderHook(() => useProjectTaskActivateEvent({ activateTab: vi.fn(), getTabs: () => tabs }));

        unmount();

        expect(off).toHaveBeenCalledTimes(1);
        expect(eventsOffMock).not.toHaveBeenCalled();
    });

    it('unsubscribes on unmount', () => {
        const { unmount } = setup();

        unmount();

        expect(eventsOffMock).toHaveBeenCalledWith('project-task:activate');
    });
});

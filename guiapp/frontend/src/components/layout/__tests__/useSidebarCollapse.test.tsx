// @vitest-environment jsdom
import { act, cleanup, fireEvent, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { EVENT_OPEN_CREATE_CODING_TASK } from '../../../constants/events';
import { useSidebarCollapse } from '../useSidebarCollapse';

const PANE_KEY = 'maclaw:task-pane-collapsed.v1';
const RAIL_KEY = 'maclaw:nav-rail-collapsed.v1';

const pressB = (shift = false) => {
    fireEvent.keyDown(window, { ctrlKey: true, shiftKey: shift, key: 'b' });
};
const flushTasks = async () => {
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });
};

beforeEach(() => {
    window.localStorage.clear();
});

afterEach(() => {
    cleanup();
});

describe('useSidebarCollapse', () => {
    it('Ctrl+B toggles only the task pane on the AI workbench tab', () => {
        const { result } = renderHook(() => useSidebarCollapse('ai'));

        act(() => { pressB(); });
        expect(result.current.taskPaneCollapsed).toBe(true);
        expect(result.current.navRailCollapsed).toBe(false);

        act(() => { pressB(); });
        expect(result.current.taskPaneCollapsed).toBe(false);
    });

    it('Ctrl+B is inert for the task pane on non-AI tabs, Ctrl+Shift+B always toggles the rail', () => {
        const { result } = renderHook(() => useSidebarCollapse('settings'));

        act(() => { pressB(); });
        expect(result.current.taskPaneCollapsed).toBe(false);

        act(() => { pressB(true); });
        expect(result.current.navRailCollapsed).toBe(true);
        expect(result.current.taskPaneCollapsed).toBe(false);
    });

    it('collapses restore from localStorage on mount', () => {
        window.localStorage.setItem(PANE_KEY, '1');
        window.localStorage.setItem(RAIL_KEY, '1');
        const { result } = renderHook(() => useSidebarCollapse('ai'));

        expect(result.current.taskPaneCollapsed).toBe(true);
        expect(result.current.navRailCollapsed).toBe(true);
    });

    it('persists toggles back to localStorage', () => {
        const { result } = renderHook(() => useSidebarCollapse('ai'));

        act(() => { result.current.toggleTaskPaneCollapsed(); });
        act(() => { result.current.toggleNavRailCollapsed(); });
        expect(window.localStorage.getItem(PANE_KEY)).toBe('1');
        expect(window.localStorage.getItem(RAIL_KEY)).toBe('1');
    });

    it('a create-task event while collapsed restores the pane and re-dispatches once', async () => {
        window.localStorage.setItem(PANE_KEY, '1');
        const { result } = renderHook(() => useSidebarCollapse('ai'));
        expect(result.current.taskPaneCollapsed).toBe(true);

        let dispatched = 0;
        const spy = () => { dispatched += 1; };
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, spy);
        try {
            act(() => { window.dispatchEvent(new Event(EVENT_OPEN_CREATE_CODING_TASK)); });
            await flushTasks();

            expect(result.current.taskPaneCollapsed).toBe(false);
            expect(dispatched).toBe(2);
        } finally {
            window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, spy);
        }
    });

    it('a create-task event while expanded is not re-dispatched', async () => {
        const { result } = renderHook(() => useSidebarCollapse('ai'));

        let dispatched = 0;
        const spy = () => { dispatched += 1; };
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, spy);
        try {
            act(() => { window.dispatchEvent(new Event(EVENT_OPEN_CREATE_CODING_TASK)); });
            await flushTasks();

            expect(result.current.taskPaneCollapsed).toBe(false);
            expect(dispatched).toBe(1);
        } finally {
            window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, spy);
        }
    });

    it('openTaskPane restores a collapsed pane without touching the rail', () => {
        window.localStorage.setItem(PANE_KEY, '1');
        const { result } = renderHook(() => useSidebarCollapse('ai'));

        act(() => { result.current.openTaskPane(); });
        expect(result.current.taskPaneCollapsed).toBe(false);
        expect(result.current.navRailCollapsed).toBe(false);
    });
});

import { useCallback, useEffect, useState } from 'react';
import { EVENT_OPEN_CREATE_CODING_TASK } from '../../constants/events';

// Sidebar collapse state (功能栏 nav rail + 任务列表 task pane) survives
// relaunch like the pane width (TASK_PANE_WIDTH_KEY family).
export const NAV_RAIL_COLLAPSED_KEY = 'maclaw:nav-rail-collapsed.v1';
export const TASK_PANE_COLLAPSED_KEY = 'maclaw:task-pane-collapsed.v1';

function readSavedSidebarCollapsed(key: string): boolean {
    try {
        return window.localStorage.getItem(key) === '1';
    } catch {
        return false;
    }
}

function persistSidebarCollapsed(key: string, collapsed: boolean): void {
    try {
        window.localStorage.setItem(key, collapsed ? '1' : '0');
    } catch {
        // localStorage may be unavailable in embedded/dev contexts.
    }
}

export interface SidebarCollapseState {
    navRailCollapsed: boolean;
    taskPaneCollapsed: boolean;
    toggleNavRailCollapsed: () => void;
    toggleTaskPaneCollapsed: () => void;
    /** Explicit "open the task pane" intent (工作台 nav item) restores the pane. */
    openTaskPane: () => void;
}

/**
 * Collapse toggles for the two sidebar columns. Consumers: the title-bar
 * button cluster (SidebarToggleCluster), the resize divider double-click, the
 * 工作台 nav item and the global shortcuts. Shortcuts live here so every
 * entry path shares one state home: Ctrl/Cmd+B toggles the task pane (AI
 * workbench only), Ctrl/Cmd+Shift+B toggles the nav rail (present on every
 * tab).
 */
export function useSidebarCollapse(navTab: string): SidebarCollapseState {
    const [navRailCollapsed, setNavRailCollapsed] = useState(() => readSavedSidebarCollapsed(NAV_RAIL_COLLAPSED_KEY));
    const [taskPaneCollapsed, setTaskPaneCollapsed] = useState(() => readSavedSidebarCollapsed(TASK_PANE_COLLAPSED_KEY));

    useEffect(() => { persistSidebarCollapsed(NAV_RAIL_COLLAPSED_KEY, navRailCollapsed); }, [navRailCollapsed]);
    useEffect(() => { persistSidebarCollapsed(TASK_PANE_COLLAPSED_KEY, taskPaneCollapsed); }, [taskPaneCollapsed]);

    // Stable toggles so the button cluster / divider / nav item share one identity.
    const toggleNavRailCollapsed = useCallback(() => setNavRailCollapsed((prev) => !prev), []);
    const toggleTaskPaneCollapsed = useCallback(() => setTaskPaneCollapsed((prev) => !prev), []);
    const openTaskPane = useCallback(() => setTaskPaneCollapsed(false), []);

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.defaultPrevented) return;
            if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
            if (event.key.toLowerCase() !== 'b') return;
            event.preventDefault();
            if (event.shiftKey) {
                setNavRailCollapsed((prev) => !prev);
            } else if (navTab === 'ai') {
                setTaskPaneCollapsed((prev) => !prev);
            }
        };
        window.addEventListener('keydown', onKeyDown);
        return () => window.removeEventListener('keydown', onKeyDown);
    }, [navTab]);

    // The create-task event's consumers (middle-pane focus + the create dialog)
    // live INSIDE the task pane, so a welcome scenario card would silently
    // no-op while the pane is collapsed. Mounted only while collapsed: restore
    // the pane, then re-dispatch once the pane has remounted and its listeners
    // are attached again (next macrotask). Expanded panes keep their existing
    // untouched handling path.
    useEffect(() => {
        if (!taskPaneCollapsed) return;
        const handler = () => {
            openTaskPane();
            window.setTimeout(() => {
                window.dispatchEvent(new Event(EVENT_OPEN_CREATE_CODING_TASK));
            }, 0);
        };
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
        return () => window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
    }, [taskPaneCollapsed, openTaskPane]);

    return { navRailCollapsed, taskPaneCollapsed, toggleNavRailCollapsed, toggleTaskPaneCollapsed, openTaskPane };
}

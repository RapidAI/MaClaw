// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { AppSidebarShell } from '../AppSidebarShell';
import { SIDEBAR_AI_PANE_GAP, SIDEBAR_NAV_RAIL_WIDTH } from '../sidebarLayout';

// Contract under test: AppSidebarShell computes the shell width per the
// responsive breakpoints (rail 112 / 84 at ≤1200 / 64 at ≤560, pane hidden
// ≤900) and publishes it through the --mc-sidebar-shell-width inline var —
// the 120-app-responsive.css !important block (the AI-tab width authority)
// consumes it with the expanded calc as fallback. Collapsed columns UNMOUNT
// SidebarNavRail / SidebarAiPane (pane mount follows the manual collapse
// state only, so CSS hiding at ≤900px keeps its listeners alive).
const noop = () => {};

const setWindowWidth = (w: number) => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: w });
    window.dispatchEvent(new Event('resize'));
};

const baseProps = {
    navTab: 'ai',
    taskManagementPaneWidth: 308,
    aiThemeMode: 'light',
    aiDarkSchemeId: 'default',
    brandInfo: { id: 'maclaw' },
    currentIcon: null,
    brandSidebarName: 'MaClaw',
    switchTool: noop,
    lang: 'zh-Hans',
    maclawLLMOnline: true,
    remoteActivationStatus: null,
    qqBotStatus: '',
    telegramStatus: '',
    weixinStatus: '',
    lansengerStatus: '',
    t: (key: string) => key,
    gossipAllowed: false,
    config: {},
    activeTool: 'claude',
    toolDropdownOpen: false,
    setToolDropdownOpen: noop,
    tasks: [],
    renamingTaskPath: null,
    setRenamingTaskPath: noop,
    renameValue: '',
    setRenameValue: noop,
    resumeTask: noop,
    createTask: noop,
    refreshTasks: noop,
    taskContextMenu: { x: 0, y: 0, task: null },
    setTaskContextMenu: noop,
    renameTask: noop,
    pinTask: noop,
    hideTask: noop,
    handleTaskManagementResizeStart: noop,
    isTaskManagementResizing: false,
    sidebarCurrentProviderTokenUsage: {},
    sidebarHubCredits: null,
    formatSidebarTokens: () => '',
    formatSidebarHubExpiry: () => '',
    formatSidebarHubTotalCredits: () => '',
    formatSidebarHubUsedCredits: () => '',
    formatSidebarCredit: () => '',
    unlimitedHubCreditText: '',
    noHubAuthorizationText: '',
    showHubCreditAction: false,
    openHubCreditsPage: noop,
} as any;

const renderShell = (overrides: Record<string, unknown> = {}) => {
    const view = render(<AppSidebarShell {...baseProps} {...(overrides as any)} />);
    const shell = view.container.querySelector<HTMLElement>('.sidebar.office-agent-shell');
    const strip = view.container.querySelector<HTMLElement>('.mc-sidebar-drag-strip');
    if (!shell || !strip) throw new Error('sidebar shell/drag strip not rendered');
    const railMounted = () => !!view.container.querySelector('.mc-nav-rail');
    return { view, shell, strip, railMounted };
};

// Expanding the pane mounts the task list, whose effects call generated Wails
// bindings (window.go.main.App.*) that do not exist under jsdom.
beforeEach(() => {
    (window as any).go = {
        main: { App: new Proxy({}, { get: () => () => Promise.resolve(undefined) }) },
    };
});

afterEach(() => {
    cleanup();
});

describe('AppSidebarShell collapse contract', () => {
    it('expanded shell matches the wide breakpoint calc (rail + pane + gap) and publishes the var', () => {
        setWindowWidth(1400);
        const { shell, strip, railMounted } = renderShell();
        const expected = `${SIDEBAR_NAV_RAIL_WIDTH + 308 + SIDEBAR_AI_PANE_GAP}px`;

        expect(shell.style.width).toBe(expected);
        expect(strip.style.width).toBe(expected);
        expect(shell.style.getPropertyValue('--mc-sidebar-shell-width')).toBe(expected);
        expect(strip.style.getPropertyValue('--mc-sidebar-shell-width')).toBe(expected);
        expect(railMounted()).toBe(true);
        expect(screen.getByTestId('task-pane-resize-handle')).toBeTruthy();
        expect(shell.getAttribute('data-nav-rail-collapsed')).toBeNull();
        expect(shell.getAttribute('data-task-pane-collapsed')).toBeNull();
    });

    it('mirrors the compact-rail breakpoint (84px) at ≤1200px', () => {
        setWindowWidth(1024);
        const { shell } = renderShell();

        expect(shell.style.width).toBe(`${84 + 308 + SIDEBAR_AI_PANE_GAP}px`);
        expect(shell.style.getPropertyValue('--mc-sidebar-shell-width')).toBe(`${84 + 308 + SIDEBAR_AI_PANE_GAP}px`);
    });

    it('navRailCollapsed drops the rail and the gap, keeps the pane at its width', () => {
        setWindowWidth(1400);
        const { shell, railMounted } = renderShell({ navRailCollapsed: true });

        expect(shell.style.width).toBe('308px');
        expect(shell.style.getPropertyValue('--mc-sidebar-shell-width')).toBe('308px');
        expect(railMounted()).toBe(false);
        expect(screen.getByTestId('task-pane-resize-handle')).toBeTruthy();
        expect(shell.getAttribute('data-nav-rail-collapsed')).toBe('true');
        expect(shell.getAttribute('data-task-pane-collapsed')).toBeNull();
    });

    it('taskPaneCollapsed keeps the rail at the breakpoint width and drops the pane', () => {
        setWindowWidth(1400);
        const wide = renderShell({ taskPaneCollapsed: true });
        expect(wide.shell.style.width).toBe(`${SIDEBAR_NAV_RAIL_WIDTH}px`);
        expect(wide.railMounted()).toBe(true);
        expect(screen.queryByTestId('task-pane-resize-handle')).toBeNull();
        expect(wide.shell.getAttribute('data-task-pane-collapsed')).toBe('true');
        cleanup();

        setWindowWidth(1024);
        const compact = renderShell({ taskPaneCollapsed: true });
        expect(compact.shell.style.width).toBe('84px');
        expect(compact.shell.style.getPropertyValue('--mc-sidebar-shell-width')).toBe('84px');
    });

    it('both collapsed renders an empty shell with zero width', () => {
        setWindowWidth(1400);
        const { shell, railMounted } = renderShell({ navRailCollapsed: true, taskPaneCollapsed: true });

        expect(shell.style.width).toBe('0px');
        expect(shell.style.getPropertyValue('--mc-sidebar-shell-width')).toBe('0px');
        expect(railMounted()).toBe(false);
        expect(screen.queryByTestId('task-pane-resize-handle')).toBeNull();
        expect(shell.getAttribute('data-nav-rail-collapsed')).toBe('true');
        expect(shell.getAttribute('data-task-pane-collapsed')).toBe('true');
    });

    it('non-AI tabs never mount the task pane', () => {
        setWindowWidth(1400);
        const { shell, railMounted } = renderShell({ navTab: 'bots' });

        expect(shell.style.width).toBe(`${SIDEBAR_NAV_RAIL_WIDTH}px`);
        expect(railMounted()).toBe(true);
        expect(screen.queryByTestId('task-pane-resize-handle')).toBeNull();
        expect(shell.getAttribute('data-task-pane-collapsed')).toBeNull();
    });

    it('follows resize events across breakpoint buckets without unmounting the pane', () => {
        setWindowWidth(1024);
        const { shell, railMounted } = renderShell();
        const compactWidth = `${84 + 308 + SIDEBAR_AI_PANE_GAP}px`;
        expect(shell.style.width).toBe(compactWidth);
        expect(screen.getByTestId('task-pane-resize-handle')).toBeTruthy();

        act(() => { setWindowWidth(1400); });
        expect(shell.style.width).toBe(`${SIDEBAR_NAV_RAIL_WIDTH + 308 + SIDEBAR_AI_PANE_GAP}px`);
        expect(railMounted()).toBe(true);
    });
});

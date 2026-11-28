// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MainTopHeader } from '../MainTopHeader';
import { noteDesktopBotRooms, resetDesktopBotHeaderBridgeForTests } from '../../bots/desktopBotHeaderBridge';

const baseProps = {
    navTab: 'codex',
    lang: 'en',
    t: (key: string) => key,
    activeTool: 'codex',
    switchTool: vi.fn(),
    handleAddNewProject: vi.fn(),
    setRefreshStatus: vi.fn(),
    setTutorialContent: vi.fn(),
    setRefreshKey: vi.fn(),
    setShowModelSettings: vi.fn(),
    setSelectedSkillsToInstall: vi.fn(),
    setShowInstallSkillModal: vi.fn(),
    handleWindowHide: vi.fn(),
    handleWindowMaximizeToggle: vi.fn(),
    windowMaximized: false,
};

describe('MainTopHeader', () => {
    beforeEach(() => {
        resetDesktopBotHeaderBridgeForTests();
    });

    it('does not surface removed active tools in the coding tool switcher', () => {
        render(<MainTopHeader {...baseProps} activeTool="cursor" />);

        const select = screen.getByLabelText('Coding tool') as HTMLSelectElement;
        expect(select.value).toBe('claude');
        expect(screen.queryByText(new RegExp(['Cursor', 'Agent'].join(' '), 'i'))).toBeNull();
    });
    it('keeps all coding tools available in the top switcher even when sidebar entries are hidden', () => {
        render(<MainTopHeader {...baseProps} />);

        const select = screen.getByLabelText('Coding tool') as HTMLSelectElement;
        expect(Array.from(select.options).map((option) => option.value)).toEqual([
            'claude',
            'codex',
            'opencode',
            'codebuddy',
            'iflow',
            'kilo',
        ]);
    });
    it('keeps the notification bell out of the main header (it moved to the sidebar status row)', () => {
        render(<MainTopHeader {...baseProps} />);
        expect(screen.queryByTestId('main-header-notifications')).toBeNull();
    });

    it('renders the MaClaw wordmark without a duplicate M mark in the main header', () => {
        render(<MainTopHeader {...baseProps} />);

        expect(screen.getByLabelText('MaClaw').textContent).toContain('MaClaw');
        expect(document.querySelector('.top-header .mc-header-brand-mark')).toBeNull();
    });

    it('keeps the bot rooms and bug-report actions off the header on other tabs', () => {
        noteDesktopBotRooms({ visible: true, open: vi.fn() });
        render(<MainTopHeader {...baseProps} navTab="monitor" />);

        expect(screen.queryByTestId('bot-rooms-open')).toBeNull();
        expect(screen.queryByTestId('task-bug-report-btn')).toBeNull();
    });

    it('hosts the bot rooms button registered by the workspace, next to the bug report', () => {
        const openRooms = vi.fn();
        render(<MainTopHeader {...baseProps} navTab="bots" />);

        // Before the workspace registers, the rooms button is absent while
        // the bug-report control (bots-tab resident) already shows.
        expect(screen.queryByTestId('bot-rooms-open')).toBeNull();
        expect(screen.getByTestId('task-bug-report-btn')).toBeTruthy();

        act(() => { noteDesktopBotRooms({ visible: true, open: openRooms }); });
        fireEvent.click(screen.getByTestId('bot-rooms-open'));
        expect(openRooms).toHaveBeenCalledTimes(1);
    });

    it('drops the rooms button when the workspace revokes registration (no bot selected)', () => {
        const openRooms = vi.fn();
        render(<MainTopHeader {...baseProps} navTab="bots" />);
        act(() => { noteDesktopBotRooms({ visible: true, open: openRooms }); });
        expect(screen.getByTestId('bot-rooms-open')).toBeTruthy();

        act(() => { noteDesktopBotRooms(null); });
        expect(screen.queryByTestId('bot-rooms-open')).toBeNull();
    });

    it('keeps a double-click on the bot header actions from toggling window maximize', () => {
        const maximize = vi.fn();
        render(<MainTopHeader {...baseProps} navTab="bots" handleWindowMaximizeToggle={maximize} />);
        act(() => { noteDesktopBotRooms({ visible: true, open: vi.fn() }); });

        // The cluster sits in the drag region whose double-click maximizes;
        // both cluster buttons must stop the event at the container.
        fireEvent.doubleClick(screen.getByTestId('bot-rooms-open'));
        fireEvent.doubleClick(screen.getByTestId('task-bug-report-btn'));
        expect(maximize).not.toHaveBeenCalled();
    });

    it('keeps a double-click on the header action buttons from toggling window maximize', () => {
        const maximize = vi.fn();
        render(<MainTopHeader {...baseProps} navTab="codex" handleWindowMaximizeToggle={maximize} />);

        fireEvent.doubleClick(screen.getByText('Provider Config'));
        fireEvent.doubleClick(screen.getByText('apiStore'));
        expect(maximize).not.toHaveBeenCalled();
    });

    it('keeps a double-click on the window control buttons from toggling maximize', () => {
        const maximize = vi.fn();
        render(<MainTopHeader {...baseProps} navTab="bots" handleWindowMaximizeToggle={maximize} />);
        act(() => { noteDesktopBotRooms({ visible: true, open: vi.fn() }); });

        // The hide button's maximize toggle would fire on the just-hidden
        // window; the maximize button's two mousedowns already balance out.
        const hide = screen.getByLabelText(/Hide window/);
        const winMaximize = screen.getByLabelText(/Maximize window/);
        fireEvent.doubleClick(hide);
        fireEvent.doubleClick(winMaximize);
        expect(maximize).not.toHaveBeenCalled();
    });
});

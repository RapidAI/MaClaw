// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MainTopHeader } from '../MainTopHeader';

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
    it('routes the global bell to the shared system notification center', () => {
        const dispatchSpy = vi.spyOn(window, 'dispatchEvent');
        try {
            render(<MainTopHeader {...baseProps} />);
            fireEvent.click(screen.getByTestId('main-header-notifications'));
            expect(dispatchSpy).toHaveBeenCalledWith(expect.objectContaining({
                type: 'maclaw:open-system-notifications',
                detail: { toggle: true },
            }));
        } finally {
            dispatchSpy.mockRestore();
        }
    });

    it('renders the MaClaw wordmark and M mark together in the main header', () => {
        render(<MainTopHeader {...baseProps} />);

        expect(screen.getByLabelText('MaClaw').textContent).toContain('MaClaw');
        expect(document.querySelector('.mc-header-brand-mark')).toBeTruthy();
        expect(document.querySelector('.mc-header-brand-mark svg path')?.getAttribute('d')).toBe('M14 58V22l26 25 26-25v36');
    });
});

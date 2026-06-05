// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
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
});

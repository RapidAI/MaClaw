// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SidebarToolSelector } from '../SidebarToolSelector';

const baseProps = {
    activeTool: 'codex',
    toolDropdownOpen: false,
    setToolDropdownOpen: vi.fn(),
    config: {},
    switchTool: vi.fn(),
};

describe('SidebarToolSelector', () => {
    it('renders current tool as an accessible toggle button', () => {
        const setToolDropdownOpen = vi.fn();
        render(<SidebarToolSelector {...baseProps} setToolDropdownOpen={setToolDropdownOpen} />);

        const toggle = screen.getByRole('button', { name: /OpenAI Codex/i });
        expect(toggle.getAttribute('aria-expanded')).toBe('false');

        fireEvent.click(toggle);
        expect(setToolDropdownOpen).toHaveBeenCalledOnce();
    });

    it('filters hidden tools and switches from visible dropdown options', () => {
        const switchTool = vi.fn();
        render(
            <SidebarToolSelector
                {...baseProps}
                toolDropdownOpen
                config={{ show_kilo: false }}
                switchTool={switchTool}
            />,
        );

        expect(screen.getByRole('group', { name: 'Coding tools' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /Kilo Code/i })).toBeNull();

        const claude = screen.getByRole('button', { name: /Claude Code/i });
        fireEvent.click(claude);
        expect(switchTool).toHaveBeenCalledWith('claude');
    });

    it('falls back to Claude Code when stored active tool was removed', () => {
        render(<SidebarToolSelector {...baseProps} activeTool="cursor" toolDropdownOpen />);

        const claudeButtons = screen.getAllByRole('button', { name: /Claude Code/i });
        expect(claudeButtons.length).toBeGreaterThan(0);
        expect(screen.queryByText(new RegExp(['Cursor', 'Agent'].join(' '), 'i'))).toBeNull();
        expect(claudeButtons.some((button) => button.getAttribute('aria-current') === 'true')).toBe(true);
    });
});

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SettingsTabsRail } from '../SettingsTabsRail';
import type { SettingsTabOption } from '../../../config/settingsTabs';

const tabs: SettingsTabOption[] = [
    { id: 'general', label: 'General', desc: 'Language, projects, and environment', icon: '<svg></svg>' },
    { id: 'pet', label: 'Pet', desc: 'Desktop pet appearance and interaction settings', icon: '<svg></svg>' },
];

describe('SettingsTabsRail', () => {
    it('keeps tab descriptions out of the rail and shows them in a tooltip', () => {
        render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />);

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        expect(petTab).toBeTruthy();
        expect(petTab.textContent).toBe('Pet');
        expect(screen.queryByRole('tooltip')).toBeNull();

        fireEvent.mouseEnter(petTab);
        const tooltip = screen.getByRole('tooltip');
        expect(tooltip.textContent).toContain('Pet');
        expect(tooltip.textContent).toContain('Desktop pet appearance and interaction settings');
    });

    it('selects tabs through the compact rail button', () => {
        const onChange = vi.fn();
        render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={onChange} />);

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        fireEvent.mouseEnter(petTab);
        expect(screen.getByRole('tooltip')).toBeTruthy();
        fireEvent.click(petTab);

        expect(onChange).toHaveBeenCalledWith('pet');
        expect(screen.queryByRole('tooltip')).toBeNull();
    });

    it('dismisses the tooltip from the keyboard', () => {
        const parentKeyHandler = vi.fn();
        render(
            <div onKeyDown={parentKeyHandler}>
                <SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />
            </div>,
        );

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        fireEvent.focus(petTab);
        expect(screen.getByRole('tooltip')).toBeTruthy();
        fireEvent.keyDown(petTab, { key: 'Escape' });

        expect(screen.queryByRole('tooltip')).toBeNull();
        expect(parentKeyHandler).not.toHaveBeenCalled();
    });

    it('keeps tooltip position inside the viewport near the right edge', () => {
        render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />);

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        vi.spyOn(petTab, 'getBoundingClientRect').mockReturnValue({
            x: 790,
            y: 120,
            width: 80,
            height: 40,
            top: 120,
            right: 870,
            bottom: 160,
            left: 790,
            toJSON: () => ({}),
        });

        fireEvent.mouseEnter(petTab);
        const tooltip = screen.getByRole('tooltip');

        expect(Number.parseFloat(tooltip.style.left)).toBeLessThanOrEqual(window.innerWidth - 312);
    });

    it('dismisses stale tooltip on viewport changes', () => {
        render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />);

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        fireEvent.mouseEnter(petTab);
        expect(screen.getByRole('tooltip')).toBeTruthy();

        fireEvent(window, new Event('resize'));

        expect(screen.queryByRole('tooltip')).toBeNull();
    });

    it('dismisses stale tooltip when a scroll container moves', () => {
        render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />);

        const petTab = screen.getByRole('button', { name: 'Pet: Desktop pet appearance and interaction settings' });
        fireEvent.mouseEnter(petTab);
        expect(screen.getByRole('tooltip')).toBeTruthy();

        fireEvent.scroll(petTab.parentElement as Element);

        expect(screen.queryByRole('tooltip')).toBeNull();
    });
});

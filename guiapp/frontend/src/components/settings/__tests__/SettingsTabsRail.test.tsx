import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { revealElementInScrollport, scrollportContentInset, SettingsTabsRail } from '../SettingsTabsRail';
import type { SettingsTabOption } from '../../../config/settingsTabs';

const tabs: SettingsTabOption[] = [
    { id: 'general', label: 'General', desc: 'Language, projects, and environment', icon: '<svg></svg>' },
    { id: 'pet', label: 'Pet', desc: 'Desktop pet appearance and interaction settings', icon: '<svg></svg>' },
];

describe('revealElementInScrollport', () => {
    const rect = (top: number, bottom: number) => ({
        x: 0, y: top, width: 80, height: bottom - top, top, right: 80, bottom, left: 0, toJSON: () => ({}),
    });

    it('scrolls a target below the port into view without moving an ancestor', () => {
        const scrollport = {
            scrollTop: 20,
            getBoundingClientRect: () => rect(40, 140),
        };
        const target = { getBoundingClientRect: () => rect(200, 240) };

        revealElementInScrollport(scrollport, target);

        expect(scrollport.scrollTop).toBe(20 + (240 - (140 - 8)));
    });

    it('clears the rail padding and border, not only the outer border box', () => {
        expect(scrollportContentInset({
            clientTop: 1,
            clientHeight: 500,
            offsetHeight: 502,
            paddingTop: 12,
            paddingBottom: 12,
        })).toEqual({ top: 13, bottom: 13 });

        const scrollport = {
            scrollTop: 20,
            getBoundingClientRect: () => rect(40, 140),
        };
        const target = { getBoundingClientRect: () => rect(200, 240) };

        revealElementInScrollport(scrollport, target, 8, { top: 13, bottom: 13 });

        expect(scrollport.scrollTop).toBe(20 + (240 - (140 - 13 - 8)));
    });

    it('leaves the scrollport alone when the target is already visible', () => {
        const scrollport = {
            scrollTop: 12,
            getBoundingClientRect: () => rect(40, 240),
        };
        const target = { getBoundingClientRect: () => rect(80, 120) };

        revealElementInScrollport(scrollport, target);

        expect(scrollport.scrollTop).toBe(12);
    });
});

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

    it('renders group labels in first-appearance order and groups tabs under them', () => {
        const groupedTabs: SettingsTabOption[] = [
            { id: 'general', label: 'General', desc: 'g', icon: '<svg></svg>', group: 'essentials', groupLabel: 'Essentials' },
            { id: 'proxy', label: 'Proxy', desc: 'p', icon: '<svg></svg>', group: 'essentials', groupLabel: 'Essentials' },
            { id: 'llm', label: 'LLM', desc: 'l', icon: '<svg></svg>', group: 'ai', groupLabel: 'AI & Models' },
            { id: 'embedding', label: 'Embedding', desc: 'e', icon: '<svg></svg>', group: 'ai', groupLabel: 'AI & Models' },
        ];
        const { container } = render(<SettingsTabsRail tabs={groupedTabs} activeTab="llm" onChange={vi.fn()} />);

        const labels = Array.from(container.querySelectorAll('.settings-top-tabs__group-label')).map((node) => node.textContent);
        expect(labels).toEqual(['Essentials', 'AI & Models']);

        const groups = container.querySelectorAll('.settings-top-tabs__group');
        expect(groups).toHaveLength(2);
        expect(groups[0].querySelectorAll('.settings-top-tab')).toHaveLength(2);
        expect(groups[1].querySelectorAll('.settings-top-tab')).toHaveLength(2);
        expect(groups[1].querySelector('.settings-top-tab.active')?.textContent).toBe('LLM');
    });

    it('renders tabs without a group under no group label', () => {
        const { container } = render(<SettingsTabsRail tabs={tabs} activeTab="general" onChange={vi.fn()} />);

        expect(container.querySelector('.settings-top-tabs__group-label')).toBeNull();
        expect(container.querySelectorAll('.settings-top-tab')).toHaveLength(2);
    });
});

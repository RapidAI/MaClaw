import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { SettingsTabId, SettingsTabOption } from '../../config/settingsTabs';

interface SettingsTabsRailProps {
    tabs: SettingsTabOption[];
    activeTab: SettingsTabId;
    onChange: (tab: SettingsTabId) => void;
}

type SettingsTabTooltip = {
    id: SettingsTabId;
    label: string;
    desc: string;
    left: number;
    top: number;
} | null;

const tooltipOffset = 10;
const tooltipMaxWidth = 300;
const tooltipViewportPadding = 12;
const activeTabRevealPadding = 8;

export type ScrollportContentBox = {
    clientTop: number;
    clientHeight: number;
    offsetHeight: number;
    paddingTop: number;
    paddingBottom: number;
};

/** Border plus padding. The border box is outside the area that can show a tab. */
export function scrollportContentInset(box: ScrollportContentBox) {
    const borderBottom = box.offsetHeight - box.clientHeight - box.clientTop;
    return {
        top: box.clientTop + box.paddingTop,
        bottom: Math.max(0, borderBottom) + box.paddingBottom,
    };
}

/** Move only this scrollport. scrollIntoView would also scroll the page. */
export function revealElementInScrollport(
    scrollport: Pick<HTMLElement, 'scrollTop' | 'getBoundingClientRect'>,
    target: Pick<HTMLElement, 'getBoundingClientRect'>,
    padding = activeTabRevealPadding,
    inset = { top: 0, bottom: 0 },
) {
    const portRect = scrollport.getBoundingClientRect();
    const itemRect = target.getBoundingClientRect();
    const top = portRect.top + inset.top + padding;
    const bottom = portRect.bottom - inset.bottom - padding;
    if (bottom <= top) return;
    if (itemRect.top < top) {
        scrollport.scrollTop -= top - itemRect.top;
    } else if (itemRect.bottom > bottom) {
        scrollport.scrollTop += itemRect.bottom - bottom;
    }
}

export const SettingsTabsRail = ({ tabs, activeTab, onChange }: SettingsTabsRailProps) => {
    const [tooltip, setTooltip] = useState<SettingsTabTooltip>(null);
    const navRef = useRef<HTMLElement>(null);

    useEffect(() => {
        const nav = navRef.current;
        if (!nav) return;
        const active = nav.querySelector<HTMLElement>('.settings-top-tab.active');
        if (!active) return;
        const frame = requestAnimationFrame(() => {
            const style = getComputedStyle(nav);
            revealElementInScrollport(nav, active, activeTabRevealPadding, scrollportContentInset({
                clientTop: nav.clientTop,
                clientHeight: nav.clientHeight,
                offsetHeight: nav.offsetHeight,
                paddingTop: Number.parseFloat(style.paddingTop) || 0,
                paddingBottom: Number.parseFloat(style.paddingBottom) || 0,
            }));
        });
        return () => cancelAnimationFrame(frame);
    }, [activeTab]);

    useEffect(() => {
        if (!tooltip) return;
        const hideTooltip = () => setTooltip(null);
        window.addEventListener('resize', hideTooltip);
        window.addEventListener('scroll', hideTooltip, true);
        return () => {
            window.removeEventListener('resize', hideTooltip);
            window.removeEventListener('scroll', hideTooltip, true);
        };
    }, [tooltip]);

    const showTooltip = (tab: SettingsTabOption, target: HTMLElement) => {
        const rect = target.getBoundingClientRect();
        const shouldPlaceLeft = rect.right + tooltipOffset + tooltipMaxWidth > window.innerWidth - tooltipViewportPadding;
        const preferredLeft = shouldPlaceLeft ? rect.left - tooltipOffset - tooltipMaxWidth : rect.right + tooltipOffset;
        const maxLeft = Math.max(window.innerWidth - tooltipViewportPadding - tooltipMaxWidth, tooltipViewportPadding);
        const nextLeft = Math.min(Math.max(preferredLeft, tooltipViewportPadding), maxLeft);
        const nextTop = Math.min(
            Math.max(rect.top + rect.height / 2, 48),
            Math.max(window.innerHeight - 48, 48),
        );

        setTooltip({
            id: tab.id,
            label: tab.label,
            desc: tab.desc,
            left: nextLeft,
            top: nextTop,
        });
    };

    const tooltipId = tooltip ? `settings-tab-tooltip-${tooltip.id}` : undefined;

    // Bucket tabs into their nav groups, preserving first-appearance order so the
    // rail matches the flat tab order from getSettingsTabOptions.
    const railGroups: { key: string; label?: string; items: SettingsTabOption[] }[] = [];
    for (const tab of tabs) {
        const key = tab.group ?? '__ungrouped__';
        let group = railGroups.find((entry) => entry.key === key);
        if (!group) {
            group = { key, label: tab.groupLabel, items: [] };
            railGroups.push(group);
        }
        group.items.push(tab);
    }

    // Portal tooltips to document.body so they never become a third grid item
    // inside .settings-shell (hovering tabs could otherwise distort the nav/content layout).
    const tooltipNode = tooltip && typeof document !== 'undefined'
        ? createPortal(
            <div
                id={tooltipId}
                role="tooltip"
                className="settings-tab-tooltip"
                style={{ left: tooltip.left, top: tooltip.top }}
            >
                <strong>{tooltip.label}</strong>
                <span>{tooltip.desc}</span>
            </div>,
            document.body,
        )
        : null;

    return (
        <>
            <nav ref={navRef} className="settings-top-tabs" aria-label="Settings sections">
                {railGroups.map((group) => (
                    <div className="settings-top-tabs__group" key={group.key}>
                        {group.label ? (
                            <div className="settings-top-tabs__group-label">{group.label}</div>
                        ) : null}
                        {group.items.map((tab) => (
                            <button
                                key={tab.id}
                                type="button"
                                className={`settings-top-tab ${activeTab === tab.id ? 'active' : ''}`}
                                onClick={() => {
                                    setTooltip(null);
                                    onChange(tab.id);
                                }}
                                onKeyDown={(event) => {
                                    if (event.key === 'Escape') {
                                        event.stopPropagation();
                                        setTooltip(null);
                                    }
                                }}
                                onMouseEnter={(event) => showTooltip(tab, event.currentTarget)}
                                onMouseLeave={() => setTooltip(null)}
                                onFocus={(event) => showTooltip(tab, event.currentTarget)}
                                onBlur={() => setTooltip(null)}
                                aria-current={activeTab === tab.id ? 'page' : undefined}
                                aria-describedby={tooltip?.id === tab.id ? tooltipId : undefined}
                                aria-label={`${tab.label}: ${tab.desc}`}
                            >
                                <span className="settings-top-tab__icon" aria-hidden="true" dangerouslySetInnerHTML={{ __html: tab.icon }} />
                                <span className="settings-top-tab__text">
                                    <span className="settings-top-tab__label">{tab.label}</span>
                                </span>
                            </button>
                        ))}
                    </div>
                ))}
            </nav>
            {tooltipNode}
        </>
    );
};

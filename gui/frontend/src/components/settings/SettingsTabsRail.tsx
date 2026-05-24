import { useEffect, useState } from 'react';
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

export const SettingsTabsRail = ({ tabs, activeTab, onChange }: SettingsTabsRailProps) => {
    const [tooltip, setTooltip] = useState<SettingsTabTooltip>(null);

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

    return (
        <>
            <nav className="settings-top-tabs" aria-label="Settings sections">
                {tabs.map((tab) => (
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
                        <span className="settings-top-tab__mark" aria-hidden="true" />
                        <span className="settings-top-tab__text">
                            <span className="settings-top-tab__label">{tab.label}</span>
                        </span>
                    </button>
                ))}
            </nav>
            {tooltip && (
                <div
                    id={tooltipId}
                    role="tooltip"
                    className="settings-tab-tooltip"
                    style={{ left: tooltip.left, top: tooltip.top }}
                >
                    <strong>{tooltip.label}</strong>
                    <span>{tooltip.desc}</span>
                </div>
            )}
        </>
    );
};

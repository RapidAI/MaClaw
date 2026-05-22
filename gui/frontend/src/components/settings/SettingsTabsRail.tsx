import type { SettingsTabId, SettingsTabOption } from '../../config/settingsTabs';

interface SettingsTabsRailProps {
    tabs: SettingsTabOption[];
    activeTab: SettingsTabId;
    onChange: (tab: SettingsTabId) => void;
}

export const SettingsTabsRail = ({ tabs, activeTab, onChange }: SettingsTabsRailProps) => (
    <nav className="settings-top-tabs" aria-label="Settings sections">
        {tabs.map((tab) => (
            <button
                key={tab.id}
                type="button"
                className={`settings-top-tab ${activeTab === tab.id ? 'active' : ''}`}
                onClick={() => onChange(tab.id)}
                role="tab"
                aria-selected={activeTab === tab.id}
                title={tab.desc}
            >
                <span className="settings-top-tab__mark" aria-hidden="true" />
                <span className="settings-top-tab__text">
                    <span className="settings-top-tab__label">{tab.label}</span>
                    <span className="settings-top-tab__desc">{tab.desc}</span>
                </span>
            </button>
        ))}
    </nav>
);

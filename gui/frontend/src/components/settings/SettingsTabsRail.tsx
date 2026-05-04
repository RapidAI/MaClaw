import type { SettingsTabId, SettingsTabOption } from '../../config/settingsTabs';

interface SettingsTabsRailProps {
    tabs: SettingsTabOption[];
    activeTab: SettingsTabId;
    onChange: (tab: SettingsTabId) => void;
}

export const SettingsTabsRail = ({ tabs, activeTab, onChange }: SettingsTabsRailProps) => (
    <div className="settings-top-tabs">
        {tabs.map((tab) => (
            <button
                key={tab.id}
                type="button"
                className={`settings-top-tab ${activeTab === tab.id ? 'active' : ''}`}
                onClick={() => onChange(tab.id)}
                title={tab.desc}
            >
                {tab.label}
            </button>
        ))}
    </div>
);

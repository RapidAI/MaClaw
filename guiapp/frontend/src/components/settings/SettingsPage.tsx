import type { SettingsTabId, SettingsTabOption } from '../../config/settingsTabs';
import { SettingsActiveContent, type SettingsActiveContentProps } from './SettingsActiveContent';
import { SettingsTabsRail } from './SettingsTabsRail';

export type SettingsPageProps = Omit<SettingsActiveContentProps, 'settingsTab'> & {
    tabs: SettingsTabOption[];
    /** Already-resolved active tab (matches rail + body). */
    activeTab: SettingsTabId;
    onChangeTab: (tab: SettingsTabId) => void;
    /** Home knowledge entry: the category rail stays off this page. */
    hideNav?: boolean;
};

/**
 * Settings shell: left rail + active panel body.
 * Kept outside page-level Suspense by the caller; general panels inside are eager.
 *
 * `settings-shell__body` owns grid-area:content so Suspense/ErrorBoundary wrappers
 * inside cannot knock the panel out of the grid (which looked like a blank page).
 */
export function SettingsPage({ tabs, activeTab, onChangeTab, hideNav = false, ...contentProps }: SettingsPageProps) {
    return (
        <div className={`settings-shell settings-shell--padded${hideNav ? ' settings-shell--solo' : ''}`}>
            {hideNav ? null : (
                <SettingsTabsRail
                    tabs={tabs}
                    activeTab={activeTab}
                    onChange={onChangeTab}
                />
            )}
            <div className="settings-shell__body">
                <SettingsActiveContent
                    {...contentProps}
                    settingsTab={activeTab}
                />
            </div>
        </div>
    );
}

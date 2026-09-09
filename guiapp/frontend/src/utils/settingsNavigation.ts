import type { SettingsTabId } from '../config/settingsTabs';

export const OPEN_SETTINGS_EVENT = 'maclaw:open-settings';

export function openSettingsTab(tab: SettingsTabId): void {
    window.dispatchEvent(new CustomEvent(OPEN_SETTINGS_EVENT, { detail: { tab } }));
}

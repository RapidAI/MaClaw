import type { SettingsTabId } from '../config/settingsTabs';

export const OPEN_SETTINGS_EVENT = 'maclaw:open-settings';

export type OpenSettingsDetail = {
    tab: SettingsTabId;
    /** Home knowledge entry: show the panel without the settings category rail. */
    hideSettingsNav?: boolean;
};

export function openSettingsTab(tab: SettingsTabId, options?: { hideSettingsNav?: boolean }): void {
    const detail: OpenSettingsDetail = { tab };
    if (options?.hideSettingsNav) detail.hideSettingsNav = true;
    window.dispatchEvent(new CustomEvent(OPEN_SETTINGS_EVENT, { detail }));
}

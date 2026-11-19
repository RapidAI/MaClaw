import type { SettingsTabId } from '../config/settingsTabs';

export const OPEN_SETTINGS_EVENT = 'maclaw:open-settings';

export type OpenSettingsDetail = {
    tab: SettingsTabId;
    /** Home knowledge entry: show the panel without the settings category rail. */
    hideSettingsNav?: boolean;
    /** LLM tab: auto-open the provider-management dialog once its list is ready. */
    autoOpenProviderDialog?: boolean;
};

export function openSettingsTab(tab: SettingsTabId, options?: { hideSettingsNav?: boolean }): void {
    const detail: OpenSettingsDetail = { tab };
    if (options?.hideSettingsNav) detail.hideSettingsNav = true;
    window.dispatchEvent(new CustomEvent(OPEN_SETTINGS_EVENT, { detail }));
}

/**
 * Settings → LLM with the provider-management dialog already open (agent model
 * menu "add custom model"; bot model settings "添加服务商"). The seq consumed by
 * useProviderDialogDeepLink survives the settings panel's lazy mount; the panel
 * opens the dialog once its provider list is ready.
 */
export function openLLMProviderManagement(): void {
    const detail: OpenSettingsDetail = { tab: 'llm', autoOpenProviderDialog: true };
    window.dispatchEvent(new CustomEvent(OPEN_SETTINGS_EVENT, { detail }));
}

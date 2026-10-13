export const SOLO_SETTINGS_TABS = ['knowledge', 'im', 'email'] as const;

export type SoloSettingsTab = (typeof SOLO_SETTINGS_TABS)[number];

const soloSet = new Set<string>(SOLO_SETTINGS_TABS);

/** Hide the settings category rail only for the tab that asked, and only when it is allowed to. */
export function grantSolo(tab: string, requested: boolean): SoloSettingsTab | '' {
    if (!requested || !soloSet.has(tab)) return '';
    return tab as SoloSettingsTab;
}

export function settingsNavIsSolo(soloTab: string, resolvedTab: string): boolean {
    return soloTab !== '' && soloTab === resolvedTab;
}

/** Solo IM or solo mailbox highlights 接入. The same tabs inside full Settings do not. */
export function connectSoloActive(navTab: string, soloTab: string): boolean {
    return navTab === 'settings' && (soloTab === 'im' || soloTab === 'email');
}

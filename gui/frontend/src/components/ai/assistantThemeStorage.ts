import { AI_THEME_MODE_LEGACY_STORAGE_KEY, AI_THEME_MODE_STORAGE_KEY } from "./aiAssistantPanelTheme";

export type AssistantThemeMode = 'light' | 'dark';

export function isAssistantThemeMode(value: unknown): value is AssistantThemeMode {
    return value === 'dark' || value === 'light';
}

export function readStoredAssistantThemeMode(): AssistantThemeMode {
    if (typeof window === 'undefined') return 'light';
    try {
        const storedThemeMode = window.localStorage.getItem(AI_THEME_MODE_STORAGE_KEY) || window.localStorage.getItem(AI_THEME_MODE_LEGACY_STORAGE_KEY);
        return storedThemeMode === 'dark' ? 'dark' : 'light';
    } catch {
        return 'light';
    }
}

export function writeStoredAssistantThemeMode(themeMode: AssistantThemeMode): void {
    if (typeof window === 'undefined') return;
    try {
        window.localStorage.setItem(AI_THEME_MODE_STORAGE_KEY, themeMode);
        window.localStorage.setItem(AI_THEME_MODE_LEGACY_STORAGE_KEY, themeMode);
    } catch {
        // Ignore storage failures in restricted webviews.
    }
}

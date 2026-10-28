import {
    isAssistantDarkSchemeId,
    readStoredAssistantDarkSchemeId,
    writeStoredAssistantDarkSchemeId,
    type AssistantDarkSchemeId,
} from "./assistantDarkSchemes";
import {
    isAssistantLightSchemeId,
    readStoredAssistantLightSchemeId,
    writeStoredAssistantLightSchemeId,
    type AssistantLightSchemeId,
} from "./assistantLightSchemes";
import { readStoredAssistantThemeMode, writeStoredAssistantThemeMode, type AssistantThemeMode } from "./assistantThemeStorage";
import { enqueueUIAppearancePublish } from "../filecompanion/fileCompanionBootFlag";

export const UI_APPEARANCE_EVENT = "ui-appearance:changed";

export type UIAppearanceChoice = {
    themeMode: AssistantThemeMode;
    darkSchemeId: AssistantDarkSchemeId;
    lightSchemeId: AssistantLightSchemeId;
};

type AppearanceBinding = {
    GetUIAppearance?: () => Promise<unknown>;
    SetUIAppearance?: (value: { themeMode: string; darkScheme: string; lightScheme: string }) => Promise<unknown>;
};

function appearanceBinding(): AppearanceBinding | undefined {
    const app = (window as unknown as { go?: { main?: { App?: AppearanceBinding } } }).go?.main?.App;
    return app;
}

export function readLocalUIAppearance(): UIAppearanceChoice {
    return {
        themeMode: readStoredAssistantThemeMode(),
        darkSchemeId: readStoredAssistantDarkSchemeId(),
        lightSchemeId: readStoredAssistantLightSchemeId(),
    };
}

function field(row: Record<string, unknown>, ...keys: string[]): unknown {
    for (const key of keys) {
        if (row[key] !== undefined && row[key] !== null) return row[key];
    }
    return undefined;
}

/** A payload without a light/dark mode is ignored so a missing file cannot reset the window. */
export function parseUIAppearance(payload: unknown): UIAppearanceChoice | null {
    const raw = Array.isArray(payload) ? payload[0] : payload;
    if (!raw || typeof raw !== "object") return null;
    const row = raw as Record<string, unknown>;
    const mode = field(row, "themeMode", "ThemeMode");
    if (mode !== "dark" && mode !== "light") return null;
    const dark = field(row, "darkScheme", "DarkScheme");
    const light = field(row, "lightScheme", "LightScheme");
    return {
        themeMode: mode,
        darkSchemeId: isAssistantDarkSchemeId(dark) ? dark : readStoredAssistantDarkSchemeId(),
        lightSchemeId: isAssistantLightSchemeId(light) ? light : readStoredAssistantLightSchemeId(),
    };
}

export function writeLocalUIAppearance(choice: UIAppearanceChoice): void {
    writeStoredAssistantThemeMode(choice.themeMode);
    writeStoredAssistantDarkSchemeId(choice.darkSchemeId);
    writeStoredAssistantLightSchemeId(choice.lightSchemeId);
}

// applyDocumentThemeAttributes stamps the shell theme on the document element.
// The companion has no #App. These attributes are what the theme stylesheet matches.
export function applyDocumentThemeAttributes(choice: UIAppearanceChoice): void {
    const root = document.documentElement;
    root.setAttribute("data-ai-theme", choice.themeMode);
    root.setAttribute("data-boot-theme", choice.themeMode);
    if (choice.themeMode === "dark") {
        root.setAttribute("data-ai-dark-scheme", choice.darkSchemeId);
        root.removeAttribute("data-ai-light-scheme");
        root.style.colorScheme = "dark";
        return;
    }
    root.setAttribute("data-ai-light-scheme", choice.lightSchemeId);
    root.removeAttribute("data-ai-dark-scheme");
    root.style.colorScheme = "light";
}

export function publishUIAppearance(choice: UIAppearanceChoice): void {
    const set = appearanceBinding()?.SetUIAppearance;
    if (typeof set !== "function") return;
    // Theme clicks are not awaited. The shared queue keeps a later choice
    // behind an earlier save, including the one made before this bundle loads.
    enqueueUIAppearancePublish(set, {
        themeMode: choice.themeMode,
        darkScheme: choice.darkSchemeId,
        lightScheme: choice.lightSchemeId,
    });
}

export function fetchUIAppearance(): Promise<UIAppearanceChoice | null> {
    const get = appearanceBinding()?.GetUIAppearance;
    if (typeof get !== "function") return Promise.resolve(null);
    return Promise.resolve(get()).then(parseUIAppearance).catch(() => null);
}

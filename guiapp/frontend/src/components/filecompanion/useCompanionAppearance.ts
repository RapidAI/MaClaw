import { createContext, createElement, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createCodePreviewTheme, type CodePreviewTheme } from "../ai/CodePreviewPanel";
import type { Theme } from "../ai/aiAssistantPanelTheme";
import { getAssistantDarkScheme } from "../ai/assistantDarkSchemes";
import { getAssistantLightScheme } from "../ai/assistantLightSchemes";
import {
    UI_APPEARANCE_EVENT,
    applyDocumentThemeAttributes,
    fetchUIAppearance,
    parseUIAppearance,
    readLocalUIAppearance,
    writeLocalUIAppearance,
    type UIAppearanceChoice,
} from "../ai/uiAppearanceSync";

export type CompanionAppearance = UIAppearanceChoice & {
    assistantTheme: Theme;
    previewTheme: CodePreviewTheme;
};

function appearanceValue(choice: UIAppearanceChoice): CompanionAppearance {
    const base = choice.themeMode === "dark"
        ? getAssistantDarkScheme(choice.darkSchemeId).assistantTheme
        : getAssistantLightScheme(choice.lightSchemeId).assistantTheme;
    const assistantTheme = Object.assign({}, base, { isDark: choice.themeMode === "dark" });
    return {
        ...choice,
        assistantTheme,
        previewTheme: createCodePreviewTheme(assistantTheme),
    };
}

const CompanionAppearanceContext = createContext<CompanionAppearance>(appearanceValue(readLocalUIAppearance()));

type RuntimeAPI = {
    EventsOn?: (name: string, cb: (payload: unknown) => void) => void;
    EventsOff?: (name: string) => void;
};

function runtimeAPI(): RuntimeAPI | undefined {
    return (window as unknown as { runtime?: RuntimeAPI }).runtime;
}

function paintDocumentAppearance(choice: UIAppearanceChoice): void {
    const root = document.documentElement;
    const pageBg = choice.themeMode === "dark"
        ? getAssistantDarkScheme(choice.darkSchemeId).cssVars.pageBg
        : getAssistantLightScheme(choice.lightSchemeId).cssVars.pageBg;
    applyDocumentThemeAttributes(choice);
    // The boot script paints a literal colour before CSS loads. A literal left
    // in place overrides the scheme token and fringes at fractional DPI, so
    // hand the shell back to --theme-page-bg once the real theme is known.
    root.style.setProperty("--theme-page-bg", pageBg);
    root.style.backgroundColor = "";
    document.body?.style.setProperty("--theme-page-bg", pageBg);
    if (document.body) document.body.style.backgroundColor = "";
}

type DocumentAppearanceSnapshot = {
    theme: string | null;
    dark: string | null;
    light: string | null;
    boot: string | null;
    colorScheme: string;
    backgroundColor: string;
    pageBg: string;
    bodyPageBg: string;
};

function captureDocumentAppearance(): DocumentAppearanceSnapshot {
    const root = document.documentElement;
    return {
        theme: root.getAttribute("data-ai-theme"),
        dark: root.getAttribute("data-ai-dark-scheme"),
        light: root.getAttribute("data-ai-light-scheme"),
        boot: root.getAttribute("data-boot-theme"),
        colorScheme: root.style.colorScheme,
        backgroundColor: root.style.backgroundColor,
        pageBg: root.style.getPropertyValue("--theme-page-bg"),
        bodyPageBg: document.body?.style.getPropertyValue("--theme-page-bg") ?? "",
    };
}

function restoreDocumentAppearance(previous: DocumentAppearanceSnapshot): void {
    const root = document.documentElement;
    const restore = (name: string, value: string | null) => {
        if (value) root.setAttribute(name, value);
        else root.removeAttribute(name);
    };
    restore("data-ai-theme", previous.theme);
    restore("data-ai-dark-scheme", previous.dark);
    restore("data-ai-light-scheme", previous.light);
    restore("data-boot-theme", previous.boot);
    root.style.colorScheme = previous.colorScheme;
    root.style.backgroundColor = previous.backgroundColor;
    if (previous.pageBg) root.style.setProperty("--theme-page-bg", previous.pageBg);
    else root.style.removeProperty("--theme-page-bg");
    if (document.body) {
        if (previous.bodyPageBg) document.body.style.setProperty("--theme-page-bg", previous.bodyPageBg);
        else document.body.style.removeProperty("--theme-page-bg");
    }
}

export function useCompanionAppearanceState(): CompanionAppearance {
    const [choice, setChoice] = useState<UIAppearanceChoice>(readLocalUIAppearance);
    const originalDocument = useRef<DocumentAppearanceSnapshot | null>(null);

    useEffect(() => {
        let cancelled = false;
        let eventApplied = false;
        const apply = (next: UIAppearanceChoice) => {
            writeLocalUIAppearance(next);
            setChoice((current) => (
                current.themeMode === next.themeMode
                && current.darkSchemeId === next.darkSchemeId
                && current.lightSchemeId === next.lightSchemeId
                    ? current
                    : next
            ));
        };
        const onChange = (payload: unknown) => {
            const next = parseUIAppearance(payload);
            if (!next) return;
            eventApplied = true;
            apply(next);
        };
        const runtime = runtimeAPI();
        runtime?.EventsOn?.(UI_APPEARANCE_EVENT, onChange);
        void fetchUIAppearance().then((next) => {
            if (cancelled || eventApplied || !next) return;
            apply(next);
        });
        return () => {
            cancelled = true;
            runtime?.EventsOff?.(UI_APPEARANCE_EVENT);
        };
    }, []);

    useLayoutEffect(() => {
        if (originalDocument.current === null) {
            originalDocument.current = captureDocumentAppearance();
        }
        paintDocumentAppearance(choice);
    }, [choice]);

    useLayoutEffect(() => () => {
        if (originalDocument.current) restoreDocumentAppearance(originalDocument.current);
    }, []);

    return useMemo(() => appearanceValue(choice), [choice]);
}

export function CompanionAppearanceProvider({ children }: { children: ReactNode }) {
    const appearance = useCompanionAppearanceState();
    return createElement(CompanionAppearanceContext.Provider, { value: appearance }, children);
}

export function useCompanionAppearance(): CompanionAppearance {
    return useContext(CompanionAppearanceContext);
}

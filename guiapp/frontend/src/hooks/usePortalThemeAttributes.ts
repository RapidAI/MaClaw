import { useEffect, useState } from "react";

export type PortalThemeAttributes = {
    "data-portal-theme": "true";
    "data-ai-theme": "light" | "dark";
    "data-ai-dark-scheme"?: string;
    "data-ai-light-scheme"?: string;
};

function readPortalThemeAttributes(appElement?: HTMLElement | null): PortalThemeAttributes {
    const theme = appElement?.dataset.aiTheme === "dark" ? "dark" : "light";
    return {
        "data-portal-theme": "true",
        "data-ai-theme": theme,
        "data-ai-dark-scheme": appElement?.dataset.aiDarkScheme || undefined,
        "data-ai-light-scheme": appElement?.dataset.aiLightScheme || undefined,
    };
}

function sameTheme(a: PortalThemeAttributes, b: PortalThemeAttributes) {
    return a["data-ai-theme"] === b["data-ai-theme"]
        && a["data-ai-dark-scheme"] === b["data-ai-dark-scheme"]
        && a["data-ai-light-scheme"] === b["data-ai-light-scheme"];
}

/**
 * Portals are rendered outside #App and therefore do not inherit its CSS
 * custom properties. Mirror the theme attributes and keep them in sync when
 * the user changes theme while a dialog is open.
 */
export function usePortalThemeAttributes(enabled = true): PortalThemeAttributes {
    const [attributes, setAttributes] = useState<PortalThemeAttributes>(() => {
        if (typeof document === "undefined") return readPortalThemeAttributes();
        return readPortalThemeAttributes(document.getElementById("App"));
    });

    // A dialog may stay mounted while closed. Read the current attributes for
    // its opening render so a theme change that happened while it was closed
    // never produces a light-theme flash before the observer runs.
    const currentAttributes = typeof document === "undefined"
        ? attributes
        : readPortalThemeAttributes(document.getElementById("App"));

    useEffect(() => {
        if (!enabled) return;
        const appElement = document.getElementById("App");
        if (!appElement) return;

        const sync = () => {
            const next = readPortalThemeAttributes(appElement);
            setAttributes((current) => sameTheme(current, next) ? current : next);
        };
        sync();

        const observer = new MutationObserver(sync);
        observer.observe(appElement, {
            attributes: true,
            attributeFilter: ["data-ai-theme", "data-ai-dark-scheme", "data-ai-light-scheme"],
        });
        return () => observer.disconnect();
    }, [enabled]);

    return enabled ? currentAttributes : attributes;
}

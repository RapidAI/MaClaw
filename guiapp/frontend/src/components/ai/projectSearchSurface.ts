import type { CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

/** Full-pane overlay that replaces the assistant conversation while search is open. */
export function searchSurfaceRootStyle(theme: Theme): CSSProperties {
    return {
        position: "absolute",
        inset: 0,
        zIndex: 30000,
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        background: theme.bg,
        overflow: "hidden",
    };
}

const SEARCH_DISMISS_EXEMPT_SELECTOR = [
    "[data-testid='ai-title-bar']",
    ".mc-task-pane__search",
    ".mc-header-search-wrap",
].join(", ");

/** Task-pane search and title-bar chrome must not dismiss the results surface. */
export function isSearchDismissExemptTarget(target: EventTarget | null): boolean {
    return target instanceof Element && !!target.closest(SEARCH_DISMISS_EXEMPT_SELECTOR);
}

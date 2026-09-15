export const SESSION_TABS = ["background", "scheduled", "passthrough"] as const;
export type SessionTab = (typeof SESSION_TABS)[number];

/** Retired "remote" monitor tab maps onto background. */
export function resolveSessionTab(tab: string | undefined): SessionTab {
    if (tab === "scheduled" || tab === "passthrough") return tab;
    return "background";
}

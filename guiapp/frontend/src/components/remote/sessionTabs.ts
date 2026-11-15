export const SESSION_TABS = ["background", "scheduled", "passthrough"] as const;
export type SessionTab = (typeof SESSION_TABS)[number] | "bot";

/** Retired "remote" monitor tab maps onto background. */
export function resolveSessionTab(tab: string | undefined): SessionTab {
    if (tab === "scheduled" || tab === "passthrough" || tab === "bot") return tab;
    return "background";
}

/** Bot is present after Hub has granted this account, or while this computer
 * still has a desktop install or update on the monitor. */
export function visibleSessionTabs(botAllowed: boolean): SessionTab[] {
    return botAllowed ? [...SESSION_TABS, "bot"] : [...SESSION_TABS];
}

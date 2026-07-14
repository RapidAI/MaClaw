/**
 * Lightweight empty-state hint chips for the welcome composer.
 * Each hint points at a built-in scenario card.
 */

import { findWelcomePromptByKey, welcomePromptKey } from "./welcomeTaskMemory";
import type { WelcomePrompt } from "./welcomeScenarioTasks";

export type WelcomeQuickHint = {
    id: string;
    label: string;
    labelEn: string;
    tabId: string;
    textEn: string;
};

/** Curated "try this" chips shown when the composer is empty. */
export const WELCOME_QUICK_HINTS: WelcomeQuickHint[] = [
    {
        id: "disk",
        label: "排查磁盘",
        labelEn: "Full disk",
        tabId: "ops",
        textEn: "Investigate a full disk incident",
    },
    {
        id: "weekly",
        label: "写周报",
        labelEn: "Weekly update",
        tabId: "writing",
        textEn: "Write a project weekly update",
    },
    {
        id: "feature",
        label: "实现功能",
        labelEn: "Implement feature",
        tabId: "dev",
        textEn: "Implement a feature",
    },
    {
        id: "meeting",
        label: "会议纪要",
        labelEn: "Meeting notes",
        tabId: "writing",
        textEn: "Create meeting notes and action items",
    },
    {
        id: "competitor",
        label: "竞品分析",
        labelEn: "Competitor brief",
        tabId: "business",
        textEn: "Prepare an executive competitor brief",
    },
    {
        id: "bug",
        label: "修 Bug",
        labelEn: "Fix a bug",
        tabId: "dev",
        textEn: "Fix a bug",
    },
];

export type WelcomeQuickHintResolved = WelcomeQuickHint & {
    prompt: WelcomePrompt;
    key: string;
};

/** Resolve hint entries that still exist in the scenario catalog. */
export function resolveWelcomeQuickHints(
    hints: WelcomeQuickHint[] = WELCOME_QUICK_HINTS,
): WelcomeQuickHintResolved[] {
    const out: WelcomeQuickHintResolved[] = [];
    for (const hint of hints) {
        const key = welcomePromptKey(hint.tabId, hint.textEn);
        const found = findWelcomePromptByKey(key);
        if (!found) continue;
        out.push({ ...hint, prompt: found.prompt, key });
    }
    return out;
}

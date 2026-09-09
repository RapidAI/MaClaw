/**
 * Pure validators for the welcome-page scenario catalog.
 * Used by unit tests (and available for future build-time checks).
 */

import { extractWelcomeTemplateFields } from "./welcomePromptTemplate";
import type { ScenarioTab } from "./welcomeScenarioTasks";
import { WELCOME_SCENARIO_PROMPTS_PER_TAB } from "./welcomeScenarioTasks";

/** Icons rendered by WelcomePromptIcon in AssistantWelcomeView. */
export const WELCOME_PROMPT_ICON_NAMES = [
    "ppt", "plan", "contract", "code", "bug", "docker", "server", "install",
    "deploy", "search", "translate", "chart", "award", "write", "mail",
    "meeting", "knowledge", "qa", "checklist", "workflow", "form", "schedule",
    "strategy", "review", "monitor", "diagram", "target", "users", "shield", "spark",
] as const;

export type WelcomePromptIconName = (typeof WELCOME_PROMPT_ICON_NAMES)[number];

const KNOWN_ICONS = new Set<string>(WELCOME_PROMPT_ICON_NAMES);

export type WelcomeCatalogIssue = {
    level: "error" | "warn";
    tabId: string;
    textEn?: string;
    message: string;
};

function path(tabId: string, textEn?: string): { tabId: string; textEn?: string } {
    return textEn ? { tabId, textEn } : { tabId };
}

/** Full structural audit of SCENARIO_TABS. */
export function auditWelcomeScenarioCatalog(tabs: ScenarioTab[]): WelcomeCatalogIssue[] {
    const issues: WelcomeCatalogIssue[] = [];

    for (const tab of tabs) {
        if (tab.prompts.length !== WELCOME_SCENARIO_PROMPTS_PER_TAB) {
            issues.push({
                level: "error",
                ...path(tab.id),
                message: `expected ${WELCOME_SCENARIO_PROMPTS_PER_TAB} prompts, got ${tab.prompts.length}`,
            });
        }

        const zhTitles = tab.prompts.map((p) => p.text);
        const enTitles = tab.prompts.map((p) => p.textEn);
        if (new Set(zhTitles).size !== zhTitles.length) {
            issues.push({ level: "error", ...path(tab.id), message: "duplicate Chinese titles" });
        }
        if (new Set(enTitles).size !== enTitles.length) {
            issues.push({ level: "error", ...path(tab.id), message: "duplicate English titles" });
        }

        for (const p of tab.prompts) {
            const id = path(tab.id, p.textEn);
            if (!p.text.trim() || !p.textEn.trim() || !p.desc.trim() || !p.descEn.trim()) {
                issues.push({ level: "error", ...id, message: "empty title/desc" });
            }
            if (!KNOWN_ICONS.has(p.icon)) {
                issues.push({ level: "error", ...id, message: `unknown icon "${p.icon}"` });
            }
            if ([...p.text].length > 16) {
                issues.push({ level: "error", ...id, message: `zh title too long (${[...p.text].length})` });
            }
            if (p.textEn.length > 52) {
                issues.push({ level: "error", ...id, message: `en title too long (${p.textEn.length})` });
            }

            const zhBody = (p.template || "").trim();
            const enBody = (p.templateEn || "").trim();
            if (zhBody.length < 20 || enBody.length < 20) {
                issues.push({ level: "error", ...id, message: "template too short" });
            }

            const zhFields = extractWelcomeTemplateFields(zhBody);
            const enFields = extractWelcomeTemplateFields(enBody);
            if (zhFields.length < 2 || zhFields.length > 4) {
                issues.push({
                    level: "error",
                    ...id,
                    message: `zh field count ${zhFields.length} (want 2–4)`,
                });
            }
            if (enFields.length !== zhFields.length) {
                issues.push({
                    level: "error",
                    ...id,
                    message: `zh/en field mismatch ${zhFields.length}/${enFields.length}`,
                });
            }

            // Every [slot] must sit on a 标签： line.
            for (const [lang, body] of [["zh", zhBody], ["en", enBody]] as const) {
                const re = /\[([^\[\]]+)\]/g;
                let match: RegExpExecArray | null;
                while ((match = re.exec(body)) !== null) {
                    const lineStart = body.lastIndexOf("\n", match.index - 1) + 1;
                    const nextNl = body.indexOf("\n", match.index);
                    const lineEnd = nextNl === -1 ? body.length : nextNl;
                    const line = body.slice(lineStart, lineEnd);
                    const before = line.slice(0, match.index - lineStart);
                    if (!/：\s*$|:\s*$/.test(before)) {
                        issues.push({
                            level: "error",
                            ...id,
                            message: `${lang} bare slot (need 标签：): ${line.trim().slice(0, 48)}`,
                        });
                    }
                }
            }

            // Explicit deliverable section.
            if (!/请输出：|输出：/.test(zhBody)) {
                issues.push({ level: "error", ...id, message: "zh missing 请输出：/输出：" });
            }
            if (!/\bOutput\s*:/.test(enBody)) {
                issues.push({ level: "error", ...id, message: "en missing Output:" });
            }

            // The canonical ops catalog represents local maintenance. The UI
            // derives remote variants with getWelcomeOpsPrompts(), keeping the
            // eight-card catalog stable while ensuring every remote variant
            // uses the SSH diagnosis flow.
            if (tab.id !== "dev" && p.agentMode) {
                issues.push({
                    level: "error",
                    ...id,
                    message: `non-dev tab must not set agentMode (${p.agentMode})`,
                });
            }

            // Soft: long field labels hurt the param dialog.
            for (const f of zhFields) {
                if ([...f.label].length > 18) {
                    issues.push({
                        level: "warn",
                        ...id,
                        message: `long zh label "${f.label}"`,
                    });
                }
            }
        }

        if (tab.id === "dev") {
            tab.prompts.forEach((p, i) => {
                const want = i % 2 === 0 ? "coding_dev" : "remote_coding_dev";
                if (p.agentMode !== want) {
                    issues.push({
                        level: "error",
                        ...path(tab.id, p.textEn),
                        message: `agentMode want ${want}, got ${p.agentMode}`,
                    });
                }
            });
        }
    }

    // Global key uniqueness for recent/history storage.
    const keys = tabs.flatMap((t) => t.prompts.map((p) => `${t.id}::${p.textEn}`));
    if (new Set(keys).size !== keys.length) {
        issues.push({ level: "error", tabId: "*", message: "duplicate tabId::textEn keys" });
    }

    return issues;
}

export function welcomeCatalogErrors(tabs: ScenarioTab[]): WelcomeCatalogIssue[] {
    return auditWelcomeScenarioCatalog(tabs).filter((i) => i.level === "error");
}

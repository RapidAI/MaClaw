import { localizeText } from "./aiAssistantI18n";

/**
 * Localized UI strings for AgentTaskPanel.
 * Returns all panel chrome and validation text for the given language.
 */
export function agentViewStrings(lang: string) {
    const t = (en: string, zh: string) => localizeText(lang, en, zh);
    return {
        close: t("Close", "\u5173\u95ed"),
        submit: t("Submit", "\u63d0\u4ea4"),
        submitting: t("Submitting...", "\u63d0\u4ea4\u4e2d..."),
        cancel: t("Cancel", "\u53d6\u6d88"),
        select: t("Select", "\u9009\u62e9"),
        browse: t("Browse", "\u6d4f\u89c8"),
        selectPlaceholder: t("Select...", "\u8bf7\u9009\u62e9..."),
        next: t("Next", "\u4e0b\u4e00\u6b65"),
        back: t("Back", "\u4e0a\u4e00\u6b65"),
        addRow: t("Add row", "\u6dfb\u52a0\u884c"),
        noRows: t("No rows", "\u6682\u65e0\u6570\u636e"),
        enabled: t("Enabled", "\u542f\u7528"),
        mode: t("Mode", "\u6a21\u5f0f"),
        ignore: t("Ignore", "\u5ffd\u7565"),
        approve: t("Approve", "\u6279\u51c6"),
        reject: t("Reject", "\u62d2\u7edd"),
        applyMapping: t("Apply mapping", "\u5e94\u7528\u6620\u5c04"),
        pleaseFix: t("Please fix: ", "\u8bf7\u4fee\u6b63\uff1a"),
        risk: t("Risk", "\u98ce\u9669"),
        effects: t("Effects", "\u5f71\u54cd"),
        data: t("Data", "\u6570\u636e"),
        kind: t("Kind", "\u7c7b\u578b"),
        rows: t("Rows", "\u884c"),
        resource: t("resource", "\u8d44\u6e90"),
        pending: t("pending", "\u5f85\u5904\u7406"),
        more: (n: number) => t(`+${n} more`, `+${n} \u66f4\u591a`),
        moreFields: (n: number) => t(`+${n} more fields`, `+${n} \u66f4\u591a\u5b57\u6bb5`),
        fields: (n: number) => t(`${n} fields`, `${n} \u4e2a\u5b57\u6bb5`),
        stepOf: (current: number, total: number) => t(`Step ${current} of ${total}`, `\u7b2c ${current} \u6b65\uff0c\u5171 ${total} \u6b65`),
        selectAtLeastOne: (type: string) => t(`Select at least one ${type}`, `\u8bf7\u81f3\u5c11\u9009\u62e9\u4e00\u4e2a${type}`),
        selectA: (type: string) => t(`Select a ${type}`, `\u8bf7\u9009\u62e9\u4e00\u4e2a${type}`),
        needsSourceField: (label: string) => t(`${label} needs a source field`, `${label} \u9700\u8981\u4e00\u4e2a\u6e90\u5b57\u6bb5`),
        mustBeValidNumber: (label: string) => t(`${label} must be a valid number`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u6570\u5b57`),
        mustBeAtLeast: (label: string, min: number) => t(`${label} must be at least ${min}`, `${label} \u4e0d\u80fd\u5c0f\u4e8e ${min}`),
        mustBeAtMost: (label: string, max: number) => t(`${label} must be at most ${max}`, `${label} \u4e0d\u80fd\u5927\u4e8e ${max}`),
        mustBeGreaterThan: (label: string, n: number) => t(`${label} must be greater than ${n}`, `${label} \u5fc5\u987b\u5927\u4e8e ${n}`),
        mustBeLessThan: (label: string, n: number) => t(`${label} must be less than ${n}`, `${label} \u5fc5\u987b\u5c0f\u4e8e ${n}`),
        mustBeMultipleOf: (label: string, step: number) => t(`${label} must be a multiple of ${step}`, `${label} \u5fc5\u987b\u662f ${step} \u7684\u500d\u6570`),
        mustBeValidEmail: (label: string) => t(`${label} must be a valid email`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u7684\u90ae\u7bb1\u5730\u5740`),
        mustBeValidURL: (label: string) => t(`${label} must be a valid URL`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u7684 URL`),
        mustBeValidUUID: (label: string) => t(`${label} must be a valid UUID`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u7684 UUID`),
        mustBeValidDate: (label: string) => t(`${label} must be a valid date`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u7684\u65e5\u671f`),
        mustBeValidDateTime: (label: string) => t(`${label} must be a valid date and time`, `${label} \u5fc5\u987b\u662f\u6709\u6548\u7684\u65e5\u671f\u65f6\u95f4`),
        mustBeAtLeastChars: (label: string, n: number) => t(`${label} must be at least ${n} characters`, `${label} \u81f3\u5c11\u9700\u8981 ${n} \u4e2a\u5b57\u7b26`),
        mustBeAtMostChars: (label: string, n: number) => t(`${label} must be at most ${n} characters`, `${label} \u6700\u591a ${n} \u4e2a\u5b57\u7b26`),
        invalidFormat: (label: string) => t(`${label} has an invalid format`, `${label} \u683c\u5f0f\u65e0\u6548`),
        mustBeOneOf: (label: string, options: string) => t(`${label} must be one of: ${options}`, `${label} \u5fc5\u987b\u662f\u4ee5\u4e0b\u4e4b\u4e00\uff1a${options}`),
        needsAtLeastItems: (label: string, n: number) => t(`${label} needs at least ${n} item(s)`, `${label} \u81f3\u5c11\u9700\u8981 ${n} \u9879`),
        allowsAtMostItems: (label: string, n: number) => t(`${label} allows at most ${n} item(s)`, `${label} \u6700\u591a\u5141\u8bb8 ${n} \u9879`),
        noDuplicateItems: (label: string) => t(`${label} must not contain duplicate items`, `${label} \u4e0d\u80fd\u5305\u542b\u91cd\u590d\u9879`),
        mustBe: (label: string, val: string) => t(`${label} must be ${val}`, `${label} \u5fc5\u987b\u662f ${val}`),
        requiredWhen: (required: string, trigger: string) => t(`${required} is required when ${trigger} is provided`, `\u5f53\u63d0\u4f9b\u4e86 ${trigger} \u65f6\uff0c${required} \u4e3a\u5fc5\u586b`),
        // Prefill source badges
        prefillContext: t("from dialog", "\u5bf9\u8bdd"),
        prefillMemory: t("from memory", "\u8bb0\u5fc6"),
        prefillKnowledge: t("knowledge base", "\u77e5\u8bc6\u5e93"),
        prefillWeb: t("\u26a0\ufe0f web", "\u26a0\ufe0f \u7f51\u7edc"),
    };
}

export type AgentViewStrings = ReturnType<typeof agentViewStrings>;

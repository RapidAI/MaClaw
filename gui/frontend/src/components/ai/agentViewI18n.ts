import { localizeText } from "./aiAssistantI18n";

/**
 * Localized UI strings for AgentTaskPanel.
 * Returns a record of all UI text keyed by identifier, resolved for the given language.
 */
export function agentViewStrings(lang: string) {
    const t = (en: string, zh: string) => localizeText(lang, en, zh);
    return {
        close: t("Close", "关闭"),
        submit: t("Submit", "提交"),
        submitting: t("Submitting...", "提交中..."),
        cancel: t("Cancel", "取消"),
        select: t("Select", "选择"),
        selectPlaceholder: t("Select...", "请选择..."),
        next: t("Next", "下一步"),
        back: t("Back", "上一步"),
        addRow: t("Add row", "添加行"),
        noRows: t("No rows", "暂无数据"),
        enabled: t("Enabled", "启用"),
        mode: t("Mode", "模式"),
        ignore: t("Ignore", "忽略"),
        approve: t("Approve", "批准"),
        reject: t("Reject", "拒绝"),
        applyMapping: t("Apply mapping", "应用映射"),
        pleaseFix: t("Please fix: ", "请修正："),
        risk: t("Risk", "风险"),
        effects: t("Effects", "影响"),
        data: t("Data", "数据"),
        kind: t("Kind", "类型"),
        pending: t("pending", "待处理"),
        more: (n: number) => t(`+${n} more`, `+${n} 更多`),
        moreFields: (n: number) => t(`+${n} more fields`, `+${n} 更多字段`),
        fields: (n: number) => t(`${n} fields`, `${n} 个字段`),
        stepOf: (current: number, total: number) => t(`Step ${current} of ${total}`, `第 ${current} 步，共 ${total} 步`),
        selectAtLeastOne: (type: string) => t(`Select at least one ${type}`, `请至少选择一个${type}`),
        selectA: (type: string) => t(`Select a ${type}`, `请选择一个${type}`),
        needsSourceField: (label: string) => t(`${label} needs a source field`, `${label} 需要一个源字段`),
        // Validation messages
        mustBeValidNumber: (label: string) => t(`${label} must be a valid number`, `${label} 必须是有效数字`),
        mustBeAtLeast: (label: string, min: number) => t(`${label} must be at least ${min}`, `${label} 不能小于 ${min}`),
        mustBeAtMost: (label: string, max: number) => t(`${label} must be at most ${max}`, `${label} 不能大于 ${max}`),
        mustBeGreaterThan: (label: string, n: number) => t(`${label} must be greater than ${n}`, `${label} 必须大于 ${n}`),
        mustBeLessThan: (label: string, n: number) => t(`${label} must be less than ${n}`, `${label} 必须小于 ${n}`),
        mustBeMultipleOf: (label: string, step: number) => t(`${label} must be a multiple of ${step}`, `${label} 必须是 ${step} 的倍数`),
        mustBeValidEmail: (label: string) => t(`${label} must be a valid email`, `${label} 必须是有效的邮箱地址`),
        mustBeValidURL: (label: string) => t(`${label} must be a valid URL`, `${label} 必须是有效的 URL`),
        mustBeValidUUID: (label: string) => t(`${label} must be a valid UUID`, `${label} 必须是有效的 UUID`),
        mustBeValidDate: (label: string) => t(`${label} must be a valid date`, `${label} 必须是有效的日期`),
        mustBeValidDateTime: (label: string) => t(`${label} must be a valid date and time`, `${label} 必须是有效的日期时间`),
        mustBeAtLeastChars: (label: string, n: number) => t(`${label} must be at least ${n} characters`, `${label} 至少需要 ${n} 个字符`),
        mustBeAtMostChars: (label: string, n: number) => t(`${label} must be at most ${n} characters`, `${label} 最多 ${n} 个字符`),
        invalidFormat: (label: string) => t(`${label} has an invalid format`, `${label} 格式无效`),
        mustBeOneOf: (label: string, options: string) => t(`${label} must be one of: ${options}`, `${label} 必须是以下之一：${options}`),
        needsAtLeastItems: (label: string, n: number) => t(`${label} needs at least ${n} item(s)`, `${label} 至少需要 ${n} 项`),
        allowsAtMostItems: (label: string, n: number) => t(`${label} allows at most ${n} item(s)`, `${label} 最多允许 ${n} 项`),
        noDuplicateItems: (label: string) => t(`${label} must not contain duplicate items`, `${label} 不能包含重复项`),
        mustBe: (label: string, val: string) => t(`${label} must be ${val}`, `${label} 必须是 ${val}`),
        requiredWhen: (required: string, trigger: string) => t(`${required} is required when ${trigger} is provided`, `当提供了 ${trigger} 时，${required} 为必填`),
    };
}

export type AgentViewStrings = ReturnType<typeof agentViewStrings>;

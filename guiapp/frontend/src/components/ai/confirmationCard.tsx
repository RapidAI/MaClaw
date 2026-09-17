import React from "react";
import type { ChatAction, ChatConfirmation } from "./useAIAssistant";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";

// The pre-execution confirmation card (including the P0-4 credential variant) used to
// live inside aiAssistantMarkdown.tsx, which is kept under a 2000-line guard cap.
// Everything it needs is injected instead of imported so this module stays a leaf:
// importing renderContentWithCodeBlocks / renderActions back from aiAssistantMarkdown
// would create a cycle (those live in the same file that renders this card).
export type ConfirmationCardDeps = {
    renderContentWithCodeBlocks: (content: string, t: Theme) => React.ReactNode;
    renderInlineMarkdown: (text: string, t: Theme) => React.ReactNode;
    renderActions: (
        actions: ChatAction[],
        executeAction: (command: string) => void,
        t: Theme,
        lang?: string,
    ) => React.ReactNode;
};

function renderConfirmationList(
    deps: Pick<ConfirmationCardDeps, "renderInlineMarkdown">,
    testId: string,
    title: string,
    items: string[],
    t: Theme,
): React.ReactNode {
    if (items.length === 0) return null;
    return (
        <div data-testid={testId} style={{ marginTop: "8px" }}>
            <div style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "4px" }}>{title}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                {items.map((item, index) => (
                    <div key={`${testId}-${index}`} style={{ minHeight: "1.4em", color: t.text }}>
                        <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                        {deps.renderInlineMarkdown(item, t)}
                    </div>
                ))}
            </div>
        </div>
    );
}

function formatConfirmationStatus(status: string, lang: string): string {
    const normalized = status.trim().toLowerCase();
    const labels: Record<string, string> = {
        pending: localizeText(lang, "Pending", "\u5f85\u786e\u8ba4", "\u5f85\u78ba\u8a8d"),
        running: localizeText(lang, "Running", "\u6267\u884c\u4e2d", "\u57f7\u884c\u4e2d"),
        confirmed: localizeText(lang, "Confirmed", "\u5df2\u786e\u8ba4", "\u5df2\u78ba\u8a8d"),
        cancelled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        canceled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        expired: localizeText(lang, "Expired", "\u5df2\u8fc7\u671f", "\u5df2\u904e\u671f"),
        void: localizeText(lang, "Voided", "\u5df2\u4f5c\u5e9f", "\u5df2\u4f5c\u5ee2"),
    };
    return labels[normalized] || status;
}

function formatConfirmationTaskType(taskType: string, lang: string): string {
    const normalized = taskType.trim().toLowerCase();
    const labels: Record<string, string> = {
        coding: localizeText(lang, "Coding", "\u4ee3\u7801\u4efb\u52a1", "\u7a0b\u5f0f\u78bc\u4efb\u52d9"),
        ssh: localizeText(lang, "SSH", "\u8fdc\u7a0b\u4efb\u52a1", "\u9060\u7aef\u4efb\u52d9"),
        ambiguous: localizeText(lang, "Ambiguous", "\u5f85\u6f84\u6e05\u4efb\u52a1", "\u5f85\u91d0\u6e05\u4efb\u52d9"),
    };
    return labels[normalized] || taskType;
}

export function renderConfirmationCard(
    deps: ConfirmationCardDeps,
    confirmation: ChatConfirmation,
    actions: ChatAction[] | undefined,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const targetPaths = confirmation.targetPaths || [];
    const plannedActions = confirmation.plannedActions || [];
    const riskFlags = confirmation.riskFlags || [];
    const revisionHints = confirmation.revisionHints || [];
    const taskType = confirmation.taskType?.trim() || '';
    const status = confirmation.status?.trim() || '';
    const labels = confirmation.labels;
    // P0-4 credential cards (taskType "credential") are one-shot sensitive
    // write confirmations raised mid-loop: dedicated title, no revision
    // hints, and the redacted payload review is the body.
    const isCredentialCard = taskType === 'credential';
    const titleLabel = isCredentialCard
        ? (labels?.title || localizeText(lang, "Sensitive write confirmation", "\u654f\u611f\u4fe1\u606f\u5199\u5165\u786e\u8ba4", "\u654f\u611f\u8cc7\u8a0a\u5beb\u5165\u78ba\u8a8d"))
        : (labels?.title || localizeText(lang, "Pre-execution confirmation", "\u6267\u884c\u524d\u786e\u8ba4", "\u57f7\u884c\u524d\u78ba\u8a8d"));
    const statusLabel = labels?.status || localizeText(lang, "Status", "\u72b6\u6001", "\u72c0\u614b");
    const targetPathsLabel = labels?.target_paths || localizeText(lang, "Target paths", "\u76ee\u6807\u8def\u5f84", "\u76ee\u6a19\u8def\u5f91");
    const plannedActionsLabel = labels?.planned_actions || localizeText(lang, "Planned actions", "\u8ba1\u5212\u64cd\u4f5c", "\u8a08\u5283\u64cd\u4f5c");
    const riskFlagsLabel = labels?.risk_flags || localizeText(lang, "Risk flags", "\u98ce\u9669\u6807\u8bb0", "\u98a8\u96aa\u6a19\u8a18");
    const revisionHintsLabel = labels?.revision_hints || localizeText(lang, "Revision hints", "\u4fee\u8ba2\u63d0\u793a", "\u4fee\u8a02\u63d0\u793a");
    return (
        <div
            data-testid="confirmation-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.fieldBg,
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {taskType && !isCredentialCard ? `${titleLabel} - ${formatConfirmationTaskType(taskType, lang)}` : titleLabel}
            </div>
            {status && (
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {statusLabel}: {formatConfirmationStatus(status, lang)}
                </div>
            )}
            <div data-testid="confirmation-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                {deps.renderContentWithCodeBlocks(confirmation.summary, t)}
            </div>
            {renderConfirmationList(deps, "confirmation-target-paths", targetPathsLabel, targetPaths, t)}
            {renderConfirmationList(deps, "confirmation-planned-actions", plannedActionsLabel, plannedActions, t)}
            {renderConfirmationList(deps, "confirmation-risk-flags", riskFlagsLabel, riskFlags, t)}
            {!isCredentialCard && renderConfirmationList(deps, "confirmation-revision-hints", revisionHintsLabel, revisionHints, t)}
            {actions && actions.length > 0 && deps.renderActions(actions, executeAction, t, lang)}
            {isCredentialCard && (
                <div data-testid="confirmation-credential-hint" style={{ color: t.fieldLabel, fontSize: "11px", marginTop: "6px" }}>
                    {localizeText(lang, "Use the buttons above to approve or cancel. Typing a reply will void this confirmation.", "请使用上方按钮确认或取消；直接输入文字将使本确认作废。", "請使用上方按鈕確認或取消；直接輸入文字將使本確認作廢。")}
                </div>
            )}
        </div>
    );
}

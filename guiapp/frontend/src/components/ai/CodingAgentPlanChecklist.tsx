import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import {
    codingStepGlyph,
    codingStepIsActive,
    codingStepIsDone,
    codingStepIsFailed,
    codingStepIsSkipped,
    codingStepStatusColor,
    codingStepStatusLabel,
    srOnlyStyle,
    type CodingBannerChrome,
    type CodingStepStatus,
} from "./CodingWorkbenchControlPanel";

export type CodingAgentPlanChecklistProps = {
    lang?: string;
    theme: Theme;
    chrome: CodingBannerChrome;
    steps: CodingStepStatus[];
    understanding?: string;
    pendingApproval?: boolean;
    ready?: boolean;
    onApprove?: () => void;
    onSkip?: () => void;
    onReject?: () => void;
};

export function codingPlanProgressLabel(steps: CodingStepStatus[]): string {
    // Done or skipped advances the counter; a failed step deliberately does not
    // (otherwise a failed plan could read "5/5"). `success`/`succeeded` count as
    // finished alongside `passed`/`completed`.
    const done = steps.filter((s) => codingStepIsDone(s.status) || codingStepIsSkipped(s.status)).length;
    return `${done}/${steps.length}`;
}

export function CodingAgentPlanChecklist({
    lang,
    theme,
    chrome,
    steps,
    understanding = "",
    pendingApproval = false,
    ready = true,
    onApprove,
    onSkip,
    onReject,
}: CodingAgentPlanChecklistProps) {
    const restatement = (understanding || "").trim();
    if (!pendingApproval && steps.length === 0 && !restatement) {
        return null;
    }
    // Same predicate the step rows use, so a backend `started` lights up the row
    // and the header badge together.
    const running = steps.find((s) => codingStepIsActive(s.status));
    const title = pendingApproval
        ? localizeText(lang, "Plan", "计划", "計畫")
        : localizeText(lang, "Steps", "步骤", "步驟");

    return (
        <section
            data-testid="coding-agent-plan-checklist"
            aria-label={title}
            style={{
                margin: "0 10px 8px",
                padding: "10px 12px",
                borderRadius: 8,
                border: `1px solid ${chrome.border}`,
                background: chrome.surface,
                color: theme.text,
                fontSize: 12,
                lineHeight: 1.45,
            }}
        >
            <div className="capc-head">
                <div style={{ fontWeight: 650, color: chrome.accentStrong }}>
                    {title}
                    {steps.length > 0 ? <span style={{ marginLeft: 8, fontWeight: 500, color: chrome.muted }}>{codingPlanProgressLabel(steps)}</span> : null}
                </div>
                {running ? (
                    <div data-testid="coding-agent-plan-current" style={{ color: chrome.accentStrong, fontSize: 11 }}>
                        T{running.index} {codingStepStatusLabel(lang, running.status)}
                    </div>
                ) : pendingApproval ? (
                    <div style={{ color: chrome.accentStrong, fontSize: 11, fontWeight: 600 }}>
                        {localizeText(lang, "Awaiting start", "待开始实施", "待開始實施")}
                    </div>
                ) : null}
            </div>
            {restatement ? (
                <div
                    data-testid="coding-agent-plan-understanding"
                    style={{
                        marginBottom: 10,
                        padding: "8px 10px",
                        borderRadius: 6,
                        border: `1px solid ${chrome.border}`,
                        background: chrome.insetBg,
                    }}
                >
                    <div style={{ fontWeight: 650, color: chrome.accentStrong, marginBottom: 4 }}>
                        {localizeText(lang, "Understood", "需求理解", "需求理解")}
                    </div>
                    <div>{restatement}</div>
                </div>
            ) : null}
            <ol className="capc-steps">
                {steps.map((st) => {
                    const color = codingStepStatusColor(st.status, chrome);
                    const active = codingStepIsActive(st.status);
                    const failed = codingStepIsFailed(st.status);
                    return (
                        <li
                            key={st.index}
                            data-testid={`coding-agent-plan-step-${st.index}`}
                            data-status={st.status}
                            aria-current={active ? "step" : undefined}
                            style={{
                                display: "flex",
                                gap: 8,
                                alignItems: "baseline",
                                color,
                                fontWeight: active || failed ? 650 : 400,
                                // The current step gets a soft accent pill so the eye
                                // lands on it without extra chrome.
                                ...(active
                                    ? { background: chrome.chipActiveBg, borderRadius: 4, padding: "2px 6px", margin: "0 -6px" }
                                    : null),
                            }}
                        >
                            <span aria-hidden className="capc-glyph">{codingStepGlyph(st.status)}</span>
                            <span className="capc-step-idx">T{st.index}</span>
                            <span className="capc-step-title">{st.title || codingStepStatusLabel(lang, st.status)}</span>
                            {/* The glyph is aria-hidden and the color carries no
                                meaning for screen readers, so state the status. */}
                            <span data-testid={`coding-agent-plan-step-${st.index}-sr-status`} style={srOnlyStyle}>
                                {`${localizeText(lang, "Status", "状态", "狀態")}：${codingStepStatusLabel(lang, st.status)}`}
                            </span>
                        </li>
                    );
                })}
            </ol>
            {pendingApproval && (
                <div className="capc-actions">
                    <button
                        type="button"
                        data-testid="coding-agent-plan-approve"
                        disabled={!ready}
                        onClick={onApprove}
                        style={{
                            height: 26,
                            padding: "0 10px",
                            border: "none",
                            borderRadius: 5,
                            background: chrome.btnPrimaryBg,
                            color: chrome.btnPrimaryFg,
                            fontSize: 12,
                            fontWeight: 650,
                            cursor: ready ? "pointer" : "not-allowed",
                            opacity: ready ? 1 : 0.55,
                        }}
                    >
                        {localizeText(lang, "Start", "开始实施", "開始實施")}
                    </button>
                    <button
                        type="button"
                        data-testid="coding-agent-plan-skip"
                        disabled={!ready}
                        onClick={onSkip}
                        style={{
                            height: 26,
                            padding: "0 10px",
                            borderRadius: 5,
                            border: `1px solid ${chrome.chipIdleBorder}`,
                            background: chrome.chipIdleBg,
                            color: chrome.muted,
                            fontSize: 12,
                            cursor: ready ? "pointer" : "not-allowed",
                            opacity: ready ? 1 : 0.55,
                        }}
                    >
                        {localizeText(lang, "Skip plan", "跳过规划", "跳過規劃")}
                    </button>
                    <button
                        type="button"
                        data-testid="coding-agent-plan-reject"
                        disabled={!ready}
                        onClick={onReject}
                        style={{
                            height: 26,
                            padding: "0 10px",
                            borderRadius: 5,
                            border: `1px solid ${chrome.chipIdleBorder}`,
                            background: chrome.chipIdleBg,
                            // --theme-danger (not theme.errorText): the latter is the
                            // undecorated brand red and only reaches 3.9:1 on white.
                            color: "var(--theme-danger, #dc2626)",
                            fontSize: 12,
                            cursor: ready ? "pointer" : "not-allowed",
                            opacity: ready ? 1 : 0.55,
                        }}
                    >
                        {localizeText(lang, "Reject", "拒绝", "拒絕")}
                    </button>
                </div>
            )}
        </section>
    );
}

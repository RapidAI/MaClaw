import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import {
    codingStepGlyph,
    codingStepStatusColor,
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
    const done = steps.filter((s) => {
        const st = (s.status || "").toLowerCase();
        return st === "passed" || st === "completed" || st === "skipped" || st === "cancelled";
    }).length;
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
    const dark = !!theme.isDark;
    const running = steps.find((s) => {
        const st = (s.status || "").toLowerCase();
        return st === "running" || st === "in_progress";
    });
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
            <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 8, marginBottom: 8 }}>
                <div style={{ fontWeight: 650, color: chrome.accentStrong }}>
                    {title}
                    {steps.length > 0 ? <span style={{ marginLeft: 8, fontWeight: 500, color: chrome.muted }}>{codingPlanProgressLabel(steps)}</span> : null}
                </div>
                {running ? (
                    <div data-testid="coding-agent-plan-current" style={{ color: chrome.accentStrong, fontSize: 11 }}>
                        T{running.index} {localizeText(lang, "in progress", "进行中", "進行中")}
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
            <ol style={{ margin: 0, padding: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 4 }}>
                {steps.map((st) => {
                    const color = codingStepStatusColor(st.status, dark, chrome);
                    const active = (st.status || "").toLowerCase() === "running" || (st.status || "").toLowerCase() === "in_progress";
                    return (
                        <li
                            key={st.index}
                            data-testid={`coding-agent-plan-step-${st.index}`}
                            data-status={st.status}
                            style={{
                                display: "flex",
                                gap: 8,
                                alignItems: "baseline",
                                color,
                                fontWeight: active ? 650 : 400,
                            }}
                        >
                            <span aria-hidden style={{ width: 14, flexShrink: 0 }}>{codingStepGlyph(st.status)}</span>
                            <span style={{ flexShrink: 0, opacity: 0.86 }}>T{st.index}</span>
                            <span style={{ minWidth: 0, flex: 1 }}>{st.title || st.status}</span>
                        </li>
                    );
                })}
            </ol>
            {pendingApproval && (
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 10 }}>
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
                            color: theme.errorText || "#dc2626",
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

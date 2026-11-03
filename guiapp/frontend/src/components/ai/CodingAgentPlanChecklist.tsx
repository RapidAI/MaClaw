/**
 * Task-breakdown checklist (T1/T2/T3…) under the coding agent composer.
 * Collapsible through its header: the header stays visible and keeps the
 * running step badge, so a collapsed card is still an ambient progress line.
 * The start / skip / reject gate stays expanded (pendingApproval forces open)
 * so the start decision can never hide behind a stored collapse.
 */
import { useCallback, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import {
    codingPlanAwaitingStart,
    codingStepIsDone,
    codingStepIsActive,
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

/** Done or skipped advances the counter; a failed step deliberately does not.
 *  `success`/`succeeded` count as finished alongside `passed`/`completed`. */
export function codingPlanDoneCount(steps: CodingStepStatus[]): number {
    return steps.reduce(
        (done, s) => (codingStepIsDone(s.status) || codingStepIsSkipped(s.status) ? done + 1 : done),
        0,
    );
}

export function codingPlanProgressLabel(steps: CodingStepStatus[]): string {
    return `${codingPlanDoneCount(steps)}/${steps.length}`;
}

const COLLAPSE_KEY = "mc.planChecklist.collapsed";

function readStoredCollapsed(): boolean {
    try {
        return window.localStorage.getItem(COLLAPSE_KEY) === "1";
    } catch {
        return false;
    }
}

/** Codex checklist marks as compact stroke icons; currentColor carries the row status color. */
function PlanStepIcon({ status }: { status: string }) {
    if (codingStepIsDone(status)) {
        return (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.5" />
                <path d="M5 8.35l2 2 4-4.2" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
        );
    }
    if (codingStepIsFailed(status)) {
        return (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.5" />
                <path d="M5.7 5.7l4.6 4.6M10.3 5.7l-4.6 4.6" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
        );
    }
    if (codingStepIsSkipped(status)) {
        return (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.5" />
                <path d="M5.2 8h5.6" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
        );
    }
    if (codingStepIsActive(status)) {
        return (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <g className="capc-spin">
                    <path d="M8 1.85A6.15 6.15 0 1 1 3.65 3.65" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                    <circle cx="8" cy="8" r="2.1" fill="currentColor" opacity="0.55" />
                </g>
            </svg>
        );
    }
    return (
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.5" />
            <circle cx="8" cy="8" r="1.9" fill="currentColor" opacity="0.35" />
        </svg>
    );
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
    // Hooks before the empty-early-return below.
    const [collapsed, setCollapsed] = useState(readStoredCollapsed);
    const toggleCollapsed = useCallback(() => {
        setCollapsed((prev) => {
            const next = !prev;
            try {
                window.localStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
            } catch {
                // Read-only storage keeps the toggle working for this visit only.
            }
            return next;
        });
    }, []);

    const restatement = (understanding || "").trim();
    if (!pendingApproval && steps.length === 0 && !restatement) {
        return null;
    }
    // Same predicate the step rows use, so a backend `started` lights up the row
    // and the header badge together.
    const running = steps.find((s) => codingStepIsActive(s.status));
    // Confirmed plans keep the checklist. The start / skip / reject gate is
    // only for a plan that has not begun, even if the approval flag is stale.
    const showPlanGate = pendingApproval && codingPlanAwaitingStart(steps);
    const title = pendingApproval
        ? localizeText(lang, "Plan", "计划", "計畫")
        : localizeText(lang, "Steps", "步骤", "步驟");
    // The start decision must never sit behind a stored collapse; the collapse
    // preference only applies once the plan has begun executing.
    const open = pendingApproval || !collapsed;
    const progressPct = steps.length > 0
        ? Math.round(Math.min(100, Math.max(0, (codingPlanDoneCount(steps) / steps.length) * 100)))
        : 0;

    return (
        <section
            data-testid="coding-agent-plan-checklist"
            aria-label={title}
            className={"capc-checklist" + (open ? "" : " capc-collapsed")}
            style={{
                // Horizontal inset comes from --mc-composer-inset-x (same rule as
                // the composer card). An inline margin shorthand would override it.
                marginBottom: 8,
                border: `1px solid ${chrome.border}`,
                background: chrome.surface,
                color: theme.text,
            }}
        >
            <button
                type="button"
                data-testid="coding-agent-plan-toggle"
                className="capc-toggle"
                aria-expanded={open}
                aria-controls="coding-agent-plan-body"
                // While the start decision is open the list cannot collapse, so
                // the toggle must not advertise itself as a working control.
                disabled={pendingApproval}
                title={open
                    ? localizeText(lang, "Collapse the step list", "收起步骤列表", "收起步驟列表")
                    : localizeText(lang, "Expand the step list", "展开步骤列表", "展開步驟列表")}
                onClick={toggleCollapsed}
            >
                <span className="capc-head">
                    <span className="capc-title" style={{ color: chrome.accentStrong }}>
                        <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                            <path d="M2.6 3.6h10.8M2.6 8h10.8M2.6 12.4h6.8" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                        </svg>
                        {title}
                        {steps.length > 0 ? (
                            <span data-testid="coding-agent-plan-progress" className="capc-progress">
                                {codingPlanProgressLabel(steps)}
                                <span className="capc-track" aria-hidden="true">
                                    <span className="capc-fill" style={{ width: `${progressPct}%`, background: chrome.accent }} />
                                </span>
                            </span>
                        ) : null}
                    </span>
                    <span className="capc-head-end">
                        {running ? (
                            // span not div: the toggle is a <button>, which may
                            // only contain phrasing content.
                            <span data-testid="coding-agent-plan-current" className="capc-current" style={{ color: chrome.accentStrong }}>
                                <span aria-hidden="true" className="capc-pulse-dot" />
                                {`T${running.index} ${codingStepStatusLabel(lang, running.status)}`}
                            </span>
                        ) : showPlanGate ? (
                            <span className="capc-current" style={{ color: chrome.accentStrong }}>
                                {localizeText(lang, "Awaiting start", "待开始实施", "待開始實施")}
                            </span>
                        ) : null}
                        {!pendingApproval ? (
                            <span aria-hidden="true" className="capc-chevron">
                                <svg viewBox="0 0 16 16" width="14" height="14">
                                    <path d="M4.6 6.4l3.4 3.4 3.4-3.4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                            </span>
                        ) : null}
                    </span>
                </span>
            </button>
            <div
                id="coding-agent-plan-body"
                data-testid="coding-agent-plan-body"
                className="capc-body"
                aria-hidden={!open}
            >
                <div className="capc-body-flow">
                    {restatement ? (
                        <div
                            data-testid="coding-agent-plan-understanding"
                            className="capc-understanding"
                            style={{ border: `1px solid ${chrome.border}`, background: chrome.insetBg }}
                        >
                            <div className="capc-u-title" style={{ color: chrome.accentStrong }}>
                                {localizeText(lang, "Understood", "需求理解", "需求理解")}
                            </div>
                            <div className="capc-u-text">{restatement}</div>
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
                                    className="capc-step"
                                    style={{
                                        color,
                                        fontWeight: active || failed ? 600 : 400,
                                        // The current step gets a soft accent pill so the eye
                                        // lands on it without extra chrome.
                                        ...(active ? { background: chrome.chipActiveBg } : null),
                                    }}
                                >
                                    <span aria-hidden className="capc-glyph"><PlanStepIcon status={st.status} /></span>
                                    <span className="capc-step-idx">T{st.index}</span>
                                    <span className="capc-step-main">
                                        <span className="capc-step-title">{st.title || codingStepStatusLabel(lang, st.status)}</span>
                                        {(st.summary || "").trim() ? (
                                            <span
                                                data-testid={`coding-agent-plan-step-${st.index}-note`}
                                                className="capc-step-note"
                                                style={{
                                                    color: failed ? chrome.stepFailedFg : chrome.muted,
                                                    // Notes are single-line, but a long path or URL can
                                                    // still be one unbroken token; keep it inside the row.
                                                    overflowWrap: "anywhere",
                                                }}
                                            >
                                                {st.summary}
                                            </span>
                                        ) : null}
                                    </span>
                                    {/* The glyph is aria-hidden and the color carries no
                                        meaning for screen readers, so state the status. */}
                                    <span data-testid={`coding-agent-plan-step-${st.index}-sr-status`} style={srOnlyStyle}>
                                        {`${localizeText(lang, "Status", "状态", "狀態")}：${codingStepStatusLabel(lang, st.status)}`}
                                    </span>
                                </li>
                            );
                        })}
                    </ol>
                    {showPlanGate && (
                        <div className="capc-actions">
                            <button
                                type="button"
                                data-testid="coding-agent-plan-approve"
                                className="capc-gate"
                                disabled={!ready}
                                onClick={onApprove}
                                style={{ background: chrome.btnPrimaryBg, color: chrome.btnPrimaryFg, border: "none", fontWeight: 600 }}
                            >
                                {localizeText(lang, "Start", "开始实施", "開始實施")}
                            </button>
                            <button
                                type="button"
                                data-testid="coding-agent-plan-skip"
                                className="capc-gate capc-gate--ghost"
                                disabled={!ready}
                                onClick={onSkip}
                                style={{ background: chrome.chipIdleBg, border: `1px solid ${chrome.chipIdleBorder}`, color: chrome.muted }}
                            >
                                {localizeText(lang, "Skip plan", "跳过规划", "跳過規劃")}
                            </button>
                            <button
                                type="button"
                                data-testid="coding-agent-plan-reject"
                                className="capc-gate capc-gate--ghost"
                                disabled={!ready}
                                onClick={onReject}
                                style={{
                                    background: chrome.chipIdleBg,
                                    border: `1px solid ${chrome.chipIdleBorder}`,
                                    // --theme-danger (not theme.errorText): the latter is the
                                    // undecorated brand red and only reaches 3.9:1 on white.
                                    color: "var(--theme-danger, #dc2626)",
                                }}
                            >
                                {localizeText(lang, "Reject", "拒绝", "拒絕")}
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </section>
    );
}


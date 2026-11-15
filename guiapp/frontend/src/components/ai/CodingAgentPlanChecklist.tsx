/**
 * Task-breakdown checklist (T1/T2/T3…) under the coding agent composer.
 * The bar itself is only the summary (count, current step). The full list
 * floats above that bar while the pointer or keyboard focus is on it, at
 * half the chat width. Start / skip / reject stay on the bar, so the start
 * decision is never hover-only.
 */
import { useEffect, useRef, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { CodingAgentFileChanges, useCapcFloatingRoom } from "./CodingAgentFileChanges";
import type { CodingAgentFileChange } from "./CodingAgentProgressStatus";
import {
    codingPlanAwaitingStart,
    codingStepIsDone,
    codingStepIsActive,
    codingStepIsFailed,
    codingStepIsSkipped,
    codingStepPresentedLabel,
    codingStepStatusColor,
    srOnlyStyle,
    type CodingBannerChrome,
    type CodingStepStatus,
} from "./CodingWorkbenchControlPanel";

export type CodingAgentPlanChecklistProps = {
    lang?: string;
    theme: Theme;
    chrome: CodingBannerChrome;
    steps: CodingStepStatus[];
    fileChanges?: CodingAgentFileChange[];
    understanding?: string;
    pendingApproval?: boolean;
    ready?: boolean;
    /**
     * The task header is actually in progress. A stored `running` step is only
     * the cursor: after an interrupt, pause, or cancel it must not keep spinning.
     * Omitted means live, so a checklist rendered on its own still shows motion.
     */
    executionLive?: boolean;
    /** Header status shown on the current step when the run is not live. */
    executionStatusLabel?: string;
    /** Clicking a changed-file row focuses it in the code preview. */
    onOpenFile?: (path: string) => void;
    /**
     * While a goal command owns the session the goal quick bar hosts the
     * changed-files chip in its right half; the checklist then skips its own
     * copy so the chip is not rendered twice.
     */
    hideFileChanges?: boolean;
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

/** Codex checklist marks as compact stroke icons; currentColor carries the row status color. */
function PlanStepIcon({ status, live = true }: { status: string; live?: boolean }) {
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
        // The cursor stays on this row after the task stops. A pause mark does
        // not use capc-spin, so an interrupted run cannot look still in progress.
        if (!live) {
            return (
                <svg data-icon="hold" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                    <circle cx="8" cy="8" r="6.15" fill="none" stroke="currentColor" strokeWidth="1.3" opacity="0.5" />
                    <path d="M6.35 5.35v5.3M9.65 5.35v5.3" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
            );
        }
        return (
            <svg data-icon="spin" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
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
    fileChanges = [],
    understanding = "",
    pendingApproval = false,
    ready = true,
    executionLive = true,
    executionStatusLabel = "",
    onOpenFile,
    hideFileChanges = false,
    onApprove,
    onSkip,
    onReject,
}: CodingAgentPlanChecklistProps) {
    // Hover and keyboard focus are separate. A mouse leave must not close a
    // list the keyboard still has focused, and clicking the summary must not
    // take focus away from the composer.
    const [hovered, setHovered] = useState(false);
    const [focused, setFocused] = useState(false);
    const closeTimer = useRef<number | null>(null);
    const summaryRef = useRef<HTMLButtonElement>(null);
    const rootRef = useRef<HTMLElement>(null);
    const hoverRef = useRef<HTMLDivElement>(null);
    const popoverRef = useRef<HTMLDivElement>(null);
    // Last pointer x, so a resize can keep the list under the cursor.
    const anchorX = useRef<number | null>(null);
    const restatement = (understanding || "").trim();
    const hasList = steps.length > 0 || !!restatement;
    const hasFiles = fileChanges.length > 0;
    useEffect(() => () => {
        if (closeTimer.current != null) window.clearTimeout(closeTimer.current);
    }, []);
    useEffect(() => {
        if (hasList) return;
        if (closeTimer.current != null) {
            window.clearTimeout(closeTimer.current);
            closeTimer.current = null;
        }
        setHovered(false);
        setFocused(false);
        if (summaryRef.current && document.activeElement === summaryRef.current) {
            summaryRef.current.blur();
        }
    }, [hasList]);
    const showList = () => {
        if (!hasList) return;
        if (closeTimer.current != null) {
            window.clearTimeout(closeTimer.current);
            closeTimer.current = null;
        }
        setHovered(true);
    };
    const hideList = () => {
        if (closeTimer.current != null) window.clearTimeout(closeTimer.current);
        // The list can appear under a pointer that already left the summary,
        // so :hover stays true and mouseleave does not fire again. Keep
        // checking until the pointer is actually gone.
        const tick = () => {
            closeTimer.current = null;
            if (hoverRef.current?.matches(":hover")) {
                closeTimer.current = window.setTimeout(tick, 120);
                return;
            }
            setHovered(false);
        };
        closeTimer.current = window.setTimeout(tick, 120);
    };
    const open = hasList && (hovered || focused);
    // The list is only half the bar. Pinning it to the left means a pointer
    // on the current-step status leaves the list as soon as it moves up.
    // Center it on the pointer, and keep it inside the bar.
    const placePopover = () => {
        const clientX = anchorX.current;
        const root = rootRef.current;
        const pop = popoverRef.current;
        if (clientX == null || !root || !pop) return;
        const width = pop.offsetWidth;
        const innerWidth = root.clientWidth;
        if (width <= 0 || innerWidth <= 0) return;
        const x = clientX - root.getBoundingClientRect().left - root.clientLeft;
        const left = Math.max(0, Math.min(x - width / 2, innerWidth - width));
        pop.style.left = `${Math.round(left)}px`;
    };
    const placePopoverRef = useRef(placePopover);
    placePopoverRef.current = placePopover;
    useEffect(() => {
        if (open) return;
        anchorX.current = null;
        if (popoverRef.current) popoverRef.current.style.left = "";
    }, [open]);
    useCapcFloatingRoom(open, () => rootRef.current, () => placePopoverRef.current());

    // While a goal command owns the session the goal quick bar hosts the
    // changed-files chip; this bar skips its own copy then.
    const showFilesChip = hasFiles && !hideFileChanges;
    if (!pendingApproval && !hasList && !showFilesChip) {
        return null;
    }
    // Same predicate the step rows use, so a backend `started` lights up the row
    // and the header badge together.
    const running = steps.find((s) => codingStepIsActive(s.status));
    const stepLabel = (status: string) => codingStepPresentedLabel(lang, status, executionLive, executionStatusLabel);
    // Confirmed plans keep the checklist. The start / skip / reject gate is
    // only for a plan that has not begun, even if the approval flag is stale.
    const showPlanGate = pendingApproval && codingPlanAwaitingStart(steps);
    const title = pendingApproval
        ? localizeText(lang, "Plan", "计划", "計畫")
        : localizeText(lang, "Steps", "步骤", "步驟");
    const progressPct = steps.length > 0
        ? Math.round(Math.min(100, Math.max(0, (codingPlanDoneCount(steps) / steps.length) * 100)))
        : 0;

    return (
        <section
            ref={rootRef}
            data-testid="coding-agent-plan-checklist"
            aria-label={title}
            className={"capc-checklist" + (open ? " capc-open" : "")}
            style={{
                // Horizontal inset comes from --mc-composer-inset-x (same rule as
                // the composer card). An inline margin shorthand would override it.
                marginBottom: 8,
                border: `1px solid ${chrome.border}`,
                background: chrome.surface,
                color: theme.text,
            }}
        >
            <div className="capc-bar">
            {hasList || pendingApproval ? (
            <div
                ref={hoverRef}
                className={"capc-hover" + (open ? " capc-open" : "")}
                data-testid="coding-agent-plan-hover"
                onMouseEnter={hasList ? showList : undefined}
                onMouseLeave={hasList ? hideList : undefined}
            >
            <button
                ref={summaryRef}
                type="button"
                data-testid="coding-agent-plan-toggle"
                className="capc-toggle"
                aria-expanded={hasList ? open : undefined}
                aria-controls={hasList ? "coding-agent-plan-body" : undefined}
                tabIndex={hasList ? 0 : -1}
                title={hasList
                    ? localizeText(lang, "Hover to see the step list", "悬停查看步骤列表", "懸停查看步驟列表")
                    : undefined}
                onMouseEnter={hasList ? (event) => {
                    anchorX.current = event.clientX;
                    placePopover();
                } : undefined}
                onMouseMove={hasList ? (event) => {
                    anchorX.current = event.clientX;
                    placePopover();
                } : undefined}
                onMouseDown={hasList ? (event) => {
                    // Keep the caret in the composer. Keyboard focus still opens the list.
                    event.preventDefault();
                } : undefined}
                onFocus={hasList ? () => setFocused(true) : undefined}
                onBlur={hasList ? () => setFocused(false) : undefined}
                onKeyDown={hasList ? (event) => {
                    if (event.key !== "Escape") return;
                    event.preventDefault();
                    setFocused(false);
                    event.currentTarget.blur();
                } : undefined}
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
                                {executionLive ? <span aria-hidden="true" className="capc-pulse-dot" /> : null}
                                {`T${running.index} ${stepLabel(running.status)}`}
                            </span>
                        ) : showPlanGate ? (
                            <span className="capc-current" style={{ color: chrome.accentStrong }}>
                                {localizeText(lang, "Awaiting start", "待开始实施", "待開始實施")}
                            </span>
                        ) : null}
                        {hasList ? (
                            <span aria-hidden="true" className="capc-chevron">
                                <svg viewBox="0 0 16 16" width="14" height="14">
                                    <path d="M4.6 6.4l3.4 3.4 3.4-3.4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                            </span>
                        ) : null}
                    </span>
                </span>
            </button>
            {hasList ? (
                <div ref={popoverRef} className="capc-popover" aria-hidden={!open}>
                    <div
                        id="coding-agent-plan-body"
                        data-testid="coding-agent-plan-body"
                        className="capc-popover-card"
                        aria-hidden={!open}
                        style={{
                            border: `1px solid ${chrome.border}`,
                            background: chrome.surface,
                            color: theme.text,
                        }}
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
                            {steps.length > 0 ? (
                            <ol className="capc-steps">
                                {steps.map((st) => {
                                    const color = codingStepStatusColor(st.status, chrome);
                                    const active = codingStepIsActive(st.status);
                                    const failed = codingStepIsFailed(st.status);
                                    // Bold and the accent pill mean "executing now". A stopped
                                    // cursor keeps its color and pause mark without that chrome.
                                    const moving = active && executionLive;
                                    const statusText = stepLabel(st.status);
                                    return (
                                        <li
                                            key={st.index}
                                            data-testid={`coding-agent-plan-step-${st.index}`}
                                            data-status={st.status}
                                            aria-current={active ? "step" : undefined}
                                            className="capc-step"
                                            style={{
                                                color,
                                                fontWeight: moving || failed ? 600 : 400,
                                                // The current step gets a soft accent pill so the eye
                                                // lands on it without extra chrome.
                                                ...(moving ? { background: chrome.chipActiveBg } : null),
                                            }}
                                        >
                                            <span aria-hidden className="capc-glyph"><PlanStepIcon status={st.status} live={executionLive} /></span>
                                            <span className="capc-step-idx">T{st.index}</span>
                                            <span className="capc-step-main">
                                                <span className="capc-step-title">{st.title || statusText}</span>
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
                                                {`${localizeText(lang, "Status", "状态", "狀態")}：${statusText}`}
                                            </span>
                                        </li>
                                    );
                                })}
                            </ol>
                            ) : null}
                        </div>
                    </div>
                </div>
            ) : null}
            </div>
            ) : null}
            {showFilesChip ? (
                <CodingAgentFileChanges
                    lang={lang}
                    theme={theme}
                    chrome={chrome}
                    fileChanges={fileChanges}
                    onOpenFile={onOpenFile}
                />
            ) : null}
            </div>
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
        </section>
    );
}


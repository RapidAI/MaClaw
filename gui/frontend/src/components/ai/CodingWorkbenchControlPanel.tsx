import { useEffect, useId, useMemo, useRef, type CSSProperties, type ReactNode } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { isFormFieldTarget, isVisibleCodingConflictPanelPresent } from "./codingUiGuards";

export { isFormFieldTarget } from "./codingUiGuards";

/** Chrome tokens for coding workbench floating control (light/dark + remote/local). */
export type CodingBannerChrome = {
    accent: string;
    accentStrong: string;
    surface: string;
    border: string;
    chipActiveBg: string;
    chipIdleBg: string;
    chipIdleBorder: string;
    iconWellBg: string;
    insetBg: string;
    muted: string;
    btnPrimaryBg: string;
    btnPrimaryFg: string;
};

/** Dark local accent — muted sage (not neon #4ade80 / #86efac). */
export const CODING_BANNER_LOCAL_DARK_ACCENT = "#5a9074";
export const CODING_BANNER_LOCAL_DARK_ACCENT_STRONG = "#9db8a8";

type BuildCodingBannerChromeOpts = {
    isDark: boolean;
    remote: boolean;
    theme: Pick<Theme, "btnColor" | "titleBarBg" | "bg" | "titleBarBorder" | "fieldBorder" | "fieldBg" | "textMuted" | "promptColor">;
};

/**
 * Build float-panel chrome for local/remote × light/dark.
 * Dark local deliberately avoids bright greens so the panel does not cast green over dark UI.
 */
export function buildCodingBannerChrome({ isDark, remote, theme: t }: BuildCodingBannerChromeOpts): CodingBannerChrome {
    const productBlue = t.btnColor || "#2f5f98";
    const accent = remote
        ? (isDark ? "#38bdf8" : "#0284c7")
        : (isDark ? CODING_BANNER_LOCAL_DARK_ACCENT : productBlue);
    const accentStrong = remote
        ? (isDark ? "#7dd3fc" : "#0c4a6e")
        : (isDark ? CODING_BANNER_LOCAL_DARK_ACCENT_STRONG : "#1e4a7a");
    const surface = isDark
        ? `color-mix(in srgb, ${accent} 6%, ${t.titleBarBg || t.bg})`
        : "#f5f8fc";
    const border = isDark
        ? `color-mix(in srgb, ${accent} 16%, ${t.titleBarBorder})`
        : (t.titleBarBorder || "#d8dee8");
    const chipActiveBg = isDark
        ? `color-mix(in srgb, ${accent} 14%, transparent)`
        : "rgba(47, 95, 152, 0.10)";
    const chipIdleBg = isDark ? "transparent" : "#ffffff";
    const chipIdleBorder = t.fieldBorder || (isDark ? "rgba(148,163,184,0.35)" : "#d8dee8");
    const iconWellBg = isDark
        ? `color-mix(in srgb, ${accent} 14%, transparent)`
        : "rgba(47, 95, 152, 0.08)";
    const insetBg = isDark ? (t.fieldBg || "transparent") : "#ffffff";
    const muted = t.textMuted || t.promptColor || (isDark ? "#a8b8c8" : "#64748b");
    return {
        accent,
        accentStrong,
        surface,
        border,
        chipActiveBg,
        chipIdleBg,
        chipIdleBorder,
        iconWellBg,
        insetBg,
        muted,
        btnPrimaryBg: productBlue,
        btnPrimaryFg: "#ffffff",
    };
}

/** Step row color for the control-panel checklist (dark keeps sage/coral, not neon). */
export function codingStepStatusColor(status: string, isDark: boolean, chrome: Pick<CodingBannerChrome, "accentStrong" | "muted">): string {
    const s = (status || "").toLowerCase();
    if (s === "passed") return isDark ? CODING_BANNER_LOCAL_DARK_ACCENT : "#16a34a";
    if (s === "failed" || s === "verify_failed") return isDark ? "#e07a72" : "#dc2626";
    if (s === "running") return chrome.accentStrong;
    return chrome.muted;
}

export type CodingStepStatus = {
    index: number;
    title?: string;
    status: string;
    summary?: string;
    verify_cmd?: string;
    verify_ok?: boolean;
};

export type CodingWorkbenchControlPanelProps = {
    lang?: string;
    theme: Theme;
    chrome: CodingBannerChrome;
    remote: boolean;
    remoteHost?: string;
    preparing?: boolean;
    prepareMode?: string;
    stepStatuses: CodingStepStatus[];
    pendingApproval: boolean;
    conflictCount: number;
    /** When true, click-outside / Escape will not collapse (e.g. SSH reconnect). */
    lockExpanded?: boolean;
    expanded: boolean;
    onExpandedChange: (next: boolean) => void;
    /** Optional tooltip / title describing the full environment (long copy). */
    envDescription?: string;
    children: ReactNode;
};

/** Pure helper — exported for unit tests. */
export function deriveChipStatus(
    lang: string | undefined,
    steps: CodingStepStatus[],
    pendingApproval: boolean,
    preparing?: boolean,
    prepareMode?: string,
    needsAttention?: boolean,
): string {
    if (preparing && prepareMode === "new-agent") {
        return localizeText(lang, "Starting…", "启动中…", "啟動中…");
    }
    const failed = steps.find((s) => s.status === "failed" || s.status === "verify_failed");
    if (failed) {
        return `T${failed.index} ✗`;
    }
    const running = steps.find((s) => s.status === "running");
    if (running) {
        return `T${running.index}…`;
    }
    if (pendingApproval) {
        return localizeText(lang, "Pending", "待批", "待批");
    }
    if (needsAttention) {
        return localizeText(lang, "Attention", "需处理", "需處理");
    }
    if (steps.length > 0) {
        const last = steps[steps.length - 1];
        if (last.status === "passed") {
            return localizeText(lang, "Done", "完成", "完成");
        }
        return `T${last.index}`;
    }
    return localizeText(lang, "Ready", "就绪", "就緒");
}

const srOnlyStyle: CSSProperties = {
    position: "absolute",
    width: 1,
    height: 1,
    padding: 0,
    margin: -1,
    overflow: "hidden",
    clipPath: "inset(50%)",
    whiteSpace: "nowrap",
    border: 0,
};

/**
 * Floating coding-workbench control: collapsed chip (zero document-flow cost)
 * + optional top-right popover with full controls as children.
 *
 * Layout: chip stays pinned at the top-right; popover drops below the chip.
 */
export function CodingWorkbenchControlPanel({
    lang,
    theme: t,
    chrome,
    remote,
    remoteHost,
    preparing,
    prepareMode,
    stepStatuses,
    pendingApproval,
    conflictCount,
    lockExpanded = false,
    expanded,
    onExpandedChange,
    envDescription,
    children,
}: CodingWorkbenchControlPanelProps) {
    const rootRef = useRef<HTMLDivElement | null>(null);
    // Keep latest callback without re-binding document listeners every render.
    const onExpandedChangeRef = useRef(onExpandedChange);
    onExpandedChangeRef.current = onExpandedChange;
    const lockExpandedRef = useRef(lockExpanded);
    lockExpandedRef.current = lockExpanded;

    const reactId = useId();
    const popoverId = `coding-control-popover-${reactId.replace(/:/g, "")}`;

    // Dismiss on Escape / outside click (unless locked for a blocking interrupt).
    useEffect(() => {
        if (!expanded) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || lockExpandedRef.current) return;
            // Let text fields own Escape (cancel edit / leave field) without discarding the panel.
            if (isFormFieldTarget(e.target)) return;
            // Visible CF tab owns Escape (hidden CF slot under SRC/WF must not block float Esc).
            if (isVisibleCodingConflictPanelPresent()) return;
            e.stopPropagation();
            onExpandedChangeRef.current(false);
        };
        const onPointer = (e: PointerEvent) => {
            if (lockExpandedRef.current) return;
            // Only primary button dismisses (ignore right-click / pen barrel).
            if (typeof e.button === "number" && e.button !== 0) return;
            const el = rootRef.current;
            if (!el) return;
            const target = e.target;
            // Sibling chrome (e.g. reconnect success toast) can opt out of outside-dismiss.
            if (target instanceof Element && target.closest("[data-coding-float-ignore-outside]")) return;
            if (target instanceof Node && !el.contains(target)) {
                onExpandedChangeRef.current(false);
            }
        };
        document.addEventListener("keydown", onKey, true);
        document.addEventListener("pointerdown", onPointer, true);
        return () => {
            document.removeEventListener("keydown", onKey, true);
            document.removeEventListener("pointerdown", onPointer, true);
        };
    }, [expanded]);

    const label = useMemo(() => (
        remote
            ? (remoteHost
                ? localizeText(lang, `Remote · ${remoteHost}`, `远程 · ${remoteHost}`, `遠端 · ${remoteHost}`)
                : localizeText(lang, "Remote coding", "远程编程", "遠端程式"))
            : localizeText(lang, "Coding", "编程", "程式")
    ), [lang, remote, remoteHost]);

    // Keep test-friendly long names available via title + sr-only text.
    const longTitle = useMemo(() => (
        remote
            ? (remoteHost
                ? localizeText(lang, `Remote coding environment · ${remoteHost}`, `远程编程环境 · ${remoteHost}`, `遠端程式開發環境 · ${remoteHost}`)
                : localizeText(lang, "Remote coding environment", "远程编程环境", "遠端程式開發環境"))
            : localizeText(lang, "Full coding environment", "全功能编程环境", "全功能程式開發環境")
    ), [lang, remote, remoteHost]);

    // Prefer step progress on the chip; pending/conflict badges carry those states.
    const status = useMemo(
        () => deriveChipStatus(lang, stepStatuses, false, preparing, prepareMode, false),
        [lang, stepStatuses, preparing, prepareMode],
    );
    // Hide idle "Ready" to keep the chip compact when nothing is happening.
    const showStatusText = useMemo(() => {
        if (preparing && prepareMode === "new-agent") return true;
        if (stepStatuses.length > 0) return true;
        return false;
    }, [preparing, prepareMode, stepStatuses.length]);

    const bannerTestId = remote ? "remote-coding-env-banner" : "coding-env-banner";
    const chipTitle = useMemo(
        () => [longTitle, envDescription].filter(Boolean).join("\n\n"),
        [longTitle, envDescription],
    );
    const ariaLabel = useMemo(() => {
        const parts = [longTitle];
        if (showStatusText) parts.push(status);
        if (pendingApproval) parts.push(localizeText(lang, "plan pending approval", "计划待批准", "計畫待批准"));
        if (conflictCount > 0) {
            parts.push(localizeText(lang, `${conflictCount} conflicts`, `${conflictCount} 个冲突`, `${conflictCount} 個衝突`));
        }
        if (expanded) parts.push(localizeText(lang, "expanded", "已展开", "已展開"));
        else parts.push(localizeText(lang, "collapsed", "已收起", "已收起"));
        return parts.filter(Boolean).join(" · ");
    }, [longTitle, showStatusText, status, pendingApproval, conflictCount, expanded, lang]);

    const toggle = () => {
        if (lockExpanded && expanded) return;
        onExpandedChange(!expanded);
    };

    const rootStyle = useMemo((): CSSProperties => ({
        position: "absolute",
        top: 8,
        right: 10,
        // Only pin bottom while expanded so the collapsed chip is not a full-height overlay.
        ...(expanded ? { bottom: 8 } : null),
        zIndex: 40,
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-end",
        gap: 6,
        maxWidth: "min(360px, calc(100% - 16px))",
        // Let chat scroll/select under the empty area of this root.
        pointerEvents: "none",
    }), [expanded]);

    const chipStyle = useMemo((): CSSProperties => ({
        pointerEvents: "auto",
        position: "relative",
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        maxWidth: "min(320px, calc(100vw - 24px))",
        height: 30,
        padding: "0 10px 0 6px",
        borderRadius: 999,
        border: `1px solid ${expanded ? chrome.accent : chrome.border}`,
        background: chrome.surface,
        color: t.text,
        boxShadow: t.isDark
            ? "0 4px 14px rgba(0,0,0,0.35)"
            : "0 4px 14px rgba(15,23,42,0.10)",
        cursor: lockExpanded && expanded ? "default" : "pointer",
        fontSize: 11,
        lineHeight: 1,
        fontFamily: "inherit",
        flexShrink: 0,
    }), [expanded, chrome.accent, chrome.border, chrome.surface, t.text, t.isDark, lockExpanded]);

    const popoverStyle = useMemo((): CSSProperties => ({
        pointerEvents: "auto",
        width: "min(360px, calc(100vw - 32px))",
        // 100% is the top/bottom-pinned root; reserve room for the chip + gap.
        maxHeight: "min(520px, calc(100% - 40px))",
        minHeight: 0,
        flex: "0 1 auto",
        overflow: "auto",
        overscrollBehavior: "contain",
        padding: "10px 12px",
        borderRadius: 10,
        border: `1px solid ${chrome.border}`,
        background: chrome.surface,
        color: t.text,
        boxShadow: t.isDark
            ? "0 16px 40px rgba(0,0,0,0.45), 0 0 0 1px rgba(148,163,184,0.12)"
            : "0 14px 36px rgba(15,23,42,0.16), 0 0 0 1px rgba(15,23,42,0.04)",
        fontSize: 12,
        lineHeight: 1.4,
    }), [chrome.border, chrome.surface, t.text, t.isDark]);

    return (
        <div
            ref={rootRef}
            data-testid="coding-control-float-root"
            data-expanded={expanded ? "true" : "false"}
            style={rootStyle}
        >
            {/* Chip first so it stays pinned at the top-right while the popover drops below. */}
            <button
                type="button"
                data-testid={bannerTestId}
                aria-label={ariaLabel}
                aria-expanded={expanded}
                aria-haspopup="dialog"
                aria-controls={expanded ? popoverId : undefined}
                title={chipTitle}
                data-env-description={envDescription || ""}
                onClick={toggle}
                style={chipStyle}
            >
                <span
                    aria-hidden="true"
                    style={{
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        width: 18,
                        height: 18,
                        borderRadius: 5,
                        flexShrink: 0,
                        background: chrome.iconWellBg,
                        color: chrome.accentStrong,
                        border: `1px solid ${chrome.border}`,
                        boxSizing: "border-box",
                    }}
                >
                    {remote ? (
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
                            <rect x="2.5" y="4" width="11" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.4" />
                            <path d="M5 7h3M5 9.5h5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
                            <path d="m10.5 7 1.5 1.2L10.5 9.4" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    ) : (
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
                            <path d="M5.5 4.5 2.5 8l3 3.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                            <path d="m10.5 4.5 3 3.5-3 3.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                            <path d="m9 3.5-2 9" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                        </svg>
                    )}
                </span>
                <span style={{ fontWeight: 700, color: chrome.accentStrong, whiteSpace: "nowrap" }}>{label}</span>
                {showStatusText && (
                    <span
                        aria-live="polite"
                        style={{ color: chrome.muted, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", maxWidth: 96 }}
                    >
                        {status}
                    </span>
                )}
                {pendingApproval && (
                    <span
                        data-testid="coding-control-chip-pending"
                        style={{
                            fontSize: 10,
                            fontWeight: 700,
                            color: chrome.accentStrong,
                            background: chrome.chipActiveBg,
                            borderRadius: 999,
                            padding: "2px 6px",
                            whiteSpace: "nowrap",
                        }}
                    >
                        {localizeText(lang, "Pending", "待批", "待批")}
                    </span>
                )}
                {conflictCount > 0 && (
                    <span
                        data-testid="coding-control-chip-conflicts"
                        style={{
                            fontSize: 10,
                            fontWeight: 700,
                            color: "#dc2626",
                            background: "color-mix(in srgb, #dc2626 12%, transparent)",
                            borderRadius: 999,
                            padding: "2px 6px",
                            whiteSpace: "nowrap",
                        }}
                    >
                        {localizeText(lang, `Conflict ${conflictCount}`, `冲突 ${conflictCount}`, `衝突 ${conflictCount}`)}
                    </span>
                )}
                <span
                    aria-hidden="true"
                    style={{
                        color: chrome.muted,
                        fontSize: 10,
                        transform: expanded ? "rotate(180deg)" : "none",
                        transition: "transform 0.12s ease",
                        flexShrink: 0,
                    }}
                >
                    ▾
                </span>
                {/* Hidden long title for legacy tests that scan textContent. */}
                <span style={srOnlyStyle}>{longTitle}</span>
            </button>

            {expanded && (
                <div
                    id={popoverId}
                    data-testid="coding-control-popover"
                    role="dialog"
                    aria-label={longTitle}
                    aria-modal="false"
                    style={popoverStyle}
                >
                    <div
                        style={{
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "space-between",
                            gap: 8,
                            marginBottom: 8,
                            paddingBottom: 6,
                            borderBottom: `1px solid ${chrome.border}`,
                            position: "sticky",
                            top: 0,
                            marginTop: -10,
                            marginLeft: -12,
                            marginRight: -12,
                            paddingTop: 10,
                            paddingLeft: 12,
                            paddingRight: 12,
                            background: chrome.surface,
                            zIndex: 1,
                        }}
                    >
                        <strong
                            style={{
                                fontWeight: 700,
                                color: chrome.accentStrong,
                                fontSize: 12,
                                minWidth: 0,
                                overflow: "hidden",
                                textOverflow: "ellipsis",
                                whiteSpace: "nowrap",
                            }}
                        >
                            {longTitle}
                        </strong>
                        {!lockExpanded && (
                            <button
                                type="button"
                                data-testid="coding-control-popover-close"
                                onClick={() => onExpandedChange(false)}
                                style={{
                                    border: "none",
                                    background: "transparent",
                                    color: chrome.muted,
                                    cursor: "pointer",
                                    fontSize: 11,
                                    padding: "2px 4px",
                                    flexShrink: 0,
                                }}
                            >
                                {localizeText(lang, "Close", "收起", "收起")}
                            </button>
                        )}
                    </div>
                    {children}
                </div>
            )}
        </div>
    );
}

/** Section header used inside the coding control popover. */
export function CodingControlSection({
    title,
    chrome,
    children,
    testId,
}: {
    title: string;
    chrome: CodingBannerChrome;
    children: ReactNode;
    testId?: string;
}) {
    return (
        <div data-testid={testId} style={{ marginBottom: 10 }}>
            <div
                style={{
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: "0.04em",
                    textTransform: "uppercase",
                    color: chrome.muted,
                    marginBottom: 6,
                }}
            >
                {title}
            </div>
            {children}
        </div>
    );
}

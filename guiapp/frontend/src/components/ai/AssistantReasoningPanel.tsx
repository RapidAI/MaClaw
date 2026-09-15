import React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { markdownPreviewIsDark } from "./markdownPreviewInk";
import { useNestedPinnedScroll } from "./useNestedPinnedScroll";

/** Mix muted ink into the panel surface — never into `transparent` (black). */
export function reasoningPanelChrome(t: Pick<Theme, "bg" | "textMuted" | "isDark">): React.CSSProperties {
    const dark = markdownPreviewIsDark(t.bg, t.isDark);
    const ink = t.textMuted;
    const surface = t.bg;
    return {
        margin: "5px 0 7px 0",
        fontSize: "12px",
        color: ink,
        borderLeft: `2px solid color-mix(in srgb, ${ink} ${dark ? "48%" : "32%"}, ${surface})`,
        background: `color-mix(in srgb, ${ink} ${dark ? "8%" : "4.5%"}, ${surface})`,
        borderRadius: "0 7px 7px 0",
    };
}

/**
 * Collapsible activity panel used by the assistant transcript and the coding
 * timeline. Extracted from aiAssistantMarkdown.tsx so the markdown renderer
 * keeps its size budget; behaviour (open state, pinned nested scroll, live
 * styling) is unchanged. Live sheen follows `live`, not a specific status string.
 */
export function AssistantReasoningPanel({
    defaultOpen,
    label,
    step,
    lang,
    preview,
    theme: t,
    contentKey,
    live = false,
    objectLabel,
    children,
}: {
    defaultOpen: boolean;
    label: string;
    /** Optional timeline step number for coding-agent thoughts. */
    step?: number;
    lang?: string;
    /** Short single-line summary shown while the panel is collapsed. */
    preview?: string;
    theme: Theme;
    contentKey: string;
    /** True while the current round is in flight — any live activity title + sheen. */
    live?: boolean;
    /** Plain object after the live action, e.g. model or tool name. Sheen stays on `label`. */
    objectLabel?: string;
    children: React.ReactNode;
}) {
    const [isOpen, setIsOpen] = React.useState(defaultOpen);
    const { bodyRef, contentRef, handleScroll, handleUserScrollIntent } = useNestedPinnedScroll(isOpen, contentKey);
    React.useLayoutEffect(() => {
        setIsOpen(defaultOpen);
    }, [defaultOpen]);
    const dark = markdownPreviewIsDark(t.bg, t.isDark);
    const liveFg = dark ? "#cbd5e1" : t.textMuted;
    const panelChrome = reasoningPanelChrome(t);
    const summaryStyle: React.CSSProperties = {
        cursor: "pointer",
        display: "flex",
        alignItems: "center",
        gap: 7,
        minHeight: 26,
        padding: "2px 9px 2px 8px",
        color: liveFg,
        fontWeight: 650,
        opacity: live ? 1 : 0.94,
        listStyleType: "none",
        minWidth: 0,
        width: "100%",
        boxSizing: "border-box",
    };
    const hasBody = hasRenderableReasoningBody(children);
    const liveDot = (
        <span
            aria-hidden="true"
            className={live ? "assistant-reasoning-live-dot" : undefined}
            style={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                background: dark ? "#94a3b8" : t.textMuted,
                flex: "0 0 auto",
                boxShadow: hasBody && isOpen
                    ? `0 0 0 3px color-mix(in srgb, ${t.textMuted} ${dark ? "16%" : "10%"}, ${t.bg})`
                    : undefined,
            }}
        />
    );
    const objectText = live ? String(objectLabel || "").trim() : "";
    const statusLabel = objectText ? `${label} ${objectText}` : label;
    const liveLabel = (
        <span
            aria-live={live ? "polite" : undefined}
            style={{
                display: "inline-flex",
                alignItems: "baseline",
                gap: 6,
                minWidth: 0,
                flex: objectText ? "1 1 auto" : "0 1 auto",
            }}
        >
            <span
                className={live ? "assistant-reasoning-live-label" : undefined}
                data-testid="assistant-reasoning-label"
                style={{
                    flex: "0 0 auto",
                    whiteSpace: "nowrap",
                }}
            >
                {label}
            </span>
            {objectText ? (
                <span
                    data-testid="assistant-reasoning-object"
                    title={objectText}
                    style={{
                        flex: "1 1 auto",
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        fontWeight: 450,
                        opacity: 0.78,
                    }}
                >
                    {objectText}
                </span>
            ) : null}
        </span>
    );
    const stepMark = typeof step === "number" ? (
        <span style={{ fontSize: 10, fontWeight: 600, opacity: .72, flex: "0 0 auto", whiteSpace: "nowrap" }}>#{step}</span>
    ) : null;
    const summaryChildren = (showToggle: boolean) => (
        <>
            {liveDot}
            {liveLabel}
            {stepMark}
            {preview && !isOpen && !live && <span style={{ flex: "1 1 auto", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 450, opacity: .72 }}>{preview}</span>}
            {showToggle ? (
                <span aria-live="polite" data-testid="assistant-reasoning-toggle-state" style={{ marginLeft: "auto", fontSize: 10, fontWeight: 500, opacity: .65 }}>
                    {isOpen
                        ? (lang === "en" ? "Collapse" : lang === "zh-Hant" ? "收起" : "收起")
                        : (lang === "en" ? "Expand" : lang === "zh-Hant" ? "展開" : "展开")}
                </span>
            ) : null}
        </>
    );
    const panelAttrs = {
        className: "assistant-reasoning-panel",
        "data-testid": "assistant-reasoning-panel",
        "data-live": live ? "true" : "false",
        "aria-label": statusLabel,
        style: panelChrome,
    };
    if (!hasBody) {
        return (
            <div {...panelAttrs} role="status">
                <div className="assistant-reasoning-summary assistant-reasoning-summary--plain" style={{ ...summaryStyle, cursor: "default" }}>
                    {summaryChildren(false)}
                </div>
            </div>
        );
    }
    return (
        <details
            open={isOpen}
            onToggle={(event) => setIsOpen(event.currentTarget.open)}
            {...panelAttrs}
        >
            <summary className="assistant-reasoning-summary" style={summaryStyle}>
                {summaryChildren(true)}
            </summary>
            <div
                ref={bodyRef}
                data-testid="assistant-reasoning-body"
                data-nested-scroll=""
                onWheel={(event) => {
                    event.stopPropagation();
                    handleUserScrollIntent(event);
                }}
                onTouchMove={(event) => {
                    event.stopPropagation();
                    handleUserScrollIntent(event);
                }}
                onPointerDown={handleUserScrollIntent}
                onScroll={handleScroll}
                // Keep CJK punctuation and its adjacent text together in the
                // thinking trail. `line-break: strict` handles East-Asian
                // opening/closing punctuation without disabling normal CJK
                // wrapping. Keep `word-break: normal` so long Chinese thought
                // streams remain readable instead of overflowing the panel.
                // `overflow-wrap` is an emergency fallback for unbroken tokens.
                style={{ padding: "5px 10px 8px 25px", color: t.text, opacity: 0.88, maxHeight: "400px", overflow: "auto", lineBreak: "strict", wordBreak: "normal", overflowWrap: "break-word", lineHeight: 1.6 }}
            >
                <div ref={contentRef}>{children}</div>
            </div>
        </details>
    );
}

function hasRenderableReasoningBody(children: React.ReactNode): boolean {
    if (children == null || children === false || children === true) return false;
    if (typeof children === "string") return children.trim().length > 0;
    if (Array.isArray(children)) return children.some(hasRenderableReasoningBody);
    if (React.isValidElement(children) && children.type === React.Fragment) {
        return hasRenderableReasoningBody((children.props as { children?: React.ReactNode }).children);
    }
    return true;
}

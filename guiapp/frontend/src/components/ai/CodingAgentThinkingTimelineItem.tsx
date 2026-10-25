import React from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { createReasoningTrailCleanCache, rememberReasoningTrailClean } from "./reasoningTrailCleanCache";
import { createIncrementalRenderState, renderContentIncremental, streamingMarkdownUsesIncremental, type IncrementalRenderState } from "./IncrementalMarkdownRenderer";
import { reasoningTrailMarkdownOptions, renderContentWithCodeBlocks } from "./aiAssistantMarkdown";
import { codingThoughtTextIsBlank, stripCodingThoughtChrome } from "./assistantLiveActivity";
import type { CodingAgentTimelineItem } from "./useAIAssistant";
import { CodingAgentProcessChevron, CodingAgentProcessIcon } from "./CodingAgentProcessIcon";
import { formatCodingAgentDuration } from "./CodingAgentProgressStatus";
import { useNestedPinnedScroll } from "./useNestedPinnedScroll";

/**
 * Open thoughts re-parse on every token. Once the cleaned text only grows,
 * freeze the stable prefix and re-parse the tail. A repair that rewrites the
 * prefix, or a folded thought, falls back to one full parse.
 */
function renderGrowingThought(
    text: string,
    theme: Theme,
    expanded: boolean,
    previousText: React.MutableRefObject<string>,
    rendered: React.MutableRefObject<React.ReactNode>,
    trailState: React.MutableRefObject<IncrementalRenderState>,
): React.ReactNode {
    const previous = previousText.current;
    // Folding does not change the text. Keep the tree already on screen.
    if (!expanded && previous === text && rendered.current != null) return rendered.current;
    const extend = expanded
        && streamingMarkdownUsesIncremental(text.length)
        && previous.length > 0
        && text.startsWith(previous);
    previousText.current = text;
    const nodes = extend
        ? renderContentIncremental(text, theme, trailState.current, reasoningTrailMarkdownOptions)
        : renderContentWithCodeBlocks(text, theme, reasoningTrailMarkdownOptions);
    if (!extend) trailState.current = createIncrementalRenderState();
    rendered.current = nodes;
    return nodes;
}

/** One reasoning node in a coding turn. The newest thought of the current turn stays open. */
export const CodingAgentThinkingTimelineItem = React.memo(function CodingAgentThinkingTimelineItem({
    item,
    theme: t,
    lang,
    liveLabel,
    liveObject,
    expanded = false,
    durationMs,
}: {
    item: CodingAgentTimelineItem;
    theme: Theme;
    lang: string;
    /** Kept so callers can still pass the ordinal. The flat row does not print it. */
    step?: number;
    liveLabel?: string;
    liveObject?: string;
    /** Open for the newest thought of the current turn. Earlier thoughts stay folded. */
    expanded?: boolean;
    /** Milliseconds until the next timeline row. Under one second is omitted. */
    durationMs?: number;
}) {
    const trailCleanRef = React.useRef(createReasoningTrailCleanCache());
    // The audit cleaner cuts a thought at ### T and would drop prose that
    // shared a row with the step banner. Remove those lines first.
    const readable = React.useMemo(() => stripCodingThoughtChrome(item.content || ""), [item.content]);
    const displayReasoning = React.useMemo(
        () => rememberReasoningTrailClean(trailCleanRef.current, item.id, readable),
        [readable, item.id],
    );
    const live = !!liveLabel;
    const previousTextRef = React.useRef("");
    const renderedRef = React.useRef<React.ReactNode>(null);
    const trailStateRef = React.useRef(createIncrementalRenderState());
    const blank = codingThoughtTextIsBlank(item.content);
    const body = React.useMemo(
        () => !blank && displayReasoning.trim()
            ? renderGrowingThought(displayReasoning.trim(), t, expanded, previousTextRef, renderedRef, trailStateRef)
            : null,
        [blank, displayReasoning, expanded, t],
    );
    const [isOpen, setIsOpen] = React.useState(expanded);
    const { bodyRef, contentRef, handleScroll, handleUserScrollIntent } = useNestedPinnedScroll(isOpen, displayReasoning);
    React.useLayoutEffect(() => {
        setIsOpen(expanded);
    }, [expanded]);
    // A settled rule or step banner is not a thought. A live label still needs
    // its header while the model has not written anything yet.
    if (!live && (blank || !displayReasoning.trim())) return null;
    const label = liveLabel || localizeText(lang, "Thought", "思考过程", "思考過程");
    const objectText = live ? String(liveObject || "").trim() : "";
    const durationText = !live && typeof durationMs === "number" && durationMs >= 1000
        ? formatCodingAgentDuration(durationMs)
        : undefined;
    const hasBody = body != null;
    const summary = (
        <>
            <CodingAgentProcessIcon kind="think" className="caps-process-icon" />
            <span className="caps-process-body">
                <span
                    className={live ? "assistant-reasoning-live-label caps-process-verb" : "caps-process-verb"}
                    data-testid="assistant-reasoning-label"
                >
                    {label}
                </span>
                {objectText ? (
                    <span data-testid="assistant-reasoning-object" className="arp-object" title={objectText}>
                        {objectText}
                    </span>
                ) : null}
                {durationText ? <span className="caps-process-count">{`· ${durationText}`}</span> : null}
            </span>
            <span className="caps-process-trail">
                {live ? <span className="caps-process-live-dot assistant-reasoning-live-dot" data-testid="coding-agent-process-live-dot" /> : null}
                {hasBody ? (
                    <span className="caps-process-chevron" data-testid="coding-agent-process-chevron">
                        <CodingAgentProcessChevron />
                    </span>
                ) : null}
            </span>
        </>
    );
    const attrs = {
        className: "caps-process-thought",
        "data-testid": "coding-agent-thought",
        "data-live": live ? "true" : "false",
        "data-process-kind": "think" as const,
        "aria-label": objectText ? `${label} ${objectText}` : label,
    };
    // The shared reasoning summary paints its own leading chevron. This row
    // already has the process chevron, so that mark would shift the bulb icon.
    const summaryClass = "caps-process-row assistant-reasoning-summary assistant-reasoning-summary--plain";
    if (!hasBody) {
        return (
            <div {...attrs} role="status">
                <div className={summaryClass}>{summary}</div>
            </div>
        );
    }
    return (
        <details
            {...attrs}
            open={isOpen}
            onToggle={(event) => setIsOpen(event.currentTarget.open)}
        >
            <summary className={summaryClass}>
                {summary}
            </summary>
            <div
                ref={bodyRef}
                className="assistant-reasoning-body caps-process-thought-body"
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
            >
                <div ref={contentRef}>{body}</div>
            </div>
        </details>
    );
});

export function renderCodingAgentThinkingTimelineItem(
    item: CodingAgentTimelineItem,
    t: Theme,
    lang: string,
    step?: number,
    liveLabel?: string,
    liveObject?: string,
    expanded = false,
    durationMs?: number,
): React.ReactNode {
    return (
        <CodingAgentThinkingTimelineItem
            item={item}
            theme={t}
            lang={lang}
            step={step}
            liveLabel={liveLabel}
            liveObject={liveObject}
            expanded={expanded}
            durationMs={durationMs}
        />
    );
}

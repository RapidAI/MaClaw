import React from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { cleanReasoningTrailForBody } from "./assistantReasoningBody";
import { AssistantReasoningPanel } from "./AssistantReasoningPanel";
import { createIncrementalRenderState, renderContentIncremental, type IncrementalRenderState } from "./IncrementalMarkdownRenderer";
import { reasoningTrailMarkdownOptions, renderContentWithCodeBlocks } from "./aiAssistantMarkdown";
import type { CodingAgentTimelineItem } from "./useAIAssistant";

/** Long enough that a full re-parse on every token stalls the open thought. */
const INCREMENTAL_TRAIL_CHARS = 2000;

function reasoningPreviewText(text: string, maxLength = 96): string | undefined {
    const compact = text.replace(/\s+/gu, " ").trim();
    if (compact.length <= 48) return undefined;
    return compact.length > maxLength ? `${compact.slice(0, maxLength - 1).trimEnd()}…` : compact;
}

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
        && text.length >= INCREMENTAL_TRAIL_CHARS
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
    step,
    liveLabel,
    liveObject,
    expanded = false,
}: {
    item: CodingAgentTimelineItem;
    theme: Theme;
    lang: string;
    step?: number;
    liveLabel?: string;
    liveObject?: string;
    /** Open for the newest thought of the current turn. Earlier thoughts stay folded. */
    expanded?: boolean;
}) {
    const displayReasoning = React.useMemo(() => cleanReasoningTrailForBody(item.content || ""), [item.content]);
    const live = !!liveLabel;
    const previousTextRef = React.useRef("");
    const renderedRef = React.useRef<React.ReactNode>(null);
    const trailStateRef = React.useRef(createIncrementalRenderState());
    const body = React.useMemo(
        () => displayReasoning.trim()
            ? renderGrowingThought(displayReasoning.trim(), t, expanded, previousTextRef, renderedRef, trailStateRef)
            : null,
        [displayReasoning, expanded, t],
    );
    if (!displayReasoning.trim() && !live) return null;
    const preview = live ? undefined : reasoningPreviewText(displayReasoning);
    return (
        <AssistantReasoningPanel
            defaultOpen={expanded}
            label={liveLabel || localizeText(lang, "Thought", "思考过程", "思考過程")}
            objectLabel={live ? liveObject : undefined}
            step={step}
            lang={lang}
            preview={preview}
            theme={t}
            contentKey={displayReasoning}
            live={live}
        >
            {body}
        </AssistantReasoningPanel>
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
        />
    );
}

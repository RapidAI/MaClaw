import { codingThoughtTextIsBlank } from "./assistantLiveActivity";
import { normalizeAssistantSessionKey } from "./aiAssistantPanelSessionUtils";
import { openCodingAgentToolProgress, parseCodingAgentProgress, type CodingAgentProgress } from "./CodingAgentProgressStatus";
import type { ChatMessage, CodingAgentTimelineItem } from "./useAIAssistant";

const pendingDisplayCache = new WeakMap<CodingAgentTimelineItem[], CodingAgentTimelineItem[]>();

/**
 * Timeline the coding transcript can paint.
 * A goal stage streams thoughts into pendingCodingThoughts until the first
 * tool event. Those thoughts are the stage detail, so they render as rows
 * before a codingTimeline exists. A list that is already all visible thoughts
 * is returned as itself, so a token flush does not allocate a new row list.
 */
export function codingTimelineForDisplay(
    message: Pick<ChatMessage, "codingTimeline" | "pendingCodingThoughts"> | null | undefined,
): CodingAgentTimelineItem[] | undefined {
    const timeline = message?.codingTimeline;
    if (timeline?.length) return timeline;
    const pending = message?.pendingCodingThoughts;
    if (!pending?.length) return undefined;
    let needsFilter = false;
    for (const item of pending) {
        if (item?.kind !== "thinking" || codingThoughtTextIsBlank(item.content)) {
            needsFilter = true;
            break;
        }
    }
    if (!needsFilter) return pending;
    const cached = pendingDisplayCache.get(pending);
    if (cached) return cached;
    const visible = pending.filter((item) => item?.kind === "thinking" && !codingThoughtTextIsBlank(item.content));
    if (visible.length === 0) return undefined;
    pendingDisplayCache.set(pending, visible);
    return visible;
}

/**
 * The tool_started that a later tool_finished has not closed.
 * The progress feed can already show the next model request while this
 * tool is still the open row on the stage timeline.
 */
export function latestOpenCodingTool(
    timeline: ReadonlyArray<Pick<CodingAgentTimelineItem, "kind" | "content">> | null | undefined,
): CodingAgentProgress | null {
    if (!timeline?.length) return null;
    let open: CodingAgentProgress | null = null;
    for (const item of timeline) {
        if (item?.kind !== "progress") continue;
        const progress = parseCodingAgentProgress(item.content || "");
        if (!progress) continue;
        const event = (progress.event || "").trim().toLowerCase();
        if (event === "tool_started") {
            open = progress;
            continue;
        }
        if (event === "tool_finished") open = null;
    }
    return open;
}

/**
 * Progress rows that can name this stage's header.
 * The feed is shared by every running task. A row with a session key stays
 * when it matches the open tab or the stage assistant. An empty owner stays.
 */
export function progressMessagesForStage<T extends { sessionKey?: string | null }>(
    messages: readonly T[] | null | undefined,
    displayMessages: readonly { role?: string; sessionKey?: string | null }[] | null | undefined,
    activeSessionKey?: string | null,
): T[] {
    const rows = Array.isArray(messages) ? messages : [];
    let stageKey = "";
    const shown = displayMessages || [];
    for (let index = shown.length - 1; index >= 0; index -= 1) {
        if (shown[index]?.role === "assistant") {
            stageKey = shown[index]?.sessionKey || "";
            break;
        }
    }
    const active = normalizeAssistantSessionKey(activeSessionKey);
    const stage = normalizeAssistantSessionKey(stageKey);
    return rows.filter((message) => {
        const owner = normalizeAssistantSessionKey(message?.sessionKey);
        return !owner || owner === active || (!!stage && owner === stage);
    });
}

/**
 * Header progress for the stage on screen.
 * An in-flight feed tool is the newest status for this stage, even when the
 * timeline still shows the previous command. Once that tool finishes, an
 * unclosed timeline row keeps the command on the header.
 */
export function codingLiveHeaderProgress(
    feed: CodingAgentProgress | null | undefined,
    timeline: ReadonlyArray<Pick<CodingAgentTimelineItem, "kind" | "content">> | null | undefined,
    inFlight?: { tool?: string; toolOutcome?: string } | null,
    progressMessages?: Array<{ content?: string }> | null,
): CodingAgentProgress | null {
    const openTool = latestOpenCodingTool(timeline);
    const flight = inFlight?.tool && !inFlight.toolOutcome ? inFlight.tool : "";
    if (flight) {
        const feedEvent = (feed?.event || "").trim().toLowerCase();
        if (feed && feedEvent === "tool_started" && (feed.detail || "") === flight) return feed;
        const opened = openCodingAgentToolProgress(progressMessages || undefined, flight);
        if (opened) return opened;
        return { phase: "running", title: "", event: "tool_started", detail: flight };
    }
    if ((feed?.event || "").trim().toLowerCase() === "tool_started" && !!feed && (!openTool || (openTool.detail || "") === (feed.detail || ""))) {
        return feed;
    }
    return openTool || feed || null;
}

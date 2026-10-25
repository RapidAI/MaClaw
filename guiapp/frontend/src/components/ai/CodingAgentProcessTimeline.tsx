import React, { Fragment } from "react";
import { localizeText } from "../../i18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { codingThoughtTextIsBlank } from "./assistantLiveActivity";
import { CodingAgentThinkingTimelineItem } from "./CodingAgentThinkingTimelineItem";
import { CodingAgentProcessChevron } from "./CodingAgentProcessIcon";
import {
    CodingAgentTimelineProgressItem,
    codingAgentProcessSummaryText,
    codingProcessDiffCheckDisplay,
    codingProcessTimelineVisibility,
    type CodingAgentProcessSummaryItem,
    type CodingProcessTimelineVisibility,
} from "./CodingAgentProgressStatus";
import type { CodingAgentTimelineItem } from "./useAIAssistant";

function rowStartMs(item: CodingAgentTimelineItem | undefined): number {
    const started = item?.startedAt || 0;
    if (started > 0) return started;
    return item?.timestamp || 0;
}

function thoughtDurationMs(
    items: CodingAgentTimelineItem[],
    index: number,
    hidden: ReadonlySet<number>,
): number | undefined {
    const start = rowStartMs(items[index]);
    if (start <= 0) return undefined;
    for (let next = index + 1; next < items.length; next++) {
        if (hidden.has(next)) continue;
        const item = items[next];
        if (item?.kind === "thinking" && codingThoughtTextIsBlank(item.content)) continue;
        const stamp = rowStartMs(item);
        if (stamp > start) return stamp - start;
    }
    return undefined;
}

const EMPTY_TIMELINE_VISIBILITY: CodingProcessTimelineVisibility = {
    hidden: new Set<number>(),
    notes: new Map<number, string>(),
};

/**
 * A thought token must not reparse tool payloads. Visibility only changes
 * when a progress row's identity or content changes.
 */
function useCodingProcessTimelineVisibility(items: CodingAgentTimelineItem[]): CodingProcessTimelineVisibility {
    const cache = React.useRef<{ items: CodingAgentTimelineItem[]; visibility: CodingProcessTimelineVisibility } | null>(null);
    const cached = cache.current;
    if (items.length === 0) return EMPTY_TIMELINE_VISIBILITY;
    if (cached && sameSummaryInputs(cached.items, items)) return cached.visibility;
    const visibility = codingProcessTimelineVisibility(items);
    cache.current = { items, visibility };
    return visibility;
}

const SUMMARY_DURATION = /^(?:\d+(?:\.\d+)?ms|\d+(?:\.\d+)?s|\d+m(?: \d+s)?)$/;

/** Keep the elapsed time outside the ellipsis. The joined string stays the same. */
function splitProcessSummaryDuration(summary: string): { label: string; duration?: string } {
    if (SUMMARY_DURATION.test(summary)) return { label: "", duration: summary };
    const mark = " · ";
    const at = summary.lastIndexOf(mark);
    if (at < 0) return { label: summary };
    const tail = summary.slice(at + mark.length);
    if (!SUMMARY_DURATION.test(tail)) return { label: summary };
    return { label: summary.slice(0, at), duration: tail };
}

/**
 * True when a thought token does not change the summary. Progress rows are
 * compared by identity, so their payloads are not copied or parsed again.
 */
function sameSummaryInputs(prev: CodingAgentTimelineItem[], next: CodingAgentTimelineItem[]): boolean {
    if (prev.length !== next.length) return false;
    for (let index = 0; index < next.length; index++) {
        const before = prev[index];
        const after = next[index];
        if (before === after) continue;
        if (!before || before.kind !== after.kind || before.id !== after.id) return false;
        if (after.kind === "thinking") {
            if (codingThoughtTextIsBlank(before.content) !== codingThoughtTextIsBlank(after.content)) return false;
            continue;
        }
        if (before.content !== after.content || before.timestamp !== after.timestamp || before.startedAt !== after.startedAt) return false;
    }
    return true;
}

/**
 * The clock lives here so a one-second tick does not re-render the rows.
 * The list above owns whether the rows are open.
 */
function CodingAgentProcessSummaryBar({
    items,
    hiddenDiffUpdates,
    lang,
    live,
    thoughtLive,
    open,
    onToggle,
}: {
    items: CodingAgentTimelineItem[];
    hiddenDiffUpdates: ReadonlySet<number>;
    lang: string;
    live: boolean;
    /** False once the latest thought is no longer the live activity. */
    thoughtLive: boolean;
    open: boolean;
    onToggle: () => void;
}): React.ReactElement | null {
    const [nowMs, setNowMs] = React.useState(() => (live ? Date.now() : 0));
    React.useEffect(() => {
        if (!live) return undefined;
        const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, [live]);
    const summaryCache = React.useRef<{ items: CodingAgentTimelineItem[]; lang: string; live: boolean; thoughtLive: boolean; second: number; summary: string } | null>(null);
    const second = live ? Math.floor(nowMs / 1000) : 0;
    const cached = summaryCache.current;
    let summary: string;
    if (cached && cached.lang === lang && cached.live === live && cached.thoughtLive === thoughtLive && cached.second === second && sameSummaryInputs(cached.items, items)) {
        summary = cached.summary;
    } else {
        const visible: CodingAgentProcessSummaryItem[] = [];
        for (let index = 0; index < items.length; index++) {
            if (hiddenDiffUpdates.has(index)) continue;
            const item = items[index];
            if (item.kind === "thinking" && codingThoughtTextIsBlank(item.content)) continue;
            visible.push({
                kind: item.kind,
                content: item.kind === "progress" ? item.content : "",
                timestamp: item.timestamp,
                startedAt: item.startedAt,
            });
        }
        summary = codingAgentProcessSummaryText(visible, lang, {
            live,
            thoughtLive,
            nowMs: live ? nowMs || Date.now() : undefined,
        });
        summaryCache.current = { items, lang, live, thoughtLive, second, summary };
    }
    if (!summary) return null;
    const summaryParts = splitProcessSummaryDuration(summary);
    return (
        <button
            type="button"
            className="caps-process-summary"
            data-testid="coding-agent-process-summary"
            aria-expanded={open}
            onClick={onToggle}
        >
            <span className="caps-process-summary-main">
                {summaryParts.label ? <span className="caps-process-summary-text">{summaryParts.label}</span> : null}
                {summaryParts.duration ? <span className="caps-process-summary-time">{`${summaryParts.label ? " · " : ""}${summaryParts.duration}`}</span> : null}
            </span>
            <span className="caps-process-summary-chevron" aria-hidden="true">
                <CodingAgentProcessChevron />
            </span>
        </button>
    );
}

/**
 * Flat coding-process list: a collapsible count line, then thought and tool rows.
 * The list starts open. Collapse state stays in this component.
 */
export function CodingAgentProcessTimeline({
    items,
    theme,
    lang,
    lastThinkingIndex,
    openThoughtIndex,
    liveLabel,
    liveObject,
    live = false,
}: {
    items: CodingAgentTimelineItem[];
    theme: Theme;
    lang: string;
    lastThinkingIndex: number;
    openThoughtIndex: number;
    liveLabel?: string;
    liveObject?: string;
    /** True while this assistant turn is still in flight. */
    live?: boolean;
}): React.ReactElement {
    const [open, setOpen] = React.useState(true);
    const visibility = useCodingProcessTimelineVisibility(items);
    // The sheen string is the thinking label only while this thought is the
    // current activity. "正在调用工具" and a dropped label are not thinking.
    const thoughtLive = lastThinkingIndex >= 0
        && liveLabel === localizeText(lang, "Thinking", "正在思考", "正在思考");
    return (
        <div data-testid="coding-agent-interleaved-timeline" data-process-open={open ? "true" : "false"}>
            <CodingAgentProcessSummaryBar
                items={items}
                hiddenDiffUpdates={visibility.hidden}
                lang={lang}
                live={live}
                thoughtLive={thoughtLive}
                open={open}
                onToggle={() => setOpen((value) => !value)}
            />
            {open ? items.map((item, index) => {
                if (visibility.hidden.has(index)) return null;
                const checkNote = visibility.notes.get(index);
                if (checkNote) {
                    return (
                        <div key={item.id} className="caps-process-note-line caps-process-note-line--failed" data-testid="coding-agent-diff-check">
                            {codingProcessDiffCheckDisplay(checkNote, lang)}
                        </div>
                    );
                }
                if (item.kind === "thinking" && codingThoughtTextIsBlank(item.content)) return null;
                const thoughtOwnsLive = index === lastThinkingIndex;
                return (
                    <Fragment key={item.id}>
                        {item.kind === "thinking"
                            ? (
                                <CodingAgentThinkingTimelineItem
                                    item={item}
                                    theme={theme}
                                    lang={lang}
                                    liveLabel={thoughtOwnsLive ? liveLabel : undefined}
                                    liveObject={thoughtOwnsLive ? liveObject : undefined}
                                    expanded={index === openThoughtIndex}
                                    durationMs={thoughtDurationMs(items, index, visibility.hidden)}
                                />
                            )
                            : (
                                <CodingAgentTimelineProgressItem
                                    id={item.id}
                                    content={item.content}
                                    timestamp={item.timestamp}
                                    theme={theme}
                                    lang={lang}
                                />
                            )}
                    </Fragment>
                );
            }) : null}
        </div>
    );
}

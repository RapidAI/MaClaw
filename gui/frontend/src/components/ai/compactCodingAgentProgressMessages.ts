import type { ChatMessage } from "./useAIAssistant";
import { isCodingAgentBoardProgressContent } from "./codingAgentUserFinish";
import {
    codingAgentFeedStableKey,
    codingAgentProgressLooksCritical,
    isCodingAgentActivityEvent,
    isCodingAgentChatHiddenEvent,
    isCodingAgentProgressContent,
    isCodingAgentTaskStatusOnly,
    isCodingAgentTerminalPhase,
    parseCodingAgentProgress,
    type CodingAgentProgress,
} from "./CodingAgentProgressStatus";

/** Keep a Codex-like tool trail; the chat feed scrolls or expands past this. */
const MAX_RECENT_ACTIVITY_EVENTS = 20;

type ParsedRow = {
    msg: ChatMessage;
    progress: CodingAgentProgress | null;
};

/**
 * Compact coding-agent progress for chat display:
 * 1) keep only the recent tool trail for the latest turn
 * 2) coalesce start→finish and drop status noise
 * 3) re-cap every visible activity line after coalesce
 */
export function compactCodingAgentProgressMessages(messages: ChatMessage[]): ChatMessage[] {
    const hasCodingTrail = messages.some((msg) => msg.role === "progress" && isCodingAgentProgressContent(msg.content || ""));
    if (hasCodingTrail) {
        messages = messages.filter((msg) => msg.role !== "progress" || !isCodingAgentBoardProgressContent(msg.content || ""));
    }
    // Parse once per message for this pipeline.
    const rows: ParsedRow[] = messages.map((msg) => ({
        msg,
        progress: msg.role === "progress" ? parseCodingAgentProgress(msg.content || "") : null,
    }));

    let latestCodingIndex = -1;
    for (let i = rows.length - 1; i >= 0; i--) {
        const progress = rows[i].progress;
        if (progress && !isCodingAgentChatHiddenEvent(progress)) {
            latestCodingIndex = i;
            break;
        }
    }
    if (latestCodingIndex < 0) {
        return messages.filter((msg, index) => {
            const progress = rows[index].progress;
            return !progress || !isCodingAgentChatHiddenEvent(progress);
        });
    }

    const latest = rows[latestCodingIndex].progress!;
    // Soft pre-filter: keep a generous window of raw tool events (starts+finishes),
    // then coalesce, then hard-cap finished/started lines.
    const recentToolIndices: number[] = [];
    for (let i = 0; i < rows.length; i++) {
        const progress = rows[i].progress;
        if (!progress || !sameCodingProgressTurn(progress, latest)) continue;
        const event = (progress.event || "").trim().toLowerCase();
        if (event === "tool_finished" || event === "tool_started") {
            recentToolIndices.push(i);
        }
    }
    // 2× window so paired starts don't squeeze out finished tools before coalesce.
    const keepTools = new Set(recentToolIndices.slice(-(MAX_RECENT_ACTIVITY_EVENTS * 2)));

    const filtered: ParsedRow[] = [];
    for (let index = 0; index < rows.length; index++) {
        if (index === latestCodingIndex) {
            filtered.push(rows[index]);
            continue;
        }
        const progress = rows[index].progress;
        if (!progress) {
            filtered.push(rows[index]); // ordinary chat / non-coding progress
            continue;
        }
        if (isCodingAgentChatHiddenEvent(progress)) {
            continue;
        }
        if (keepTools.has(index) || shouldPreserveCodingProgress(progress, latest)) {
            filtered.push(rows[index]);
        }
    }

    const coalesced = coalesceCodingAgentToolLifecycleRows(filtered);
    return capRecentActivityEvents(coalesced, latest).map((row) => row.msg);
}

/**
 * Make the activity feed denser and more tool-like:
 * - drop tool_started once the matching tool_finished exists (LIFO per tool)
 * - drop intermediate (non-terminal) task_status when tools/summaries already tell the story
 * - always keep terminal task_status (completed/failed/…) so header phase stays accurate
 */
export function coalesceCodingAgentToolLifecycle(messages: ChatMessage[]): ChatMessage[] {
    const rows: ParsedRow[] = messages.map((msg) => ({
        msg,
        progress: msg.role === "progress" ? parseCodingAgentProgress(msg.content || "") : null,
    }));
    return coalesceCodingAgentToolLifecycleRows(rows).map((row) => row.msg);
}

function coalesceCodingAgentToolLifecycleRows(rows: ParsedRow[]): ParsedRow[] {
    if (rows.length === 0) return rows;
    const drop = new Set<number>();

    // 1) LIFO: each tool_finished closes the nearest open tool_started (same tool + turn).
    const openStarts = new Map<string, number[]>();
    for (let i = 0; i < rows.length; i++) {
        const progress = rows[i].progress;
        if (!progress) continue;
        const event = (progress.event || "").trim().toLowerCase();
        const tool = (progress.detail || "").trim();
        if (!tool || (event !== "tool_started" && event !== "tool_finished")) continue;
        const key = `${codingProgressTurnKey(progress)}\0${tool}`;
        if (event === "tool_started") {
            const stack = openStarts.get(key);
            if (stack) stack.push(i);
            else openStarts.set(key, [i]);
            continue;
        }
        const stack = openStarts.get(key);
        const startIdx = stack?.pop();
        if (startIdx !== undefined) drop.add(startIdx);
    }

    // 2) Collapse task_status noise in one pass:
    //    - drop non-terminal status when the turn already has tool/summary activity
    //    - keep only the latest non-terminal status per turn
    //    - keep only the latest terminal status per (turn, phase)
    const turnHasActivity = new Set<string>();
    const lastNonTerminalStatusByTurn = new Map<string, number>();
    const lastTerminalByTurnPhase = new Map<string, number>();
    for (let i = 0; i < rows.length; i++) {
        if (drop.has(i)) continue;
        const progress = rows[i].progress;
        if (!progress) continue;
        const turnKey = codingProgressTurnKey(progress);
        if (!isCodingAgentTaskStatusOnly(progress)) {
            if (isCodingAgentActivityEvent(progress)) turnHasActivity.add(turnKey);
            continue;
        }
        if (isCodingAgentTerminalPhase(progress.phase)) {
            lastTerminalByTurnPhase.set(`${turnKey}\0${progress.phase}`, i);
        } else {
            lastNonTerminalStatusByTurn.set(turnKey, i);
        }
    }
    for (let i = 0; i < rows.length; i++) {
        if (drop.has(i)) continue;
        const progress = rows[i].progress;
        if (!progress || !isCodingAgentTaskStatusOnly(progress)) continue;
        const turnKey = codingProgressTurnKey(progress);
        if (isCodingAgentTerminalPhase(progress.phase)) {
            const keep = lastTerminalByTurnPhase.get(`${turnKey}\0${progress.phase}`);
            if (keep !== undefined && keep !== i) drop.add(i);
            continue;
        }
        // Non-terminal: drop when activity exists, else keep only latest.
        if (turnHasActivity.has(turnKey)) {
            drop.add(i);
            continue;
        }
        const keep = lastNonTerminalStatusByTurn.get(turnKey);
        if (keep !== undefined && keep !== i) drop.add(i);
    }

    // 3) Codex: one Edit/Write line per file. Drop diff_updated / diff_summary
    //    when a write/edit tool_finished in the same turn already carries that path.
    const editedFilesByTurn = new Map<string, Set<string>>();
    for (let i = 0; i < rows.length; i++) {
        if (drop.has(i)) continue;
        const progress = rows[i].progress;
        if (!progress) continue;
        const event = (progress.event || "").trim().toLowerCase();
        if (event !== "tool_finished") continue;
        const tool = (progress.detail || "").toLowerCase().replace(/^ssh_/, "");
        if (
            tool !== "write_file" &&
            tool !== "edit_file" &&
            tool !== "edit_lines" &&
            tool !== "apply_patch" &&
            tool !== "str_replace"
        ) {
            continue;
        }
        const turnKey = codingProgressTurnKey(progress);
        const files = new Set(editedFilesByTurn.get(turnKey) || []);
        for (const path of progress.files || []) {
            if (path) files.add(path.replace(/\\/g, "/"));
        }
        editedFilesByTurn.set(turnKey, files);
    }
    for (let i = 0; i < rows.length; i++) {
        if (drop.has(i)) continue;
        const progress = rows[i].progress;
        if (!progress) continue;
        const event = (progress.event || "").trim().toLowerCase();
        if (event !== "diff_updated" && event !== "diff_summary") continue;
        const files = editedFilesByTurn.get(codingProgressTurnKey(progress));
        if (!files || files.size === 0) continue;
        const updated = [
            ...(progress.files || []),
            ...(progress.fileChanges || []).map((row) => row.path),
        ].map((path) => path.replace(/\\/g, "/")).filter(Boolean);
        if (updated.length === 0 || updated.some((path) => files.has(path))) {
            drop.add(i);
        }
    }

    if (drop.size === 0) return rows;
    return rows.filter((_, index) => !drop.has(index));
}

/**
 * After coalescing, keep at most MAX_RECENT_ACTIVITY_EVENTS visible activity rows
 * for the latest turn. Terminal task status remains so the feed header is accurate.
 */
function capRecentActivityEvents(rows: ParsedRow[], latest: CodingAgentProgress): ParsedRow[] {
    const activityIndices: number[] = [];
    for (let i = 0; i < rows.length; i++) {
        const progress = rows[i].progress;
        if (!progress || !sameCodingProgressTurn(progress, latest)) continue;
        if (!isCodingAgentTaskStatusOnly(progress)) activityIndices.push(i);
    }
    if (activityIndices.length <= MAX_RECENT_ACTIVITY_EVENTS) return rows;

    const keepActivity = new Set(activityIndices.slice(-MAX_RECENT_ACTIVITY_EVENTS));
    return rows.filter((row, index) => {
        if (!row.progress) return true;
        if (!sameCodingProgressTurn(row.progress, latest)) return true;
        if (isCodingAgentTaskStatusOnly(row.progress)) return true;
        return keepActivity.has(index);
    });
}

function codingProgressTurnKey(progress: CodingAgentProgress): string {
    if (progress.turnID) return `turn:${progress.turnID}`;
    if (progress.runID && progress.taskID) return `run:${progress.runID}|task:${progress.taskID}`;
    if (progress.taskID) return `task:${progress.taskID}`;
    return "turn:default";
}

function shouldPreserveCodingProgress(progress: CodingAgentProgress | null, latest: CodingAgentProgress | null): boolean {
    if (!progress || !latest || !sameCodingProgressTurn(progress, latest)) return false;
    // Terminal task status must survive compaction even when not the latest message.
    if (isCodingAgentTaskStatusOnly(progress) && isCodingAgentTerminalPhase(progress.phase)) return true;
    const event = (progress.event || "").trim().toLowerCase();
    const outcome = (progress.outcome || "").trim().toLowerCase();
    switch (event) {
        case "command_summary":
        case "diff_check":
            return outcome === "failed";
        case "tool_finished":
            // Keep true failures/blocks for the trail; drop diagnostic probes from the soft preserve path
            // (they still appear inside the recent tool window when fresh).
            return codingAgentProgressLooksCritical(progress);
        case "assistant_note":
            return true;
        default:
            return false;
    }
}

/**
 * Match events to the latest coding turn.
 * Prefer turn_id, then run+task, then task. An unscoped latest must not pull in
 * scoped historical turns (that used to revive every prior turn in the feed).
 */
function sameCodingProgressTurn(a: CodingAgentProgress, b: CodingAgentProgress): boolean {
    if (b.turnID) return a.turnID === b.turnID;
    if (b.runID && b.taskID) return a.runID === b.runID && a.taskID === b.taskID;
    if (b.taskID) return a.taskID === b.taskID;
    // Latest has no turn/task/run: only match similarly unscoped peers (legacy stream).
    return !a.turnID && !a.taskID && !a.runID;
}

/** Group consecutive coding-agent progress messages for feed-style rendering. */
export type CodingProgressRenderItem =
    | { kind: "message"; message: ChatMessage }
    | { kind: "coding-feed"; messages: ChatMessage[]; key: string };

/**
 * Consecutive coding progress → one feed panel (including a single row).
 * Uses a cheap content check so we don't JSON-parse every progress line twice.
 */
export function groupCodingAgentProgressForRender(messages: ChatMessage[]): CodingProgressRenderItem[] {
    const items: CodingProgressRenderItem[] = [];
    let feed: ChatMessage[] = [];
    const flushFeed = () => {
        if (feed.length === 0) return;
        items.push({
            kind: "coding-feed",
            messages: feed.slice(),
            // Stable across streaming tool lines (turn/task), no full JSON parse.
            key: codingAgentFeedStableKey(feed),
        });
        feed = [];
    };
    for (const msg of messages) {
        const isCoding = msg.role === "progress" && isCodingAgentProgressContent(msg.content || "");
        if (isCoding) {
            feed.push(msg);
            continue;
        }
        flushFeed();
        items.push({ kind: "message", message: msg });
    }
    flushFeed();
    return items;
}

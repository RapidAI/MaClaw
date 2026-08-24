import React from "react";
import type { ChatMessage } from "./useAIAssistant";

interface CodingAgentProgressTheme {
    text: string;
    fieldLabel: string;
    isDark?: boolean;
}

/** Clicking a Read/Edit/Write path focuses the existing code preview. */
export const CodingAgentPreviewFocusContext = React.createContext<((path: string) => void) | undefined>(undefined);

export const CODING_AGENT_KNOWN_PHASES = ["starting", "running", "completed", "failed", "retrying", "skipped", "result"] as const;
export type CodingAgentKnownPhase = (typeof CODING_AGENT_KNOWN_PHASES)[number];
export type CodingAgentStatusPhase = CodingAgentKnownPhase | "unknown";
export type CodingAgentStatusTone = { accent: string; bg: string; border: string };
export type CodingAgentToolOutcome = "success" | "failed" | "blocked" | "unknown";
export type CodingAgentGuardrailStatus = "blocked" | "unknown";
export type CodingAgentCommandStatus = "passed" | "failed" | "none" | "unknown";
export type CodingAgentFileActivityStatus = "changed" | "read_only" | "none" | "unknown";
export type CodingAgentQualityStatus = "passed" | "warning" | "failed" | "unknown";
export type CodingAgentExplorationStatus = "explored" | "read_only" | "missing" | "not_needed" | "unknown";
export type CodingAgentVerificationStatus = "passed" | "failed" | "missing" | "not_needed" | "unknown";
export type CodingAgentDiffCheckStatus = "checked" | "skipped" | "failed" | "unknown";

/** Shared palette — keep coding-agent chrome calm and consistent. */
const successTone: CodingAgentStatusTone = {
    accent: "#4f7f6f",
    bg: "rgba(79, 127, 111, 0.08)",
    border: "rgba(79, 127, 111, 0.22)",
};
const runningTone: CodingAgentStatusTone = {
    accent: "#2f6fbc",
    bg: "rgba(47, 111, 188, 0.08)",
    border: "rgba(47, 111, 188, 0.22)",
};
const slateTone: CodingAgentStatusTone = {
    accent: "#64748b",
    bg: "rgba(100, 116, 139, 0.08)",
    border: "rgba(100, 116, 139, 0.20)",
};

const neutralAttentionTone: CodingAgentStatusTone = slateTone;

const neutralAttentionToneDark: CodingAgentStatusTone = {
    accent: "#8a9ab0",
    bg: "rgba(138, 154, 176, 0.10)",
    border: "rgba(138, 154, 176, 0.22)",
};

/**
 * Soft amber for coding-agent failures / hard checks.
 * Prefer this over alarmist red so tool trail and status prompts stay calm
 * during normal exploratory / compile-fail loops.
 */
export const CODING_AGENT_FAILURE_ACCENT = "#a16207";
/** Lighter gold for dark UI — #a16207 is too dim on dark panels. */
export const CODING_AGENT_FAILURE_ACCENT_DARK = "#e0b253";
const codingAgentFailureTone: CodingAgentStatusTone = {
    accent: CODING_AGENT_FAILURE_ACCENT,
    bg: "rgba(161, 98, 7, 0.09)",
    border: "rgba(161, 98, 7, 0.22)",
};
const codingAgentFailureToneDark: CodingAgentStatusTone = {
    accent: CODING_AGENT_FAILURE_ACCENT_DARK,
    bg: "rgba(224, 178, 83, 0.12)",
    border: "rgba(224, 178, 83, 0.28)",
};

export interface CodingAgentProgress {
    phase: CodingAgentStatusPhase;
    taskID?: string;
    title: string;
    detail?: string;
    command?: string;
    outcome?: string;
    severity?: string;
    summary?: string;
    event?: string;
    runID?: string;
    turnID?: string;
    timestamp?: string;
    durationMs?: number;
    count?: number;
    files?: string[];
    added?: number;
    removed?: number;
    fileChanges?: CodingAgentFileChange[];
}

export interface CodingAgentFileChange {
    path: string;
    added: number;
    removed: number;
}

export interface CodingAgentToolTrace {
    name: string;
    outcome?: string;
    durationMs?: number;
    summary?: string;
}

export interface CodingAgentTurnSnapshot {
    latest: CodingAgentProgress;
    turnID?: string;
    runID?: string;
    taskID?: string;
    title: string;
    phase: CodingAgentStatusPhase;
    tool?: string;
    toolOutcome?: string;
    toolDurationMs?: number;
    tools?: CodingAgentToolTrace[];
    guardrailStatus?: string;
    guardrailSummary?: string;
    guardrailCount?: number;
    commandStatus?: string;
    commandSummary?: string;
    commandCount?: number;
    fileActivityStatus?: string;
    fileActivitySummary?: string;
    fileActivityCount?: number;
    fileActivityDetail?: string;
    qualityStatus?: string;
    qualitySummary?: string;
    qualityCount?: number;
    explorationStatus?: string;
    explorationSummary?: string;
    explorationCount?: number;
    verificationStatus?: string;
    verificationSummary?: string;
    verificationCount?: number;
    diffCheckStatus?: string;
    diffCheckSummary?: string;
    changeCount?: number;
    files?: string[];
    diffSummary?: string;
}

export type CodingAgentStatusVariant = "chat-progress" | "title-bar" | "sidebar" | "status-bar";

const CODING_AGENT_PHASE_LABELS: Record<CodingAgentStatusPhase, { en: string; zh: string }> = {
    starting: { en: "Starting", zh: "\u542f\u52a8\u4e2d" },
    running: { en: "Running", zh: "\u6267\u884c\u4e2d" },
    completed: { en: "Completed", zh: "\u5df2\u5b8c\u6210" },
    failed: { en: "Failed", zh: "\u5931\u8d25" },
    retrying: { en: "Retrying", zh: "\u91cd\u8bd5\u4e2d" },
    skipped: { en: "Skipped", zh: "\u5df2\u8df3\u8fc7" },
    result: { en: "Result", zh: "\u7ed3\u679c" },
    unknown: { en: "Status", zh: "\u72b6\u6001" },
};

const CODING_AGENT_PHASE_TONES: Record<CodingAgentStatusPhase, CodingAgentStatusTone> = {
    starting: runningTone,
    running: runningTone,
    completed: successTone,
    failed: codingAgentFailureTone,
    retrying: neutralAttentionTone,
    skipped: slateTone,
    result: successTone,
    unknown: runningTone,
};

export function isCodingAgentKnownPhase(phase: string): phase is CodingAgentKnownPhase {
    return (CODING_AGENT_KNOWN_PHASES as readonly string[]).includes(phase.toLowerCase());
}

export function normalizeCodingAgentPhase(phase: string): CodingAgentStatusPhase {
    const normalized = phase.toLowerCase();
    return isCodingAgentKnownPhase(normalized) ? normalized : "unknown";
}

export function normalizeCodingAgentTaskID(taskID?: string): string | undefined {
    const normalized = (taskID || "").trim().toUpperCase();
    return normalized || undefined;
}

export function normalizeCodingAgentTitle(title?: string): string {
    return (title || "").trim();
}

function normalizeCodingAgentOptionalText(value?: string): string | undefined {
    const normalized = normalizeCodingAgentTitle(value);
    return normalized || undefined;
}

export function normalizeCodingAgentProgress(progress: CodingAgentProgress): CodingAgentProgress {
    return {
        phase: normalizeCodingAgentPhase(progress.phase),
        taskID: normalizeCodingAgentTaskID(progress.taskID),
        title: normalizeCodingAgentTitle(progress.title),
        detail: normalizeCodingAgentOptionalText(progress.detail),
        command: normalizeCodingAgentOptionalText(progress.command),
        outcome: normalizeCodingAgentOptionalText(progress.outcome),
        severity: normalizeCodingAgentOptionalText(progress.severity),
        summary: normalizeCodingAgentOptionalText(progress.summary),
        event: normalizeCodingAgentOptionalText(progress.event),
        runID: normalizeCodingAgentOptionalText(progress.runID),
        turnID: normalizeCodingAgentOptionalText(progress.turnID),
        timestamp: normalizeCodingAgentOptionalText(progress.timestamp),
        durationMs: Number.isFinite(progress.durationMs) && progress.durationMs !== undefined && progress.durationMs >= 0 ? progress.durationMs : undefined,
        count: Number.isFinite(progress.count) && progress.count !== undefined && progress.count >= 0 ? progress.count : undefined,
        files: normalizeCodingAgentFiles(progress.files),
        added: Number.isFinite(progress.added) && progress.added !== undefined && progress.added >= 0 ? progress.added : undefined,
        removed: Number.isFinite(progress.removed) && progress.removed !== undefined && progress.removed >= 0 ? progress.removed : undefined,
        fileChanges: normalizeCodingAgentFileChanges(progress.fileChanges),
    };
}

function normalizeCodingAgentFiles(files?: string[]): string[] | undefined {
    if (!Array.isArray(files)) return undefined;
    const normalized = files.map((file) => normalizeCodingAgentTitle(file)).filter(Boolean);
    return normalized.length > 0 ? normalized : undefined;
}

function normalizeCodingAgentFileChanges(changes?: CodingAgentFileChange[]): CodingAgentFileChange[] | undefined {
    if (!Array.isArray(changes)) return undefined;
    const normalized: CodingAgentFileChange[] = [];
    for (const change of changes) {
        const path = normalizeCodingAgentTitle(change?.path);
        if (!path) continue;
        normalized.push({
            path,
            added: Number.isFinite(change.added) && change.added > 0 ? Math.round(change.added) : 0,
            removed: Number.isFinite(change.removed) && change.removed > 0 ? Math.round(change.removed) : 0,
        });
    }
    return normalized.length > 0 ? normalized : undefined;
}

export function codingAgentFileChangeRows(progress: CodingAgentProgress): CodingAgentFileChange[] {
    const normalized = normalizeCodingAgentProgress(progress);
    if (normalized.fileChanges?.length) return normalized.fileChanges;
    return (normalized.files || []).map((path) => ({ path, added: 0, removed: 0 }));
}

/** Paths a trail row can open in the code preview. */
export function codingAgentPreviewTargetPaths(progress: CodingAgentProgress): string[] {
    const seen = new Set<string>();
    const paths: string[] = [];
    for (const row of codingAgentFileChangeRows(progress)) {
        const path = (row.path || "").trim();
        if (!path || seen.has(path)) continue;
        seen.add(path);
        paths.push(path);
    }
    return paths;
}

export function isCodingAgentActivePhase(phase: string): boolean {
    return ["starting", "running", "retrying", "unknown"].includes(normalizeCodingAgentPhase(phase));
}

export function isCodingAgentTerminalPhase(phase: string): boolean {
    return ["completed", "failed", "skipped", "result"].includes(normalizeCodingAgentPhase(phase));
}

export function codingAgentStatusDataAttrs(progress: CodingAgentProgress, variant: CodingAgentStatusVariant): Record<string, string> {
    const normalized = normalizeCodingAgentProgress(progress);
    return {
        "data-agent": "coding",
        "data-active": isCodingAgentActivePhase(normalized.phase) ? "true" : "false",
        "data-event": normalized.event || "",
        "data-phase": normalized.phase,
        "data-run-id": normalized.runID || "",
        "data-terminal": isCodingAgentTerminalPhase(normalized.phase) ? "true" : "false",
        "data-task-id": normalized.taskID || "",
        "data-turn-id": normalized.turnID || "",
        "data-change-count": normalized.count !== undefined ? String(normalized.count) : "",
        "data-variant": variant,
    };
}

export function codingAgentStatusClassName(progress: CodingAgentProgress, variant: CodingAgentStatusVariant): string {
    const normalized = normalizeCodingAgentProgress(progress);
    return ["coding-agent-status", `coding-agent-status--${variant}`, `coding-agent-status--${normalized.phase}`].join(" ");
}

export function codingAgentStatusSelector(variant?: CodingAgentStatusVariant, phase?: CodingAgentStatusPhase, taskID?: string): string {
    const normalizedTaskID = normalizeCodingAgentTaskID(taskID);
    return [
        ".coding-agent-status",
        variant ? `[data-variant="${variant}"]` : "",
        phase ? `[data-phase="${phase}"]` : "",
        normalizedTaskID ? `[data-task-id="${normalizedTaskID}"]` : "",
    ].join("");
}

/**
 * Cheap content check for grouping (avoids full JSON parse on every progress line).
 * Intentionally stricter than a bare prefix so garbage lines don't enter a feed shell.
 */
export function isCodingAgentProgressContent(content: string): boolean {
    const trimmed = content.trimStart();
    if (trimmed.startsWith("Coding Agent Event:")) {
        // Require the coding agent marker near the start of the payload.
        const head = trimmed.slice(0, 240);
        return /"agent"\s*:\s*"coding"/i.test(head);
    }
    // Legacy: "Coding Agent: running T2 - title"
    return /^Coding Agent:\s*[a-z]+(?:\s+T\d+)?(?:\s+-|\s*$)/i.test(trimmed);
}

/**
 * Stable React key for a coding feed panel across streaming tool events.
 * Prefers turn_id / task_id so the shell does not remount on every new line.
 */
export function codingAgentFeedStableKey(messages: { id: string; content?: string }[]): string {
    for (let i = messages.length - 1; i >= 0; i--) {
        const content = messages[i]?.content || "";
        const turn = content.match(/"turn_id"\s*:\s*"([^"]+)"/i);
        if (turn?.[1]) return `feed-turn-${turn[1]}`;
        const task = content.match(/"task_id"\s*:\s*"([^"]+)"/i);
        if (task?.[1]) return `feed-task-${task[1]}`;
        // Legacy plain status: "... T2 - title"
        const legacy = content.match(/^Coding Agent:\s*[a-z]+\s+(T\d+)/i);
        if (legacy?.[1]) return `feed-task-${legacy[1]}`;
    }
    const lastId = messages[messages.length - 1]?.id || "coding";
    return `feed-${lastId}`;
}

/** Whether a progress row is a user-visible critical failure (for feed chrome). */
export function codingAgentProgressLooksCritical(progress: CodingAgentProgress): boolean {
    const outcome = (progress.outcome || "").trim().toLowerCase();
    if (outcome !== "failed" && outcome !== "blocked" && outcome !== "missing") return false;
    // Diagnostic / exploratory tool failures stay neutral in the trail.
    if ((progress.event || "").trim().toLowerCase() === "tool_finished") {
        if (codingAgentToolFailureLooksDiagnostic(progress) || codingAgentToolFailureLooksExpectedOrRecoverable(progress)) {
            return false;
        }
    }
    return true;
}

/**
 * Visual tone for the whole feed: prefer header phase, but elevate to failed
 * when the trail has real failures while the task is still marked running.
 */
export function resolveCodingAgentFeedTone(
    header: CodingAgentProgress,
    lineProgress: CodingAgentProgress[],
    isDark?: boolean,
): CodingAgentStatusTone {
    const base = resolveCodingAgentStatusTone(header, isDark);
    if (!isCodingAgentActivePhase(header.phase)) return base;
    if (!lineProgress.some(codingAgentProgressLooksCritical)) return base;
    return resolveCodingAgentStatusTone({ phase: "failed", title: header.title || "" }, isDark);
}

export function parseCodingAgentProgress(content: string): CodingAgentProgress | null {
    const event = parseCodingAgentEventProgress(content);
    if (event) return event;
    const match = content.trim().match(/^Coding Agent:\s*([a-z]+)(?:\s+(T\d+))?(?:\s+-\s*(.*))?$/i);
    if (!match) return null;
    return normalizeCodingAgentProgress({
        phase: normalizeCodingAgentPhase(match[1] || ""),
        taskID: match[2],
        title: match[3] || "",
    });
}

export function isCodingAgentTaskStatusOnly(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    return event === "" || event === "task_status";
}

export function isCodingAgentActivityEvent(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    return (
        event === "tool_started" ||
        event === "tool_finished" ||
        event === "assistant_note" ||
        event.endsWith("_summary") ||
        event === "diff_check" ||
        event === "diff_summary" ||
        event === "diff_updated"
    );
}

/** Codex trail items: tools, notes, file cards — not audit/status banners. */
export function isCodingAgentPlainTrailEvent(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    return (
        event === "tool_started" ||
        event === "tool_finished" ||
        event === "assistant_note" ||
        event === "diff_updated" ||
        event === "diff_summary"
    );
}

/** Audit banners stay on the result object / sidebar, not in the chat trail. */
export function isCodingAgentChatHiddenEvent(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    if (event === "assistant_note") {
        return codingAssistantNoteLooksInternal((progress.detail || progress.summary || "").trim());
    }
    return (
        event === "quality_summary" ||
        event === "exploration_summary" ||
        event === "verification_summary" ||
        event === "guardrail_summary" ||
        event === "file_activity_summary" ||
        event === "command_summary"
    );
}

/** True when the chat already has a Codex Read/Edit/$ (or note) line. */
export function codingAgentMessagesHavePlainTrail(messages: ChatMessage[]): boolean {
    return codingAgentLatestTurnHasPlainTrail(messages);
}

/** True when the latest coding turn already has a visible Read/Edit/$ (or note). */
export function codingAgentLatestTurnHasPlainTrail(messages: ChatMessage[]): boolean {
    const parsed: CodingAgentProgress[] = [];
    for (const msg of messages) {
        const progress = parseCodingAgentProgress(msg.content || "");
        if (progress) parsed.push(progress);
    }
    if (parsed.length === 0) return false;
    const latest = parsed[parsed.length - 1];
    for (const progress of parsed) {
        if (!codingAgentSameTurn(progress, latest)) continue;
        if (isCodingAgentPlainTrailEvent(progress) && !isCodingAgentChatHiddenEvent(progress)) {
            return true;
        }
    }
    return false;
}

/** Pre-tool wait: same mono trail as Read/Edit/$, not a chat "思考中" bubble. */
export function renderCodingAgentWorkingTrail(t: CodingAgentProgressTheme, lang: string): React.ReactNode {
    const accent = runningTone.accent;
    return (
        <div
            data-testid="coding-agent-working-trail"
            role="status"
            aria-live="off"
            aria-label={lang.startsWith("zh") ? "工作中" : "Working"}
            style={{
                display: "grid",
                gridTemplateColumns: "12px auto",
                alignItems: "baseline",
                columnGap: 6,
                margin: "2px 0",
                padding: 0,
                fontSize: 11,
                lineHeight: 1.4,
                color: t.text,
                opacity: 0.9,
            }}
        >
            <span
                aria-hidden="true"
                className="coding-agent-working-dot"
                style={{
                    color: accent,
                    fontWeight: 700,
                    fontFamily: monoFont,
                    textAlign: "center",
                    fontSize: 11,
                    width: 12,
                    animation: "blink 1.2s step-end infinite",
                }}
            >
                {"\u00b7"}
            </span>
            <span
                style={{
                    color: accent,
                    fontFamily: monoFont,
                    fontSize: 11,
                    fontWeight: 600,
                }}
            >
                Working
            </span>
        </div>
    );
}

/** Drop thinking dumps, tool JSON, and leftover audit headings from mid-turn notes. */
export function codingAssistantNoteLooksInternal(text: string): boolean {
    const trimmed = (text || "").trim();
    if (!trimmed) return true;
    if (trimmed.startsWith("##") || trimmed.includes("## ")) return true;
    const lower = trimmed.toLowerCase();
    const needles = [
        "质量审计",
        "执行报告",
        "验证结果",
        "涉及文件",
        "验证状态",
        "探索状态",
        "diff 自检",
        "quality audit",
        "execution report",
        "\u8ba1\u5212\u6267\u884c\u7ed3\u679c",
        "\u6267\u884c\u6b65\u9aa4",
        "tool_call",
        "<think>",
        "</think>",
    ];
    if (needles.some((needle) => trimmed.includes(needle) || lower.includes(needle))) {
        return true;
    }
    return trimmed.startsWith("{") && (lower.includes('"name"') || lower.includes("tool"));
}

/** Codex-style trail labels. Never show an ssh_ prefix. */
export function codingAgentToolDisplayName(name: string): string {
    const raw = (name || "").trim();
    if (!raw) return "tool";
    const lower = raw.toLowerCase();
    const bare = lower.startsWith("ssh_") ? lower.slice(4) : lower;
    if (bare === "bash") return "$";
    if (bare === "read_file" || bare === "read") return "Read";
    if (bare === "write_file" || bare === "write") return "Write";
    if (bare === "edit_file" || bare === "edit_lines" || bare === "str_replace" || bare === "apply_patch") return "Edit";
    if (
        bare === "ripgrep" ||
        bare === "grep_search" ||
        bare === "glob" ||
        bare === "list_dir" ||
        bare === "list_directory" ||
        bare === "search_files"
    ) {
        return "Search";
    }
    if (bare === "git_diff") return "Diff";
    if (lower.startsWith("ssh_")) return codingAgentToolDisplayName(raw.slice(4));
    return raw;
}

export function parseCodingAgentEventProgress(content: string): CodingAgentProgress | null {
    const prefix = "Coding Agent Event:";
    const trimmed = content.trim();
    if (!trimmed.startsWith(prefix)) return null;
    try {
        const raw = JSON.parse(trimmed.slice(prefix.length).trim()) as Record<string, unknown>;
        if (raw.agent !== "coding") return null;
        const taskID = typeof raw.task_id === "string" ? raw.task_id : typeof raw.taskID === "string" ? raw.taskID : undefined;
        const files = Array.isArray(raw.files) ? raw.files.filter((file): file is string => typeof file === "string") : undefined;
        const fileChanges = Array.isArray(raw.file_changes)
            ? raw.file_changes
                .filter((item): item is Record<string, unknown> => !!item && typeof item === "object")
                .map((item) => ({
                    path: typeof item.path === "string" ? item.path : "",
                    added: typeof item.added === "number" ? item.added : 0,
                    removed: typeof item.removed === "number" ? item.removed : 0,
                }))
            : undefined;
        return normalizeCodingAgentProgress({
            phase: normalizeCodingAgentPhase(String(raw.phase || "")),
            taskID,
            title: typeof raw.title === "string" ? raw.title : "",
            detail: typeof raw.detail === "string" ? raw.detail : "",
            command: typeof raw.command === "string" ? raw.command : "",
            outcome: typeof raw.outcome === "string" ? raw.outcome : "",
            severity: typeof raw.severity === "string" ? raw.severity : "",
            summary: typeof raw.summary === "string" ? raw.summary : "",
            event: typeof raw.event === "string" ? raw.event : "",
            runID: typeof raw.run_id === "string" ? raw.run_id : typeof raw.runID === "string" ? raw.runID : "",
            turnID: typeof raw.turn_id === "string" ? raw.turn_id : typeof raw.turnID === "string" ? raw.turnID : "",
            timestamp: typeof raw.ts === "string" ? raw.ts : typeof raw.timestamp === "string" ? raw.timestamp : "",
            durationMs: typeof raw.duration_ms === "number" ? raw.duration_ms : typeof raw.durationMs === "number" ? raw.durationMs : undefined,
            count: typeof raw.count === "number" ? raw.count : undefined,
            files,
            added: typeof raw.added === "number" ? raw.added : undefined,
            removed: typeof raw.removed === "number" ? raw.removed : undefined,
            fileChanges,
        });
    } catch {
        return null;
    }
}

export function codingAgentStatusLabel(phase: string, lang: string): string {
    const labels = CODING_AGENT_PHASE_LABELS[normalizeCodingAgentPhase(phase)];
    return lang.startsWith("zh") ? labels.zh : labels.en;
}

export function codingAgentStatusTone(phase: string): CodingAgentStatusTone {
    return CODING_AGENT_PHASE_TONES[normalizeCodingAgentPhase(phase)];
}

export function codingAgentProgressTone(progress: CodingAgentProgress): CodingAgentStatusTone {
    const normalized = normalizeCodingAgentProgress(progress);
    switch ((normalized.event || "").trim().toLowerCase()) {
        case "guardrail_summary":
            return codingAgentGuardrailStatusTone(normalized.outcome);
        case "command_summary":
            if (codingAgentCommandFailureLooksExploratory(normalized)) return neutralAttentionTone;
            return codingAgentCommandStatusTone(normalized.outcome);
        case "file_activity_summary":
            return codingAgentFileActivityStatusTone(normalized.outcome);
        case "quality_summary":
            return codingAgentQualityStatusTone(normalized.outcome);
        case "exploration_summary":
            return codingAgentExplorationStatusTone(normalized.outcome);
        case "verification_summary":
            return codingAgentVerificationStatusTone(normalized.outcome);
        case "diff_check":
            return codingAgentDiffCheckStatusTone(normalized.outcome);
        case "tool_finished":
            if (codingAgentToolFailureLooksDiagnostic(normalized)) return neutralAttentionTone;
            if (codingAgentToolFailureLooksExpectedOrRecoverable(normalized)) return neutralAttentionTone;
            return codingAgentToolOutcomeTone(normalized.outcome);
        default:
            return codingAgentStatusTone(normalized.phase);
    }
}

export function codingAgentToolProgressTone(outcome: string | undefined, summary?: string, detail?: string, severity?: string): CodingAgentStatusTone {
    const progress = normalizeCodingAgentProgress({
        phase: "running",
        title: "",
        event: "tool_finished",
        detail,
        outcome,
        severity,
        summary,
    });
    if (codingAgentToolFailureLooksDiagnostic(progress)) return neutralAttentionTone;
    if (codingAgentToolFailureLooksExpectedOrRecoverable(progress)) return neutralAttentionTone;
    return codingAgentToolOutcomeTone(outcome);
}

export function codingAgentToolProgressLabel(outcome: string | undefined, lang: string, summary?: string, detail?: string, severity?: string): string {
    const progress = normalizeCodingAgentProgress({
        phase: "running",
        title: "",
        event: "tool_finished",
        detail,
        outcome,
        severity,
        summary,
    });
    if (codingAgentToolFailureLooksExpectedOrRecoverable(progress)) {
        return lang.startsWith("zh") ? "\u68c0\u67e5" : "Check";
    }
    return codingAgentToolOutcomeLabel(outcome, lang);
}

export function codingAgentProgressStatusText(progress: CodingAgentProgress, lang: string): string {
    const normalized = normalizeCodingAgentProgress(progress);
    const event = (normalized.event || "").trim().toLowerCase();
    const label = (en: string, zh: string) => lang.startsWith("zh") ? zh : en;
    if (!normalized.outcome) return codingAgentStatusLabel(normalized.phase, lang);
    switch (event) {
        case "guardrail_summary":
            return `${label("Guard", "\u8fb9\u754c")} ${codingAgentGuardrailStatusLabel(normalized.outcome, lang)}`;
        case "command_summary":
            if (codingAgentCommandFailureLooksExploratory(normalized)) {
                return codingAgentCommandProgressLabel(normalized.outcome, lang, normalized.summary, normalized.detail, true);
            }
            return `${label("Commands", "\u547d\u4ee4")} ${codingAgentCommandStatusLabel(normalized.outcome, lang)}`;
        case "file_activity_summary":
            return `${label("Activity", "\u6587\u4ef6\u52a8\u4f5c")} ${codingAgentFileActivityStatusLabel(normalized.outcome, lang)}`;
        case "quality_summary":
            return `${label("Quality", "\u8d28\u91cf")} ${codingAgentQualityStatusLabel(normalized.outcome, lang)}`;
        case "exploration_summary":
            return `${label("Explore", "\u63a2\u7d22")} ${codingAgentExplorationStatusLabel(normalized.outcome, lang)}`;
        case "verification_summary":
            return `${label("Verify", "\u9a8c\u8bc1")} ${codingAgentVerificationStatusLabel(normalized.outcome, lang)}`;
        case "diff_check":
            return `${label("Diff check", "Diff \u81ea\u68c0")} ${codingAgentDiffCheckStatusLabel(normalized.outcome, lang)}`;
        case "tool_finished":
            if (codingAgentToolFailureLooksDiagnostic(normalized)) {
                return codingAgentToolDiagnosticStatusText(normalized, lang);
            }
            if (codingAgentToolFailureLooksExpectedOrRecoverable(normalized)) {
                return label("Tool Check", "\u5de5\u5177\u68c0\u67e5");
            }
            return `${label("Tool", "\u5de5\u5177")} ${codingAgentToolOutcomeLabel(normalized.outcome, lang)}`;
        default:
            return codingAgentStatusLabel(normalized.phase, lang);
    }
}

const monoFont = "ui-monospace, SFMono-Regular, Menlo, Consolas, \"Cascadia Mono\", monospace";

/** Glyph + color for a progress line (Codex / Claude Code style trail). */
export function codingAgentOutcomeMark(progress: CodingAgentProgress, isDark?: boolean): { glyph: string; color: string } {
    // Resolve through the same dark/light adapter as row chrome so glyphs stay in sync.
    const tone = resolveCodingAgentStatusTone(progress, isDark);
    const event = (progress.event || "").trim().toLowerCase();
    const rawOutcome = (progress.outcome || "").trim().toLowerCase();
    if (
        event === "tool_started"
        || ((event === "" || event === "task_status") && isCodingAgentActivePhase(progress.phase) && !rawOutcome)
    ) {
        return { glyph: "\u00b7", color: tone.accent }; // · running
    }
    if (event === "tool_finished" || event.endsWith("_summary") || event === "diff_check") {
        if (codingAgentToolFailureLooksDiagnostic(progress) || codingAgentToolFailureLooksExpectedOrRecoverable(progress)) {
            return { glyph: "\u00b7", color: tone.accent };
        }
        if (rawOutcome === "success" || rawOutcome === "passed" || rawOutcome === "checked" || rawOutcome === "explored" || rawOutcome === "changed") {
            return { glyph: "\u2713", color: successTone.accent }; // ✓
        }
        if (rawOutcome === "failed" || rawOutcome === "missing") {
            return { glyph: "\u2717", color: tone.accent }; // ✗ soft amber (not red)
        }
        if (rawOutcome === "blocked" || rawOutcome === "skipped" || rawOutcome === "none" || rawOutcome === "not_needed") {
            return { glyph: "\u2013", color: tone.accent }; // –
        }
    }
    if (progress.phase === "completed" || progress.phase === "result") {
        return { glyph: "\u2713", color: successTone.accent };
    }
    if (progress.phase === "failed") {
        return { glyph: "\u2717", color: tone.accent };
    }
    return { glyph: "\u00b7", color: tone.accent };
}

/** Primary tool/event name column (monospace, restrained). */
export function codingAgentToolNameText(progress: CodingAgentProgress): string {
    const event = (progress.event || "").trim().toLowerCase();
    if (event === "tool_finished" || event === "tool_started") {
        return codingAgentToolDisplayName(progress.detail || "tool");
    }
    if (event === "assistant_note") return "note";
    if (event === "diff_updated") return "Edit";
    if (event === "quality_summary") return "quality";
    if (event === "command_summary") return "commands";
    if (event === "file_activity_summary") return "files";
    if (event === "exploration_summary") return "explore";
    if (event === "verification_summary") return "verify";
    if (event === "guardrail_summary") return "guard";
    if (event === "diff_check") return "diff";
    if (event === "diff_summary") return "diff";
    if (event === "task_status" || !event) return progress.taskID || "task";
    return event.replace(/_summary$/, "").replace(/_/g, " ") || "task";
}

/** Codex-style trail path: repo-relative, never a drive-letter dump. */
export function compactCodingTrailPath(path: string): string {
    const next = (path || "").trim().replace(/\\/g, "/");
    if (!next) return "";
    const abs = next.startsWith("/") || /^[A-Za-z]:\//.test(next);
    if (!abs) return next;
    const parts = next.split("/").filter((part) => part && !/^[A-Za-z]:$/.test(part));
    if (parts.length >= 2) return parts.slice(-2).join("/");
    return parts[0] || next;
}

/** Codex-style file card: `foo.go (+12 -3)`. */
export function codingAgentEditedFileDetailText(progress: CodingAgentProgress, maxRunes = 88): string | undefined {
    const rows = codingAgentFileChangeRows(progress).filter((row) => row.path);
    const added = progress.added ?? rows.reduce((sum, row) => sum + (row.added || 0), 0);
    const removed = progress.removed ?? rows.reduce((sum, row) => sum + (row.removed || 0), 0);
    let text = "";
    if (rows.length > 0) {
        text = rows.map((row) => compactCodingTrailPath(row.path)).filter(Boolean).join(", ");
        if (added > 0 || removed > 0) {
            text = `${text} (+${added} -${removed})`;
        }
    } else if ((progress.detail || "").trim().startsWith("Edited ")) {
        text = (progress.detail || "").trim().replace(
            /^Edited\s+(.+?)(?:\s+(\(\+\d+\s+-\d+\)))?\s*$/,
            (_match, filePath: string, delta?: string) => (
                `Edited ${compactCodingTrailPath(filePath) || filePath}${delta ? ` ${delta}` : ""}`
            ),
        );
    }
    return text ? truncateCodingAgentInlineText(text, maxRunes) : undefined;
}

function codingAgentToolLooksLikeFileEdit(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    if (event === "diff_updated") return true;
    if (event !== "tool_finished" && event !== "tool_started") return false;
    const label = codingAgentToolDisplayName(progress.detail || "");
    return label === "Edit" || label === "Write";
}

/** Secondary detail: command, path hint, or short summary — not label soup. */
export function codingAgentToolDetailText(progress: CodingAgentProgress, lang: string, maxRunes = 88): string | undefined {
    const event = (progress.event || "").trim().toLowerCase();
    if ((event === "tool_finished" || event === "tool_started") && progress.command) {
        return truncateCodingAgentInlineText(progress.command, maxRunes);
    }
    if (codingAgentToolLooksLikeFileEdit(progress)) {
        const edited = codingAgentEditedFileDetailText(progress, maxRunes);
        if (edited) return edited;
    }
    if ((event === "tool_finished" || event === "tool_started" || event === "diff_updated") && progress.files?.length) {
        return truncateCodingAgentInlineText(progress.files.map(compactCodingTrailPath).filter(Boolean).join(", "), maxRunes);
    }
    if (event === "assistant_note" || event === "diff_updated") {
        if (progress.detail) return truncateCodingAgentInlineText(progress.detail, maxRunes);
        if (progress.summary) return truncateCodingAgentInlineText(progress.summary, maxRunes);
        return undefined;
    }
    if (event === "tool_finished" || event === "tool_started") {
        // Prefer human status for diagnostics; else title.
        if (codingAgentToolFailureLooksDiagnostic(progress)) {
            return codingAgentToolDiagnosticStatusText(progress, lang);
        }
        if (progress.summary) return truncateCodingAgentInlineText(progress.summary, maxRunes);
        if (progress.title) return truncateCodingAgentInlineText(progress.title, maxRunes);
        return undefined;
    }
    if (progress.summary) return truncateCodingAgentInlineText(progress.summary, maxRunes);
    if (progress.title) return truncateCodingAgentInlineText(progress.title, maxRunes);
    return codingAgentProgressMetaText(progress, lang);
}

/**
 * Remap shared palette accents for dark surfaces (failure amber brightens;
 * neutral slate lightens). Success / running stay as-is.
 * Prefer object identity (shared palette constants), with accent fallback for
 * any ad-hoc tone objects that still use the same hex.
 */
export function adaptCodingAgentStatusTone(tone: CodingAgentStatusTone, isDark?: boolean): CodingAgentStatusTone {
    if (!isDark) return tone;
    if (
        tone === neutralAttentionTone
        || tone === slateTone
        || tone.accent === neutralAttentionTone.accent
    ) {
        return neutralAttentionToneDark;
    }
    if (
        tone === codingAgentFailureTone
        || tone.accent === codingAgentFailureTone.accent
    ) {
        return codingAgentFailureToneDark;
    }
    return tone;
}

/** Progress → base tone, then dark-adapt when needed. */
export function resolveCodingAgentStatusTone(progress: CodingAgentProgress, isDark?: boolean): CodingAgentStatusTone {
    return adaptCodingAgentStatusTone(codingAgentProgressTone(progress), isDark);
}

/** Prefer explicit flag; else read app shell `data-ai-theme` (no prop drilling). */
export function codingAgentUiIsDark(isDark?: boolean): boolean {
    if (typeof isDark === "boolean") return isDark;
    if (typeof document === "undefined") return false;
    const theme =
        document.getElementById("App")?.getAttribute("data-ai-theme")
        || document.documentElement.getAttribute("data-ai-theme");
    return theme === "dark";
}

/** True while a tool (or pure task status) is still in-flight — not finished outcomes. */
function codingAgentLineIsRunning(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    if (event === "tool_started") return true;
    if (event === "tool_finished" || event === "assistant_note" || event.endsWith("_summary") || event === "diff_check" || event === "diff_summary" || event === "diff_updated") {
        return false;
    }
    // Pure task_status / legacy lines: active phase and no outcome yet.
    return isCodingAgentActivePhase(progress.phase) && !(progress.outcome || "").trim();
}

/** Completed, non-critical tool calls can collapse to one compact receipt. */
function codingAgentToolLineCanCollapse(progress: CodingAgentProgress): boolean {
    const event = (progress.event || "").trim().toLowerCase();
    return event === "tool_finished"
        && !codingAgentLineIsRunning(progress)
        && !codingAgentProgressLooksCritical(progress);
}

/**
 * Header snapshot for a feed:
 * - title/task from the best available row
 * - phase from the last terminal task_status only if no activity follows it
 *   (stale "completed" must not win over tools that ran afterward)
 */
export function pickCodingAgentFeedHeader(rows: CodingAgentProgress[]): CodingAgentProgress {
    if (rows.length === 0) {
        return { phase: "unknown", title: "" };
    }
    const latest = rows[rows.length - 1];
    let phase = latest.phase;
    let title = latest.title;
    let taskID = latest.taskID;
    let event = latest.event;
    let outcome = latest.outcome;

    let terminalIdx = -1;
    for (let i = rows.length - 1; i >= 0; i--) {
        const p = rows[i];
        if (isCodingAgentTaskStatusOnly(p) && isCodingAgentTerminalPhase(p.phase)) {
            terminalIdx = i;
            break;
        }
    }
    if (terminalIdx >= 0) {
        let activityAfter = false;
        for (let i = terminalIdx + 1; i < rows.length; i++) {
            if (isCodingAgentActivityEvent(rows[i])) {
                activityAfter = true;
                break;
            }
        }
        if (!activityAfter) {
            const terminal = rows[terminalIdx];
            phase = terminal.phase;
            event = terminal.event;
            outcome = terminal.outcome;
            if (terminal.title) title = terminal.title;
            if (terminal.taskID) taskID = terminal.taskID;
        }
    }
    if (!title || !taskID) {
        for (let i = rows.length - 1; i >= 0; i--) {
            if (!title && rows[i].title) title = rows[i].title;
            if (!taskID && rows[i].taskID) taskID = rows[i].taskID;
            if (title && taskID) break;
        }
    }
    return normalizeCodingAgentProgress({
        ...latest,
        phase,
        title,
        taskID,
        event,
        outcome,
    });
}

function renderCodingAgentAssistantNote(
    progress: CodingAgentProgress,
    t: CodingAgentProgressTheme,
    key?: string,
): React.ReactNode {
    const text = (progress.detail || progress.summary || "").trim();
    if (!text || codingAssistantNoteLooksInternal(text)) return null;
    return (
        <div
            key={key}
            data-testid="coding-agent-assistant-note"
            style={{
                margin: "2px 0 3px",
                paddingLeft: 18,
                color: t.text,
                fontSize: 12,
                lineHeight: 1.45,
                opacity: 0.92,
            }}
        >
            {text}
        </div>
    );
}

/** One terminal-style tool/activity line (used inside the feed panel). */
const CODING_AGENT_FILE_TABLE_PREVIEW = 8;
const codingAgentAddColor = { light: "#2f7a58", dark: "#5dba8a" };
const codingAgentDelColor = { light: "#b45309", dark: "#f0b35a" };

function renderCodingAgentFileChangeTable(
    progress: CodingAgentProgress,
    t: CodingAgentProgressTheme,
    lang: string,
    key?: string,
): React.ReactNode {
    const rows = codingAgentFileChangeRows(progress);
    if (rows.length === 0) return null;
    return (
        <CodingAgentFileChangeTable
            key={key}
            progress={progress}
            rows={rows}
            t={t}
            lang={lang}
        />
    );
}

function CodingAgentFileChangeTable({
    progress,
    rows,
    t,
    lang,
}: {
    progress: CodingAgentProgress;
    rows: CodingAgentFileChange[];
    t: CodingAgentProgressTheme;
    lang: string;
}): React.ReactElement {
    const onOpenPreviewFile = React.useContext(CodingAgentPreviewFocusContext);
    const [expanded, setExpanded] = React.useState(false);
    const hidden = Math.max(0, rows.length - CODING_AGENT_FILE_TABLE_PREVIEW);
    const visible = expanded || hidden === 0 ? rows : rows.slice(0, CODING_AGENT_FILE_TABLE_PREVIEW);
    const added = progress.added ?? rows.reduce((sum, row) => sum + row.added, 0);
    const removed = progress.removed ?? rows.reduce((sum, row) => sum + row.removed, 0);
    const addColor = t.isDark ? codingAgentAddColor.dark : codingAgentAddColor.light;
    const delColor = t.isDark ? codingAgentDelColor.dark : codingAgentDelColor.light;
    const count = progress.count && progress.count > rows.length ? progress.count : rows.length;
    const header = lang.startsWith("zh") ? `${count} 个文件已更改` : `${count} files changed`;
    const showAll = lang.startsWith("zh") ? "全部显示" : "Show all";
    const more = lang.startsWith("zh") ? `+${hidden} 个文件` : `+${hidden} files`;
    return (
        <div
            data-testid="coding-agent-file-changes"
            style={{
                margin: "2px 0 1px",
                padding: "2px 0 1px",
            }}
        >
            <div
                style={{
                    display: "flex",
                    alignItems: "baseline",
                    gap: 8,
                    marginBottom: 4,
                    fontFamily: monoFont,
                    fontSize: 11,
                    lineHeight: 1.35,
                }}
            >
                <span style={{ fontWeight: 600, color: t.text, flex: 1, minWidth: 0 }}>{header}</span>
                <span style={{ color: addColor, fontVariantNumeric: "tabular-nums" }}>+{added}</span>
                <span style={{ color: delColor, fontVariantNumeric: "tabular-nums" }}>-{removed}</span>
                {hidden > 0 && !expanded && (
                    <button
                        type="button"
                        data-testid="coding-agent-file-changes-expand"
                        onClick={() => setExpanded(true)}
                        style={{
                            padding: 0,
                            border: "none",
                            background: "none",
                            color: "#3b82f6",
                            fontFamily: monoFont,
                            fontSize: 11,
                            cursor: "pointer",
                        }}
                    >
                        {showAll}
                    </button>
                )}
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
                {visible.map((row) => (
                    <div
                        key={row.path}
                        role={onOpenPreviewFile ? "button" : undefined}
                        tabIndex={onOpenPreviewFile ? 0 : undefined}
                        data-testid="coding-agent-file-change-row"
                        data-preview-path={onOpenPreviewFile ? row.path : undefined}
                        onClick={onOpenPreviewFile ? () => onOpenPreviewFile(row.path) : undefined}
                        onKeyDown={onOpenPreviewFile ? (event) => {
                            if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                onOpenPreviewFile(row.path);
                            }
                        } : undefined}
                        style={{
                            display: "grid",
                            gridTemplateColumns: "minmax(0, 1fr) auto auto",
                            columnGap: 10,
                            alignItems: "baseline",
                            fontFamily: monoFont,
                            fontSize: 11,
                            lineHeight: 1.45,
                            color: t.text,
                            cursor: onOpenPreviewFile ? "pointer" : undefined,
                        }}
                        title={row.path}
                    >
                        <span
                            style={{
                                minWidth: 0,
                                overflow: "hidden",
                                textOverflow: "ellipsis",
                                whiteSpace: "nowrap",
                            }}
                        >
                            {compactCodingTrailPath(row.path) || row.path}
                        </span>
                        <span style={{ color: addColor, fontVariantNumeric: "tabular-nums" }}>+{row.added}</span>
                        <span style={{ color: delColor, fontVariantNumeric: "tabular-nums" }}>-{row.removed}</span>
                    </div>
                ))}
            </div>
            {hidden > 0 && !expanded && (
                <div style={{ marginTop: 3, color: t.fieldLabel, fontFamily: monoFont, fontSize: 10 }}>
                    {more}
                </div>
            )}
        </div>
    );
}

function CodingAgentToolLine({
    progress,
    t,
    lang,
    showCommandTestId,
    hideDetailIfEquals,
    onOpenPreviewFile,
}: {
    progress: CodingAgentProgress;
    t: CodingAgentProgressTheme;
    lang: string;
    showCommandTestId?: boolean;
    hideDetailIfEquals?: string;
    onOpenPreviewFile?: (path: string) => void;
}): React.ReactElement {
    const tone = resolveCodingAgentStatusTone(progress, t.isDark);
    const mark = codingAgentOutcomeMark(progress, t.isDark);
    const toolName = codingAgentToolNameText(progress);
    let detail = codingAgentToolDetailText(progress, lang);
    const previewPaths = codingAgentPreviewTargetPaths(progress);
    const previewPath = previewPaths[0];
    const canOpenPreview = !!(onOpenPreviewFile && previewPath);
    // Avoid repeating the feed header title on every tool row.
    if (detail && hideDetailIfEquals && detail === hideDetailIfEquals) {
        detail = undefined;
    }
    const duration = formatCodingAgentDuration(progress.durationMs);
    const command = (progress.command || "").trim();
    const showCmdPreview = !!codingAgentCommandPreviewText(progress, lang);
    const running = codingAgentLineIsRunning(progress);
    const canCollapse = codingAgentToolLineCanCollapse(progress);
    const [expanded, setExpanded] = React.useState(false);
    const collapsed = canCollapse && !expanded;
    const collapseLabel = lang.startsWith("zh") ? "已完成，展开详情" : "Completed, show details";
    const expandedLabel = lang.startsWith("zh") ? "收起工具详情" : "Collapse tool details";
    return (
        <details
            data-testid="coding-agent-tool-line"
            data-tool-running={running ? "true" : "false"}
            data-tool-collapsed={collapsed ? "true" : "false"}
            open={!collapsed}
            onToggle={(event) => setExpanded((event.currentTarget as HTMLDetailsElement).open)}
            style={{
                padding: "0",
                fontSize: 11,
                lineHeight: 1.4,
                color: t.text,
                opacity: running ? 0.9 : 1,
            }}
        >
            <summary
                aria-label={collapsed ? collapseLabel : expandedLabel}
                style={{
                    display: "grid",
                    gridTemplateColumns: "12px minmax(52px, 72px) minmax(0, 1fr) auto",
                    alignItems: "baseline",
                    columnGap: 6,
                    cursor: canCollapse ? "pointer" : "default",
                    listStyle: "none",
                }}
            >
                <span aria-hidden="true" style={{ color: mark.color, fontWeight: 700, fontFamily: monoFont, textAlign: "center", fontSize: 11, width: 12 }}>
                    {mark.glyph}
                </span>
                <span style={{ color: tone.accent, fontFamily: monoFont, fontSize: 11, fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={toolName}>
                    {toolName}
                </span>
                <span style={{ color: t.fieldLabel, fontFamily: monoFont, fontSize: 11, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {collapsed ? (lang.startsWith("zh") ? "已完成" : "Completed") : (detail || "")}
                </span>
                <span style={{ color: t.fieldLabel, fontFamily: monoFont, fontSize: 10, flexShrink: 0, opacity: 0.85, fontVariantNumeric: "tabular-nums" }}>
                    {duration || ""}
                </span>
            </summary>
            {(detail || command || canOpenPreview) && (
                <div style={{ margin: "2px 0 3px 18px", color: t.fieldLabel, fontFamily: monoFont, fontSize: 10, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    <span
                        data-testid={canOpenPreview ? "coding-agent-preview-link" : (showCmdPreview || showCommandTestId ? "coding-agent-command-preview" : undefined)}
                        role={canOpenPreview ? "button" : showCmdPreview ? "note" : undefined}
                        tabIndex={canOpenPreview ? 0 : undefined}
                        data-preview-path={canOpenPreview ? previewPath : undefined}
                        aria-label={canOpenPreview
                            ? `${lang.startsWith("zh") ? "打开预览" : "Open preview"}: ${compactCodingTrailPath(previewPath) || previewPath}`
                            : showCmdPreview ? `${lang.startsWith("zh") ? "\u547d\u4ee4" : "Command"}: ${command}` : undefined}
                        title={command || detail || undefined}
                        onClick={canOpenPreview ? () => onOpenPreviewFile?.(previewPath) : undefined}
                        onKeyDown={canOpenPreview ? (event) => {
                            if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                onOpenPreviewFile?.(previewPath);
                            }
                        } : undefined}
                        style={{ cursor: canOpenPreview ? "pointer" : undefined }}
                    >
                        {detail || command}
                    </span>
                </div>
            )}
        </details>
    );
}

/**
 * Chat surface for one coding-agent progress event.
 * Always uses the same terminal-style feed shell (never a separate badge card).
 */
export function renderCodingAgentProgressStatus(msg: ChatMessage, t: CodingAgentProgressTheme, lang: string): React.ReactNode {
    return renderCodingAgentActivityFeed([msg], t, lang);
}

/**
 * Programming-tool activity log: one panel, many mono lines
 * (Claude Code / Codex trail — not N stacked status chips).
 */
export function renderCodingAgentActivityFeed(
    messages: ChatMessage[],
    t: CodingAgentProgressTheme,
    lang: string,
): React.ReactNode {
    const rows = messages
        .map((msg) => ({ msg, progress: parseCodingAgentProgress(msg.content) }))
        .filter((row): row is { msg: ChatMessage; progress: CodingAgentProgress } => !!row.progress);
    if (rows.length === 0) return null;

    const header = pickCodingAgentFeedHeader(rows.map((r) => r.progress));
    // When tools/summaries exist, omit pure task_status lines — header already shows phase.
    const activityRows = rows.filter(({ progress }) => !isCodingAgentTaskStatusOnly(progress) && !isCodingAgentChatHiddenEvent(progress));
    const lineRows = activityRows.length > 0 ? activityRows : rows.filter(({ progress }) => !isCodingAgentChatHiddenEvent(progress));
    // Codex shows Read/Edit/$ as they happen — not a "T1 running" board before
    // the first tool. Structured assistant notes and file summaries are visible
    // coding activity too, and must retain their place in an interleaved trail.
    const hasRenderableActivity = lineRows.some(({ progress }) =>
        isCodingAgentPlainTrailEvent(progress)
        || (progress.event || "").trim().toLowerCase() === "assistant_note"
        || ((progress.event || "").trim().toLowerCase() === "diff_summary" && codingAgentFileChangeRows(progress).length > 0),
    );
    if (!hasRenderableActivity) {
        return null;
    }
    return (
        <CodingAgentActivityFeedShell
            header={header}
            lineRows={lineRows}
            t={t}
            lang={lang}
        />
    );
}

const MAX_VISIBLE_FEED_LINES = 20;

function CodingAgentActivityFeedShell({
    header,
    lineRows,
    t,
    lang,
}: {
    header: CodingAgentProgress;
    lineRows: Array<{ msg: ChatMessage; progress: CodingAgentProgress }>;
    t: CodingAgentProgressTheme;
    lang: string;
}): React.ReactElement {
    const onOpenPreviewFile = React.useContext(CodingAgentPreviewFocusContext);
    const [expanded, setExpanded] = React.useState(false);
    const hiddenCount = Math.max(0, lineRows.length - MAX_VISIBLE_FEED_LINES);
    const visibleLineRows = expanded || hiddenCount === 0 ? lineRows : lineRows.slice(-MAX_VISIBLE_FEED_LINES);
    const lineProgress = visibleLineRows.map((r) => r.progress);
    const tone = resolveCodingAgentFeedTone(header, lineProgress, t.isDark);
    const headerTask = header.taskID;
    const headerTitle = header.title;
    const phaseLabel = codingAgentStatusLabel(header.phase, lang);
    const statusText = codingAgentProgressStatusText(header, lang);
    const isMulti = visibleLineRows.length > 1;
    const criticalCount = lineProgress.reduce(
        (n, p) => n + (codingAgentProgressLooksCritical(p) ? 1 : 0),
        0,
    );
    // Multi: short phase, but surface failure count when chrome is elevated while still running.
    // Single: full status (e.g. "Quality Not Passed", not just "Result").
    const headerStatus = isMulti
        ? (
            criticalCount > 0 && isCodingAgentActivePhase(header.phase)
                ? (lang.startsWith("zh") ? `${criticalCount} \u9879\u5931\u8d25` : `${criticalCount} failed`)
                : phaseLabel
        )
        : statusText;
    // Single visible line keeps the established a11y string (via header snapshot).
    const feedLabel = isMulti
        ? joinCodingAgentStatusParts(
            codingAgentBrandLabel(lang),
            headerTask,
            headerStatus,
            headerTitle,
            `${visibleLineRows.length} ${lang.startsWith("zh") ? "\u6b65" : "steps"}`,
        )
        : codingAgentDisplayText(header, lang);
    const borderColor = t.isDark ? "rgba(148,163,184,0.18)" : "rgba(47, 111, 188, 0.14)";
    const bg = t.isDark ? "rgba(15, 23, 42, 0.42)" : "rgba(246, 248, 251, 0.98)";
    const hairline = t.isDark ? "rgba(148,163,184,0.12)" : "rgba(47, 111, 188, 0.09)";
    const hasToolTrail = lineRows.some(({ progress }) => !isCodingAgentTaskStatusOnly(progress));
    const hasPlainTrail = lineRows.some(({ progress }) => isCodingAgentPlainTrailEvent(progress));
    const showFailureChip = criticalCount > 0 && !hasPlainTrail;
    const showBoardHeader = !hasToolTrail || showFailureChip;
    const showHeaderRule = isMulti && showBoardHeader && !hasToolTrail;
    // Parent Fragment owns the React list key; avoid remount-churning key on the shell.

    return (
        <div
            className={codingAgentStatusClassName(header, "chat-progress")}
            data-testid="coding-agent-progress"
            data-coding-feed={isMulti ? "true" : lineRows.some(({ progress }) => !isCodingAgentTaskStatusOnly(progress)) ? "activity" : "single"}
            data-coding-trail={hasPlainTrail ? "plain" : "board"}
            data-tone-accent={tone.accent}
            {...codingAgentStatusDataAttrs(header, "chat-progress")}
            role="status"
            aria-live="polite"
            // Only announce meaningful header changes, not every mono line rewrite.
            aria-atomic="false"
            aria-label={feedLabel}
            style={{
                margin: hasPlainTrail ? "2px 0" : "4px 0",
                padding: hasPlainTrail ? "0" : "4px 7px 4px",
                borderRadius: hasPlainTrail ? 0 : 5,
                border: hasPlainTrail ? "none" : `1px solid ${borderColor}`,
                background: hasPlainTrail ? "transparent" : bg,
                color: t.text,
                boxShadow: hasPlainTrail
                    ? "none"
                    : (t.isDark
                        ? `inset 0 1.5px 0 0 ${tone.accent}`
                        : `inset 0 1.5px 0 0 ${tone.accent}, 0 1px 1px rgba(15,23,42,0.03)`),
            }}
        >
            {showBoardHeader && (
            <div
                data-testid="coding-agent-feed-header"
                style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 5,
                    marginBottom: lineRows.length > 0 ? 2 : 0,
                    paddingBottom: showHeaderRule ? 3 : 0,
                    borderBottom: showHeaderRule ? `1px solid ${hairline}` : "none",
                    fontSize: 11,
                    lineHeight: 1.25,
                    fontFamily: monoFont,
                }}
            >
                {!hasToolTrail && (
                <span style={{ fontWeight: 700, color: tone.accent, letterSpacing: "0.01em" }}>
                    {codingAgentBrandLabel(lang)}
                </span>
                )}
                {!hasToolTrail && headerTask && (
                    <span style={{ fontWeight: 600, color: t.fieldLabel }}>{headerTask}</span>
                )}
                {!hasToolTrail && headerTitle && (
                    <span
                        style={{
                            flex: 1,
                            minWidth: 0,
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                            color: t.text,
                            fontWeight: 500,
                            fontFamily: "inherit",
                        }}
                        title={headerTitle}
                    >
                        {headerTitle}
                    </span>
                )}
                {(!headerTitle || hasToolTrail) && <span style={{ flex: 1 }} />}
                <span style={{ color: tone.accent, fontWeight: 600, flexShrink: 0, fontSize: 10 }}>
                    {hasToolTrail ? (showFailureChip ? (lang.startsWith("zh") ? `${criticalCount} \u9879\u5931\u8d25` : `${criticalCount} failed`) : "") : headerStatus}
                </span>
            </div>
            )}
            <div
                data-testid="coding-agent-feed-lines"
                style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 0,
                    paddingLeft: 2,
                    maxHeight: 280,
                    overflowY: "auto",
                }}
            >
                {hiddenCount > 0 && !expanded && (
                    <button
                        type="button"
                        data-testid="coding-agent-feed-expand"
                        onClick={() => setExpanded(true)}
                        style={{
                            alignSelf: "flex-start",
                            margin: "0 0 2px",
                            padding: 0,
                            border: "none",
                            background: "none",
                            color: t.fieldLabel,
                            fontFamily: monoFont,
                            fontSize: 10,
                            cursor: "pointer",
                        }}
                    >
                        {lang.startsWith("zh") ? `展开更早 ${hiddenCount} 步` : `Show earlier ${hiddenCount} steps`}
                    </button>
                )}
                {visibleLineRows.map(({ msg, progress }) =>
                    (progress.event || "").trim().toLowerCase() === "assistant_note"
                        ? renderCodingAgentAssistantNote(progress, t, msg.id)
                        : codingAgentFileChangeRows(progress).length > 0 && (progress.event || "").trim().toLowerCase() === "diff_summary"
                        ? renderCodingAgentFileChangeTable(progress, t, lang, msg.id)
                        : <CodingAgentToolLine
                            key={msg.id}
                            progress={progress}
                            t={t}
                            lang={lang}
                            showCommandTestId={visibleLineRows.length === 1}
                            hideDetailIfEquals={headerTitle || undefined}
                            onOpenPreviewFile={onOpenPreviewFile}
                        />,
                )}
            </div>
        </div>
    );
}

export function latestCodingAgentProgress(messages: ChatMessage[]): CodingAgentProgress | null {
    for (let i = messages.length - 1; i >= 0; i--) {
        const parsed = parseCodingAgentProgress(messages[i]?.content || "");
        if (parsed) return parsed;
    }
    return null;
}

export function activeCodingAgentProgress(messages: ChatMessage[], active: boolean): CodingAgentProgress | null {
    if (!active) return null;
    const latest = latestCodingAgentProgress(messages);
    if (!latest) return null;
    const normalized = normalizeCodingAgentProgress(latest);
    return isCodingAgentActivePhase(normalized.phase) || normalized.event === "diff_summary" || normalized.event === "verification_summary" || normalized.event === "diff_check" || normalized.event === "exploration_summary" || normalized.event === "guardrail_summary" || normalized.event === "command_summary" || normalized.event === "file_activity_summary" || normalized.event === "quality_summary" ? normalized : null;
}

export function latestCodingAgentTurnSnapshot(messages: ChatMessage[]): CodingAgentTurnSnapshot | null {
    const parsed = messages
        .map((msg) => parseCodingAgentProgress(msg?.content || ""))
        .filter((progress): progress is CodingAgentProgress => !!progress)
        .map(normalizeCodingAgentProgress);
    if (parsed.length === 0) return null;

    const latest = parsed[parsed.length - 1];
    const sameTurn = parsed.filter((progress) => codingAgentSameTurn(progress, latest));
    const tools = collectCodingAgentToolTrace(sameTurn);
    const latestTool = tools?.[tools.length - 1];
    const tool = latestTool?.name || findLatestCodingAgentEventDetail(sameTurn, "tool_started");
    const guardrailSummaryEvent = findLatestCodingAgentEvent(sameTurn, "guardrail_summary");
    const commandSummaryEvent = findLatestCodingAgentEvent(sameTurn, "command_summary");
    const fileActivitySummaryEvent = findLatestCodingAgentEvent(sameTurn, "file_activity_summary");
    const qualitySummaryEvent = findLatestCodingAgentEvent(sameTurn, "quality_summary");
    const explorationSummaryEvent = findLatestCodingAgentEvent(sameTurn, "exploration_summary");
    const verificationSummaryEvent = findLatestCodingAgentEvent(sameTurn, "verification_summary");
    const diffCheckEvent = findLatestCodingAgentEvent(sameTurn, "diff_check");
    const diffSummaryEvent = findLatestCodingAgentEvent(sameTurn, "diff_summary");
    const diffUpdatedEvent = findLatestCodingAgentEvent(sameTurn, "diff_updated");
    const files = diffSummaryEvent?.files || fileActivitySummaryEvent?.files || latest.files;
    const changeCount = diffSummaryEvent?.count ?? latest.count;

    return {
        latest,
        turnID: latest.turnID,
        runID: latest.runID,
        taskID: latest.taskID,
        title: latest.title,
        phase: latest.phase,
        tool,
        toolOutcome: latestTool?.outcome,
        toolDurationMs: latestTool?.durationMs,
        tools,
        guardrailStatus: guardrailSummaryEvent?.outcome,
        guardrailSummary: guardrailSummaryEvent?.summary,
        guardrailCount: guardrailSummaryEvent?.count,
        commandStatus: commandSummaryEvent?.outcome,
        commandSummary: commandSummaryEvent?.summary,
        commandCount: commandSummaryEvent?.count,
        fileActivityStatus: fileActivitySummaryEvent?.outcome,
        fileActivitySummary: fileActivitySummaryEvent?.summary,
        fileActivityCount: fileActivitySummaryEvent?.count,
        fileActivityDetail: fileActivitySummaryEvent?.detail,
        qualityStatus: qualitySummaryEvent?.outcome,
        qualitySummary: qualitySummaryEvent?.summary,
        qualityCount: qualitySummaryEvent?.count,
        explorationStatus: explorationSummaryEvent?.outcome,
        explorationSummary: explorationSummaryEvent?.summary,
        explorationCount: explorationSummaryEvent?.count,
        verificationStatus: verificationSummaryEvent?.outcome,
        verificationSummary: verificationSummaryEvent?.summary,
        verificationCount: verificationSummaryEvent?.count,
        diffCheckStatus: diffCheckEvent?.outcome,
        diffCheckSummary: diffCheckEvent?.summary,
        changeCount,
        files,
        diffSummary: diffSummaryEvent?.detail || diffUpdatedEvent?.detail,
    };
}

function collectCodingAgentToolTrace(events: CodingAgentProgress[], maxTools = 3): CodingAgentToolTrace[] | undefined {
    const tools: CodingAgentToolTrace[] = [];
    for (const event of events) {
        if ((event.event !== "tool_started" && event.event !== "tool_finished") || !event.detail) continue;
        if (event.event === "tool_started") {
            tools.push({ name: event.detail });
            continue;
        }
        const existing = findPendingCodingAgentToolTrace(tools, event.detail);
        const finished = existing || { name: event.detail };
        finished.outcome = event.outcome;
        finished.durationMs = event.durationMs;
        finished.summary = event.summary;
        if (!existing) tools.push(finished);
    }
    const recent = tools.filter((tool) => tool.name).slice(-Math.max(1, maxTools));
    return recent.length > 0 ? recent : undefined;
}

function findPendingCodingAgentToolTrace(tools: CodingAgentToolTrace[], name: string): CodingAgentToolTrace | undefined {
    for (let i = tools.length - 1; i >= 0; i--) {
        if (tools[i]?.name === name && !tools[i]?.outcome) return tools[i];
    }
    return undefined;
}

function codingAgentSameTurn(progress: CodingAgentProgress, latest: CodingAgentProgress): boolean {
    if (latest.turnID) return progress.turnID === latest.turnID;
    if (latest.runID && latest.taskID) return progress.runID === latest.runID && progress.taskID === latest.taskID;
    if (latest.taskID) return progress.taskID === latest.taskID;
    return true;
}

function findLatestCodingAgentEvent(events: CodingAgentProgress[], eventName: string): CodingAgentProgress | undefined {
    for (let i = events.length - 1; i >= 0; i--) {
        if (events[i]?.event === eventName) return events[i];
    }
    return undefined;
}

function findLatestCodingAgentEventDetail(events: CodingAgentProgress[], eventName: string): string | undefined {
    return findLatestCodingAgentEvent(events, eventName)?.detail;
}

/** Short brand label shared by chat feed, sidebar, and status bar. */
export function codingAgentBrandLabel(lang: string): string {
    return lang.startsWith("zh") ? "\u7f16\u7a0b" : "Coding";
}

/** Professional thin-space separator for compact status copy. */
export const CODING_AGENT_STATUS_SEP = " \u00b7 ";

function joinCodingAgentStatusParts(...parts: Array<string | undefined | null | false>): string {
    return parts.filter((part): part is string => typeof part === "string" && part.trim().length > 0).join(CODING_AGENT_STATUS_SEP);
}

export function codingAgentDisplayText(progress: CodingAgentProgress, lang: string): string {
    const normalized = normalizeCodingAgentProgress(progress);
    return joinCodingAgentStatusParts(
        codingAgentBrandLabel(lang),
        codingAgentProgressStatusText(normalized, lang),
        normalized.taskID,
        codingAgentProgressMetaText(normalized, lang),
        normalized.title,
    );
}

export function codingAgentVariantDisplayText(progress: CodingAgentProgress, lang: string, variant: CodingAgentStatusVariant): string {
    // Sidebar keeps the same short line, but appends file preview for a11y/title
    // when files are present on the progress object itself (body may hide them).
    if (variant !== "sidebar") return codingAgentDisplayText(progress, lang);
    const normalized = normalizeCodingAgentProgress(progress);
    return joinCodingAgentStatusParts(
        codingAgentDisplayText(normalized, lang),
        codingAgentFilePreviewText(normalized, lang),
    );
}

/** Input placeholder: current Read/Edit/$ line, never a "Coding · T2 running" scorecard. */
export function codingAgentInputStatusText(progress: CodingAgentProgress | null | undefined, lang: string): string | undefined {
    if (!progress) return undefined;
    const event = (progress.event || "").trim().toLowerCase();
    if (event === "tool_started" || event === "tool_finished") {
        const name = codingAgentToolNameText(progress);
        const detail = codingAgentToolDetailText(progress, lang, 48);
        const line = [name, detail].filter(Boolean).join(" ");
        return line || (lang.startsWith("zh") ? "\u5904\u7406\u4e2d\u2026" : "Working...");
    }
    return lang.startsWith("zh") ? "\u5904\u7406\u4e2d\u2026" : "Working...";
}

/** Programming composer: only a real tool line, never generic 处理中 / Working. */
export function codingAgentComposerStatusText(progress: CodingAgentProgress | null | undefined, lang: string): string | undefined {
    if (!progress) return undefined;
    const event = (progress.event || "").trim().toLowerCase();
    if (event !== "tool_started" && event !== "tool_finished") return undefined;
    const name = codingAgentToolNameText(progress);
    const detail = codingAgentToolDetailText(progress, lang, 48);
    const line = [name, detail].filter(Boolean).join(" ").trim();
    return line || undefined;
}

export function codingAgentCompactText(progress: CodingAgentProgress, lang: string): string {
    const normalized = normalizeCodingAgentProgress(progress);
    // Processing strip: brand · status · task · optional meta (no long title).
    return joinCodingAgentStatusParts(
        codingAgentBrandLabel(lang),
        codingAgentProgressStatusText(normalized, lang),
        normalized.taskID,
        codingAgentProgressMetaText(normalized, lang),
    );
}

export function codingAgentProgressMetaText(progress: CodingAgentProgress, lang: string): string | undefined {
    const normalized = normalizeCodingAgentProgress(progress);
    const event = (normalized.event || "").trim().toLowerCase();
    // tool_finished: detail is the tool name (already clear from status/trail).
    // Prefer count meta only — keeps compact strips denser.
    if (event === "tool_finished") {
        if (normalized.count !== undefined) return codingAgentCountMetaText(normalized, lang);
        return undefined;
    }
    if (normalized.detail) return normalized.detail;
    if (normalized.count !== undefined) return codingAgentCountMetaText(normalized, lang);
    return undefined;
}

export function codingAgentCommandPreviewText(progress: CodingAgentProgress, lang: string, maxRunes = 72): string | undefined {
    const normalized = normalizeCodingAgentProgress(progress);
    if ((normalized.event || "").trim().toLowerCase() !== "tool_finished") return undefined;
    const toolName = (normalized.detail || "").trim().toLowerCase();
    if (toolName !== "bash" && toolName !== "ssh_bash") return undefined;
    const outcome = normalizeCodingAgentToolOutcome(normalized.outcome);
    if (outcome !== "failed" && outcome !== "blocked") return undefined;
    const command = (normalized.command || "").trim();
    if (!command) return undefined;
    const label = lang.startsWith("zh") ? "\u547d\u4ee4" : "cmd";
    return `${label}: ${truncateCodingAgentInlineText(command, maxRunes)}`;
}

function truncateCodingAgentInlineText(text: string, maxRunes: number): string {
    const chars = Array.from(text.replace(/\s+/g, " ").trim());
    if (chars.length <= maxRunes) return chars.join("");
    return `${chars.slice(0, Math.max(1, maxRunes - 1)).join("")}\u2026`;
}

function codingAgentCountMetaText(progress: CodingAgentProgress, lang: string): string {
    const count = progress.count ?? 0;
    const event = (progress.event || "").trim().toLowerCase();
    if (lang.startsWith("zh")) {
        switch (event) {
            case "command_summary":
                return `${count} \u6761\u547d\u4ee4`;
            case "verification_summary":
                return `${count} \u6761\u9a8c\u8bc1`;
            case "exploration_summary":
                return `${count} \u6b21\u63a2\u7d22`;
            case "guardrail_summary":
                return `${count} \u4e2a\u62e6\u622a`;
            case "file_activity_summary":
                return `${count} \u4e2a\u6587\u4ef6\u52a8\u4f5c`;
            case "quality_summary":
                return `${count} \u4e2a\u95ee\u9898`;
            default:
                return `${count} \u4e2a\u53d8\u66f4`;
        }
    }
    switch (event) {
        case "command_summary":
            return `${count} commands`;
        case "verification_summary":
            return `${count} checks`;
        case "exploration_summary":
            return `${count} explorations`;
        case "guardrail_summary":
            return `${count} blocks`;
        case "file_activity_summary":
            return `${count} file actions`;
        case "quality_summary":
            return `${count} issues`;
        default:
            return `${count} changes`;
    }
}

export function codingAgentFilePreviewText(progress: CodingAgentProgress, lang: string, maxFiles = 3): string | undefined {
    const normalized = normalizeCodingAgentProgress(progress);
    const files = normalized.files || [];
    if (files.length === 0) return undefined;
    const shown = files.slice(0, Math.max(1, maxFiles));
    const remaining = files.length - shown.length;
    const suffix = remaining > 0
        ? lang.startsWith("zh") ? ` \u7b49 ${remaining} \u4e2a` : ` +${remaining} more`
        : "";
    return `${shown.join(", ")}${suffix}`;
}

export function codingAgentTurnSnapshotText(snapshot: CodingAgentTurnSnapshot, lang: string): string {
    const latest = normalizeCodingAgentProgress(snapshot.latest);
    const toolLabel = lang.startsWith("zh") ? "\u5de5\u5177" : "Tool";
    const outcomeLabel = lang.startsWith("zh") ? "\u7ed3\u679c" : "Result";
    const durationLabel = lang.startsWith("zh") ? "\u8017\u65f6" : "Duration";
    const filesLabel = lang.startsWith("zh") ? "\u6587\u4ef6" : "Files";
    const traceLabel = lang.startsWith("zh") ? "\u8f68\u8ff9" : "Trace";
    const guardLabel = lang.startsWith("zh") ? "\u8fb9\u754c" : "Guard";
    const commandLabel = lang.startsWith("zh") ? "\u547d\u4ee4" : "Commands";
    const fileActivityLabel = lang.startsWith("zh") ? "\u52a8\u4f5c" : "Activity";
    const qualityLabel = lang.startsWith("zh") ? "\u8d28\u91cf" : "Quality";
    const exploreLabel = lang.startsWith("zh") ? "\u63a2\u7d22" : "Explore";
    const verifyLabel = lang.startsWith("zh") ? "\u9a8c\u8bc1" : "Verify";
    const diffCheckLabel = lang.startsWith("zh") ? "Diff" : "Diff check";
    const diffLabel = "Diff";
    const files = snapshot.files?.length ? codingAgentFilePreviewText({ ...latest, files: snapshot.files }, lang) : undefined;
    const trace = codingAgentToolTraceText(snapshot, lang);
    // Accessibility copy stays complete; visual UI is denser chips.
    // Use display text (not sidebar variant) so files are only listed once below.
    return joinCodingAgentStatusParts(
        codingAgentDisplayText(latest, lang),
        trace ? `${traceLabel}: ${trace}` : undefined,
        snapshot.tool ? `${toolLabel}: ${snapshot.tool}` : undefined,
        snapshot.toolOutcome ? `${outcomeLabel}: ${snapshot.toolOutcome}` : undefined,
        snapshot.toolDurationMs !== undefined ? `${durationLabel}: ${formatCodingAgentDuration(snapshot.toolDurationMs)}` : undefined,
        snapshot.guardrailStatus ? `${guardLabel}: ${codingAgentGuardrailStatusLabel(snapshot.guardrailStatus, lang)}${snapshot.guardrailSummary ? ` (${snapshot.guardrailSummary})` : ""}` : undefined,
        snapshot.commandStatus ? `${commandLabel}: ${codingAgentCommandProgressLabel(snapshot.commandStatus, lang, snapshot.commandSummary)}${snapshot.commandSummary ? ` (${snapshot.commandSummary})` : ""}` : undefined,
        snapshot.fileActivityStatus ? `${fileActivityLabel}: ${codingAgentFileActivityStatusLabel(snapshot.fileActivityStatus, lang)}${snapshot.fileActivityDetail ? ` (${snapshot.fileActivityDetail})` : snapshot.fileActivitySummary ? ` (${snapshot.fileActivitySummary})` : ""}` : undefined,
        snapshot.qualityStatus ? `${qualityLabel}: ${codingAgentQualityStatusLabel(snapshot.qualityStatus, lang)}${snapshot.qualitySummary ? ` (${snapshot.qualitySummary})` : ""}` : undefined,
        snapshot.explorationStatus ? `${exploreLabel}: ${codingAgentExplorationStatusLabel(snapshot.explorationStatus, lang)}${snapshot.explorationSummary ? ` (${snapshot.explorationSummary})` : ""}` : undefined,
        snapshot.verificationStatus ? `${verifyLabel}: ${codingAgentVerificationStatusLabel(snapshot.verificationStatus, lang)}${snapshot.verificationSummary ? ` (${snapshot.verificationSummary})` : ""}` : undefined,
        snapshot.diffCheckStatus ? `${diffCheckLabel}: ${codingAgentDiffCheckStatusLabel(snapshot.diffCheckStatus, lang)}${snapshot.diffCheckSummary ? ` (${snapshot.diffCheckSummary})` : ""}` : undefined,
        files ? `${filesLabel}: ${files}` : undefined,
        snapshot.diffSummary ? `${diffLabel}: ${snapshot.diffSummary}` : undefined,
    );
}

export function codingAgentToolTraceText(snapshot: CodingAgentTurnSnapshot, lang: string): string | undefined {
    const tools = snapshot.tools || [];
    if (tools.length === 0) return undefined;
    return tools.map((tool) => {
        const outcome = tool.outcome ? codingAgentToolProgressLabel(tool.outcome, lang, tool.summary, tool.name) : undefined;
        const duration = formatCodingAgentDuration(tool.durationMs);
        const summary = tool.summary ? `(${tool.summary})` : undefined;
        return [tool.name, outcome, duration, summary].filter(Boolean).join(" ");
    }).join(" -> ");
}

export function formatCodingAgentDuration(durationMs?: number): string | undefined {
    if (!Number.isFinite(durationMs) || durationMs === undefined || durationMs < 0) return undefined;
    if (durationMs === 0) return "0ms";
    if (durationMs < 1000) return `${Math.max(1, Math.round(durationMs))}ms`;
    if (durationMs < 60000) {
        const seconds = durationMs / 1000;
        return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s`;
    }
    const minutes = Math.floor(durationMs / 60000);
    const seconds = Math.round((durationMs % 60000) / 1000);
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
}

export function normalizeCodingAgentToolOutcome(outcome?: string): CodingAgentToolOutcome {
    const normalized = (outcome || "").trim().toLowerCase();
    return normalized === "success" || normalized === "failed" || normalized === "blocked" ? normalized : "unknown";
}

export function codingAgentToolOutcomeLabel(outcome: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentToolOutcome(outcome);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "success": return "\u6210\u529f";
            case "failed": return "\u5931\u8d25";
            case "blocked": return "\u5df2\u963b\u65ad";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "success": return "Success";
        case "failed": return "Failed";
        case "blocked": return "Blocked";
        default: return "Unknown";
    }
}

export function codingAgentToolOutcomeTone(outcome: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentToolOutcome(outcome)) {
        case "success":
            return successTone;
        case "failed":
            return codingAgentFailureTone;
        case "blocked":
            return neutralAttentionTone;
        default:
            return slateTone;
    }
}

function codingAgentToolFailureLooksDiagnostic(progress: CodingAgentProgress): boolean {
    if (normalizeCodingAgentToolOutcome(progress.outcome) !== "failed") return false;
    // Real MSVC compile/link failures must keep the hard-failure tone even when the command
    // embeds full Visual Studio paths or PowerShell wrappers (those used to match probe markers).
    if (codingAgentCommandLooksLikeMSVCCompile(progress.command || "")) {
        return false;
    }
    const text = [progress.detail, progress.command, progress.summary].filter(Boolean).join("\n").toLowerCase();
    const hardFailureMarkers = [
        "assert",
        "access denied",
        "build stopped",
        "check(",
        "check (",
        "error c", // MSVC compiler errors (C2065, …)
        "fatal error",
        "fail at",
        "failed test",
        "linker command failed",
        "lnk",
        "ninja:",
        "panic:",
        "permission denied",
        "pytest",
        "test failed",
        "traceback",
        "undefined reference",
        "undeclared identifier",
    ];
    if (hardFailureMarkers.some((marker) => text.includes(marker))) return false;
    if ((progress.severity || "").trim().toLowerCase() === "diagnostic") return true;
    if (!text) return false;
    if (codingAgentCommandLooksLikeEnvironmentProbe(progress.command)) return true;
    if (codingAgentCommandLooksLikeExploratoryTest(progress.command, progress.summary)) return true;
    const toolName = (progress.detail || "").trim().toLowerCase();
    // Local CodingSubAgent + remote coding tools share the same progress surface.
    // Exploratory existence probes should stay neutral (gray), not hard-failure amber.
    if ([
        "read_file",
        "list_directory",
        "ripgrep",
        "glob",
        "ssh_read_file",
        "ssh_list_dir",
    ].includes(toolName) && codingAgentPathLookupLooksUnsuccessful(text)) {
        return true;
    }
    const diagnosticMarkers = [
        "--version",
        "command not found",
        "could not find files for the given pattern",
        "diagnostic",
        "environment probe",
        "expectedexpression",
        "expected expression",
        "execution_policies",
        "execution policies",
        "fullyqualifiederrorid : expectedexpression",
        "fullyqualifiederrorid : unexpectedtoken",
        "missingopenparenthesisinifstatement",
        "missing closing",
        "missing variable name after foreach",
        "no generator specified for -g",
        "output was likely consumed by a pipeline filter",
        "parsererror",
        "powershell command compatibility",
        "powershell exception",
        "get-command",
        "not recognized as",
        "version check",
        "where.exe",
        "authorizationmanager",
        "unexpected token",
        "unexpectedtoken",
        "在此系统上禁止运行脚本",
        "无法加载文件",
        "表达式或语句中包含意外的标记",
        "意外的标记",
        "无法将",
        "识别为 cmdlet",
    ];
    return diagnosticMarkers.some((marker) => text.includes(marker));
}

function codingAgentPathLookupLooksUnsuccessful(text: string): boolean {
    return [
        "file not found",
        "no such file",
        "path does not exist",
        "path not found",
        "cannot find the path",
        "could not find the path",
        "cannot access",
        "not a directory",
        "is not a directory",
        "not found:",
        "文件不存在",
        "路径不存在",
        "找不到文件",
        "找不到路径",
        "没有那个文件",
        "不是目录",
    ].some((marker) => text.includes(marker));
}

function codingAgentCommandLooksLikeExploratoryTest(command?: string, summary?: string): boolean {
    const text = `${command || ""}\n${summary || ""}`.toLowerCase();
    if (!text.trim()) return false;
    if (["assert", "access denied", "fatal error", "linker command failed", "panic:", "permission denied", "traceback", "undefined reference"].some((marker) => text.includes(marker))) {
        return false;
    }
    return [
        "\\build\\tests\\",
        "/build/tests/",
        "\\tests\\release\\",
        "/tests/release/",
        "ctest",
        "--gtest_list_tests",
        "--list-tests",
    ].some((marker) => text.includes(marker));
}

function codingAgentToolDiagnosticStatusText(progress: CodingAgentProgress, lang: string): string {
    const command = (progress.command || "").toLowerCase();
    const summary = (progress.summary || "").toLowerCase();
    const detail = (progress.detail || "").trim().toLowerCase();
    const label = (en: string, zh: string) => lang.startsWith("zh") ? zh : en;
    if (codingAgentCommandLooksLikeExploratoryTest(progress.command, progress.summary)) {
        return label("Exploratory test did not pass", "探索性测试未通过");
    }
    if ([
        "read_file",
        "list_directory",
        "ripgrep",
        "glob",
        "ssh_read_file",
        "ssh_list_dir",
    ].includes(detail) && codingAgentPathLookupLooksUnsuccessful(summary)) {
        return label("File or path not found", "文件或路径不存在");
    }
    const commandUnavailable = ["command not found", "not recognized as", "\u65e0\u6cd5\u5c06", "\u8bc6\u522b\u4e3a cmdlet"].some((marker) => summary.includes(marker));
    if (command.includes("clang++") && (commandUnavailable || command.includes("--version"))) return label("clang++ not found", "clang++ \u4e0d\u5b58\u5728");
    if (command.includes("choco") && commandUnavailable) return label("choco not found", "choco \u4e0d\u5b58\u5728");
    // Real compile failures are not labeled as PATH probes (see probe classifier).
    if (codingAgentCommandLooksLikeMSVCCompile(command)) {
        return label("MSVC build did not succeed", "MSVC \u7f16\u8bd1\u672a\u6210\u529f");
    }
    // MSVC: cl is almost never on PATH even when Visual Studio is installed.
    // Prefer "need vcvars" over "VS missing" so the UI matches host reality.
    if (
        command.includes("where cl.exe")
        || command.includes("where.exe cl")
        || (/\bcl(\.exe)?\b/.test(command) && (commandUnavailable || command.includes("get-command")) && !codingAgentCommandLooksLikeMSVCCompile(command))
    ) {
        return label("cl.exe not on PATH (use vcvars)", "cl.exe \u4e0d\u5728 PATH\uff08\u9700\u5148 vcvars\uff09");
    }
    if (
        !codingAgentCommandLooksLikeMSVCCompile(command)
        && (command.includes("visualstudio") || command.includes("microsoft visual studio") || command.includes("get-itemproperty") || command.includes("vswhere"))
    ) {
        return label("VS path probe miss (cl needs vcvars)", "VS \u8def\u5f84\u63a2\u6d4b\u672a\u547d\u4e2d\uff08cl \u9700 vcvars\uff09");
    }
    return label("Environment probe did not succeed", "\u73af\u5883\u63a2\u6d4b\u672a\u6210\u529f");
}

/** True when a command looks like a real MSVC compile/link, not a path probe. */
function codingAgentCommandLooksLikeMSVCCompile(command: string): boolean {
    const normalized = command.toLowerCase();
    // Recipe: vcvars / VS install path + cl with sources or output flags.
    const hasVCEnv =
        normalized.includes("vcvars")
        || normalized.includes("vc\\auxiliary\\build")
        || normalized.includes("vc/auxiliary/build");
    const hasCl = /\bcl(\.exe)?\b/.test(normalized);
    if (!hasCl && !hasVCEnv) return false;
    if (/\.(c|cc|cpp|cxx|hpp|hh)\b/.test(normalized)) return true;
    if (normalized.includes("/fe:") || normalized.includes("/fo:") || normalized.includes("-o ")) return true;
    if (normalized.includes("link ") || normalized.includes("link.exe")) return true;
    // "call ...vcvars64.bat && cl ..." without explicit sources still real work.
    if (hasVCEnv && hasCl) return true;
    return false;
}

function codingAgentCommandLooksLikeEnvironmentProbe(command: string | undefined): boolean {
    const normalized = (command || "").trim().toLowerCase();
    if (!normalized) return false;
    // Real compile recipes often embed full VS paths — never treat as probe.
    if (codingAgentCommandLooksLikeMSVCCompile(normalized)) {
        return false;
    }
    if ([
        "--version",
        "where cl.exe",
        "where msbuild.exe",
        "where.exe",
        "get-command",
        "choco list",
        "get-itemproperty",
        "visualstudio\\sxs\\vs7",
        "vswhere",
        "launch-vsdevshell",
    ].some((marker) => normalized.includes(marker))) {
        return true;
    }
    // Install-root / VS directory probes only (never bare "Microsoft Visual Studio"
    // substring — that also appears in real vcvars compile recipes).
    if (
        normalized.includes("get-childitem")
        || normalized.includes("test-path")
        || normalized.startsWith("dir ")
    ) {
        if (
            normalized.includes("program files")
            || normalized.includes("programfiles")
            || normalized.includes("visual studio")
            || normalized.includes("mingw")
            || normalized.includes("msys64")
            || normalized.includes("chocolatey")
            || normalized.includes("vcvars")
        ) {
            return true;
        }
    }
    return false;
}

function codingAgentToolFailureLooksExpectedOrRecoverable(progress: CodingAgentProgress): boolean {
    if (normalizeCodingAgentToolOutcome(progress.outcome) !== "failed") return false;
    if (codingAgentToolFailureLooksDiagnostic(progress)) return false;
    const text = [progress.detail, progress.summary].filter(Boolean).join("\n").toLowerCase();
    if (!text) return false;
    const hardFailureMarkers = [
        "access denied",
        "fatal error",
        "linker command failed",
        "ninja:",
        "panic:",
        "permission denied",
        "traceback",
        "undefined reference",
    ];
    if (hardFailureMarkers.some((marker) => text.includes(marker))) return false;
    if (codingAgentRemoteCommandLooksExploratory(progress)) return true;
    const expectedFailureMarkers = [
        "all tests should fail",
        "driver not implemented",
        "expected behavior for a test-only",
        "expected - driver not implemented",
        "is a directory; use list_directory",
        "red light",
        "test-only",
        "tdd red",
    ];
    if (expectedFailureMarkers.some((marker) => text.includes(marker))) return true;
    const assertionMarkers = [
        "expected: (0) !=",
    ];
    const expectedContextMarkers = [
        "driver",
        "not implemented",
        "red phase",
        "red-light",
        "red_light",
        "test-only",
        "tdd",
    ];
    return assertionMarkers.some((marker) => text.includes(marker)) && expectedContextMarkers.some((marker) => text.includes(marker));
}

function codingAgentRemoteCommandLooksExploratory(progress: CodingAgentProgress): boolean {
    if ((progress.detail || "").trim().toLowerCase() !== "ssh_bash") return false;
    const command = (progress.command || "").trim().toLowerCase();
    if (!command) return false;

    // Remote agents frequently probe or prepare a workspace before deciding how
    // to proceed. A non-zero result here is useful evidence, not a task failure.
    // Keep build, test, permission, and other operational failures in the hard-failure
    // amber state by only accepting narrowly scoped inspection/setup commands.
    return [
        /^mkdir\s+-p\s+/,
        /^(?:pwd|ls|find|which|where|id|uname|whoami)(?:\s|$)/,
        /^(?:test|\[)\s+/,
        /^(?:cat|head|tail|stat|file|rg|grep)\s+/,
        /^git\s+(?:status|diff|rev-parse)\b/,
    ].some((pattern) => pattern.test(command));
}

export function normalizeCodingAgentGuardrailStatus(status?: string): CodingAgentGuardrailStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "blocked" ? normalized : "unknown";
}

export function codingAgentGuardrailStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentGuardrailStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "blocked": return "\u5df2\u62e6\u622a";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "blocked": return "Blocked";
        default: return "Unknown";
    }
}

export function codingAgentGuardrailStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentGuardrailStatus(status)) {
        case "blocked":
            return neutralAttentionTone;
        default:
            return slateTone;
    }
}

export function normalizeCodingAgentCommandStatus(status?: string): CodingAgentCommandStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "passed" || normalized === "failed" || normalized === "none" ? normalized : "unknown";
}

export function codingAgentCommandStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentCommandStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "passed": return "\u5df2\u901a\u8fc7";
            case "failed": return "\u5931\u8d25";
            case "none": return "\u672a\u8fd0\u884c";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "passed": return "Passed";
        case "failed": return "Failed";
        case "none": return "None";
        default: return "Unknown";
    }
}

export function codingAgentCommandStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentCommandStatus(status)) {
        case "passed":
            return successTone;
        case "failed":
            return codingAgentFailureTone;
        case "none":
            return slateTone;
        default:
            return slateTone;
    }
}

export function codingAgentCommandProgressTone(status: string | undefined, summary?: string, detail?: string): CodingAgentStatusTone {
    const progress = normalizeCodingAgentProgress({
        phase: "result",
        title: "",
        event: "command_summary",
        detail,
        outcome: status,
        summary,
    });
    if (codingAgentCommandFailureLooksExploratory(progress)) return neutralAttentionTone;
    return codingAgentCommandStatusTone(status);
}

export function codingAgentCommandProgressLabel(status: string | undefined, lang: string, summary?: string, detail?: string, includeNoun = false): string {
    const progress = normalizeCodingAgentProgress({
        phase: "result",
        title: "",
        event: "command_summary",
        detail,
        outcome: status,
        summary,
    });
    if (codingAgentCommandFailureLooksExploratory(progress)) {
        if (includeNoun) {
            return lang.startsWith("zh") ? "\u547d\u4ee4\u68c0\u67e5" : "Commands Check";
        }
        return lang.startsWith("zh") ? "\u68c0\u67e5" : "Check";
    }
    return codingAgentCommandStatusLabel(status, lang);
}

function codingAgentCommandFailureLooksExploratory(progress: CodingAgentProgress): boolean {
    if (normalizeCodingAgentCommandStatus(progress.outcome) !== "failed") return false;
    const text = [progress.detail, progress.summary].filter(Boolean).join("\n").toLowerCase();
    if (!text) return false;
    const hardFailureMarkers = [
        "access denied",
        "fatal error",
        "linker command failed",
        "ninja:",
        "panic:",
        "permission denied",
        "traceback",
        "undefined reference",
    ];
    if (hardFailureMarkers.some((marker) => text.includes(marker))) return false;
    const exploratoryMarkers = [
        "all tests should fail",
        "command not found",
        "could not find files for the given pattern",
        "driver not implemented",
        "environment probe",
        "expectedexpression",
        "expected expression",
        "execution_policies",
        "execution policies",
        "fullyqualifiederrorid : expectedexpression",
        "fullyqualifiederrorid : unexpectedtoken",
        "is a directory; use list_directory",
        "missingopenparenthesisinifstatement",
        "missing closing",
        "missing variable name after foreach",
        "no generator specified for -g",
        "not recognized as",
        "output was likely consumed by a pipeline filter",
        "parsererror",
        "powershell command compatibility",
        "powershell exception",
        "probe",
        "red light",
        "test-only",
        "tdd red",
        "version check",
        "where.exe",
        "authorizationmanager",
        "unexpected token",
        "unexpectedtoken",
        "在此系统上禁止运行脚本",
        "无法加载文件",
        "表达式或语句中包含意外的标记",
        "意外的标记",
        "\u65e0\u6cd5\u5c06",
        "\u8bc6\u522b\u4e3a cmdlet",
    ];
    if (exploratoryMarkers.some((marker) => text.includes(marker))) return true;
    return text.includes("expected: (0) !=") && [
        "driver",
        "not implemented",
        "red phase",
        "red-light",
        "red_light",
        "test-only",
        "tdd",
    ].some((marker) => text.includes(marker));
}

export function normalizeCodingAgentFileActivityStatus(status?: string): CodingAgentFileActivityStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "changed" || normalized === "read_only" || normalized === "none" ? normalized : "unknown";
}

export function codingAgentFileActivityStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentFileActivityStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "changed": return "\u5df2\u53d8\u66f4";
            case "read_only": return "\u4ec5\u8bfb\u53d6";
            case "none": return "\u65e0\u52a8\u4f5c";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "changed": return "Changed";
        case "read_only": return "Read only";
        case "none": return "None";
        default: return "Unknown";
    }
}

export function codingAgentFileActivityStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentFileActivityStatus(status)) {
        case "changed":
            return successTone;
        case "read_only":
            return runningTone;
        case "none":
            return slateTone;
        default:
            return slateTone;
    }
}

export function normalizeCodingAgentQualityStatus(status?: string): CodingAgentQualityStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "passed" || normalized === "warning" || normalized === "failed" ? normalized : "unknown";
}

export function codingAgentQualityStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentQualityStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "passed": return "\u5df2\u901a\u8fc7";
            case "warning": return "\u8b66\u544a";
            case "failed": return "\u672a\u901a\u8fc7";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "passed": return "Passed";
        case "warning": return "Warning";
        case "failed": return "Not Passed";
        default: return "Unknown";
    }
}

export function codingAgentQualityStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentQualityStatus(status)) {
        case "passed":
            return successTone;
        case "warning":
            return neutralAttentionTone;
        case "failed":
            return codingAgentFailureTone;
        default:
            return slateTone;
    }
}

export function normalizeCodingAgentExplorationStatus(status?: string): CodingAgentExplorationStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "explored" || normalized === "read_only" || normalized === "missing" || normalized === "not_needed" ? normalized : "unknown";
}

export function codingAgentExplorationStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentExplorationStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "explored": return "\u5df2\u641c\u7d22";
            case "read_only": return "\u5df2\u8bfb\u53d6";
            case "missing": return "\u672a\u63a2\u7d22";
            case "not_needed": return "\u4e0d\u9700\u8981";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "explored": return "Explored";
        case "read_only": return "Read";
        case "missing": return "Missing";
        case "not_needed": return "Not needed";
        default: return "Unknown";
    }
}

export function codingAgentExplorationStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentExplorationStatus(status)) {
        case "explored":
        case "read_only":
        case "not_needed":
            return successTone;
        case "missing":
            return neutralAttentionTone;
        default:
            return slateTone;
    }
}

export function normalizeCodingAgentVerificationStatus(status?: string): CodingAgentVerificationStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "passed" || normalized === "failed" || normalized === "missing" || normalized === "not_needed" ? normalized : "unknown";
}

export function codingAgentVerificationStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentVerificationStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "passed": return "\u5df2\u901a\u8fc7";
            case "failed": return "\u672a\u901a\u8fc7";
            case "missing": return "\u672a\u8fd0\u884c";
            case "not_needed": return "\u4e0d\u9700\u8981";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "passed": return "Passed";
        case "failed": return "Not Passed";
        case "missing": return "Not run";
        case "not_needed": return "Not needed";
        default: return "Unknown";
    }
}

export function codingAgentVerificationStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentVerificationStatus(status)) {
        case "passed":
        case "not_needed":
            return successTone;
        case "failed":
            return codingAgentFailureTone;
        case "missing":
            return neutralAttentionTone;
        default:
            return slateTone;
    }
}

export function normalizeCodingAgentDiffCheckStatus(status?: string): CodingAgentDiffCheckStatus {
    const normalized = (status || "").trim().toLowerCase();
    return normalized === "checked" || normalized === "skipped" || normalized === "failed" ? normalized : "unknown";
}

export function codingAgentDiffCheckStatusLabel(status: string | undefined, lang: string): string {
    const normalized = normalizeCodingAgentDiffCheckStatus(status);
    if (lang.startsWith("zh")) {
        switch (normalized) {
            case "checked": return "\u5df2\u68c0\u67e5";
            case "skipped": return "\u5df2\u8df3\u8fc7";
            case "failed": return "\u5931\u8d25";
            default: return "\u672a\u77e5";
        }
    }
    switch (normalized) {
        case "checked": return "Checked";
        case "skipped": return "Skipped";
        case "failed": return "Failed";
        default: return "Unknown";
    }
}

export function codingAgentDiffCheckStatusTone(status: string | undefined): CodingAgentStatusTone {
    switch (normalizeCodingAgentDiffCheckStatus(status)) {
        case "checked":
            return successTone;
        case "failed":
            return codingAgentFailureTone;
        case "skipped":
            return slateTone;
        default:
            return slateTone;
    }
}

import React from "react";
import type { ChatMessage } from "./useAIAssistant";

interface CodingAgentProgressTheme {
    text: string;
    fieldLabel: string;
    isDark?: boolean;
}

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

const neutralAttentionTone: CodingAgentStatusTone = {
    accent: "#64748b",
    bg: "rgba(100, 116, 139, 0.08)",
    border: "rgba(100, 116, 139, 0.20)",
};

const neutralAttentionToneDark: CodingAgentStatusTone = {
    accent: "#8a9ab0",
    bg: "rgba(138, 154, 176, 0.10)",
    border: "rgba(138, 154, 176, 0.22)",
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
    starting: { accent: "#2f5f98", bg: "rgba(47, 95, 152, 0.08)", border: "rgba(47, 95, 152, 0.22)" },
    running: { accent: "#2f5f98", bg: "rgba(47, 95, 152, 0.08)", border: "rgba(47, 95, 152, 0.22)" },
    completed: { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" },
    failed: { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" },
    retrying: neutralAttentionTone,
    skipped: { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" },
    result: { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" },
    unknown: { accent: "#2f5f98", bg: "rgba(47, 95, 152, 0.08)", border: "rgba(47, 95, 152, 0.22)" },
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
    };
}

function normalizeCodingAgentFiles(files?: string[]): string[] | undefined {
    if (!Array.isArray(files)) return undefined;
    const normalized = files.map((file) => normalizeCodingAgentTitle(file)).filter(Boolean);
    return normalized.length > 0 ? normalized : undefined;
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

export function parseCodingAgentEventProgress(content: string): CodingAgentProgress | null {
    const prefix = "Coding Agent Event:";
    const trimmed = content.trim();
    if (!trimmed.startsWith(prefix)) return null;
    try {
        const raw = JSON.parse(trimmed.slice(prefix.length).trim()) as Record<string, unknown>;
        if (raw.agent !== "coding") return null;
        const taskID = typeof raw.task_id === "string" ? raw.task_id : typeof raw.taskID === "string" ? raw.taskID : undefined;
        const files = Array.isArray(raw.files) ? raw.files.filter((file): file is string => typeof file === "string") : undefined;
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

export function renderCodingAgentProgressStatus(msg: ChatMessage, t: CodingAgentProgressTheme, lang: string): React.ReactNode {
    const progress = parseCodingAgentProgress(msg.content);
    if (!progress) return null;
    let tone = codingAgentProgressTone(progress);
    // In dark mode, the neutral accent #64748b has insufficient contrast on dark surfaces.
    // Swap to the brighter dark-mode variant when rendering on dark backgrounds.
    if (t.isDark && tone.accent === neutralAttentionTone.accent) {
        tone = neutralAttentionToneDark;
    }
    const agentLabel = lang.startsWith("zh") ? "\u7f16\u7a0b\u667a\u80fd\u4f53" : "Coding Agent";
    const statusText = codingAgentProgressStatusText(progress, lang);
    const metaText = codingAgentProgressMetaText(progress, lang);
    const commandPreview = codingAgentCommandPreviewText(progress, lang);
    const commandAriaText = commandPreview ? `${lang.startsWith("zh") ? "\u547d\u4ee4" : "Command"}: ${(progress.command || "").trim()}` : undefined;
    const displayText = codingAgentDisplayText(progress, lang);
    return (
        <div
            key={msg.id}
            className={codingAgentStatusClassName(progress, "chat-progress")}
            data-testid="coding-agent-progress"
            {...codingAgentStatusDataAttrs(progress, "chat-progress")}
            role="status"
            aria-live="polite"
            aria-label={displayText}
            style={{
                display: "flex",
                alignItems: "center",
                gap: "8px",
                minHeight: "28px",
                margin: "4px 0",
                padding: "5px 8px",
                border: `1px solid ${tone.border}`,
                borderRadius: "6px",
                background: tone.bg,
                color: t.text,
                fontSize: "12px",
                lineHeight: 1.35,
                whiteSpace: "normal",
                wordBreak: "break-word",
            }}
        >
            <span style={{ color: tone.accent, fontWeight: 700, flexShrink: 0 }}>{agentLabel}</span>
            <span style={{ color: tone.accent, fontWeight: 600, flexShrink: 0 }}>{statusText}</span>
            {progress.taskID && <span style={{ color: t.fieldLabel, flexShrink: 0 }}>{progress.taskID}</span>}
            {metaText && <span style={{ color: t.fieldLabel, flexShrink: 0 }}>{metaText}</span>}
            {progress.title && <span style={{ color: t.text, minWidth: 0, overflowWrap: "anywhere" }}>{progress.title}</span>}
            {commandPreview && (
                <span
                    data-testid="coding-agent-command-preview"
                    role="note"
                    aria-label={commandAriaText}
                    title={progress.command}
                    style={{
                        color: t.fieldLabel,
                        minWidth: 0,
                        marginLeft: "auto",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                        fontSize: "11px",
                        maxWidth: "min(44ch, 42%)",
                    }}
                >
                    {commandPreview}
                </span>
            )}
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

export function codingAgentDisplayText(progress: CodingAgentProgress, lang: string): string {
    const normalized = normalizeCodingAgentProgress(progress);
    const agentLabel = lang.startsWith("zh") ? "\u7f16\u7a0b\u667a\u80fd\u4f53" : "Coding Agent";
    return [agentLabel, codingAgentProgressStatusText(normalized, lang), normalized.taskID, codingAgentProgressMetaText(normalized, lang), normalized.title].filter(Boolean).join(" | ");
}

export function codingAgentVariantDisplayText(progress: CodingAgentProgress, lang: string, variant: CodingAgentStatusVariant): string {
    if (variant !== "sidebar") return codingAgentDisplayText(progress, lang);
    const normalized = normalizeCodingAgentProgress(progress);
    const agentLabel = lang.startsWith("zh") ? "\u7f16\u7a0b\u667a\u80fd\u4f53" : "Coding Agent";
    const taskStatusLabel = lang.startsWith("zh") ? "\u4efb\u52a1\u72b6\u6001" : "Task status";
    return [agentLabel, taskStatusLabel, codingAgentProgressStatusText(normalized, lang), normalized.taskID, codingAgentProgressMetaText(normalized, lang), normalized.title, codingAgentFilePreviewText(normalized, lang)].filter(Boolean).join(" | ");
}

export function codingAgentCompactText(progress: CodingAgentProgress, lang: string): string {
    const normalized = normalizeCodingAgentProgress(progress);
    const agentLabel = lang.startsWith("zh") ? "\u7f16\u7a0b\u667a\u80fd\u4f53" : "Coding Agent";
    return [agentLabel, codingAgentProgressStatusText(normalized, lang), normalized.taskID, codingAgentProgressMetaText(normalized, lang)].filter(Boolean).join(" | ");
}

export function codingAgentProgressMetaText(progress: CodingAgentProgress, lang: string): string | undefined {
    const normalized = normalizeCodingAgentProgress(progress);
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
    const filesLabel = lang.startsWith("zh") ? "\u53d8\u66f4\u6587\u4ef6" : "Files";
    const traceLabel = lang.startsWith("zh") ? "\u8f68\u8ff9" : "Trace";
    const guardLabel = lang.startsWith("zh") ? "\u8fb9\u754c" : "Guard";
    const commandLabel = lang.startsWith("zh") ? "\u547d\u4ee4" : "Commands";
    const fileActivityLabel = lang.startsWith("zh") ? "\u6587\u4ef6\u52a8\u4f5c" : "Activity";
    const qualityLabel = lang.startsWith("zh") ? "\u8d28\u91cf" : "Quality";
    const exploreLabel = lang.startsWith("zh") ? "\u63a2\u7d22" : "Explore";
    const verifyLabel = lang.startsWith("zh") ? "\u9a8c\u8bc1" : "Verify";
    const diffCheckLabel = lang.startsWith("zh") ? "Diff \u81ea\u68c0" : "Diff check";
    const diffLabel = "Diff";
    const files = snapshot.files?.length ? codingAgentFilePreviewText({ ...latest, files: snapshot.files }, lang) : undefined;
    const trace = codingAgentToolTraceText(snapshot, lang);
    return [
        codingAgentVariantDisplayText(latest, lang, "sidebar"),
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
    ].filter(Boolean).join(" | ");
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "failed":
            return { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" };
        case "blocked":
            return neutralAttentionTone;
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
    }
}

function codingAgentToolFailureLooksDiagnostic(progress: CodingAgentProgress): boolean {
    if (normalizeCodingAgentToolOutcome(progress.outcome) !== "failed") return false;
    const text = [progress.detail, progress.command, progress.summary].filter(Boolean).join("\n").toLowerCase();
    const hardFailureMarkers = [
        "assert",
        "access denied",
        "build stopped",
        "check(",
        "check (",
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
    ];
    if (hardFailureMarkers.some((marker) => text.includes(marker))) return false;
    if ((progress.severity || "").trim().toLowerCase() === "diagnostic") return true;
    if (!text) return false;
    if (codingAgentCommandLooksLikeEnvironmentProbe(progress.command)) return true;
    if (codingAgentCommandLooksLikeExploratoryTest(progress.command, progress.summary)) return true;
    const toolName = (progress.detail || "").trim().toLowerCase();
    if (["read_file", "list_directory", "ripgrep", "glob"].includes(toolName) && codingAgentPathLookupLooksUnsuccessful(text)) {
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
        "probe",
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
        "not found:",
        "文件不存在",
        "路径不存在",
        "找不到文件",
        "找不到路径",
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
    if (["read_file", "list_directory", "ripgrep", "glob"].includes(detail) && codingAgentPathLookupLooksUnsuccessful(summary)) {
        return label("File or path not found", "文件或路径不存在");
    }
    const commandUnavailable = ["command not found", "not recognized as", "\u65e0\u6cd5\u5c06", "\u8bc6\u522b\u4e3a cmdlet"].some((marker) => summary.includes(marker));
    if (command.includes("clang++") && (commandUnavailable || command.includes("--version"))) return label("clang++ not found", "clang++ \u4e0d\u5b58\u5728");
    if (command.includes("choco") && commandUnavailable) return label("choco not found", "choco \u4e0d\u5b58\u5728");
    if (command.includes("visualstudio") || command.includes("microsoft visual studio") || command.includes("get-itemproperty")) {
        return label("Visual Studio path not found", "Visual Studio \u8def\u5f84\u4e0d\u5b58\u5728");
    }
    if (command.includes("where cl.exe")) return label("cl.exe not found", "cl.exe \u4e0d\u5b58\u5728");
    return label("Environment probe did not succeed", "\u73af\u5883\u63a2\u6d4b\u672a\u6210\u529f");
}

function codingAgentCommandLooksLikeEnvironmentProbe(command: string | undefined): boolean {
    const normalized = (command || "").trim().toLowerCase();
    if (!normalized) return false;
    return [
        "--version",
        "where cl.exe",
        "where msbuild.exe",
        "where.exe",
        "get-command",
        "choco list",
        "get-itemproperty",
        "visualstudio\\sxs\\vs7",
    ].some((marker) => normalized.includes(marker));
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
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "failed":
            return { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" };
        case "none":
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "read_only":
            return { accent: "#2f5f98", bg: "rgba(47, 95, 152, 0.08)", border: "rgba(47, 95, 152, 0.22)" };
        case "none":
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "warning":
            return neutralAttentionTone;
        case "failed":
            return { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" };
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "missing":
            return neutralAttentionTone;
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "failed":
            return { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" };
        case "missing":
            return neutralAttentionTone;
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
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
            return { accent: "#4f7f6f", bg: "rgba(79, 127, 111, 0.08)", border: "rgba(79, 127, 111, 0.22)" };
        case "failed":
            return { accent: "#c43d34", bg: "rgba(196, 61, 52, 0.08)", border: "rgba(196, 61, 52, 0.22)" };
        case "skipped":
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
        default:
            return { accent: "#64748b", bg: "rgba(100, 116, 139, 0.08)", border: "rgba(100, 116, 139, 0.20)" };
    }
}

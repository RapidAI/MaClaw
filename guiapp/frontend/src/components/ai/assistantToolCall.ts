/** One tool invocation kept in the assistant transcript, not the 3-line progress tray. */
export interface AssistantToolCall {
    id: string;
    /** Raw tool name when the progress card includes one, otherwise the action label. */
    name: string;
    /** Localized action, e.g. "执行命令". Empty when it repeats the name. */
    action: string;
    /** Arguments or command text. */
    detail: string;
}

const TOOL_STATUS_LINE = /^(?:工具|Tool)\s*·\s*(.+)$/i;
const TOOL_NAME_IN_PARENS = /^(.*?)\s*\(([A-Za-z][A-Za-z0-9_.:-]*)\)\s*$/;
const PROGRESS_ONLY_ACTION = /^(?:命令进度|脚本进度|技能进度|command progress|script progress|skill progress)$/i;

function toolMarkerPattern(): RegExp {
    return /<!--maclaw-tool:([A-Za-z0-9_-]+)-->/g;
}

export function assistantToolCallMarker(id: string): string {
    return `<!--maclaw-tool:${id}-->`;
}

export function stripAssistantToolCallMarkers(text: string): string {
    if (!text || !text.includes("maclaw-tool:")) return text;
    return text.replace(toolMarkerPattern(), "").replace(/\n{3,}/g, "\n\n");
}

export function appendToolCallMarker(content: string, id: string): string {
    const marker = assistantToolCallMarker(id);
    if (!content) return `\n\n${marker}\n\n`;
    const gap = content.endsWith("\n\n") ? "" : content.endsWith("\n") ? "\n" : "\n\n";
    return `${content}${gap}${marker}\n\n`;
}

export function contentHasAssistantToolCall(text: string | undefined): boolean {
    return !!text && text.includes("maclaw-tool:");
}

/**
 * Completed body for a tool-using turn.
 * The terminal answer is the model's final message, stored separately from
 * the live transcript. Older history has no such answer: show the prose
 * after the last call, and if the transcript ends on a call, show the prose
 * that remains so the bubble is not blank.
 */
export function completedAssistantSummary(content: string, resultText?: string): string {
    const answer = stripAssistantToolCallMarkers(resultText || "").trim();
    if (answer) return answer;
    const source = content || "";
    if (!source.includes("maclaw-tool:")) return source.trim();
    const parts = source
        .split(/<!--maclaw-tool:[A-Za-z0-9_-]+-->/g)
        .map((part) => part.trim())
        .filter(Boolean);
    if (!parts.length) return "";
    if (/<!--maclaw-tool:[A-Za-z0-9_-]+-->\s*$/.test(source)) return parts.join("\n\n");
    return parts[parts.length - 1];
}

const ASSISTANT_TASK_CANCELED_LINE = "任务已经应用户要求取消";

/** A finished tool turn is incomplete when the loop failed or the user cancelled it. */
export function assistantTaskSettledIncomplete(resultStatus: string | undefined, summary: string): boolean {
    if (resultStatus === "incomplete") return true;
    if (resultStatus === "completed") return false;
    return (summary || "").includes(ASSISTANT_TASK_CANCELED_LINE);
}

/** Progress ticks ("命令进度") stay in the live tray. The call itself belongs in the transcript. */
export function isProgressOnlyToolAction(action: string): boolean {
    return PROGRESS_ONLY_ACTION.test(action.trim());
}

export function parseAssistantToolStatus(text: string): Omit<AssistantToolCall, "id"> | null {
    const trimmed = text.trim();
    if (!trimmed) return null;
    const lines = trimmed.split(/\r?\n/);
    const headerMatch = lines[0].trim().match(TOOL_STATUS_LINE);
    if (!headerMatch) return null;
    let header = headerMatch[1].trim();
    let name = "";
    const paren = header.match(TOOL_NAME_IN_PARENS);
    if (paren) {
        header = paren[1].trim();
        name = paren[2].trim();
    }
    const action = header;
    if (!name) name = action;
    const detail = lines.slice(1).join("\n").trim();
    if (!name && !detail) return null;
    return { name, action, detail };
}

export function isTranscriptToolCallText(text: string): boolean {
    const parsed = parseAssistantToolStatus(text);
    return !!parsed && !isProgressOnlyToolAction(parsed.action);
}

export function transcriptAlreadyShowsToolCall(
    messages: Array<{ role?: string; toolCalls?: AssistantToolCall[] }>,
    progressText: string,
): boolean {
    const parsed = parseAssistantToolStatus(progressText);
    if (!parsed || isProgressOnlyToolAction(parsed.action)) return false;
    // Only the reply that is still being written can own the live tray line.
    // An older identical call must not hide a new one.
    for (let i = messages.length - 1; i >= 0; i--) {
        const message = messages[i];
        if (message.role !== "assistant") continue;
        return !!message.toolCalls?.some((call) =>
            call.name === parsed.name && call.action === parsed.action && call.detail === parsed.detail);
    }
    return false;
}

export interface AssistantToolCallSegment {
    kind: "text" | "tool";
    text?: string;
    call?: AssistantToolCall;
}

/** Split display text around embedded tool markers. Unknown markers are dropped. */
export function splitAssistantToolCallContent(content: string, calls: AssistantToolCall[] | undefined): AssistantToolCallSegment[] {
    const byId = new Map((calls || []).map((call) => [call.id, call]));
    const seen = new Set<string>();
    const segments: AssistantToolCallSegment[] = [];
    const source = content || "";
    const marker = toolMarkerPattern();
    let cursor = 0;
    let match: RegExpExecArray | null;
    while ((match = marker.exec(source))) {
        const chunk = source.slice(cursor, match.index);
        if (chunk.trim()) segments.push({ kind: "text", text: chunk.trim() });
        const call = byId.get(match[1]);
        if (call) {
            seen.add(call.id);
            segments.push({ kind: "tool", call });
        }
        cursor = match.index + match[0].length;
    }
    const tail = source.slice(cursor);
    if (tail.trim()) segments.push({ kind: "text", text: tail.trim() });
    for (const call of calls || []) {
        if (!seen.has(call.id)) segments.push({ kind: "tool", call });
    }
    if (segments.length === 0 && source.trim()) segments.push({ kind: "text", text: source });
    return segments;
}

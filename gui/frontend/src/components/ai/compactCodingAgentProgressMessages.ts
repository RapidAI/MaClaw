import type { ChatMessage } from "./useAIAssistant";
import { parseCodingAgentProgress, type CodingAgentProgress } from "./CodingAgentProgressStatus";

export function compactCodingAgentProgressMessages(messages: ChatMessage[]): ChatMessage[] {
    let latestCodingIndex = -1;
    const parsed = new Map<number, CodingAgentProgress | null>();
    const parseAt = (index: number): CodingAgentProgress | null => {
        if (!parsed.has(index)) parsed.set(index, parseCodingAgentProgress(messages[index]?.content || ""));
        return parsed.get(index) || null;
    };
    for (let i = messages.length - 1; i >= 0; i--) {
        if (parseAt(i)) {
            latestCodingIndex = i;
            break;
        }
    }
    if (latestCodingIndex < 0) return messages;
    const latest = parseAt(latestCodingIndex);
    return messages.filter((_message, index) => {
        if (index === latestCodingIndex) return true;
        const progress = parseAt(index);
        return !progress || shouldPreserveCodingProgress(progress, latest);
    });
}

function shouldPreserveCodingProgress(progress: CodingAgentProgress | null, latest: CodingAgentProgress | null): boolean {
    if (!progress || !latest || !sameCodingProgressTurn(progress, latest)) return false;
    const event = (progress.event || "").trim().toLowerCase();
    const outcome = (progress.outcome || "").trim().toLowerCase();
    switch (event) {
        case "guardrail_summary":
            return outcome === "blocked";
        case "command_summary":
        case "quality_summary":
        case "diff_check":
            return outcome === "failed";
        case "verification_summary":
        case "exploration_summary":
            return outcome === "failed" || outcome === "missing";
        case "tool_finished":
            return outcome === "failed" || outcome === "blocked";
        default:
            return false;
    }
}

function sameCodingProgressTurn(a: CodingAgentProgress, b: CodingAgentProgress): boolean {
    if (b.turnID) return a.turnID === b.turnID;
    if (b.runID && b.taskID) return a.runID === b.runID && a.taskID === b.taskID;
    if (b.taskID) return a.taskID === b.taskID;
    return true;
}

import { stripAssistantToolCallMarkers } from "./assistantToolCall";
import { omitGoalContinuationHeader } from "./visibleChatText";

const GOAL_STAGE_COMMAND_RUNES = 80;
// formatGoalChecklistFromRecord appends this to the last checklist line when
// the goal has a token budget and no turn cap. The card strips the same tail.
const GOAL_STAGE_TOKEN_TAIL = /\s*[|｜]\s*Token[：:]\s*\d+\s*[/／]\s*\d+\s*$/i;

function capGoalStageCommand(text: string): string {
    const trimmed = text.trim().replace(/。$/, "");
    const runes = Array.from(trimmed);
    if (runes.length <= GOAL_STAGE_COMMAND_RUNES) return trimmed;
    return `${runes.slice(0, GOAL_STAGE_COMMAND_RUNES).join("")}…`;
}

function continuationPhase(prompt: string): "" | "plan" | "execute" {
    const trimmed = prompt.trim();
    if (!trimmed.startsWith("[系统续接]")) return "";
    if (trimmed.startsWith("[系统续接][execute]")) return "execute";
    if (trimmed.startsWith("[系统续接][plan]") || omitGoalContinuationHeader(prompt).includes("本轮只做规划")) return "plan";
    return "execute";
}

function stepCommandFromPrompt(prompt: string): string {
    const body = omitGoalContinuationHeader(prompt);
    const step = body.match(/本轮只能提交步骤\s*(\d+)\s*[：:]\s*([^。\n]+)/);
    if (!step) return "";
    return capGoalStageCommand(`本轮只能提交步骤 ${step[1]}：${step[2].trim()}`);
}

function planCommandFromPrompt(prompt: string): string {
    if (continuationPhase(prompt) !== "plan") return "";
    const body = omitGoalContinuationHeader(prompt);
    const objective = body.match(/(?:^|\n)\s*目标[：:]\s*([^\n]+)/);
    const title = objective?.[1]?.trim() || "";
    if (prompt.includes("只补交规划") || body.includes("只补交规划")) return capGoalStageCommand(title ? `补交分解：${title}` : "补交分解");
    return capGoalStageCommand(title ? `分解目标：${title}` : "分解目标");
}

function commandFromChecklist(displayText: string): string {
    const lines = String(displayText || "").split(/\r?\n/).map(line => line.replace(GOAL_STAGE_TOKEN_TAIL, "").trim()).filter(Boolean);
    for (const line of lines) {
        const step = line.match(/^→\s*(\d+)\s*[.、]\s*(.+)$/);
        if (step) return capGoalStageCommand(`本轮只能提交步骤 ${step[1]}：${step[2].trim()}`);
    }
    for (const line of lines) {
        const plan = line.match(/^正在分解[：:]\s*(.+)$/);
        if (plan) return capGoalStageCommand(`分解目标：${plan[1].trim()}`);
        const fix = line.match(/^正在补交分解[：:]\s*(.*)$/);
        if (fix) return capGoalStageCommand(fix[1].trim() ? `补交分解：${fix[1].trim()}` : "补交分解");
        if (line === "正在核对验收" || line.startsWith("正在核对验收")) return "正在核对验收";
    }
    return "";
}

function shortAssignment(text: string, capLong = false): string {
    const first = text.trim();
    if (!first) return "";
    // A long body line is the phase contract. A one-line goal is the command.
    if (!capLong && Array.from(first).length > GOAL_STAGE_COMMAND_RUNES) return "";
    if (first.includes("调用 goal") || /^[0-9]+[.、]/.test(first)) return "";
    if (first.startsWith("按步骤执行目标") || first.startsWith("先理解并分解")) return "";
    return capGoalStageCommand(first);
}

function shortPromptLine(prompt: string): string {
    if (continuationPhase(prompt) === "") return "";
    const first = omitGoalContinuationHeader(prompt).split(/\r?\n/).map(line => line.trim()).find(Boolean) || "";
    return shortAssignment(first);
}

/** A one-line continuation keeps its assignment on the host header line. */
function commandAfterHeader(prompt: string): string {
    if (continuationPhase(prompt) === "") return "";
    const line = prompt.split(/\r?\n/).map(item => item.trim()).find(item => item.startsWith("[系统续接]")) || "";
    const rest = line.replace(/^\[系统续接\](?:\[(?:plan|execute)\])?\s*/, "");
    return shortAssignment(rest, true);
}

/** The one line a goal stage shows as its task command. The phase contract stays off the transcript. */
export function goalStageCommandFromRound(prompt: string, displayText: string): string {
    return stepCommandFromPrompt(prompt)
        || planCommandFromPrompt(prompt)
        || commandFromChecklist(displayText)
        || shortPromptLine(prompt)
        || commandAfterHeader(prompt);
}

/** A goal stage has filed a report the transcript can keep under the folded process. */
export function goalStageHasReport(requestId: string | undefined, content: string | undefined): boolean {
    if (!String(requestId || "").startsWith("goal-cont-")) return false;
    const body = stripAssistantToolCallMarkers(omitGoalContinuationHeader(String(content || ""))).trim();
    return !!body;
}

/**
 * A goal stage that already filed its report folds while the goal is still busy.
 * Any other turn stays open only while that turn is streaming or busy.
 */
export function goalStageProcessLive(messageOwnsLive: boolean, streaming: boolean, busy: boolean, goalStageSettled?: boolean): boolean {
    if (goalStageSettled) return false;
    return messageOwnsLive && (streaming || busy);
}

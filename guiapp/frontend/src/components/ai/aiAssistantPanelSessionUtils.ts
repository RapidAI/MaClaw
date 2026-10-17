import { useEffect } from "react";
import { buildOutgoingMessageMulti, type ChatMessage } from "./useAIAssistant";
import { expertTabId } from "./expertTypes";
import { cloudWorkspaceIdFromPath } from "./codingTaskMode";

/** Keep in sync with LATEX_EXPERT_ID. Local so this module does not import the template catalogue. */
const LATEX_PAPER_EXPERT_ID = "builtin-latex-paper";

const MAX_PROJECT_CONTEXT_MESSAGES_TO_SEND = 12;
const PROJECT_TABS_STORAGE_KEY = "ai_assistant_project_tabs";
const PROJECT_TAB_HISTORY_STORAGE_KEY = "ai_assistant_project_tab_histories";

const HOST_GOAL_CHECKLIST_PREFIX = /^(?:目标清单|目標清單|Goal checklist)/;
const HOST_GOAL_SLASH_PROMPT = /^\/goal\s+(执行步骤|继续步骤|分解目标|核对并完成|提交分解|继续推进目标|Continue goal)/;
const GENERIC_GOAL_CHECKLIST_PLACEHOLDERS = new Set([
    "Goal checklist\nContinuing the current step",
    "目标清单\n正在继续当前步骤",
    "目標清單\n正在繼續目前步驟",
]);

export function isHostGoalChecklistText(content: string): boolean {
    const text = content.trim();
    if (!text) return false;
    if (HOST_GOAL_CHECKLIST_PREFIX.test(text)) return true;
    return HOST_GOAL_SLASH_PROMPT.test(text);
}

function isVisibleHostGoalChecklist(message: ChatMessage): boolean {
    if (message.kind === "goalChecklist") return true;
    return message.role === "user" && isHostGoalChecklistText(message.content || "");
}

type HostGoalStepSnap = { index: number; title: string };

function hostGoalText(content: string): { first: string; rest: string } {
    const text = String(content || "").replace(/\r\n/g, "\n").trim();
    const lines = text.split("\n");
    return { first: (lines[0] || "").trim(), rest: lines.slice(1).join("\n").trim() };
}

function hostGoalStepSnap(tail: string): HostGoalStepSnap | null {
    const step = tail.match(/^(\d+)\s*[/／]\s*\d+\s*[:：]\s*(.+)$/) || tail.match(/^(\d+)\s*[:：]\s*(.+)$/);
    if (!step) return null;
    const index = Number(step[1]);
    const title = step[2].trim();
    if (!Number.isFinite(index) || index <= 0 || !title) return null;
    return { index, title };
}

/** Turn a stored host continuation line into the checklist body. A published checklist is kept. */
export function rewriteHostGoalChecklistBody(content: string): string {
    const { first, rest } = hostGoalText(content);
    const text = rest ? `${first}\n${rest}` : first;
    if (!text) return "目标清单\n正在继续当前步骤";
    if (HOST_GOAL_CHECKLIST_PREFIX.test(text)) return text;
    const match = first.match(/^\/goal\s+(执行步骤|继续步骤|分解目标|核对并完成|提交分解|继续推进目标|Continue goal)\s*[:：]?\s*(.*)$/);
    let body = "目标清单\n正在继续当前步骤";
    if (match) {
        const verb = match[1];
        const tail = (match[2] || "").trim();
        if (verb === "Continue goal") {
            body = tail ? `Goal checklist\nContinuing: ${tail}` : "Goal checklist\nContinuing the current step";
        } else if (verb === "分解目标") {
            body = tail ? `目标清单\n正在分解：${tail}` : "目标清单\n正在分解";
        } else if (verb === "提交分解") {
            body = tail ? `目标清单\n正在补交分解：${tail}` : "目标清单\n正在补交分解";
        } else if (verb === "核对并完成") {
            body = "目标清单\n正在核对验收";
        } else if (verb === "继续推进目标") {
            body = tail ? `目标清单\n正在继续：${tail}` : "目标清单\n正在继续当前步骤";
        } else {
            const step = hostGoalStepSnap(tail);
            if (step) body = `目标清单\n→ ${step.index}. ${step.title}`;
            else if (tail) body = `目标清单\n→ ${tail}`;
        }
    }
    return rest ? `${body}\n${rest}` : body;
}

/**
 * Old continuations stored one slash line per turn, not the step list.
 * Fold those lines into one checklist. The line is the prompt that opened
 * the turn, so it has no step result: only the last announced step is
 * current. Earlier titles stay unmarked. Real ✓ / → / ○ / × come from the
 * goal record. A published checklist already has that list and is not rebuilt.
 */
function checklistFromHostSnapshots(messages: ChatMessage[]): string {
    const steps = new Map<number, string>();
    let current = 0;
    let progress = "";
    for (const message of messages) {
        if (!isVisibleHostGoalChecklist(message)) continue;
        const { first, rest } = hostGoalText(message.content || "");
        if (HOST_GOAL_CHECKLIST_PREFIX.test(first)) continue;
        const match = first.match(/^\/goal\s+(执行步骤|继续步骤)\s*[:：]?\s*(.*)$/);
        const step = match ? hostGoalStepSnap((match[2] || "").trim()) : null;
        if (!step) continue;
        steps.set(step.index, step.title);
        current = step.index;
        if (rest.includes("进度")) progress = rest;
    }
    if (steps.size === 0 || current <= 0) return "";
    const lines = ["目标清单"];
    for (const index of [...steps.keys()].sort((left, right) => left - right)) {
        const title = `${index}. ${steps.get(index)}`;
        lines.push(index === current ? `→ ${title}` : title);
    }
    if (progress) lines.push(progress);
    return lines.join("\n");
}

export type GoalChecklistRecord = {
    user_id?: string;
    objective?: string;
    phase?: string;
    steps?: Array<{ index?: number; title?: string; status?: string } | null> | null;
    turns_used?: number;
    max_turns?: number;
    tokens_used?: number;
    token_budget?: number;
};

type GoalStepMark = "done" | "progress" | "failed" | "pending" | "other";

function goalRecordCount(value: unknown): number {
    const parsed = typeof value === "number" ? value : Number(String(value ?? "").trim());
    if (!Number.isFinite(parsed) || parsed < 0) return 0;
    return Math.trunc(parsed);
}

function goalStepMark(status: unknown): GoalStepMark {
    const key = String(status ?? "").trim().toLowerCase();
    if (key === "" || key === "pending") return "pending";
    if (key === "done" || key === "complete" || key === "completed") return "done";
    if (key === "progress" || key === "in_progress") return "progress";
    if (key === "failed" || key === "fail") return "failed";
    return "other";
}

/** Same checklist body as the host continuation display. Empty means "do not replace". */
export function formatGoalChecklistFromRecord(input: GoalChecklistRecord | null | undefined): string {
    if (!input || typeof input !== "object") return "";
    const phase = String(input.phase || "").trim().toLowerCase();
    const stepsMissing = input.steps == null;
    if (stepsMissing && phase === "execute") return "";
    if (stepsMissing && phase !== "plan") return "";
    const steps = Array.isArray(input.steps) ? input.steps.filter((step): step is NonNullable<typeof step> => !!step && typeof step === "object") : [];
    const lines = ["目标清单"];
    if (phase === "plan" || steps.length === 0) {
        const objective = String(input.objective || "").trim() || "当前目标";
        lines.push(`正在分解：${objective}`);
    } else {
        let current = 0;
        steps.forEach((step, position) => {
            if (current > 0) return;
            const mark = goalStepMark(step.status);
            if (mark === "pending" || mark === "progress") {
                const index = Number(step.index);
                current = Number.isFinite(index) && index > 0 ? Math.trunc(index) : position + 1;
            }
        });
        steps.forEach((step, position) => {
            const index = Number(step.index);
            const stepIndex = Number.isFinite(index) && index > 0 ? Math.trunc(index) : position + 1;
            const title = String(step.title || "").trim();
            const mark = goalStepMark(step.status);
            let glyph = "○";
            if (mark === "done") glyph = "✓";
            else if (mark === "failed") glyph = "×";
            else if (mark === "progress" || stepIndex === current) glyph = "→";
            lines.push(`${glyph} ${stepIndex}. ${title}`);
        });
        if (current <= 0) lines.push("正在核对验收");
    }
    const maxTurns = goalRecordCount(input.max_turns);
    if (maxTurns > 0) lines.push(`进度：第 ${goalRecordCount(input.turns_used) + 1}/${maxTurns} 轮`);
    const tokenBudget = goalRecordCount(input.token_budget);
    if (tokenBudget > 0) {
        const token = ` | Token: ${goalRecordCount(input.tokens_used)}/${tokenBudget}`;
        lines[lines.length - 1] += token;
    }
    return lines.join("\n");
}

export function goalChecklistMessageId(sessionKey?: string | null): string {
    return `goal-checklist:${normalizeAssistantSessionKey(sessionKey) || "desktop-user"}`;
}

export type ProjectTabLiveHistory = { tabId: string; messages: ChatMessage[] };

/**
 * The open tab's message state lags behind the active tab id.
 * The same array keeps its previous tab. The label moves only when this
 * render supplies a different list, so a repeated render cannot retag it.
 */
export function noteProjectTabLiveHistory(
    previous: ProjectTabLiveHistory,
    activeTabId: string,
    messages: ChatMessage[],
): ProjectTabLiveHistory {
    if (previous.tabId !== activeTabId && messages === previous.messages) return previous;
    return { tabId: activeTabId, messages };
}

/** Messages only while this list still belongs to the tab being updated. */
export function projectTabLiveMessages(live: ProjectTabLiveHistory, tabId: string): ChatMessage[] | undefined {
    return live.tabId === tabId ? live.messages : undefined;
}

/**
 * Put the goal record on the transcript the user is looking at.
 * An empty or cleared tab stays empty: one inserted card would turn history
 * non-empty and let the live sync copy residual messages back in.
 * The open tab's live list wins over the saved snapshot, which lags behind it.
 * Returns null when nothing should be written.
 */
export function nextGoalChecklistHistory(input: {
    history?: ChatMessage[] | null;
    liveHistory?: ChatMessage[] | null;
    cleared?: boolean;
    sessionKey: string;
    content: string;
}): ChatMessage[] | null {
    if (input.cleared) return null;
    const stored = Array.isArray(input.history) ? input.history : [];
    const live = Array.isArray(input.liveHistory) ? input.liveHistory : [];
    const base = live.length > 0 ? live : stored;
    if (base.length === 0) return null;
    const next = upsertGoalChecklistInHistory(base, input.sessionKey, input.content);
    return next === base ? null : next;
}

/** Replace the one checklist card for this session. Same content keeps the stored row. */
export function upsertGoalChecklistInHistory(history: ChatMessage[], sessionKey: string, content: string): ChatMessage[] {
    const text = String(content || "");
    if (!text.trim()) return history;
    const id = goalChecklistMessageId(sessionKey);
    const owner = normalizeAssistantSessionKey(sessionKey) || "desktop-user";
    const index = history.findIndex(message => message.id === id);
    const existing = index >= 0 ? history[index] : undefined;
    if (existing && existing.role === "system" && existing.kind === "goalChecklist" && existing.content === text && (normalizeAssistantSessionKey(existing.sessionKey) || "desktop-user") === owner) {
        return history;
    }
    const next: ChatMessage = {
        id,
        role: "system",
        kind: "goalChecklist",
        content: text,
        sessionKey: owner,
        timestamp: existing?.timestamp || Date.now(),
    };
    if (index < 0) return [...history, next];
    return history.map((message, at) => at === index ? { ...message, ...next } : message);
}

/**
 * The goal owns one checklist. Historical "/goal 执行步骤" rows are earlier
 * snapshots of that card. Show it once, directly above the latest goal
 * continuation reply. A later message from the user does not take the card.
 */
export function collapseHostGoalChecklist(messages: ChatMessage[]): ChatMessage[] {
    if (!Array.isArray(messages) || messages.length === 0) return messages;
    let lastHost = -1;
    let lastSlash = -1;
    let lastPublished = -1;
    let hostCount = 0;
    for (let index = 0; index < messages.length; index++) {
        if (!isVisibleHostGoalChecklist(messages[index])) continue;
        hostCount++;
        lastHost = index;
        const content = String(messages[index].content || "").trim();
        if (HOST_GOAL_CHECKLIST_PREFIX.test(content) && !GENERIC_GOAL_CHECKLIST_PLACEHOLDERS.has(content)) {
            lastPublished = index;
        } else if (!HOST_GOAL_CHECKLIST_PREFIX.test(content)) {
            lastSlash = index;
        }
    }
    if (hostCount === 0 || lastHost < 0) return messages;
    const sourceIndex = lastPublished >= 0 ? lastPublished : (lastSlash >= 0 ? lastSlash : lastHost);
    const source = messages[sourceIndex];
    const content = (lastPublished >= 0 ? "" : checklistFromHostSnapshots(messages))
        || rewriteHostGoalChecklistBody(source.content || "");
    const card: ChatMessage = source.role === "system" && source.kind === "goalChecklist" && source.content === content
        ? source
        : { ...source, role: "system", kind: "goalChecklist", content };
    const anchor = goalChecklistAnchorIndex(messages);
    const parked = anchor < 0 ? sourceIndex === messages.length - 1 : sourceIndex === anchor - 1;
    if (hostCount === 1 && card === source && parked) return messages;
    const anchorMessage = anchor >= 0 ? messages[anchor] : null;
    const rest: ChatMessage[] = [];
    let placed = false;
    for (let index = 0; index < messages.length; index++) {
        const message = messages[index];
        if (index === sourceIndex || isVisibleHostGoalChecklist(message)) continue;
        if (!placed && anchorMessage && message === anchorMessage) {
            rest.push(card);
            placed = true;
        }
        rest.push(message);
    }
    if (!placed) rest.push(card);
    return rest;
}

/** Latest goal-continuation reply. Older transcripts with no request id use the last assistant. */
function goalChecklistAnchorIndex(messages: ChatMessage[]): number {
    let goalTurn = -1;
    let lastAssistant = -1;
    for (let index = 0; index < messages.length; index++) {
        if (messages[index].role !== "assistant") continue;
        lastAssistant = index;
        if (String(messages[index].requestId || "").startsWith("goal-cont-")) goalTurn = index;
    }
    return goalTurn >= 0 ? goalTurn : lastAssistant;
}

export function buildProjectTabRecentMessages(history: ChatMessage[] | undefined): Array<{ role: 'user' | 'assistant'; content: string }> {
    if (!Array.isArray(history) || history.length === 0) return [];
    return history
        .map(message => {
            if (message.role !== 'user' && message.role !== 'assistant') return null;
            if (message.role === 'user' && isHostGoalChecklistText(message.content || '')) return null;
            // A guide-injection bubble records steering that was consumed by the
            // active loop. Do not replay it as an ordinary historical user turn
            // when this project/expert tab starts a later request.
            if (message.kind === 'guideInjection') return null;
            const visibleContent = String(message.content || '').trim();
            const attachmentPaths = message.role === 'user'
                ? (message.attachments || []).map(attachment => attachment.filePath).filter(Boolean)
                : [];
            const content = attachmentPaths.length > 0
                ? [visibleContent, buildOutgoingMessageMulti('', attachmentPaths)].filter(Boolean).join('\n\n')
                : visibleContent;
            if (!content) return null;
            return { role: message.role, content };
        })
        .filter((message): message is { role: 'user' | 'assistant'; content: string } => message !== null)
        .slice(-MAX_PROJECT_CONTEXT_MESSAGES_TO_SEND);
}

export function messageBelongsToSession(message: ChatMessage, sessionKey: string): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return owner === normalizeAssistantSessionKey(sessionKey);
}

export function messageBelongsToSessionOrLegacy(message: ChatMessage, sessionKey: string): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return !owner || owner === normalizeAssistantSessionKey(sessionKey);
}

/** Drop transcript turns that belong to a conversation cleared at floorMs. */
export function messageAfterSessionFloor(message: { timestamp?: number } | null | undefined, floorMs?: number | null): boolean {
    const floor = Number(floorMs || 0);
    if (!Number.isFinite(floor) || floor <= 0) return true;
    const ts = Number(message?.timestamp || 0);
    return Number.isFinite(ts) && ts >= floor;
}

/**
 * Copy an expert transcript onto the project tab of the paper it belongs to.
 * Session keys are rewritten so the project view does not filter them out.
 * Returns null when there is no user turn worth keeping.
 */
export function archiveExpertTranscriptForProject(
    projectPath: string | null | undefined,
    history: unknown[] | null | undefined,
): { projectPath: string; history: ChatMessage[] } | null {
    const path = normalizeProjectSessionPath(projectPath);
    if (!path || !Array.isArray(history) || history.length === 0) return null;
    const sessionKey = projectSessionKey(path);
    const kept: ChatMessage[] = [];
    let hasUser = false;
    for (const item of history) {
        if (!item || typeof item !== "object") continue;
        const message = item as ChatMessage;
        if (message.role === "user") hasUser = true;
        kept.push({ ...message, sessionKey });
    }
    if (!hasUser || kept.length === 0) return null;
    return { projectPath: path, history: kept };
}

export function projectSessionKey(projectPath?: string | null): string {
    const path = normalizeProjectSessionPath(projectPath);
    return path ? `desktop-user:${path}` : "";
}

/** Session key prefix shared by all expert conversations: desktop-user:expert:<id>. */
const EXPERT_SESSION_KEY_PREFIX = "desktop-user:expert:";
const ACP_SESSION_KEY_PREFIX = "desktop-user:acp:";
/** Stable sidebar identity for VS Code Mode B. Session ids change on reconnect. */
export const ACP_ASSISTANT_TAB_IDENTITY = "acp";
/** Durable task-management tag that binds a workspace row to an expert id. */
const EXPERT_TASK_SOURCE_PREFIX = "source:expert:";
const ASSISTANT_TAB_TASK_SOURCE_PREFIX = "source:assistant_tab:";
const ASSISTANT_TAB_TASK_BINDINGS_KEY = "ai_assistant_task_bindings";

export type AssistantTabTaskBinding = { projectPath: string; tabType: string; tabIdentity: string; sessionId?: string; title?: string };

function readAssistantTabTaskBindings(): Record<string, AssistantTabTaskBinding> {
    if (typeof localStorage === "undefined") return {};
    try {
        const raw = localStorage.getItem(ASSISTANT_TAB_TASK_BINDINGS_KEY);
        const parsed = raw ? JSON.parse(raw) : {};
        return parsed && typeof parsed === "object" ? parsed : {};
    } catch { return {}; }
}

function writeAssistantTabTaskBindings(bindings: Record<string, AssistantTabTaskBinding>) {
    try { localStorage.setItem(ASSISTANT_TAB_TASK_BINDINGS_KEY, JSON.stringify(bindings)); } catch { /* unavailable */ }
}

export function rememberAssistantTabTaskBinding(binding: AssistantTabTaskBinding): void {
    const path = normalizeProjectSessionPath(binding.projectPath);
    const type = String(binding.tabType || "").trim().toLowerCase();
    const identity = String(binding.tabIdentity || "").trim();
    if (!path || !type || !identity) return;
    const bindings = readAssistantTabTaskBindings();
    bindings[path] = { ...bindings[path], ...binding, projectPath: path, tabType: type, tabIdentity: identity };
    writeAssistantTabTaskBindings(bindings);
}

export function updateAssistantTabTaskSession(tabType: string, tabIdentity: string, sessionId: string): void {
    const type = String(tabType || "").trim().toLowerCase();
    const identity = String(tabIdentity || "").trim();
    const session = String(sessionId || "").trim();
    if (!type || !identity || !session) return;
    const bindings = readAssistantTabTaskBindings();
    let changed = false;
    for (const [path, binding] of Object.entries(bindings)) {
        const sameType = binding.tabType === type
            || (type === "group" && (binding.tabType === "discussion" || binding.tabType === "ve"));
        if (sameType && binding.tabIdentity === identity && binding.sessionId !== session) {
            bindings[path] = { ...binding, sessionId: session };
            changed = true;
        }
    }
    if (changed) writeAssistantTabTaskBindings(bindings);
}

export function assistantTabTaskBinding(projectPath: string): AssistantTabTaskBinding | null {
    const path = normalizeProjectSessionPath(projectPath);
    if (!path) return null;
    return readAssistantTabTaskBindings()[path] || null;
}

/** Return the secondary assistant tab type encoded in a durable task row. */
export function assistantTabTypeFromTaskTags(tags?: string[] | null): string {
    for (const rawTag of tags || []) {
        const tag = String(rawTag || "").trim();
        if (!tag.startsWith(ASSISTANT_TAB_TASK_SOURCE_PREFIX)) continue;
        const type = tag.slice(ASSISTANT_TAB_TASK_SOURCE_PREFIX.length).split(":", 1)[0].trim().toLowerCase();
        if (/^[a-z0-9_-]+$/.test(type)) return type;
    }
    return "";
}

/** Sidebar rows minted for a digital-employee chat. Those chats are not local tasks. */
export function isVEAssistantTabTaskItem(item: { tags?: unknown } | null | undefined): boolean {
    if (!item || typeof item !== "object") return false;
    const tags = Array.isArray(item.tags) ? item.tags.map((tag) => String(tag)) : [];
    return assistantTabTypeFromTaskTags(tags) === "ve";
}

/** Sidebar snapshot/list rows created for VS Code Mode B. */
export function isAutoACPAssistantTabTaskItem(item: { project_path?: unknown; tags?: unknown; name?: unknown } | null | undefined): boolean {
    if (!item || typeof item !== "object") return false;
    const tags = Array.isArray(item.tags) ? item.tags.map((tag) => String(tag)) : [];
    if (assistantTabTypeFromTaskTags(tags) === "acp") return true;
    const path = normalizeProjectSessionPath(typeof item.project_path === "string" ? item.project_path : "");
    const base = path.split("/").filter(Boolean).pop() || "";
    const dash = base.lastIndexOf("-");
    const slug = dash > 0 && /^\d{10,}$/.test(base.slice(dash + 1)) ? base.slice(0, dash) : base;
    if (slug.toLowerCase() !== "vs-code-acp") return false;
    const name = typeof item.name === "string" ? item.name.trim().toLowerCase() : "";
    return name === "" || name === "vs code / acp";
}

/** Return the expert identity carried by a durable task-management row. */
export function expertIDFromTaskTags(tags?: string[] | null): string {
    for (const rawTag of tags || []) {
        const tag = String(rawTag || "").trim();
        if (!tag.startsWith(EXPERT_TASK_SOURCE_PREFIX)) continue;
        const expertID = tag.slice(EXPERT_TASK_SOURCE_PREFIX.length).trim();
        if (expertID) return expertID;
    }
    return "";
}

/**
 * The durable task-list row that corresponds to the currently visible AI tab.
 * Local / VE / group tabs have no task-list row, so this is null and the
 * sidebar highlight must clear.
 */
export type ActiveAssistantTaskIdentity = {
    projectPath?: string;
    expertId?: string;
    cloudWorkspaceId?: string;
};

/** Collapse empty / mixed identities to null so the sidebar highlight can clear. */
export function coerceActiveAssistantTask(
    identity?: ActiveAssistantTaskIdentity | null,
): ActiveAssistantTaskIdentity | null {
    if (!identity) return null;
    const expertId = String(identity.expertId || "").trim();
    if (expertId) {
        // Each LaTeX paper is its own sidebar row. Keep the path so the
        // highlight lands on the paper that is actually open.
        if (expertId === LATEX_PAPER_EXPERT_ID) {
            const projectPath = normalizeProjectSessionPath(identity.projectPath);
            return projectPath ? { expertId, projectPath } : { expertId };
        }
        return { expertId };
    }
    const projectPath = normalizeProjectSessionPath(identity.projectPath);
    if (!projectPath) return null;
    const cloudWorkspaceId = String(identity.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(projectPath);
    return cloudWorkspaceId ? { projectPath, cloudWorkspaceId } : { projectPath };
}

export function sameActiveAssistantTask(
    a?: ActiveAssistantTaskIdentity | null,
    b?: ActiveAssistantTaskIdentity | null,
): boolean {
    const left = coerceActiveAssistantTask(a);
    const right = coerceActiveAssistantTask(b);
    return (left?.expertId || "") === (right?.expertId || "")
        && (left?.projectPath || "") === (right?.projectPath || "")
        && (left?.cloudWorkspaceId || "") === (right?.cloudWorkspaceId || "");
}

export function activeAssistantTaskIdentity(tab: {
    type?: string;
    projectPath?: string;
    cloudWorkspaceId?: string;
    expertId?: string;
} | null | undefined, workingDir?: string | null): ActiveAssistantTaskIdentity | null {
    if (!tab) return null;
    if (tab.type === "expert") return coerceActiveAssistantTask({ expertId: tab.expertId, projectPath: tab.projectPath });
    if (tab.type === "project") {
        return coerceActiveAssistantTask({
            projectPath: tab.projectPath || workingDir || undefined,
            cloudWorkspaceId: String(tab.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(tab.projectPath) || cloudWorkspaceIdFromPath(workingDir),
        });
    }
    return null;
}

/** Session key for an expert tab conversation. Aligns with the backend userID (ExpertID branch). */
export function expertSessionKey(expertId?: string | null): string {
    const id = String(expertId || "").trim();
    return id ? `${EXPERT_SESSION_KEY_PREFIX}${id}` : "";
}

export function isExpertSessionKey(sessionKey?: string | null): boolean {
    return typeof sessionKey === "string" && sessionKey.trim().startsWith(EXPERT_SESSION_KEY_PREFIX);
}

export function isACPAssistantSessionKey(sessionKey?: string | null): boolean {
    return typeof sessionKey === "string" && sessionKey.trim().startsWith(ACP_SESSION_KEY_PREFIX);
}

/**
 * Discussion tabs must not open if their sidebar row cannot be saved.
 * A digital-employee chat has no task-list row, so a missing registration must
 * not block the conversation. ACP is a VS Code connection: a dismissed or
 * missing sidebar row must not block the mirror tab, or a deleted
 * "VS Code / ACP" task would hide the chat.
 */
export function shouldBlockAssistantTabOpenOnTaskRegistration(tabType?: string | null): boolean {
    const type = String(tabType || "").trim().toLowerCase();
    return type !== "acp" && type !== "ve";
}

/** Reverse of expertSessionKey: extract the expert id, or "" for non-expert keys. */
export function expertIdFromSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    return key.startsWith(EXPERT_SESSION_KEY_PREFIX) ? key.slice(EXPERT_SESSION_KEY_PREFIX.length).trim() : "";
}

/**
 * Publishes every busy task identity (normalized project paths and expert
 * IDs, deduped and sorted for a stable payload) so the sidebar can mark all
 * concurrently running task rows as in progress — including runs that keep
 * executing as detached rounds after the user switches away from their tab.
 * Non-project sessions (local assistant, ACP) carry no identity and drop out.
 */
export function useBusyTaskRunsSignal(
    busySessionKeys: string[],
    onChange?: (runs: { projectPaths: string[]; expertIds: string[] }) => void,
) {
    useEffect(() => {
        if (typeof onChange !== "function") return;
        const projectPaths = Array.from(new Set(
            busySessionKeys.map(key => projectPathFromSessionKey(key)).filter(Boolean),
        )).sort();
        const expertIds = Array.from(new Set(
            busySessionKeys.map(key => expertIdFromSessionKey(key)).filter(Boolean),
        )).sort();
        onChange({ projectPaths, expertIds });
    }, [busySessionKeys, onChange]);
}

export function projectPathFromSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return "";
    // Expert session keys carry no project path — never hand "expert:<id>" to
    // path-based routing (it would be mistaken for a real project path).
    if (isExpertSessionKey(key) || isACPAssistantSessionKey(key)) return "";
    return normalizeProjectSessionPath(key.slice(prefix.length));
}

export function normalizeProjectSessionPath(projectPath?: string | null): string {
    let path = typeof projectPath === "string" ? projectPath.trim() : "";
    if (!path) return "";
    path = path.replace(/\\+/g, "/").replace(/\/+/g, "/");
    const drive = path.match(/^([a-zA-Z]):(\/.*)?$/);
    if (drive) path = `${drive[1].toUpperCase()}:${drive[2] || "/"}`;
    const parts: string[] = [];
    const absolute = path.startsWith("/") || /^[A-Z]:\//.test(path);
    const prefix = /^[A-Z]:\//.test(path) ? path.slice(0, 3) : (path.startsWith("/") ? "/" : "");
    const body = prefix ? path.slice(prefix.length) : path;
    // Trim each segment, not just the whole string. A segment may carry
    // incidental padding around a separator (for example "dir /sub"), and
    // leaving it in place made this function non-idempotent: re-normalizing
    // the stored result then stripped it. Callers compare paths by
    // re-normalizing both sides, so that asymmetry silently broke tab
    // deduplication and produced duplicate tabs for one project.
    for (const rawPart of body.split("/")) {
        const part = rawPart.trim();
        if (!part || part === ".") continue;
        if (part === ".." && parts.length > 0 && parts[parts.length - 1] !== "..") {
            parts.pop();
            continue;
        }
        if (part === ".." && absolute) continue;
        parts.push(part);
    }
    const joined = parts.join("/");
    if (prefix) return joined ? `${prefix}${joined}` : prefix.replace(/\/$/, "") || "/";
    return joined || ".";
}

/** Remove matching tab metadata and history blobs from localStorage. */
function purgeLocalTabCacheByIDs(deletedIDs: Set<string>): void {
    if (deletedIDs.size === 0 || typeof localStorage === "undefined") return;
    try {
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        if (rawTabs) {
            const tabs = JSON.parse(rawTabs);
            if (Array.isArray(tabs)) {
                const keptTabs = tabs.filter(tab => !deletedIDs.has(String(tab?.id || "")));
                if (keptTabs.length === 0) localStorage.removeItem(PROJECT_TABS_STORAGE_KEY);
                else if (keptTabs.length !== tabs.length) {
                    localStorage.setItem(PROJECT_TABS_STORAGE_KEY, JSON.stringify(keptTabs));
                }
            }
        }

        const rawHistories = localStorage.getItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        if (!rawHistories) return;
        const histories = JSON.parse(rawHistories);
        if (!histories || typeof histories !== "object") return;
        let changed = false;
        for (const id of deletedIDs) {
            if (Object.prototype.hasOwnProperty.call(histories, id)) {
                delete histories[id];
                changed = true;
            }
        }
        if (!changed) return;
        if (Object.keys(histories).length === 0) localStorage.removeItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        else localStorage.setItem(PROJECT_TAB_HISTORY_STORAGE_KEY, JSON.stringify(histories));
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

// A task deletion is stronger than closing a tab: no browser-side tab metadata
// or orphaned history for that project may survive to be restored later.
export function purgeDeletedProjectTabLocalCache(projectPath?: string | null): void {
    const normalizedPath = normalizeProjectSessionPath(projectPath);
    if (!normalizedPath || typeof localStorage === "undefined") return;
    // Cloud tasks persist tabs under the local cache path while the deletion
    // arrives with the durable list-row path; the workspace id embedded in the
    // path bridges the two so no orphaned cache entry survives.
    const workspaceId = cloudWorkspaceIdFromPath(normalizedPath);
    try {
        // Task aliases (VE/group tabs) keep their identity in a separate
        // binding map. Drop that entry together with the deleted task so a
        // future task reusing the path cannot inherit a stale session.
        const bindings = readAssistantTabTaskBindings();
        if (Object.prototype.hasOwnProperty.call(bindings, normalizedPath)) {
            delete bindings[normalizedPath];
            writeAssistantTabTaskBindings(bindings);
        }
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        if (!rawTabs) return;
        const tabs = JSON.parse(rawTabs);
        if (!Array.isArray(tabs)) return;

        const deletedIDs = new Set(
            tabs
                .filter(tab => normalizeProjectSessionPath(tab?.projectPath) === normalizedPath
                    || (!!workspaceId && (String(tab?.cloudWorkspaceId || "") === workspaceId || cloudWorkspaceIdFromPath(tab?.projectPath) === workspaceId)))
                .map(tab => String(tab.id || ""))
                .filter(Boolean),
        );
        purgeLocalTabCacheByIDs(deletedIDs);
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

/**
 * Expert tabs are keyed by expert id (tab id `expert-<id>`), not by the durable
 * task workspace path. Deleting an expert task must purge this separate cache or
 * the next open resurrects the closed conversation from localStorage.
 */
export function purgeDeletedExpertTabLocalCache(expertId?: string | null): void {
    const id = String(expertId || "").trim();
    if (!id || typeof localStorage === "undefined") return;
    const tabId = expertTabId(id);
    try {
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        const deletedIDs = new Set<string>([tabId]);
        if (rawTabs) {
            const tabs = JSON.parse(rawTabs);
            if (Array.isArray(tabs)) {
                for (const tab of tabs) {
                    const tabExpertId = String(tab?.expertId || "").trim();
                    const storedId = String(tab?.id || "").trim();
                    if (tab?.type === "expert" && (tabExpertId === id || storedId === tabId)) {
                        if (storedId) deletedIDs.add(storedId);
                    }
                }
            }
        }
        purgeLocalTabCacheByIDs(deletedIDs);
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

export function normalizeAssistantSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    if (!key) return "";
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return key;
	// Expert and ACP owners are opaque identifiers, not project paths. Normalizing
	// them as paths changes their identity and can merge separate sessions.
    if (isExpertSessionKey(key) || isACPAssistantSessionKey(key)) return key;
    const path = normalizeProjectSessionPath(key.slice(prefix.length));
    return path ? `${prefix}${path}` : "desktop-user";
}

export function logAIPanelDiagnostic(payload: Record<string, unknown>) {
    const logFrontendDiagnostic = typeof window !== "undefined"
        ? (window as any).go?.main?.App?.LogFrontendDiagnostic
        : undefined;
    if (typeof logFrontendDiagnostic !== "function") return;
    try {
        void Promise.resolve(logFrontendDiagnostic({ tag: "ai-panel", ...payload })).catch(() => {});
    } catch {
        // diagnostics only
    }
}

export function messageIsLocalSession(message: ChatMessage): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return !owner || owner === "desktop-user";
}

export function chatHistoriesEquivalent(left: ChatMessage[] | undefined, right: ChatMessage[]): boolean {
    const a = Array.isArray(left) ? left : [];
    if (a.length !== right.length) return false;
    for (let i = 0; i < a.length; i += 1) {
        const leftMessage = a[i];
        const rightMessage = right[i];
        // The common live-sync path reuses message objects that did not change.
        // Avoid serializing large markdown/tool payloads for those entries.
        if (leftMessage === rightMessage) continue;
        // This predicate guards a persistence write, not just a text rerender.
        // Comparing a selected subset silently loses later updates such as an
        // injection label, streamed reasoning, task actions, or file evidence
        // after switching tabs. ChatMessage is intentionally data-only, so a
        // structural comparison is safe and keeps saved history faithful to the
        // bubble the user actually saw.
        if (JSON.stringify(leftMessage) !== JSON.stringify(rightMessage)) return false;
    }
    return true;
}

import { sanitizeAIAssistantStreamText, type ChatMessage } from "./useAIAssistant";
import { cloneCodePreviewState, type CodeFile, type CodePreviewUIState } from "./useCodePreviewState";
import type { WorkflowUIState } from "./useWorkflowState";
import type { AITab } from "./AITabTypes";
import { normalizeProjectSessionPath, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { isCloudWorkspacePath } from "./codingTaskMode";
import { getWailsAppModule } from "../../utils/wailsAppModule";

export type ConversationBranchPointLike = {
    index?: number;
    entry_id?: string;
    role?: string;
    preview?: string;
    branches?: number;
    labels?: string[];
};

export function canShowAssistantCodingPreviewForTab(tab: Pick<AITab, "type"> | null | undefined): boolean { return tab?.type === "local" || tab?.type === "project" || tab?.type === "expert"; }

/** Source files are only evidence for a programming workflow, including its review state. */
export function shouldShowSourcePreviewForWorkflow(workflowType: string): boolean {
    const normalizedType = workflowType.trim().toLowerCase();
    return normalizedType === "coding";
}

/** Pure coding environments (local/remote agentMode) always allow the right-hand source panel. */
export function shouldShowSourcePreviewForAgentMode(agentMode?: string | null): boolean {
    return agentMode === "coding_dev" || agentMode === "remote_coding_dev";
}

/**
 * After tab switch / localStorage restore, keep the right-hand source panel open when
 * there is still open file content and the user did not manually close it.
 * Without this, a snapshot with active=false (or a restore race) leaves the panel
 * hidden until the next forceOpen code:file_update event.
 *
 * Identity-preserving: returns the same object when no change is needed.
 */
export function withCodePreviewVisibleIfContent(state: CodePreviewUIState): CodePreviewUIState {
    if (state.userClosed || state.active || state.files.size === 0) {
        return state;
    }
    return { ...state, active: true };
}

/** Preview mode label for persistence / tab snapshots. */
export function codePreviewModeFromState(state: Pick<CodePreviewUIState, "active" | "files" | "userClosed">): "workflow" | "code" {
    return state.active || (state.files.size > 0 && !state.userClosed) ? "code" : "workflow";
}

/**
 * Apply a saved code-preview snapshot: repair visibility, deep-clone into the live ref,
 * and push into React state. Returns the clone for optional map persistence.
 */
export function commitRestoredCodePreview(
    snapshot: CodePreviewUIState,
    restoreCodePreviewState: (state: CodePreviewUIState) => void,
    codePreviewStateRef: { current: CodePreviewUIState },
): CodePreviewUIState {
    const clone = cloneCodePreviewState(withCodePreviewVisibleIfContent(snapshot));
    codePreviewStateRef.current = clone;
    restoreCodePreviewState(clone);
    return clone;
}

/**
 * Whether a localStorage-restored source/workflow preview snapshot may be painted
 * onto the currently active tab.
 *
 * Tab switches reassign `previewOwnerTabRef` to the active tab *before* the
 * restore effect runs. Without project-path (or original tab-id) identity checks,
 * a pending snapshot from an old project would incorrectly fill a brand-new
 * remote/local coding tab that has no content yet.
 */
export function shouldApplyRestoredAssistantPreview(args: {
    restoredOwnerTabId: string;
    restoredOwnerProjectPath?: string;
    activeTabId: string;
    activeTabType?: string | null;
    activeTabProjectPath?: string | null;
}): boolean {
    const restoredPath = normalizeProjectSessionPath(args.restoredOwnerProjectPath);
    const activePath = normalizeProjectSessionPath(args.activeTabProjectPath);
    if (restoredPath) {
        // Snapshot belongs to a specific project — only apply when that project is active.
        if (args.activeTabType !== "project") return false;
        return activePath === restoredPath && activePath !== "";
    }
    // No project path: only apply to the original tab id (typically "local").
    // Never paint onto a newly created project tab that merely claimed ownership.
    return args.activeTabId === args.restoredOwnerTabId;
}

const ASSISTANT_PREVIEW_STATE_KEY = "ai_assistant_preview_state_v1";
const ASSISTANT_PREVIEW_STATE_MAX_BYTES = 900_000;

export type StoredAssistantPreviewState = {
    ownerTabId: string;
    ownerProjectPath?: string;
    previewMode: "workflow" | "code";
    workflow: WorkflowUIState;
    code: CodePreviewUIState;
};

function encodeWorkflowStateForStorage(state: WorkflowUIState) {
    return {
        ...state,
        phaseDocuments: Array.from(state.phaseDocuments.entries()),
        gateResults: Array.from(state.gateResults.entries()),
        docUpdatePhaseIDs: Array.from(state.docUpdatePhaseIDs),
    };
}

function decodeWorkflowStateFromStorage(raw: any): WorkflowUIState | null {
    if (!raw || typeof raw !== "object") return null;
    return {
        active: raw.active === true,
        splitMode: raw.splitMode === true,
        splitRatio: typeof raw.splitRatio === "number" ? raw.splitRatio : 0.6,
        workflowType: typeof raw.workflowType === "string" ? raw.workflowType : "",
        currentPhaseID: typeof raw.currentPhaseID === "string" ? raw.currentPhaseID : "",
        latestDocumentPhaseID: typeof raw.latestDocumentPhaseID === "string" ? raw.latestDocumentPhaseID : "",
        phaseDocuments: new Map(Array.isArray(raw.phaseDocuments) ? raw.phaseDocuments : []),
        gateResults: new Map(Array.isArray(raw.gateResults) ? raw.gateResults : []),
        phases: Array.isArray(raw.phases) ? raw.phases : [],
        suggestMaximize: raw.suggestMaximize === true,
        suggestMaximizeType: typeof raw.suggestMaximizeType === "string" ? raw.suggestMaximizeType : "",
        awaitingForm: raw.awaitingForm === true,
        transientText: typeof raw.transientText === "string" ? raw.transientText : "",
        workingDir: typeof raw.workingDir === "string" ? raw.workingDir : "",
        workflowID: typeof raw.workflowID === "string" ? raw.workflowID : "",
        docUpdatePhaseIDs: new Set(Array.isArray(raw.docUpdatePhaseIDs) ? raw.docUpdatePhaseIDs : []),
    };
}

function encodeCodeStateForStorage(state: CodePreviewUIState) {
    return {
        ...state,
        files: Array.from(state.files.entries()).map(([path, file]) => [
            path,
            isCloudWorkspacePath(file.absPath) ? { ...file, absPath: undefined } : file,
        ]),
    };
}

function decodeCodeStateFromStorage(raw: any): CodePreviewUIState | null {
    if (!raw || typeof raw !== "object") return null;
    const files = new Map<string, CodeFile>();
    if (Array.isArray(raw.files)) {
        for (const entry of raw.files) {
            if (!Array.isArray(entry) || typeof entry[0] !== "string" || !entry[1] || typeof entry[1] !== "object") continue;
            const file = entry[1] as CodeFile;
            files.set(entry[0], isCloudWorkspacePath(file.absPath) ? { ...file, absPath: undefined } : file);
        }
    }
    return {
        active: raw.active === true,
        files,
        activeFilePath: typeof raw.activeFilePath === "string" ? raw.activeFilePath : "",
        sessionID: typeof raw.sessionID === "string" ? raw.sessionID : "",
        sessionActive: raw.sessionActive === true,
        userClosed: raw.userClosed === true,
        pinnedPaths: Array.isArray(raw.pinnedPaths) ? raw.pinnedPaths.map(String) : [],
        mruOrder: Array.isArray(raw.mruOrder) ? raw.mruOrder.map(String) : [],
    };
}

export function readStoredAssistantPreviewState(): StoredAssistantPreviewState | null {
    try {
        const raw = localStorage.getItem(ASSISTANT_PREVIEW_STATE_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw);
        const workflow = decodeWorkflowStateFromStorage(parsed?.workflow);
        const code = decodeCodeStateFromStorage(parsed?.code);
        if (!workflow || !code) return null;
        return {
            ownerTabId: typeof parsed.ownerTabId === "string" ? parsed.ownerTabId : "local",
            ownerProjectPath: typeof parsed.ownerProjectPath === "string" ? parsed.ownerProjectPath : undefined,
            previewMode: parsed.previewMode === "code" ? "code" : "workflow",
            workflow,
            code,
        };
    } catch {
        return null;
    }
}

export function writeStoredAssistantPreviewState(state: StoredAssistantPreviewState) {
    try {
        const hasWorkflowPreview = state.workflow.splitMode || state.workflow.phaseDocuments.size > 0 || state.workflow.active;
        const hasCodePreview = state.code.active || state.code.files.size > 0;
        if (!hasWorkflowPreview && !hasCodePreview) {
            localStorage.removeItem(ASSISTANT_PREVIEW_STATE_KEY);
            return;
        }
        const payload = {
            ownerTabId: state.ownerTabId,
            ownerProjectPath: state.ownerProjectPath,
            previewMode: state.previewMode,
            workflow: encodeWorkflowStateForStorage(state.workflow),
            code: encodeCodeStateForStorage(state.code),
        };
        let serialized = JSON.stringify(payload);
        if (serialized.length > ASSISTANT_PREVIEW_STATE_MAX_BYTES && hasCodePreview) {
            if (!hasWorkflowPreview) {
                localStorage.removeItem(ASSISTANT_PREVIEW_STATE_KEY);
                return;
            }
            const codeDroppedPayload = {
                ...payload,
                previewMode: "workflow" as const,
                code: encodeCodeStateForStorage({
                    active: false,
                    files: new Map(),
                    activeFilePath: "",
                    sessionID: "",
                    sessionActive: false,
                    userClosed: false,
                    pinnedPaths: [],
                    mruOrder: [],
                }),
            };
            serialized = JSON.stringify(codeDroppedPayload);
        }
        if (serialized.length > ASSISTANT_PREVIEW_STATE_MAX_BYTES && state.workflow.phaseDocuments.size > 0) {
            const workflowWithoutDocsPayload = {
                ...payload,
                previewMode: "workflow" as const,
                workflow: encodeWorkflowStateForStorage({
                    ...state.workflow,
                    phaseDocuments: new Map(),
                    gateResults: new Map(),
                    docUpdatePhaseIDs: new Set(),
                    transientText: "",
                }),
                code: encodeCodeStateForStorage({
                    active: false,
                    files: new Map(),
                    activeFilePath: "",
                    sessionID: "",
                    sessionActive: false,
                    userClosed: false,
                    pinnedPaths: [],
                    mruOrder: [],
                }),
            };
            serialized = JSON.stringify(workflowWithoutDocsPayload);
        }
        if (serialized.length <= ASSISTANT_PREVIEW_STATE_MAX_BYTES) {
            localStorage.setItem(ASSISTANT_PREVIEW_STATE_KEY, serialized);
        } else {
            localStorage.removeItem(ASSISTANT_PREVIEW_STATE_KEY);
        }
    } catch {
        try { localStorage.removeItem(ASSISTANT_PREVIEW_STATE_KEY); } catch { /* ignore storage failures */ }
    }
}

export function normalizeWorkflowPhaseStatus(status: unknown): string {
    return String(status || "").trim().toLowerCase();
}

export function isWorkflowPhaseRunningStatus(status: unknown): boolean {
    const normalized = normalizeWorkflowPhaseStatus(status);
    return normalized === "running" || normalized === "executing" || normalized === "active";
}

export function isWorkflowPhaseTerminalStatus(status: unknown): boolean {
    const normalized = normalizeWorkflowPhaseStatus(status);
    return normalized === "completed" || normalized === "skipped" || normalized === "cancelled" || normalized === "canceled";
}

export function agentViewHiddenFieldValue(view: unknown, fieldName: string): string {
    const fields = (view as any)?.fields;
    if (!Array.isArray(fields)) return "";
    const field = fields.find((item: any) => item && item.name === fieldName);
    const value = field?.value;
    return typeof value === "string" ? value.trim() : "";
}

export function hasRestorableProjectConversation(history: unknown[] | undefined): boolean {
    if (!Array.isArray(history)) return false;
    return history.some((message) => {
        if (!message || typeof message !== "object") return false;
        const role = typeof (message as ChatMessage).role === "string" ? (message as ChatMessage).role : "";
        return role === "user" || role === "assistant";
    });
}

export function normalizeRestoredProjectHistoryContent(value: unknown): string {
    if (typeof value === "string") return sanitizeAIAssistantStreamText(value).replace(/^\x01/, '').trim();
    if (value == null) return "";
    try {
        const serialized = JSON.stringify(value);
        return typeof serialized === "string" ? sanitizeAIAssistantStreamText(serialized) : "";
    } catch {
        return sanitizeAIAssistantStreamText(String(value));
    }
}

export function suppressWorkflowReviewActions(message: ChatMessage): ChatMessage {
    if (!Array.isArray(message.actions) || message.actions.length === 0) return message;
    const actions = message.actions.filter(action => !String(action?.command || "").startsWith("__wf_review__"));
    return actions.length === message.actions.length ? message : { ...message, actions: actions.length > 0 ? actions : undefined };
}

export async function loadRestoredProjectConversationHistory(projectPath: string): Promise<ChatMessage[]> {
    const { LoadProjectConversationHistory } = await getWailsAppModule();
    if (typeof LoadProjectConversationHistory !== "function") return [];
    const sessionKey = projectSessionKey(projectPath);
    const restored = await LoadProjectConversationHistory(projectPath);
    if (!Array.isArray(restored) || restored.length === 0) return [];
    const baseTimestamp = Date.now();
    const pathToken = projectPath.replace(/[^a-zA-Z0-9]+/g, "-").slice(-24) || "project";
    return restored
        .map((entry: any, index: number): ChatMessage | null => {
            const role = typeof entry?.role === "string" ? entry.role.trim() : "";
            if (role !== "user" && role !== "assistant" && role !== "system" && role !== "error") return null;
            const content = normalizeRestoredProjectHistoryContent(entry?.content);
            const reasoning = normalizeRestoredProjectHistoryContent(entry?.reasoning_content ?? entry?.reasoningContent);
            if (!content && !reasoning) return null;
            return {
                id: `restored-${pathToken}-${index}`,
                role,
                content,
                reasoning: reasoning || undefined,
                sessionKey,
                timestamp: baseTimestamp + index,
            };
        })
        .filter((message): message is ChatMessage => message !== null);
}

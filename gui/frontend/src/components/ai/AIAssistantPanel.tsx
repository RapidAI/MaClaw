import { Fragment, lazy, Suspense, useState, useRef, useCallback, useEffect, useMemo, type ClipboardEvent, type DragEvent } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti, setActiveSessionKey, getActiveSessionKey, forgetAIAssistantSessionRounds } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { normalizeASRText, shouldDispatchASRText } from "./asrTextUtils";
import { cloneWorkflowUIState, useWorkflowState, type WorkflowUIState } from "./useWorkflowState";
import { cloneCodePreviewState, initialState as initialCodePreviewState, useCodePreviewState, type CodePreviewUIState } from "./useCodePreviewState";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { renderMessage } from "./aiAssistantMarkdown";
import {
    formatRecordingCompletionDisplay,
    formatRecordingCompletionMessage,
    isRecordingInputLocked,
    type RecordingCompleteResult,
} from "./RecordingSessionCard";
import { createIncrementalRenderState, renderContentIncremental, type IncrementalRenderState } from "./IncrementalMarkdownRenderer";
import { formFieldInputStyle, formFieldLabelColor, lightTheme, maximizedInlineStyle, overlayStyle, overlayTheme, primaryFilledButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { getAssistantDarkScheme } from "./assistantDarkSchemes";
import { getAssistantLightScheme } from "./assistantLightSchemes";
import "./ensureAIAssistantPanelStyles";
import { localizeText } from "./aiAssistantI18n";
import { ProjectSearchPanel, useProjectSearch } from "./ProjectSearchPanel";
import { useTTSReadback } from "./useTTSReadback";
import { useAIAssistantVoiceControls } from "./useAIAssistantVoiceControls";
import { useAssistantOutputScroll } from "./useAssistantOutputScroll";
import { useResizableAssistantInput } from "./useResizableAssistantInput";
import { useAssistantInputHistory } from "./useAssistantInputHistory";
import { usePastedImageAttachments } from "./usePastedImageAttachments";
import { useAssistantPreviewResize } from "./useAssistantPreviewResize";
import { getAssistantInitLabel } from "./aiAssistantStatusLabels";
import { AssistantConversationBody } from "./AssistantConversationBody";
import { AssistantTitleBar } from "./AssistantTitleBar";
import { KnowledgeDialog } from "./KnowledgeDialog";
import { AssistantInputStack } from "./AssistantInputStack";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";
import type { ComposeAction, FireSlashCommand, PlusMenuActionId } from "./composeAction";
import { applyComposeActionToText, btwQueryFromText, getComposeActionPlaceholder, isBtwCommandText, isInstallCommandText, normalizeInstallCommandText } from "./composeAction";
import { InlineChatCard } from "./InlineChatCard";
import { AssistantWelcomeView, syncLocalStartMenuTemplates, type WelcomePromptSubmitMeta } from "./AssistantWelcomeView";
import { WelcomeTemplateSaveOfferBanner } from "./WelcomeTemplateSaveOffer";
import {
    loadRemoteSSHPassword,
    remoteSSHPasswordVaultKey,
    saveRemoteSSHPassword,
    saveWelcomeCustomTemplate,
    shouldOfferWelcomeTemplateSave,
    type WelcomeTemplateSaveOffer,
} from "./welcomeTaskMemory";
import { AssistantWorkflowMaximizeSuggestion } from "./AssistantWorkflowMaximizeSuggestion";
import { useAssistantThemeMode } from "./useAssistantThemeMode";
import { activeCodingAgentProgress, codingAgentCompactText, latestCodingAgentTurnSnapshot } from "./CodingAgentProgressStatus";
import { findLatestToolProgressText, formatToolProgressStatus, isToolProgressMessage } from "./aiAssistantProgressUtils";
import { IconBranch, IconRocket } from "./WorkbenchIcons";
import { AITabBar } from "./AITabBar";
import { localAssistantTabTitle } from "./aiAssistantI18n";
import { getAITabDisplayTitle } from "./AITabItem";
import type { AITab } from "./AITabTypes";
import { useAITabManager } from "./useAITabManager";
import { ProjectDirBar } from "./ProjectDirBar";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { useAddGroupParticipantToTab } from "./useAddGroupParticipantToTab";
import { useAddLocalMaclawToTab } from "./useAddLocalMaclawToTab";
import { useProjectContextLoader } from "./useProjectContextLoader";
import { AssistantActiveTabContent } from "./AssistantActiveTabContent";
import { AssistantDragHandle } from "./AssistantDragHandle";
import { usePendingAssistantTabOpen } from "./usePendingAssistantTabOpen";
import type { PendingProjectTabOpen } from "./usePendingAssistantTabOpen";
import type { AIAssistantPanelProps } from "./aiAssistantPanelTypes";
import { loadProjectTabMsgIds, mergeChatMessages, PROJECT_TAB_MSG_IDS_KEY, withoutProjectContextMessages } from "./aiAssistantProjectTabState";
import { compactCodingAgentProgressMessages, groupCodingAgentProgressForRender } from "./compactCodingAgentProgressMessages";
import { renderCodingAgentActivityFeed } from "./CodingAgentProgressStatus";
import { TabParticipantInviteDialog } from "./TabParticipantInviteDialog";
import { AIAssistantRenameGroupDialog } from "./AIAssistantRenameGroupDialog";
import { WorkflowFormInlinePrompt, WorkflowReviewInlinePrompt } from "./WorkflowInlinePrompts";
import { buildProjectTabRecentMessages, chatHistoriesEquivalent, expertIdFromSessionKey, expertSessionKey, isACPAssistantSessionKey, logAIPanelDiagnostic, messageBelongsToSession, messageBelongsToSessionOrLegacy, messageIsLocalSession, normalizeAssistantSessionKey, normalizeProjectSessionPath, projectPathFromSessionKey, projectSessionKey, purgeDeletedProjectTabLocalCache } from "./aiAssistantPanelSessionUtils";
import { DEFAULT_EXPERT_ICON, expertWelcomeMessageText } from "./expertTypes";
import { AdoptBaseCodingWorkbenchConflict, AdoptCodingWorkbenchConflict, ApplyCodingWorkbenchConflictPreviewSide, CancelAIAssistantSessionForSession, ClearCodingWorkbenchConflictLog, ComputerUseStop, DiscardAllCodingWorkbenchConflicts, DiscardCodingWorkbenchConflict, EnsureCodingWorkbenchArmed, ExportCodingWorkbenchConflictLog, GetCodingWorkbenchCheckpointSidecarStats, GetCodingWorkbenchConflictDiffs, GetCodingWorkbenchConflictFilePreview, GetCodingWorkbenchConflictFileTriple, GetCodingWorkbenchPermission, GetCodingWorkbenchPlanMode, GetCodingWorkbenchRoutePref, GetCodingWorkbenchStatus, GetCodingWorkbenchWorktreeMode, GetComputerUseStatus, GetConversationBranchPoints, GroupDiscussionRenameConsultation, KeepMainCodingWorkbenchConflict, ListCodingWorkbenchCheckpoints, ListCodingWorkbenchConflicts, LoadConfig, OpenCodingWorkbenchConflictFile, PatchConfigFields, PrepareRemoteCodingEnvironment, PrepareRemoteOpsDiagnosisEnvironment, PruneCodingWorkbenchCheckpoints, RefreshWorkflowV2StateForTab, ResolveCodingWorkbenchConflict, RestoreCodingWorkbenchCheckpointByLabel, RestoreCodingWorkbenchCheckpointEx, RunCodingWorkbenchBackgroundVerify, SaveCodingWorkbenchCheckpoint, SetCodingWorkbenchConflictUIState, SetCodingWorkbenchPermission, SetCodingWorkbenchPlanMode, SetCodingWorkbenchRoutePref, SetCodingWorkbenchSessionPlan, SetCodingWorkbenchWorktreeMode, UpdateCodingWorkbenchPendingPlan, WriteCodingWorkbenchConflictFileContent } from "../../../wailsjs/go/main/App";
import { suggestSessionPlanFromMessages } from "./codingSessionPlanUtils";
import { buildCodingBannerChrome, codingStepStatusColor, CodingWorkbenchControlPanel, CodingControlSection } from "./CodingWorkbenchControlPanel";
import { CodingConflictSidePanel } from "./CodingConflictSidePanel";
import { agentModeFromTaskTags, remoteHostFromTaskTags } from "./codingTaskMode";
import { canDispatchCodingIntent, resolveCodingTaskPhase } from "./codingTaskRuntime";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { EVENT_PROJECT_TASK_CLOSED, EVENT_PROJECT_TASK_DELETED } from "../../constants/events";
import { getWailsAppModule } from "../../utils/wailsAppModule";
import { useDialog } from "../CustomDialog";
import { ComputerUseOperatorPanel } from "./ComputerUseOperatorPanel";
import { ComputerUseQuickBar } from "./ComputerUseQuickBar";
import { AssistantQuickSettingsBar } from "./AssistantQuickSettingsBar";
import { ComputerUseReadinessBanner } from "./ComputerUseReadinessBanner";
export { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";

const LOCAL_HIGH_RISK_APPROVAL_KIND = "local_high_risk_bash";
const REMOTE_HIGH_RISK_APPROVAL_KIND = "remote_high_risk_bash";
const REMOTE_DIRECTORY_WRITE_APPROVAL_KIND = "remote_shell_directory_write";
const REMOTE_PATH_ACCESS_APPROVAL_KIND = "remote_path_access";
const AssistantPreviewPane = lazy(() => import("./AssistantPreviewPane").then((module) => ({ default: module.AssistantPreviewPane })));

type ConversationBranchPointLike = {
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
function commitRestoredCodePreview(
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

type StoredAssistantPreviewState = {
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
        files: Array.from(state.files.entries()),
    };
}

function decodeCodeStateFromStorage(raw: any): CodePreviewUIState | null {
    if (!raw || typeof raw !== "object") return null;
    return {
        active: raw.active === true,
        files: new Map(Array.isArray(raw.files) ? raw.files : []),
        activeFilePath: typeof raw.activeFilePath === "string" ? raw.activeFilePath : "",
        sessionID: typeof raw.sessionID === "string" ? raw.sessionID : "",
        sessionActive: raw.sessionActive === true,
        userClosed: raw.userClosed === true,
        pinnedPaths: Array.isArray(raw.pinnedPaths) ? raw.pinnedPaths.map(String) : [],
        mruOrder: Array.isArray(raw.mruOrder) ? raw.mruOrder.map(String) : [],
    };
}

function readStoredAssistantPreviewState(): StoredAssistantPreviewState | null {
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

function writeStoredAssistantPreviewState(state: StoredAssistantPreviewState) {
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

function normalizeWorkflowPhaseStatus(status: unknown): string {
    return String(status || "").trim().toLowerCase();
}

function isWorkflowPhaseRunningStatus(status: unknown): boolean {
    const normalized = normalizeWorkflowPhaseStatus(status);
    return normalized === "running" || normalized === "executing" || normalized === "active";
}

function isWorkflowPhaseTerminalStatus(status: unknown): boolean {
    const normalized = normalizeWorkflowPhaseStatus(status);
    return normalized === "completed" || normalized === "skipped" || normalized === "cancelled" || normalized === "canceled";
}

function agentViewHiddenFieldValue(view: unknown, fieldName: string): string {
    const fields = (view as any)?.fields;
    if (!Array.isArray(fields)) return "";
    const field = fields.find((item: any) => item && item.name === fieldName);
    const value = field?.value;
    return typeof value === "string" ? value.trim() : "";
}

function hasRestorableProjectConversation(history: unknown[] | undefined): boolean {
    if (!Array.isArray(history)) return false;
    return history.some((message) => {
        if (!message || typeof message !== "object") return false;
        const role = typeof (message as ChatMessage).role === "string" ? (message as ChatMessage).role : "";
        return role === "user" || role === "assistant";
    });
}

function normalizeRestoredProjectHistoryContent(value: unknown): string {
    if (typeof value === "string") return value.trim();
    if (value == null) return "";
    try {
        return JSON.stringify(value);
    } catch {
        return String(value);
    }
}

function suppressWorkflowReviewActions(message: ChatMessage): ChatMessage {
    if (!Array.isArray(message.actions) || message.actions.length === 0) return message;
    const actions = message.actions.filter(action => !String(action?.command || "").startsWith("__wf_review__"));
    return actions.length === message.actions.length ? message : { ...message, actions: actions.length > 0 ? actions : undefined };
}

async function loadRestoredProjectConversationHistory(projectPath: string): Promise<ChatMessage[]> {
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

export function AIAssistantPanel(props: AIAssistantPanelProps & any) {
    const { onClose, lang, chatFontSize = 14, themeMode: controlledThemeMode, darkSchemeId, lightSchemeId, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0, pendingVEOpen, onPendingVEOpenHandled, pendingHistoryDiscussionOpen, onPendingHistoryDiscussionOpenHandled, appUpdateAvailable, onOpenAppUpdate, onDismissAppUpdate, availableProviders, currentModel, modelOptions, modelsLoading, onSwitchProvider, onSwitchModel, onOpenModelMenu, onLanguageChange, statusSlot } = props;
    const state = props.state || props;
    const actions = props.actions || props;
    const panelWindow = props.window || props;
    const { messages, progressMessages = [], sending, sendingSessionKey: rawSendingSessionKey, busySessionKeys: rawBusySessionKeys, streaming, streamingSessionKey: rawStreamingSessionKey, streamingSessionKeys: rawStreamingSessionKeys, visualBusy, ready, initStatus, selectedFilePath: selectedFilePathFromState = "", submittedPrompts = [], draftInputValue = "", trialReflectEnabled = false, scrollToTopSeq, onboardingIncomplete, showTraceEntry = false, active: panelActive = true, agentView = null } = state;
    const { browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, injectSupplementary, guideLaunchReference, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews, onOpenOnboarding, cancelSession, onOpenTutorial, onTaskPrefsChanged, submitAgentView, dismissAgentView, deactivateRecordingSession } = actions;
    const selectedFilePaths = Array.isArray(state.selectedFilePaths) ? state.selectedFilePaths : (selectedFilePathFromState ? [selectedFilePathFromState] : []);
    const selectedFilePath = selectedFilePaths[0] || "";
    const { inline, maximized = false, onToggleMaximize, onHideWindow } = panelWindow || {};
    const [localDraftInputValue, setLocalDraftInputValue] = useState(draftInputValue);
    const [composeAction, setComposeAction] = useState<ComposeAction | null>(null);
    const [cancelPending, setCancelPending] = useState(false);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const [queueEditDraftActive, setQueueEditDraftActive] = useState(false);
    const [knowledgeDialogOpen, setKnowledgeDialogOpen] = useState(false);
    const [saveTaskDialogOpen, setSaveTaskDialogOpen] = useState(false);
    const [saveTaskName, setSaveTaskName] = useState("");
    const [savingTask, setSavingTask] = useState(false);
    const [workflowEnabled, setWorkflowEnabled] = useState(false);
    const [permissionMode, setPermissionMode] = useState<AssistantPermissionMode>("request");
    /** Remote coding SSH reconnect form. Password is recalled from localStorage vault only. */
    const [remoteReconnect, setRemoteReconnect] = useState<{
        needsReconnect: boolean;
        safety?: "diagnosis";
        host: string;
        user: string;
        port: number;
        workDir: string;
        password: string;
        connecting: boolean;
        error: string;
        success: string;
        sessionPlan: string;
    }>({ needsReconnect: false, host: "", user: "", port: 22, workDir: "", password: "", connecting: false, error: "", success: "", sessionPlan: "" });
    /** Project whose reconnect state has been hydrated from the backend. */
    const [remoteReconnectStatusPath, setRemoteReconnectStatusPath] = useState("");
    /** Advances after SSH reconnect so the already-mounted source tree reloads. */
    const [remoteWorkspaceRefreshToken, setRemoteWorkspaceRefreshToken] = useState(0);
    /** Avoid auto-reconnect loops: key = projectPath|user@host:port after one attempt (success or fail). */
    const remoteAutoReconnectKeyRef = useRef("");
    /** In-flight SSH reconnect owner (`projectPath|user@host:port`), so different remote tabs never block each other. */
    const remoteReconnectInFlightRef = useRef("");
    /**
     * Identity (`user@host:port`) the current form password is bound to.
     * Used so blur/status can detect real host switches vs same-target edits.
     */
    const remotePasswordBoundIdentityRef = useRef("");
    /** Latest reconnect form snapshot — keeps reconnect handler identity stable. */
    const remoteReconnectRef = useRef(remoteReconnect);
    remoteReconnectRef.current = remoteReconnect;
    /** Pure coding session plan editor (local + remote). */
    const [codingSessionPlan, setCodingSessionPlan] = useState("");
    const [codingExecutionPlan, setCodingExecutionPlan] = useState("");
    const [codingPlanMode, setCodingPlanMode] = useState<"auto" | "approve" | "off">("auto");
    const [codingPendingApproval, setCodingPendingApproval] = useState(false);
    const [codingPendingPlanEditing, setCodingPendingPlanEditing] = useState(false);
    const [codingPendingPlanDraft, setCodingPendingPlanDraft] = useState("");
    const [codingPendingPlanSaving, setCodingPendingPlanSaving] = useState(false);
    const [codingStepStatuses, setCodingStepStatuses] = useState<Array<{ index: number; title?: string; status: string; summary?: string; verify_cmd?: string; verify_ok?: boolean }>>([]);
    const [codingSessionCost, setCodingSessionCost] = useState("");
    const [codingWorktreeMode, setCodingWorktreeMode] = useState<"auto" | "always" | "off">("auto");
    const [codingRouteInfo, setCodingRouteInfo] = useState("");
    const [codingConflictCount, setCodingConflictCount] = useState(0);
    const [codingConflicts, setCodingConflicts] = useState<Array<{ id: string; step_index?: number; path?: string; kind?: string; files?: string[] }>>([]);
    const [codingConflictOpen, setCodingConflictOpen] = useState(false);
    /** Peak isolation-conflict count in the current wave (for side-panel progress). */
    const [codingConflictPeak, setCodingConflictPeak] = useState(0);
    const [codingConflictDiffs, setCodingConflictDiffs] = useState<Array<{ path: string; status: string; unified?: string; three_way?: string; base_head?: string }>>([]);
    const [codingConflictActiveId, setCodingConflictActiveId] = useState("");
    const [codingConflictSelected, setCodingConflictSelected] = useState<string[]>([]);
    const [codingConflictFocusFile, setCodingConflictFocusFile] = useState("");
    const [codingConflictPreview, setCodingConflictPreview] = useState<{ side: string; path: string; content: string; truncated?: boolean; missing?: boolean } | null>(null);
    const [codingConflictPreviewSide, setCodingConflictPreviewSide] = useState<"main" | "theirs" | "base">("main");
    const [codingConflictEditDraft, setCodingConflictEditDraft] = useState("");
    const [codingConflictTriple, setCodingConflictTriple] = useState<{
        main?: { content?: string; missing?: boolean };
        theirs?: { content?: string; missing?: boolean };
        base?: { content?: string; missing?: boolean };
    } | null>(null);
    const [codingConflictLog, setCodingConflictLog] = useState<string[]>([]);
    const [codingConflictBusy, setCodingConflictBusy] = useState(false);
    const codingConflictUIPersistTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const codingConflictSelectedRef = useRef<string[]>([]);
    const codingConflictFocusFileRef = useRef("");
    /** Triple-column preview scroll sync (base/main/theirs). */
    const codingConflictTripleScrollRefs = useRef<Record<"base" | "main" | "theirs", HTMLDivElement | null>>({
        base: null,
        main: null,
        theirs: null,
    });
    const codingConflictTripleScrollLock = useRef(false);
    const [codingSidecarStats, setCodingSidecarStats] = useState<{ total_bytes?: number; max_bytes?: number; usage_ratio?: number; dir_count?: number; user_bytes?: number } | null>(null);
    const [codingHooksInfo, setCodingHooksInfo] = useState<{ active: boolean; phases: string[]; count: number; failOnError: boolean } | null>(null);
    const [codingRoutePref, setCodingRoutePref] = useState<"auto" | "primary" | "reasoning" | "vision">("auto");
    const [codingRouteCaps, setCodingRouteCaps] = useState<Array<{ pref: string; available?: boolean; model?: string; source?: string; note?: string }>>([]);
    const [codingCheckpointLabel, setCodingCheckpointLabel] = useState("");
    const [codingCheckpointFiles, setCodingCheckpointFiles] = useState<string[]>([]);
    const [codingCheckpointSnapshots, setCodingCheckpointSnapshots] = useState(0);
    const [codingCheckpointHistory, setCodingCheckpointHistory] = useState<Array<{ label: string; summary?: string; snapshot_count?: number; current?: boolean; created_at?: number }>>([]);
    const [codingBackgroundVerify, setCodingBackgroundVerify] = useState("");
    const [codingCheckpointBusy, setCodingCheckpointBusy] = useState(false);
    const [codingBgVerifyBusy, setCodingBgVerifyBusy] = useState(false);
    const [codingSessionPlanDraft, setCodingSessionPlanDraft] = useState("");
    const [codingSessionPlanEditing, setCodingSessionPlanEditing] = useState(false);
    const [codingSessionPlanSaving, setCodingSessionPlanSaving] = useState(false);
    const [codingControlExpanded, setCodingControlExpanded] = useState(false);
    const [workflowStartingLabel, setWorkflowStartingLabel] = useState<string | null>(null);
    const [skillRecordingTabId, setSkillRecordingTabId] = useState<string | null>(null);
    const [skillRecordingCount, setSkillRecordingCount] = useState(0);
    const [skillRecordingCard, setSkillRecordingCard] = useState<any>(null);
    const { showConfirm } = useDialog();
    // The panel remains mounted across app-page navigation to retain task-tab
    // state. Close panel-owned overlays when it becomes inactive so nothing
    // unexpected reappears when the user returns.
    useEffect(() => {
        if (panelActive) return;
        setKnowledgeDialogOpen(false);
        setSaveTaskDialogOpen(false);
        // Keep a live approval request in memory: the backend may still be
        // awaiting the decision. Its rendering is gated below and it will be
        // available again if the user returns before the request expires.
    }, [panelActive]);
    const activeTabIdForRecRef = useRef<string>("local");
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const closeAllPreviewPanelsRef = useRef<(() => void) | null>(null);
    const pendingSkillRecDataRef = useRef<any>(null);
    const skillRecResolvedRef = useRef(false);
    const { themeMode, setThemeMode } = useAssistantThemeMode(controlledThemeMode, onThemeModeChange);
    const { ttsEnabled, setTtsEnabled, ttsPlaying } = useTTSReadback(audioOutputDeviceId);
    const t = useMemo(() => {
        const base = themeMode === 'dark' ? getAssistantDarkScheme(darkSchemeId).assistantTheme : (lightSchemeId && lightSchemeId !== 'default' ? getAssistantLightScheme(lightSchemeId).assistantTheme : (inline ? lightTheme : overlayTheme));
        return Object.assign({}, base, { isDark: themeMode === 'dark' });
    }, [themeMode, darkSchemeId, lightSchemeId, inline]);
    const showMaximizeToggle = inline && !!onToggleMaximize;
    // Tracks pure-coding tab so config-changed does not stomp per-task request/workspace mode.
    const pureCodingTabRef = useRef(false);
    // Active pure-coding session key (desktop-user:<path>) for scoping goal-state-changed.
    const pureCodingSessionKeyRef = useRef("");
    // Workflow toggle: load initial state from config, sync on config-changed event.
    // Permission mode for pure coding tabs is loaded after activeTab is available.
    useEffect(() => {
        LoadConfig().then((cfg) => {
            setWorkflowEnabled(cfg?.workflow_enabled === true);
            // Pure coding tabs load tier from sticky; do not overwrite with global.
            if (!pureCodingTabRef.current) {
                setPermissionMode(cfg?.subagent_full_access === true ? "full" : "request");
            }
        }).catch(() => { /* ignore */ });
        const off = EventsOn("config-changed", (cfg: any) => {
            if (cfg && typeof cfg.workflow_enabled === "boolean") {
                setWorkflowEnabled(cfg.workflow_enabled);
            } else if (cfg && cfg.workflow_enabled === undefined) {
                setWorkflowEnabled(false);
            }
            // Global full-access only drives non-coding tabs. Pure coding uses
            // GetCodingWorkbenchPermission (request can win over global).
            if (cfg && typeof cfg.subagent_full_access === "boolean" && cfg.subagent_full_access && !pureCodingTabRef.current) {
                setPermissionMode("full");
            }
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    const handleToggleWorkflow = useCallback(() => {
        setWorkflowEnabled(prev => {
            const next = !prev;
            PatchConfigFields({ workflow_enabled: next }).then((saved) => {
                setWorkflowEnabled(saved?.workflow_enabled === true);
            }).catch(() => {
                // Revert to actual backend state on failure
                LoadConfig().then(cfg => {
                    setWorkflowEnabled(cfg?.workflow_enabled === true);
                }).catch(() => {
                    setWorkflowEnabled(!next); // last resort: toggle back
                });
            });
            return next;
        });
    }, []);
    // Stable callbacks for the memoized quick-settings bar (inline arrows would
    // defeat memo on every keystroke-triggered panel re-render).
    const handleQuickThemeToggle = useCallback(() => {
        setThemeMode(themeMode === "dark" ? "light" : "dark");
    }, [themeMode, setThemeMode]);
    const handleQuickTtsToggle = useCallback(() => {
        setTtsEnabled(!ttsEnabled);
    }, [ttsEnabled, setTtsEnabled]);
    // Skill recording: sync state from backend events (per-tab)
    useEffect(() => {
        const off = EventsOn("skill-recording-state-changed", (state: any) => {
            if (state && typeof state.recording === "boolean") {
                if (state.recording) {
                    setSkillRecordingTabId(state.tabId || null);
                } else {
                    setSkillRecordingTabId(null);
                }
            }
            if (state && typeof state.count === "number") {
                setSkillRecordingCount(state.count);
            }
        });
        // Check initial state on mount
        (window as any).go?.main?.App?.IsSkillRecording?.().then((recording: boolean) => {
            if (recording) {
                (window as any).go?.main?.App?.GetSkillRecordingTabID?.().then((tabId: string) => {
                    setSkillRecordingTabId(tabId || "local");
                }).catch(() => { setSkillRecordingTabId("local"); });
                (window as any).go?.main?.App?.GetSkillRecordingCount?.().then((count: number) => {
                    setSkillRecordingCount(count || 0);
                }).catch(() => {});
            }
        }).catch(() => { /* ignore */ });
        return () => { if (typeof off === "function") off(); };
    }, []);
    // SubAgent scope approval: interactive confirmation when accessing paths outside project
    const [scopeApprovalPending, setScopeApprovalPending] = useState<{ id: string; tool: string; path: string; projectPath: string; directory: string; timeoutSeconds: number; kind: string; message: string; autoAllow: boolean; maintenance: boolean } | null>(null);
    const [scopeApprovalCountdown, setScopeApprovalCountdown] = useState(0);
    useEffect(() => {
        const off = EventsOn("subagent-scope-approval", (payload: unknown) => {
            if (!payload || typeof payload !== "object") return;
            const data = payload as Record<string, unknown>;
            if (!data.id) return;
            const timeoutSec = typeof data.timeout_seconds === "number" ? data.timeout_seconds : 10;
            setScopeApprovalPending({
                id: data.id as string,
                tool: (data.tool as string) || "",
                path: (data.path as string) || "",
                projectPath: (data.project_path as string) || "",
                directory: (data.directory as string) || "",
                timeoutSeconds: timeoutSec,
                kind: (data.kind as string) || "",
                message: (data.message as string) || "",
                autoAllow: typeof data.auto_allow === "boolean" ? data.auto_allow : true,
                maintenance: data.maintenance === true,
            });
            setScopeApprovalCountdown(timeoutSec);
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    // Countdown timer for scope approval auto-allow.
    useEffect(() => {
        if (!scopeApprovalPending || scopeApprovalCountdown <= 0) return;
        const timer = setTimeout(() => {
            setScopeApprovalCountdown(prev => prev - 1);
        }, 1000);
        return () => clearTimeout(timer);
    }, [scopeApprovalPending, scopeApprovalCountdown]);
    // Auto-dismiss 1 second before backend timeout to avoid race where user
    // clicks "deny" but backend has already auto-allowed.
    useEffect(() => {
        if (scopeApprovalPending?.autoAllow && scopeApprovalCountdown <= 1) {
            setScopeApprovalPending(null);
        } else if (scopeApprovalPending && !scopeApprovalPending.autoAllow && scopeApprovalCountdown <= 0) {
            setScopeApprovalPending(null);
        }
    }, [scopeApprovalPending, scopeApprovalCountdown]);
    const handleScopeApprovalResolve = useCallback(async (decision: "allow_once" | "allow_dir" | "deny" | "full_access") => {
        const pending = scopeApprovalPending;
        setScopeApprovalPending(null);
        if (!pending) return;
        try {
            const { ResolveScopeApproval } = await import("../../../wailsjs/go/main/App");
            await ResolveScopeApproval(pending.id, decision);
        } catch { /* expired or already resolved */ }
    }, [scopeApprovalPending]);
    const handleToggleSkillRecording = useCallback(() => {
        const currentTabId = activeTabIdForRecRef.current;
        const isRecordingCurrentTab = skillRecordingTabId === currentTabId;
        if (isRecordingCurrentTab) {
            // Immediately update UI state (don't wait for backend event)
            setSkillRecordingTabId(null);
            // Stop recording → show result card
            (window as any).go?.main?.App?.StopSkillRecording?.().then((data: any) => {
                if (data && !data.error) {
                    // Has recorded operations → show save card
                    pendingSkillRecDataRef.current = data;
                    setSkillRecordingCard({
                        id: `skill-rec-card-${Date.now()}`,
                        type: "skill_recording_done",
                        title: lang === "en"
                            ? `Recording complete. ${data.count} operations captured.`
                            : `录制完成，共记录 ${data.count} 个操作步骤。`,
                        description: lang === "en"
                            ? "Save as a self-learned Skill?"
                            : "是否保存为自学习 Skill？",
                        fields: [
                            { key: "name", label: lang === "en" ? "Skill Name" : "Skill 名称", type: "text", defaultValue: data.suggested_name || "", placeholder: "my-skill" },
                            { key: "description", label: lang === "en" ? "Description" : "描述", type: "text", defaultValue: data.suggested_description || "" },
                        ],
                        actions: [
                            { key: "save", label: lang === "en" ? "Save as Skill" : "保存为 Skill", style: "primary" },
                            { key: "cancel", label: lang === "en" ? "Discard" : "放弃", style: "default" },
                        ],
                        metadata: { summary: data.summary || [], security_warnings: data.security_warnings || [] },
                    });
                } else {
                    // No operations recorded → show info card (no fields, just dismiss button)
                    setSkillRecordingCard({
                        id: `skill-rec-empty-${Date.now()}`,
                        type: "skill_recording_empty",
                        title: lang === "en"
                            ? "Recording stopped — no operations captured."
                            : "录制已停止 — 没有记录到可用操作。",
                        description: lang === "en"
                            ? "Only the following operations are recorded as Skill steps:\n• Commands (bash)\n• File writes (write_file)\n• File edits (edit_file)\n\nSearch, read, screenshot and other query operations are not included.\nTry again after letting the AI execute some commands or write files."
                            : "只有以下操作会被记录为 Skill 步骤：\n• 命令执行（bash）\n• 文件写入（write_file）\n• 文件编辑（edit_file）\n\n搜索、读取、截图等查询操作不会被录制。\n请让 AI 执行一些命令或写入文件后再试。",
                        fields: [],
                        actions: [
                            { key: "cancel", label: lang === "en" ? "OK" : "知道了", style: "default" },
                        ],
                        metadata: {},
                    });
                }
            }).catch(() => {
                // On failure, revert UI state
                setSkillRecordingTabId(currentTabId);
            });
        } else if (skillRecordingTabId) {
            // Another tab is recording — cannot start a new one
            return;
        } else {
            // Start recording for the current active tab
            const tabId = currentTabId;
            (window as any).go?.main?.App?.StartSkillRecording?.(tabId).then((result: string) => {
                if (result === "ok") {
                    setSkillRecordingTabId(tabId);
                    setSkillRecordingCount(0);
                    setSkillRecordingCard(null);
                    skillRecResolvedRef.current = false;
                    // Show recording started as a non-interactive info card
                    setSkillRecordingCard({
                        id: `skill-rec-start-${Date.now()}`,
                        type: "skill_recording_started",
                        title: lang === "en" ? "Skill recording started" : "Skill 录制已开始",
                        description: lang === "en"
                            ? "All commands, file writes, and edits will be recorded. Click REC again to stop.\n\nWork as usual — recording continues in the background."
                            : "所有命令执行、文件写入、文件编辑将被记录。再次点击录制按钮停止。\n\n正常使用即可，录制在后台静默进行。",
                        fields: [],
                        actions: [{ key: "cancel", label: lang === "en" ? "OK" : "知道了", style: "default" }],
                        metadata: {},
                    });
                }
            }).catch(() => { /* ignore */ });
        }
    }, [skillRecordingTabId, lang]);
    // Handle inline card resolve for skill recording
    const handleResolveSkillRecordingCard = useCallback((action: string, values: Record<string, string>) => {
        // For empty/info cards (no pending data), just dismiss
        const data = pendingSkillRecDataRef.current;
        if (!data) {
            // No pending recording data — this is the "empty recording" info card
            // Just mark it resolved, no backend call needed
            return;
        }
        // Prevent double-submit
        if (skillRecResolvedRef.current) return;
        skillRecResolvedRef.current = true;
        pendingSkillRecDataRef.current = null;

        if (action === "cancel") {
            (window as any).go?.main?.App?.ResolveSkillRecording?.("cancel", "", "");
        } else {
            // action === "save"
            const name = (values.name || "").trim() || data.suggested_name || "my-skill";
            const desc = (values.description || "").trim() || data.suggested_description || "";
            (window as any).go?.main?.App?.ResolveSkillRecording?.("save", name, desc).catch(() => { /* ignore */ });
        }
    }, []);
    const { tabState, activeTab, activateTab, createVETab, createGroupTab, createProjectTab, createExpertTab, closeTab, discardDeletedProjectTabs, clearTabConversation, saveTabState, getTabState, getTabs, hasProjectTab, upgradeVETabToGroup, renameGroupTab, tabLimitError, clearTabLimitError } = useAITabManager();
    // Publish open project-tab paths so the sidebar can block deleting active tasks.
    useEffect(() => {
        const onChange = props.onOpenProjectTabsChange as ((paths: string[]) => void) | undefined;
        if (typeof onChange !== "function") return;
        const paths = tabState.tabs
            .filter(t => t.type === "project" && t.projectPath)
            .map(t => normalizeProjectSessionPath(t.projectPath))
            .filter((p): p is string => !!p);
        // Stable order so set-equality in App does not depend on tab UI order.
        paths.sort();
        onChange(paths);
    }, [tabState.tabs, props.onOpenProjectTabsChange]);
    // Expert tabs do not have a project path, but each has a durable sidebar
    // task keyed by expert id. Publish those ids so the sidebar cannot remove
    // the task that makes an already-open expert reachable again.
    useEffect(() => {
        const onChange = props.onOpenExpertTabsChange as ((expertIDs: string[]) => void) | undefined;
        if (typeof onChange !== "function") return;
        const ids = Array.from(new Set(
            tabState.tabs
                .filter(t => t.type === "expert" && t.expertId)
                .map(t => String(t.expertId || "").trim())
                .filter(Boolean),
        )).sort();
        onChange(ids);
    }, [tabState.tabs, props.onOpenExpertTabsChange]);
    // Pure coding workbench: load per-task permission tier (request|workspace|full).
    useEffect(() => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        const pureCoding = activeTab?.agentMode === "coding_dev" || activeTab?.agentMode === "remote_coding_dev";
        pureCodingTabRef.current = !!(pureCoding && projectPath);
        pureCodingSessionKeyRef.current = pureCoding && projectPath ? projectSessionKey(projectPath) : "";
        let cancelled = false;
        if (!pureCoding || !projectPath) {
            setRemoteReconnectStatusPath("");
            // Leaving pure coding: restore global config-driven request|full (no workspace).
            void LoadConfig().then((cfg) => {
                if (cancelled || pureCodingTabRef.current) return;
                setPermissionMode(cfg?.subagent_full_access === true ? "full" : "request");
            }).catch(() => { /* ignore */ });
            return () => { cancelled = true; };
        }
        void GetCodingWorkbenchPermission(projectPath).then((mode) => {
            if (cancelled) return;
            if (mode === "full" || mode === "workspace" || mode === "request") {
                setPermissionMode(mode);
            }
        }).catch(() => { /* ignore */ });
        return () => { cancelled = true; };
    }, [activeTab?.id, activeTab?.agentMode, activeTab?.projectPath, activeTab?.type]);
    // Pure coding: when /goal creates/updates an objective, mirror it into the session-plan banner.
    useEffect(() => {
        const off = EventsOn("goal-state-changed", (payload: any) => {
            if (!pureCodingTabRef.current) return;
            // Normalize path separators (Go may emit Windows `\`, FE uses `/`).
            const goalUser = normalizeAssistantSessionKey(String(payload?.user_id || ""));
            const activeSession = normalizeAssistantSessionKey(pureCodingSessionKeyRef.current);
            // Only apply goals belonging to the active pure-coding tab.
            if (activeSession && goalUser && goalUser !== activeSession) return;
            const objective = String(payload?.objective || "").trim();
            if (!objective) return;
            const status = String(payload?.status || "").toLowerCase();
            // Active (or newly created) goals refresh the session plan; terminal states leave the plan.
            if (status && status !== "active" && status !== "paused") return;
            setCodingSessionPlan(objective);
            setCodingSessionPlanDraft(objective);
            setRemoteReconnect(prev => ({ ...prev, sessionPlan: objective }));
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    // Pure coding: load session plan + remote reconnect status.
    useEffect(() => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        const pureCoding = activeTab?.agentMode === "coding_dev" || activeTab?.agentMode === "remote_coding_dev";
        const isRemote = activeTab?.agentMode === "remote_coding_dev";
        if (!pureCoding || !projectPath) {
            setCodingSessionPlan("");
            setCodingExecutionPlan("");
            setCodingPlanMode("auto");
            setCodingPendingApproval(false);
            setCodingStepStatuses([]);
            setCodingSessionCost("");
            setCodingWorktreeMode("auto");
            setCodingRouteInfo("");
            setCodingConflictCount(0);
            setCodingConflicts([]);
            setCodingConflictOpen(false);
            setCodingConflictDiffs([]);
            setCodingConflictActiveId("");
            setCodingConflictSelected([]);
            setCodingConflictFocusFile("");
            setCodingConflictPreview(null);
            setCodingConflictEditDraft("");
            setCodingConflictTriple(null);
            setCodingConflictLog([]);
            setCodingRoutePref("auto");
            setCodingRouteCaps([]);
            setCodingCheckpointLabel("");
            setCodingCheckpointFiles([]);
            setCodingCheckpointSnapshots(0);
            setCodingCheckpointHistory([]);
            setCodingSidecarStats(null);
            setCodingHooksInfo(null);
            setCodingBackgroundVerify("");
            setCodingSessionPlanDraft("");
            setCodingSessionPlanEditing(false);
            setCodingPendingPlanEditing(false);
            setCodingPendingPlanDraft("");
            setRemoteReconnect(prev => (prev.needsReconnect || prev.host || prev.password || prev.success
                ? { needsReconnect: false, host: "", user: "", port: 22, workDir: "", password: "", connecting: false, error: "", success: "", sessionPlan: "" }
                : prev));
            return;
        }
        let cancelled = false;
        setRemoteReconnectStatusPath("");
        // The reconnect form is rendered from one shared state object. Clear
        // its visual state before hydrating the newly active coding project so
        // an in-flight connection from the previous tab cannot make this tab
        // appear to be connecting to the previous host.
        setRemoteReconnect(prev => (prev.needsReconnect || prev.host || prev.user || prev.password || prev.connecting || prev.error || prev.success || prev.sessionPlan
            ? { needsReconnect: false, host: "", user: "", port: 22, workDir: "", password: "", connecting: false, error: "", success: "", sessionPlan: "" }
            : prev));
        void GetCodingWorkbenchStatus(projectPath).then((st) => {
            if (cancelled || !st) return;
            setRemoteReconnectStatusPath(projectPath);
            const plan = String(st.session_plan || "").trim();
            const execPlan = String(st.execution_plan || "").trim();
            setCodingSessionPlan(plan);
            setCodingExecutionPlan(execPlan);
            const mode = String(st.plan_mode || "auto").toLowerCase();
            setCodingPlanMode(mode === "approve" || mode === "off" ? mode : "auto");
            {
                const pending = !!st.pending_approval;
                setCodingPendingApproval(pending);
                if (!pending) {
                    setCodingPendingPlanEditing(false);
                }
            }
            setCodingStepStatuses(Array.isArray(st.step_statuses) ? st.step_statuses.map((s: any) => ({
                index: Number(s.index) || 0,
                title: String(s.title || ""),
                status: String(s.status || "pending"),
                summary: String(s.summary || ""),
                verify_cmd: String(s.verify_cmd || ""),
                verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
            })) : []);
            {
                const inTok = Number(st.session_input_tokens) || 0;
                const outTok = Number(st.session_output_tokens) || 0;
                const cost = Number(st.session_est_cost_rmb) || 0;
                if (inTok > 0 || outTok > 0 || cost > 0) {
                    let line = `in=${inTok} out=${outTok}`;
                    if (cost > 0) line += ` · ~¥${cost.toFixed(4)}`;
                    setCodingSessionCost(line);
                } else {
                    setCodingSessionCost("");
                }
            }
            {
                const wm = String(st.worktree_mode || "auto").toLowerCase();
                setCodingWorktreeMode(wm === "always" || wm === "off" ? wm : "auto");
            }
            {
                const model = String(st.route_model || "").trim();
                const src = String(st.route_source || "").trim();
                if (model) {
                    setCodingRouteInfo(src ? `${model} (${src})` : model);
                } else {
                    setCodingRouteInfo("");
                }
            }
            setCodingConflictCount(Number(st.conflict_count) || (Array.isArray(st.conflicts) ? st.conflicts.length : 0) || 0);
            setCodingConflictLog(Array.isArray(st.conflict_log) ? st.conflict_log.map(String).slice(-8) : []);
            const mappedConflicts = Array.isArray(st.conflicts) ? st.conflicts.map((c: any) => ({
                id: String(c.id || ""),
                step_index: Number(c.step_index) || 0,
                path: String(c.path || ""),
                kind: String(c.kind || ""),
                files: Array.isArray(c.files) ? c.files.map(String) : [],
            })) : [];
            setCodingConflicts(mappedConflicts);
            const restoredActive = String(st.conflict_active_id || "").trim();
            const restoredSelected = Array.isArray(st.conflict_selected) ? st.conflict_selected.map(String).filter(Boolean) : [];
            const restoredFocus = String(st.conflict_focus_file || "").trim();
            if (restoredActive && mappedConflicts.some((c) => c.id === restoredActive)) {
                setCodingConflictActiveId(restoredActive);
                setCodingConflictSelected(restoredSelected);
                codingConflictSelectedRef.current = restoredSelected;
                setCodingConflictFocusFile(restoredFocus);
                codingConflictFocusFileRef.current = restoredFocus;
                setCodingConflictOpen(true);
                // Load diffs for restored active conflict (keep multi-select).
                void GetCodingWorkbenchConflictDiffs(projectPath, restoredActive).then((diffs) => {
                    setCodingConflictDiffs(Array.isArray(diffs) ? diffs.map((d: any) => ({
                        path: String(d.path || ""),
                        status: String(d.status || ""),
                        unified: String(d.unified || ""),
                        three_way: String(d.three_way || ""),
                        base_head: String(d.base_head || ""),
                    })) : []);
                    if (restoredFocus) {
                        void GetCodingWorkbenchConflictFilePreview(projectPath, restoredActive, restoredFocus, "main").then((prev) => {
                            setCodingConflictPreview({
                                side: String(prev?.side || "main"),
                                path: String(prev?.path || restoredFocus),
                                content: String(prev?.content || ""),
                                truncated: !!prev?.truncated,
                                missing: !!prev?.missing,
                            });
                            setCodingConflictPreviewSide("main");
                        }).catch(() => { /* ignore */ });
                    }
                }).catch(() => setCodingConflictDiffs([]));
            }
            {
                const rp = String(st.route_pref || "auto").toLowerCase();
                setCodingRoutePref(rp === "primary" || rp === "reasoning" || rp === "vision" ? rp : "auto");
            }
            setCodingRouteCaps(Array.isArray(st.route_capabilities) ? st.route_capabilities.map((c: any) => ({
                pref: String(c.pref || ""),
                available: c.available !== false,
                model: String(c.model || ""),
                source: String(c.source || ""),
                note: String(c.note || ""),
            })) : []);
            setCodingCheckpointLabel(String(st.checkpoint_label || "").trim());
            setCodingCheckpointFiles(Array.isArray(st.checkpoint_files) ? st.checkpoint_files.map(String) : []);
            setCodingCheckpointSnapshots(Number(st.checkpoint_snapshots) || 0);
            setCodingCheckpointHistory(Array.isArray(st.checkpoint_history) ? st.checkpoint_history.map((e: any) => ({
                label: String(e.label || ""),
                summary: String(e.summary || ""),
                snapshot_count: Number(e.snapshot_count) || 0,
                current: !!e.current,
                created_at: Number(e.created_at) || 0,
            })).filter((e: { label: string }) => !!e.label) : []);
            setCodingBackgroundVerify(String(st.background_verify || "").trim());
            if (st.hooks_active) {
                setCodingHooksInfo({
                    active: true,
                    phases: Array.isArray(st.hooks_phases) ? st.hooks_phases.map(String) : [],
                    count: Number(st.hooks_command_count) || 0,
                    failOnError: !!st.hooks_fail_on_error,
                });
            } else {
                setCodingHooksInfo(null);
            }
            if (!codingSessionPlanEditing) {
                setCodingSessionPlanDraft(plan);
            }
            // Apply remote reconnect fields before optional follow-up RPCs so a
            // missing/throwing sidecar binding cannot skip SSH form hydration.
            if (!isRemote) {
                setRemoteReconnect(prev => (prev.needsReconnect || prev.host || prev.password
                    ? { needsReconnect: false, host: "", user: "", port: 22, workDir: "", password: "", connecting: false, error: "", success: "", sessionPlan: "" }
                    : prev));
            } else {
                const needs = !!st.needs_reconnect;
                const safety = st.remote_safety === "diagnosis"
                    ? "diagnosis"
                    : (activeTab.remoteSafety === "diagnosis" ? "diagnosis" : undefined);
                const host = String(st.remote_host || activeTab.remoteHost || "").trim();
                const user = String(st.remote_user || "").trim();
                const port = Number(st.remote_port) > 0 ? Number(st.remote_port) : 22;
                const workDir = String(st.remote_work_dir || "").trim();
                // Vault + identity binding outside setState (updater must stay pure).
                if (!needs) {
                    const projectPrefix = `${projectPath}|`;
                    // Do not clear another tab's reconnect reservation merely
                    // because this tab is already connected.
                    if (remoteAutoReconnectKeyRef.current.startsWith(projectPrefix)) {
                        remoteAutoReconnectKeyRef.current = "";
                    }
                    if (remoteReconnectInFlightRef.current.startsWith(projectPrefix)) {
                        remoteReconnectInFlightRef.current = "";
                    }
                    remotePasswordBoundIdentityRef.current = "";
                    setRemoteReconnect(prev => ({
                        ...prev,
                        needsReconnect: false,
                        safety,
                        host: host || prev.host || "",
                        user: user || prev.user || "",
                        port: (host || user) ? port : (prev.port || 22),
                        workDir: workDir || prev.workDir || "",
                        password: "",
                        sessionPlan: plan || prev.sessionPlan,
                        error: "",
                        success: prev.success,
                        connecting: false,
                    }));
                } else if (host && user) {
                    const rememberedPassword = loadRemoteSSHPassword(host, user, port);
                    const nextBoundKey = remoteSSHPasswordVaultKey(host, user, port);
                    const boundKey = remotePasswordBoundIdentityRef.current;
                    const identityChanged = !!(boundKey && nextBoundKey !== boundKey);
                    remotePasswordBoundIdentityRef.current = nextBoundKey;
                    setRemoteReconnect(prev => {
                        let nextPassword = "";
                        if (prev.connecting) {
                            nextPassword = prev.password;
                        } else if (identityChanged) {
                            nextPassword = rememberedPassword || "";
                        } else {
                            nextPassword = prev.password || rememberedPassword || "";
                        }
                        return {
                            ...prev,
                            needsReconnect: true,
                            safety,
                            host: host || prev.host || "",
                            user: user || prev.user || "",
                            port: port || prev.port || 22,
                            workDir: workDir || prev.workDir || "",
                            password: nextPassword,
                            sessionPlan: plan || prev.sessionPlan,
                            error: prev.error || "",
                            success: "",
                            connecting: prev.connecting,
                        };
                    });
                } else {
                    // Server has not yet filled host/user — keep form, mark needs only.
                    setRemoteReconnect(prev => ({
                        ...prev,
                        needsReconnect: true,
                        safety,
                        workDir: workDir || prev.workDir || "",
                        sessionPlan: plan || prev.sessionPlan,
                        error: prev.error || "",
                        success: "",
                        connecting: prev.connecting,
                    }));
                }
            }
            try {
                void GetCodingWorkbenchCheckpointSidecarStats(projectPath).then((ss) => {
                    if (!ss) {
                        setCodingSidecarStats(null);
                        return;
                    }
                    setCodingSidecarStats({
                        total_bytes: Number(ss.total_bytes) || 0,
                        max_bytes: Number(ss.max_bytes) || 0,
                        usage_ratio: Number(ss.usage_ratio) || 0,
                        dir_count: Number(ss.dir_count) || 0,
                        user_bytes: Number(ss.user_bytes) || 0,
                    });
                }).catch(() => { /* ignore */ });
            } catch {
                /* optional binding may be absent in tests */
            }
        }).catch(() => {
            if (!cancelled && isRemote) {
                setRemoteReconnectStatusPath(projectPath);
                setRemoteReconnect(prev => ({
                    ...prev,
                    needsReconnect: true,
                    host: activeTab.remoteHost || prev.host,
                    success: "",
                }));
            }
        });
        return () => { cancelled = true; };
        // codingSessionPlanEditing intentionally omitted to avoid refetch loops while typing.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeTab?.id, activeTab?.agentMode, activeTab?.projectPath, activeTab?.type, activeTab?.remoteHost]);
    // Live Todo checklist while multi-step pure-coding (local + remote) runs:
    // backend emits coding-workbench-steps on each step status change.
    useEffect(() => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        const pureCoding = activeTab?.agentMode === "coding_dev" || activeTab?.agentMode === "remote_coding_dev";
        if (!pureCoding || !projectPath) return;
        const normPath = (p: string) => String(p || "").replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
        const expected = normPath(projectPath);
        const applySteps = (raw: any) => {
            if (!Array.isArray(raw)) {
                setCodingStepStatuses([]);
                return;
            }
            setCodingStepStatuses(raw.map((s: any) => ({
                index: Number(s.index) || 0,
                title: String(s.title || ""),
                status: String(s.status || "pending"),
                summary: String(s.summary || ""),
                verify_cmd: String(s.verify_cmd || ""),
                verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
            })));
        };
        const off = EventsOn("coding-workbench-steps", (payload: any) => {
            if (!pureCodingTabRef.current) return;
            const pp = normPath(String(payload?.project_path || payload?.projectPath || ""));
            if (pp && expected && pp !== expected) return;
            if (Array.isArray(payload?.step_statuses) || Array.isArray(payload?.stepStatuses)) {
                applySteps(payload?.step_statuses ?? payload?.stepStatuses);
                const plan = String(payload?.execution_plan || payload?.executionPlan || "").trim();
                if (plan) setCodingExecutionPlan(plan);
                return;
            }
            // Fallback: re-poll status if payload shape unexpected.
            void GetCodingWorkbenchStatus(projectPath).then((st) => {
                if (!st) return;
                applySteps(st.step_statuses);
                const execPlan = String(st.execution_plan || "").trim();
                if (execPlan) setCodingExecutionPlan(execPlan);
            });
        });
        return () => { if (typeof off === "function") off(); };
    }, [activeTab?.id, activeTab?.agentMode, activeTab?.projectPath, activeTab?.type]);
    // After a pure-coding turn finishes, refresh session/execution plan (auto-plan is written mid-turn).
    useEffect(() => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        const pureCoding = activeTab?.agentMode === "coding_dev" || activeTab?.agentMode === "remote_coding_dev";
        if (!pureCoding || !projectPath) return;
        const expectedSession = projectSessionKey(projectPath);
        const off = EventsOn("ai-assistant-response", (payload: any) => {
            if (!pureCodingTabRef.current) return;
            const sk = normalizeAssistantSessionKey(String(payload?.session_key || payload?.SessionKey || ""));
            if (sk && expectedSession && sk !== normalizeAssistantSessionKey(expectedSession)) return;
            void GetCodingWorkbenchStatus(projectPath).then((st) => {
                if (!st) return;
                setCodingSessionPlan(String(st.session_plan || "").trim());
                setCodingExecutionPlan(String(st.execution_plan || "").trim());
                const mode = String(st.plan_mode || "auto").toLowerCase();
                setCodingPlanMode(mode === "approve" || mode === "off" ? mode : "auto");
                {
                    const pending = !!st.pending_approval;
                    setCodingPendingApproval(pending);
                    if (!pending) {
                        setCodingPendingPlanEditing(false);
                    }
                }
                setCodingStepStatuses(Array.isArray(st.step_statuses) ? st.step_statuses.map((s: any) => ({
                    index: Number(s.index) || 0,
                    title: String(s.title || ""),
                    status: String(s.status || "pending"),
                    summary: String(s.summary || ""),
                    verify_cmd: String(s.verify_cmd || ""),
                    verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
                })) : []);
                {
                    const inTok = Number(st.session_input_tokens) || 0;
                    const outTok = Number(st.session_output_tokens) || 0;
                    const cost = Number(st.session_est_cost_rmb) || 0;
                    if (inTok > 0 || outTok > 0 || cost > 0) {
                        let line = `in=${inTok} out=${outTok}`;
                        if (cost > 0) line += ` · ~¥${cost.toFixed(4)}`;
                        setCodingSessionCost(line);
                    } else {
                        setCodingSessionCost("");
                    }
                }
                {
                    const model = String(st.route_model || "").trim();
                    const src = String(st.route_source || "").trim();
                    if (model) {
                        setCodingRouteInfo(src ? `${model} (${src})` : model);
                    }
                }
                setCodingConflictCount(Number(st.conflict_count) || (Array.isArray(st.conflicts) ? st.conflicts.length : 0) || 0);
                setCodingCheckpointLabel(String(st.checkpoint_label || "").trim());
                setCodingBackgroundVerify(String(st.background_verify || "").trim());
            }).catch(() => { /* ignore */ });
        });
        return () => { if (typeof off === "function") off(); };
    }, [activeTab?.id, activeTab?.agentMode, activeTab?.projectPath, activeTab?.type]);
    const handleRemoteCodingReconnect = useCallback(async (opts?: { auto?: boolean }) => {
        const maxAttempts = 3;
        const retryDelayMs = [500, 1_000] as const;
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        // Capture tab identity before awaiting SSH. The user may switch tabs
        // while the connection is in flight; its queued IM prompt must still
        // belong to the tab that initiated this reconnect.
        const reconnectTabId = activeTab?.type === "project" ? activeTab.id : "";
        if (!projectPath) return;
        const snap = remoteReconnectRef.current;
        const host = snap.host.trim();
        const user = snap.user.trim();
        const workDir = snap.workDir.trim();
        const port = snap.port > 0 ? snap.port : 22;
        // Auto: vault only (never use half-typed React state password).
        // Manual: form password (user may have edited a bad remembered value).
        const password = opts?.auto
            ? loadRemoteSSHPassword(host, user, port)
            : snap.password;
        if (!host || !user || !workDir || !password) {
            if (opts?.auto) return;
            setRemoteReconnect(prev => ({
                ...prev,
                error: localizeText(lang, "Host, user, password and remote work directory are required", "主机、用户名、密码和远程工作目录均为必填", "主機、使用者名稱、密碼和遠端工作目錄均為必填"),
                success: "",
            }));
            return;
        }
        const autoKey = `${projectPath}|${remoteSSHPasswordVaultKey(host, user, port)}`;
        if (remoteReconnectInFlightRef.current === autoKey) return;
        if (opts?.auto) {
            // Effect may have already reserved autoKey; only skip when another attempt is mid-flight.
            if (remoteAutoReconnectKeyRef.current === autoKey && remoteReconnectInFlightRef.current === autoKey) {
                return;
            }
            remoteAutoReconnectKeyRef.current = autoKey;
        }
        remoteReconnectInFlightRef.current = autoKey;
        setRemoteReconnect(prev => ({
            ...prev,
            connecting: true,
            error: "",
            success: "",
            // Prefill so the form shows what auto is trying when it fails.
            password: prev.password || password,
        }));
        try {
            let st: any;
            let lastError: unknown;
            let ok = false;
            for (let attempt = 1; attempt <= maxAttempts; attempt++) {
                try {
                    const prepareRemoteEnvironment = snap.safety === "diagnosis"
                        ? PrepareRemoteOpsDiagnosisEnvironment
                        : PrepareRemoteCodingEnvironment;
                    await prepareRemoteEnvironment(projectPath, host, user, password, workDir, port);
                    st = await GetCodingWorkbenchStatus(projectPath);
                    ok = !st?.needs_reconnect && !!st?.armed;
                    if (ok) break;
                    lastError = st?.message || "reconnect incomplete";
                } catch (error) {
                    lastError = error;
                }
                if (attempt < maxAttempts) {
                    const retryNumber = attempt + 1;
                    if (activeTabRef.current.id === reconnectTabId
                        && activeTabRef.current.type === "project"
                        && activeTabRef.current.projectPath === projectPath) {
                        setRemoteReconnect(prev => ({
                            ...prev,
                            connecting: true,
                            error: "",
                            success: localizeText(lang, `Reconnect attempt ${attempt} failed; retrying (${retryNumber}/${maxAttempts})…`, `第 ${attempt} 次重连失败，正在重试（${retryNumber}/${maxAttempts}）…`, `第 ${attempt} 次重新連線失敗，正在重試（${retryNumber}/${maxAttempts}）…`),
                        }));
                    }
                    await new Promise<void>(resolve => window.setTimeout(resolve, retryDelayMs[attempt - 1]));
                }
            }
            if (!ok) throw lastError instanceof Error ? lastError : new Error(String(lastError || "reconnect failed"));
            const reconnectTabIsActive = activeTabRef.current.id === reconnectTabId
                && activeTabRef.current.type === "project"
                && activeTabRef.current.projectPath === projectPath;
            if (ok) {
                if (snap.safety !== "diagnosis") {
                    saveRemoteSSHPassword(host, user, password, port, workDir);
                }
                if (reconnectTabIsActive) setRemoteWorkspaceRefreshToken(token => token + 1);
                // Allow a future auto-reconnect after a later disconnect.
                remoteAutoReconnectKeyRef.current = "";
                remotePasswordBoundIdentityRef.current = remoteSSHPasswordVaultKey(host, user, port);
            } else {
                // Password shown on failure is bound to this target.
                remotePasswordBoundIdentityRef.current = remoteSSHPasswordVaultKey(host, user, port);
            }
            if (reconnectTabIsActive) setRemoteReconnect(prev => ({
                ...prev,
                connecting: false,
                // Keep password filled on failure so user can retry/edit; clear only after success.
                password: ok ? "" : (prev.password || password),
                needsReconnect: !ok,
                error: ok ? "" : (st?.message || localizeText(lang, "Reconnect incomplete", "重连未完成", "重連未完成")),
                success: ok
                    ? (snap.safety === "diagnosis"
                        ? localizeText(lang, "Reconnected. You can continue this remote maintenance task.", "已重新连接。可以继续此远程维护任务。", "已重新連線。可以繼續此遠端維護任務。")
                        : localizeText(lang, "Reconnected. You can continue sending messages in this remote coding session.", "已重新连接。可以继续在本远程编程会话中发送消息。", "已重新連線。可以繼續在本遠端程式工作階段中傳送訊息。"))
                    : "",
                sessionPlan: String(st?.session_plan || prev.sessionPlan || ""),
            }));
            if (reconnectTabIsActive && ok && st?.session_plan) {
                setCodingSessionPlan(String(st.session_plan));
                setCodingSessionPlanDraft(String(st.session_plan));
            }
            if (ok) {
                if (reconnectTabIsActive) requestAnimationFrame(() => inputRef.current?.focus());
                // An IM-started remote task must not send its prompt until the
                // workbench is armed.  Keep the prompt in tab state (rather than
                // an event payload) so automatic and manual reconnect share the
                // same one-shot path and no password ever leaves the browser vault.
                const tabId = reconnectTabId;
                const pendingState = tabId ? getTabState(tabId) : undefined;
                const pendingInitial = pendingState?.pendingRemoteInitialMessage;
                if (tabId && pendingInitial?.text.trim()) {
                    if (preparingProjectTabIdsRef.current.has(tabId)) {
                        pendingRemoteInitialSendRef.current.set(tabId, { text: pendingInitial.text, projectPath });
                    } else {
                        // Clear only once the send handoff has been accepted.
                        // If the UI unmounts or a transient route error occurs,
                        // retaining this state allows a later reconnect to retry
                        // instead of silently losing an IM-started task.
                        void sendMessageForTabRef.current?.(pendingInitial.text, { tabId, project_path: projectPath })
                            .then((sent) => {
                                if (sent === false) return;
                                const current = getTabState(tabId);
                                if (current?.pendingRemoteInitialMessage?.text === pendingInitial.text) {
                                    saveTabState(tabId, { ...current, pendingRemoteInitialMessage: undefined });
                                }
                            })
                            .catch((error) => console.warn("[AIAssistantPanel] pending remote initial send failed", { tabId, projectPath, error }));
                    }
                }
            }
        } catch (err) {
            // autoKey stays set on failure so we do not loop on a bad/stale password.
            remotePasswordBoundIdentityRef.current = remoteSSHPasswordVaultKey(host, user, port);
            const msg = err instanceof Error ? err.message : String(err || "reconnect failed");
            const reconnectTabIsActive = activeTabRef.current.id === reconnectTabId
                && activeTabRef.current.type === "project"
                && activeTabRef.current.projectPath === projectPath;
            if (reconnectTabIsActive) setRemoteReconnect(prev => ({
                ...prev,
                connecting: false,
                error: msg,
                success: "",
                password: prev.password || password,
            }));
        } finally {
            if (remoteReconnectInFlightRef.current === autoKey) remoteReconnectInFlightRef.current = "";
        }
    }, [activeTab?.id, activeTab?.projectPath, activeTab?.type, getTabState, lang, saveTabState]);
    /**
     * On identity-field blur: trim values and recall vault password only when the
     * bound target actually changed — never clobber a password the user is editing
     * for the same host/user/port.
     */
    const hydrateRemoteReconnectIdentity = useCallback(() => {
        const snap = remoteReconnectRef.current;
        const host = snap.host.trim();
        const user = snap.user.trim();
        const port = snap.port > 0 ? snap.port : 22;
        const nextBoundKey = (host && user) ? remoteSSHPasswordVaultKey(host, user, port) : "";
        const boundKey = remotePasswordBoundIdentityRef.current;
        // First commit (bound empty) or real host/user/port switch.
        const targetChanged = !!nextBoundKey && nextBoundKey !== boundKey;
        const remembered = nextBoundKey ? loadRemoteSSHPassword(host, user, port) : "";
        if (targetChanged) {
            remotePasswordBoundIdentityRef.current = nextBoundKey;
            if (remembered) {
                const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
                const nextAutoKey = `${projectPath}|${nextBoundKey}`;
                // New target with a known password may auto once after blur commit.
                if (remoteAutoReconnectKeyRef.current !== nextAutoKey) {
                    remoteAutoReconnectKeyRef.current = "";
                }
            }
        }
        setRemoteReconnect(prev => {
            const nextPassword = targetChanged
                ? (remembered || "")
                : prev.password; // same bound target: keep whatever the user typed
            if (
                prev.host === host
                && prev.user === user
                && prev.port === port
                && prev.password === nextPassword
            ) {
                return prev;
            }
            return {
                ...prev,
                host,
                user,
                port,
                password: nextPassword,
            };
        });
    }, [activeTab?.projectPath, activeTab?.type]);
    // Silent reconnect only with a fully remembered vault password — never mid-typing.
    // Handler is stable (reads remoteReconnectRef), so this does not re-fire on password keystrokes.
    useEffect(() => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        // The reconnect form is project-owned; do not reuse a previous tab's
        // hydrated state during the brief tab-switch render.
        if (remoteReconnectStatusPath !== projectPath) return;
        if (!remoteReconnect.needsReconnect || remoteReconnect.connecting) return;
        const host = remoteReconnect.host.trim();
        const user = remoteReconnect.user.trim();
        const workDir = remoteReconnect.workDir.trim();
        const port = remoteReconnect.port > 0 ? remoteReconnect.port : 22;
        if (!host || !user || !workDir) return;
        if (!loadRemoteSSHPassword(host, user, port)) return;
        if (!projectPath) return;
        const autoKey = `${projectPath}|${remoteSSHPasswordVaultKey(host, user, port)}`;
        // Reserve before async so Strict Mode / double-effect cannot start two prepares.
        if (remoteAutoReconnectKeyRef.current === autoKey) return;
        if (remoteReconnectInFlightRef.current === autoKey) return;
        remoteAutoReconnectKeyRef.current = autoKey;
        remotePasswordBoundIdentityRef.current = remoteSSHPasswordVaultKey(host, user, port);
        void handleRemoteCodingReconnect({ auto: true });
    }, [
        remoteReconnect.needsReconnect,
        remoteReconnect.connecting,
        remoteReconnect.host,
        remoteReconnect.user,
        remoteReconnect.workDir,
        remoteReconnect.port,
        remoteReconnectStatusPath,
        activeTab?.projectPath,
        activeTab?.type,
        handleRemoteCodingReconnect,
    ]);
    const handleSaveCodingSessionPlan = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        const next = codingSessionPlanDraft.trim();
        setCodingSessionPlanSaving(true);
        try {
            await SetCodingWorkbenchSessionPlan(projectPath, next);
            setCodingSessionPlan(next);
            setCodingSessionPlanEditing(false);
            setRemoteReconnect(prev => ({ ...prev, sessionPlan: next }));
        } catch {
            // keep draft open on failure
        } finally {
            setCodingSessionPlanSaving(false);
        }
    }, [activeTab?.projectPath, activeTab?.type, codingSessionPlanDraft]);
    const handleCodingPlanModeChange = useCallback(async (mode: "auto" | "approve" | "off") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        const previous = codingPlanMode;
        setCodingPlanMode(mode);
        try {
            await SetCodingWorkbenchPlanMode(projectPath, mode);
            const saved = await GetCodingWorkbenchPlanMode(projectPath);
            const next = String(saved || "auto").toLowerCase();
            setCodingPlanMode(next === "approve" || next === "off" ? next : "auto");
        } catch {
            setCodingPlanMode(previous);
        }
    }, [activeTab?.projectPath, activeTab?.type, codingPlanMode]);
    /** Approve / skip / reject pending multi-step plan via the lifecycle-aware task route. */
    const handleCodingPlanGate = useCallback(async (action: "approve" | "skip" | "reject") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        // Plan controls are task intents as well. Do not let the compact
        // workbench controls dispatch into a remote session before SSH is
        // reconnected (or before the coding environment has finished setup).
        if (!canDispatchCodingIntent(resolveCodingTaskPhase({
            agentMode: activeTab?.agentMode,
            preparing: preparingProjectTabIdsRef.current.has(activeTab.id),
            remoteNeedsReconnect: activeTab?.agentMode === "remote_coding_dev" && (
                remoteReconnectStatusPath === projectPath
                    ? remoteReconnect.needsReconnect
                    : !!activeTab.remoteNeedsReconnect
            ),
        }))) {
            return;
        }
        const cmd = action === "approve" ? "/plan approve" : action === "skip" ? "/plan skip" : "/plan reject";
        // Optimistic clear for reject; approve/skip keep pending until runner starts.
        if (action === "reject") {
            setCodingPendingApproval(false);
            setCodingPendingPlanEditing(false);
        }
        try {
            // Persist in-progress edits before approve so runner uses latest plan.
            // Skip discards multi-step plan — no need to rewrite pending steps.
            if (action === "approve" && codingPendingPlanEditing && codingPendingPlanDraft.trim()) {
                try {
                    const updated = await UpdateCodingWorkbenchPendingPlan(projectPath, codingPendingPlanDraft.trim());
                    if (updated?.markdown) {
                        setCodingExecutionPlan(String(updated.markdown));
                    }
                    setCodingPendingPlanEditing(false);
                } catch { /* keep draft; still attempt gate action */ }
            }
            const sent = await sendMessageForTabRef.current?.(cmd, {
                tabId: activeTab.id,
                project_path: projectPath,
            });
            if (sent === false) return;
            // Refresh sticky status (pending flag + steps).
            const st = await GetCodingWorkbenchStatus(projectPath);
            setCodingPendingApproval(!!st?.pending_approval);
            setCodingStepStatuses(Array.isArray(st?.step_statuses) ? st.step_statuses.map((s: any) => ({
                index: Number(s.index) || 0,
                title: String(s.title || ""),
                status: String(s.status || "pending"),
                summary: String(s.summary || ""),
                verify_cmd: String(s.verify_cmd || ""),
                verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
            })) : []);
            setCodingExecutionPlan(String(st?.execution_plan || "").trim());
        } catch {
            // Re-sync pending if reject optimistic clear failed
            try {
                const st = await GetCodingWorkbenchStatus(projectPath);
                setCodingPendingApproval(!!st?.pending_approval);
            } catch { /* ignore */ }
        }
    }, [activeTab?.agentMode, activeTab?.id, activeTab?.projectPath, activeTab?.remoteNeedsReconnect, activeTab?.type, codingPendingPlanEditing, codingPendingPlanDraft, remoteReconnect.needsReconnect, remoteReconnectStatusPath]);
    const handleSavePendingPlanEdit = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        const md = codingPendingPlanDraft.trim();
        if (!md) return;
        setCodingPendingPlanSaving(true);
        try {
            const updated = await UpdateCodingWorkbenchPendingPlan(projectPath, md);
            setCodingExecutionPlan(String(updated?.markdown || md));
            setCodingPendingPlanDraft(String(updated?.markdown || md));
            setCodingPendingPlanEditing(false);
            const st = await GetCodingWorkbenchStatus(projectPath);
            setCodingStepStatuses(Array.isArray(st?.step_statuses) ? st.step_statuses.map((s: any) => ({
                index: Number(s.index) || 0,
                title: String(s.title || ""),
                status: String(s.status || "pending"),
                summary: String(s.summary || ""),
                verify_cmd: String(s.verify_cmd || ""),
                verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
            })) : []);
            setCodingPendingApproval(!!st?.pending_approval);
        } catch { /* keep editor open */ }
        finally { setCodingPendingPlanSaving(false); }
    }, [activeTab?.projectPath, activeTab?.type, codingPendingPlanDraft]);
    const handleCodingWorktreeModeChange = useCallback(async (mode: "auto" | "always" | "off") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        const previous = codingWorktreeMode;
        setCodingWorktreeMode(mode);
        try {
            await SetCodingWorkbenchWorktreeMode(projectPath, mode);
            const saved = await GetCodingWorkbenchWorktreeMode(projectPath);
            const next = String(saved || "auto").toLowerCase();
            setCodingWorktreeMode(next === "always" || next === "off" ? next : "auto");
        } catch {
            setCodingWorktreeMode(previous);
        }
    }, [activeTab?.projectPath, activeTab?.type, codingWorktreeMode]);
    const handleCodingRoutePrefChange = useCallback(async (pref: "auto" | "primary" | "reasoning" | "vision") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        const previous = codingRoutePref;
        setCodingRoutePref(pref);
        try {
            await SetCodingWorkbenchRoutePref(projectPath, pref);
            const saved = await GetCodingWorkbenchRoutePref(projectPath);
            const next = String(saved || "auto").toLowerCase();
            setCodingRoutePref(next === "primary" || next === "reasoning" || next === "vision" ? next : "auto");
        } catch {
            setCodingRoutePref(previous);
        }
    }, [activeTab?.projectPath, activeTab?.type, codingRoutePref]);
    const refreshCodingConflicts = useCallback(async (): Promise<Array<{ id: string; step_index?: number; path?: string; kind?: string; files?: string[] }>> => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return [];
        try {
            const list = await ListCodingWorkbenchConflicts(projectPath);
            const mapped = Array.isArray(list) ? list.map((c: any) => ({
                id: String(c.id || ""),
                step_index: Number(c.step_index) || 0,
                path: String(c.path || ""),
                kind: String(c.kind || ""),
                files: Array.isArray(c.files) ? c.files.map(String) : [],
            })) : [];
            setCodingConflicts(mapped);
            setCodingConflictCount(mapped.length);
            return mapped;
        } catch {
            return [];
        }
    }, [activeTab?.projectPath, activeTab?.type]);
    const persistCodingConflictUIState = useCallback((activeId: string, selected: string[], focusFile: string) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        codingConflictSelectedRef.current = selected;
        codingConflictFocusFileRef.current = focusFile;
        if (codingConflictUIPersistTimer.current) {
            clearTimeout(codingConflictUIPersistTimer.current);
        }
        codingConflictUIPersistTimer.current = setTimeout(() => {
            void SetCodingWorkbenchConflictUIState(projectPath, activeId, focusFile, selected.join(",")).catch(() => { /* ignore */ });
        }, 300);
    }, [activeTab?.projectPath, activeTab?.type]);
    const openCodingConflict = useCallback(async (id: string, opts?: { keepSelection?: boolean; focusFile?: string }) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id) return;
        setCodingConflictActiveId(id);
        setCodingConflictOpen(true);
        const selected = opts?.keepSelection ? codingConflictSelectedRef.current : [];
        if (!opts?.keepSelection) {
            setCodingConflictSelected([]);
            codingConflictSelectedRef.current = [];
        }
        const focus = String(opts?.focusFile || codingConflictFocusFileRef.current || "").trim();
        if (focus) {
            setCodingConflictFocusFile(focus);
            codingConflictFocusFileRef.current = focus;
        }
        try {
            const diffs = await GetCodingWorkbenchConflictDiffs(projectPath, id);
            setCodingConflictDiffs(Array.isArray(diffs) ? diffs.map((d: any) => ({
                path: String(d.path || ""),
                status: String(d.status || ""),
                unified: String(d.unified || ""),
                three_way: String(d.three_way || ""),
                base_head: String(d.base_head || ""),
            })) : []);
        } catch {
            setCodingConflictDiffs([]);
        }
        persistCodingConflictUIState(id, selected, focus);
    }, [activeTab?.projectPath, activeTab?.type, persistCodingConflictUIState]);
    const toggleCodingConflictFile = useCallback((path: string) => {
        setCodingConflictSelected((prev) => {
            const next = prev.includes(path) ? prev.filter((p) => p !== path) : [...prev, path];
            codingConflictSelectedRef.current = next;
            persistCodingConflictUIState(codingConflictActiveId, next, codingConflictFocusFileRef.current);
            return next;
        });
    }, [codingConflictActiveId, persistCodingConflictUIState]);
    const loadCodingConflictPreview = useCallback(async (filePath: string, side: "main" | "theirs" | "base") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !codingConflictActiveId || !filePath) return;
        setCodingConflictFocusFile(filePath);
        codingConflictFocusFileRef.current = filePath;
        setCodingConflictPreviewSide(side);
        persistCodingConflictUIState(codingConflictActiveId, codingConflictSelectedRef.current, filePath);
        try {
            const prev = await GetCodingWorkbenchConflictFilePreview(projectPath, codingConflictActiveId, filePath, side);
            const content = String(prev?.content || "");
            setCodingConflictPreview({
                side: String(prev?.side || side),
                path: String(prev?.path || filePath),
                content,
                truncated: !!prev?.truncated,
                missing: !!prev?.missing,
            });
            setCodingConflictEditDraft(prev?.missing ? "" : content);
            try {
                const triple = await GetCodingWorkbenchConflictFileTriple(projectPath, codingConflictActiveId, filePath);
                setCodingConflictTriple({
                    main: { content: String(triple?.main?.content || ""), missing: !!triple?.main?.missing },
                    theirs: { content: String(triple?.theirs?.content || ""), missing: !!triple?.theirs?.missing },
                    base: { content: String(triple?.base?.content || ""), missing: !!triple?.base?.missing },
                });
                codingConflictTripleScrollLock.current = false;
            } catch {
                setCodingConflictTriple(null);
            }
        } catch {
            setCodingConflictPreview({ side, path: filePath, content: "", missing: true });
            setCodingConflictEditDraft("");
            setCodingConflictTriple(null);
        }
    }, [activeTab?.projectPath, activeTab?.type, codingConflictActiveId, persistCodingConflictUIState]);
    const syncCodingConflictTripleScroll = useCallback((source: "base" | "main" | "theirs", scrollTop: number, scrollLeft: number) => {
        if (codingConflictTripleScrollLock.current) return;
        codingConflictTripleScrollLock.current = true;
        const refs = codingConflictTripleScrollRefs.current;
        (["base", "main", "theirs"] as const).forEach((key) => {
            if (key === source) return;
            const el = refs[key];
            if (!el) return;
            if (el.scrollTop !== scrollTop) el.scrollTop = scrollTop;
            if (el.scrollLeft !== scrollLeft) el.scrollLeft = scrollLeft;
        });
        requestAnimationFrame(() => {
            codingConflictTripleScrollLock.current = false;
        });
    }, []);
    const handleExportConflictLog = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        try {
            await ExportCodingWorkbenchConflictLog(projectPath);
            const st = await GetCodingWorkbenchStatus(projectPath);
            setCodingConflictLog(Array.isArray(st?.conflict_log) ? st.conflict_log.map(String).slice(-8) : []);
        } catch {
            /* empty log or backend error */
        }
    }, [activeTab?.projectPath, activeTab?.type]);
    const clearLocalConflictPanelState = useCallback((opts?: { close?: boolean; keepActiveId?: string }) => {
        setCodingConflictSelected([]);
        codingConflictSelectedRef.current = [];
        setCodingConflictPreview(null);
        setCodingConflictEditDraft("");
        setCodingConflictTriple(null);
        setCodingConflictFocusFile("");
        codingConflictFocusFileRef.current = "";
        setCodingConflictDiffs([]);
        if (opts?.close) {
            setCodingConflictActiveId("");
            setCodingConflictOpen(false);
        } else if (opts?.keepActiveId) {
            setCodingConflictActiveId(opts.keepActiveId);
        }
    }, []);
    const closeCodingConflictSidePanel = useCallback(() => {
        // Keep list/log; only hide the side pane so reopening is instant.
        setCodingConflictOpen(false);
    }, []);
    const handleWriteConflictEdit = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !codingConflictActiveId || !codingConflictFocusFile) return;
        const activeId = codingConflictActiveId;
        setCodingConflictBusy(true);
        try {
            await WriteCodingWorkbenchConflictFileContent(projectPath, activeId, codingConflictFocusFile, codingConflictEditDraft);
            clearLocalConflictPanelState();
            await refreshCodingConflicts();
            const list = await ListCodingWorkbenchConflicts(projectPath);
            const stillThere = Array.isArray(list) && list.some((c: any) => String(c.id || "") === activeId);
            if (stillThere) {
                await openCodingConflict(activeId, { keepSelection: false });
            } else {
                clearLocalConflictPanelState({ close: true });
            }
            // Refresh status for conflict log strip.
            try {
                const st = await GetCodingWorkbenchStatus(projectPath);
                setCodingConflictLog(Array.isArray(st?.conflict_log) ? st.conflict_log.map(String).slice(-8) : []);
            } catch { /* ignore */ }
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, codingConflictActiveId, codingConflictFocusFile, codingConflictEditDraft, refreshCodingConflicts, openCodingConflict, clearLocalConflictPanelState]);
    const handleResolveSelectedConflictFiles = useCallback(async (action: "adopt" | "keep" | "base") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !codingConflictActiveId || codingConflictSelected.length === 0) return;
        const activeId = codingConflictActiveId;
        setCodingConflictBusy(true);
        try {
            await ResolveCodingWorkbenchConflict(projectPath, activeId, action, codingConflictSelected.join(","));
            clearLocalConflictPanelState();
            await refreshCodingConflicts();
            const list = await ListCodingWorkbenchConflicts(projectPath);
            const stillThere = Array.isArray(list) && list.some((c: any) => String(c.id || "") === activeId);
            if (stillThere) {
                await openCodingConflict(activeId, { keepSelection: false });
            } else {
                clearLocalConflictPanelState({ close: true });
            }
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, codingConflictActiveId, codingConflictSelected, refreshCodingConflicts, openCodingConflict, clearLocalConflictPanelState]);
    const handleApplyPreviewSide = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !codingConflictActiveId || !codingConflictFocusFile) return;
        const activeId = codingConflictActiveId;
        setCodingConflictBusy(true);
        try {
            await ApplyCodingWorkbenchConflictPreviewSide(projectPath, activeId, codingConflictFocusFile, codingConflictPreviewSide);
            clearLocalConflictPanelState();
            await refreshCodingConflicts();
            const list = await ListCodingWorkbenchConflicts(projectPath);
            const stillThere = Array.isArray(list) && list.some((c: any) => String(c.id || "") === activeId);
            if (stillThere) {
                await openCodingConflict(activeId, { keepSelection: false });
            } else {
                clearLocalConflictPanelState({ close: true });
            }
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, codingConflictActiveId, codingConflictFocusFile, codingConflictPreviewSide, refreshCodingConflicts, openCodingConflict, clearLocalConflictPanelState]);
    const handleOpenConflictFile = useCallback(async (filePath: string, side: "main" | "theirs") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !codingConflictActiveId || !filePath) return;
        try {
            await OpenCodingWorkbenchConflictFile(projectPath, codingConflictActiveId, filePath, side);
        } catch { /* ignore */ }
    }, [activeTab?.projectPath, activeTab?.type, codingConflictActiveId]);
    const handleAdoptConflict = useCallback(async (id: string, filesCSV = "") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id) return;
        setCodingConflictBusy(true);
        try {
            await AdoptCodingWorkbenchConflict(projectPath, id, filesCSV);
            await refreshCodingConflicts();
            if (!filesCSV) {
                clearLocalConflictPanelState({ close: true });
            } else {
                clearLocalConflictPanelState();
                const list = await ListCodingWorkbenchConflicts(projectPath);
                const stillThere = Array.isArray(list) && list.some((c: any) => String(c.id || "") === id);
                if (stillThere) {
                    await openCodingConflict(id, { keepSelection: false });
                } else {
                    clearLocalConflictPanelState({ close: true });
                }
            }
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingConflicts, clearLocalConflictPanelState, openCodingConflict]);
    const handleDiscardConflict = useCallback(async (id: string) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id) return;
        setCodingConflictBusy(true);
        try {
            await DiscardCodingWorkbenchConflict(projectPath, id);
            await refreshCodingConflicts();
            if (codingConflictActiveId === id) {
                clearLocalConflictPanelState({ close: true });
            }
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingConflicts, codingConflictActiveId, clearLocalConflictPanelState]);
    const handleDiscardAllConflicts = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        setCodingConflictBusy(true);
        try {
            await DiscardAllCodingWorkbenchConflicts(projectPath);
            await refreshCodingConflicts();
            clearLocalConflictPanelState({ close: true });
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingConflicts, clearLocalConflictPanelState]);
    const refreshCodingWorkbenchStatusFields = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        try {
            const st = await GetCodingWorkbenchStatus(projectPath);
            if (!st) return;
            setCodingSessionPlan(String(st.session_plan || "").trim());
            setCodingExecutionPlan(String(st.execution_plan || "").trim());
            setCodingCheckpointLabel(String(st.checkpoint_label || "").trim());
            setCodingCheckpointFiles(Array.isArray(st.checkpoint_files) ? st.checkpoint_files.map(String) : []);
            setCodingCheckpointSnapshots(Number(st.checkpoint_snapshots) || 0);
            setCodingCheckpointHistory(Array.isArray(st.checkpoint_history) ? st.checkpoint_history.map((e: any) => ({
                label: String(e.label || ""),
                summary: String(e.summary || ""),
                snapshot_count: Number(e.snapshot_count) || 0,
                current: !!e.current,
                created_at: Number(e.created_at) || 0,
            })).filter((e: { label: string }) => !!e.label) : []);
            setCodingBackgroundVerify(String(st.background_verify || "").trim());
            if (st.hooks_active) {
                setCodingHooksInfo({
                    active: true,
                    phases: Array.isArray(st.hooks_phases) ? st.hooks_phases.map(String) : [],
                    count: Number(st.hooks_command_count) || 0,
                    failOnError: !!st.hooks_fail_on_error,
                });
            } else {
                setCodingHooksInfo(null);
            }
            {
                const pending = !!st.pending_approval;
                setCodingPendingApproval(pending);
                if (!pending) {
                    setCodingPendingPlanEditing(false);
                }
            }
            setCodingStepStatuses(Array.isArray(st.step_statuses) ? st.step_statuses.map((s: any) => ({
                index: Number(s.index) || 0,
                title: String(s.title || ""),
                status: String(s.status || "pending"),
                summary: String(s.summary || ""),
                verify_cmd: String(s.verify_cmd || ""),
                verify_ok: typeof s.verify_ok === "boolean" ? s.verify_ok : undefined,
            })) : []);
        } catch { /* ignore */ }
    }, [activeTab?.projectPath, activeTab?.type]);
    const handleSaveCodingCheckpoint = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        setCodingCheckpointBusy(true);
        try {
            const label = await SaveCodingWorkbenchCheckpoint(projectPath, "");
            setCodingCheckpointLabel(String(label || "").trim());
            await refreshCodingWorkbenchStatusFields();
        } catch { /* ignore */ }
        finally { setCodingCheckpointBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingWorkbenchStatusFields]);
    const handleRestoreCodingCheckpoint = useCallback(async (withFiles = false, label = "") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        setCodingCheckpointBusy(true);
        try {
            if (label) {
                await RestoreCodingWorkbenchCheckpointByLabel(projectPath, label, withFiles);
            } else {
                await RestoreCodingWorkbenchCheckpointEx(projectPath, withFiles);
            }
            await refreshCodingWorkbenchStatusFields();
            // Refresh list if history present.
            try {
                const list = await ListCodingWorkbenchCheckpoints(projectPath);
                setCodingCheckpointHistory(Array.isArray(list) ? list.map((e: any) => ({
                    label: String(e.label || ""),
                    summary: String(e.summary || ""),
                    snapshot_count: Number(e.snapshot_count) || 0,
                    current: !!e.current,
                    created_at: Number(e.created_at) || 0,
                })).filter((e: { label: string }) => !!e.label) : []);
            } catch { /* ignore */ }
        } catch { /* ignore */ }
        finally { setCodingCheckpointBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingWorkbenchStatusFields]);
    const handleKeepMainConflictFile = useCallback(async (id: string, filePath: string) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id || !filePath) return;
        setCodingConflictBusy(true);
        try {
            await KeepMainCodingWorkbenchConflict(projectPath, id, filePath);
            await refreshCodingConflicts();
            await openCodingConflict(id);
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingConflicts, openCodingConflict]);
    const handleAdoptBaseConflictFile = useCallback(async (id: string, filePath: string) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id || !filePath) return;
        setCodingConflictBusy(true);
        try {
            await AdoptBaseCodingWorkbenchConflict(projectPath, id, filePath);
            await refreshCodingConflicts();
            await openCodingConflict(id);
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingConflicts, openCodingConflict]);
    /** After a whole-conflict resolve: refresh list, clear active diffs, close side panel if none left. */
    const finalizeWholeConflictResolve = useCallback(async () => {
        const remaining = await refreshCodingConflicts();
        setCodingConflictDiffs([]);
        setCodingConflictActiveId("");
        setCodingConflictSelected([]);
        codingConflictSelectedRef.current = [];
        setCodingConflictPreview(null);
        setCodingConflictEditDraft("");
        setCodingConflictTriple(null);
        setCodingConflictFocusFile("");
        codingConflictFocusFileRef.current = "";
        if (remaining.length === 0) {
            clearLocalConflictPanelState({ close: true });
        }
        // remaining > 0: auto-open effect loads the next conflict when activeId is empty.
    }, [refreshCodingConflicts, clearLocalConflictPanelState]);
    const handleKeepMainConflictAll = useCallback(async (id: string) => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id) return;
        setCodingConflictBusy(true);
        try {
            // Empty filesCSV = keep main for all remaining files (discard isolation).
            await KeepMainCodingWorkbenchConflict(projectPath, id, "");
            await finalizeWholeConflictResolve();
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, finalizeWholeConflictResolve]);
    const handleResolveConflictBatch = useCallback(async (id: string, action: "adopt" | "keep" | "base") => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath || !id) return;
        setCodingConflictBusy(true);
        try {
            await ResolveCodingWorkbenchConflict(projectPath, id, action, "");
            await finalizeWholeConflictResolve();
        } catch { /* ignore */ }
        finally { setCodingConflictBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, finalizeWholeConflictResolve]);
    const refreshCodingSidecarStats = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        try {
            const st = await GetCodingWorkbenchCheckpointSidecarStats(projectPath);
            if (st && (Number(st.total_bytes) > 0 || Number(st.max_bytes) > 0 || Number(st.dir_count) > 0)) {
                setCodingSidecarStats({
                    total_bytes: Number(st.total_bytes) || 0,
                    max_bytes: Number(st.max_bytes) || 0,
                    usage_ratio: Number(st.usage_ratio) || 0,
                    dir_count: Number(st.dir_count) || 0,
                    user_bytes: Number(st.user_bytes) || 0,
                });
            } else {
                setCodingSidecarStats(st ? {
                    total_bytes: Number(st.total_bytes) || 0,
                    max_bytes: Number(st.max_bytes) || 0,
                    usage_ratio: Number(st.usage_ratio) || 0,
                    dir_count: Number(st.dir_count) || 0,
                    user_bytes: Number(st.user_bytes) || 0,
                } : null);
            }
        } catch { /* ignore */ }
    }, [activeTab?.projectPath, activeTab?.type]);
    const handlePruneCheckpoints = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        setCodingCheckpointBusy(true);
        try {
            await PruneCodingWorkbenchCheckpoints(projectPath);
            await refreshCodingWorkbenchStatusFields();
            await refreshCodingSidecarStats();
        } catch { /* ignore */ }
        finally { setCodingCheckpointBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type, refreshCodingWorkbenchStatusFields, refreshCodingSidecarStats]);
    const handleRunCodingBgVerify = useCallback(async () => {
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        if (!projectPath) return;
        setCodingBgVerifyBusy(true);
        try {
            await RunCodingWorkbenchBackgroundVerify(projectPath);
            setCodingBackgroundVerify("后台验证运行中…");
            // Poll a few times for completion.
            for (let i = 0; i < 8; i++) {
                await new Promise((r) => setTimeout(r, 1500));
                const st = await GetCodingWorkbenchStatus(projectPath);
                const sum = String(st?.background_verify || "").trim();
                if (sum && sum !== "后台验证运行中…") {
                    setCodingBackgroundVerify(sum);
                    break;
                }
                if (sum) setCodingBackgroundVerify(sum);
            }
        } catch { /* ignore */ }
        finally { setCodingBgVerifyBusy(false); }
    }, [activeTab?.projectPath, activeTab?.type]);
    // Filled after displayMessages is computed; used by "extract plan from chat".
    const displayMessagesForPlanRef = useRef<Array<{ role?: string; content?: string }>>([]);
    const handleExtractCodingSessionPlan = useCallback(() => {
        const suggested = suggestSessionPlanFromMessages(displayMessagesForPlanRef.current);
        if (!suggested) {
            setCodingSessionPlanEditing(true);
            setCodingSessionPlanDraft(codingSessionPlan || "");
            return;
        }
        setCodingSessionPlanDraft(suggested);
        setCodingSessionPlanEditing(true);
    }, [codingSessionPlan]);
    const handlePermissionModeChange = useCallback(async (mode: AssistantPermissionMode) => {
        const previous = permissionMode;
        const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
        const pureCoding = activeTab?.agentMode === "coding_dev" || activeTab?.agentMode === "remote_coding_dev";
        if (pureCoding && projectPath) {
            const next = mode === "full" || mode === "workspace" ? mode : "request";
            setPermissionMode(next);
            try {
                // Await sticky/global write so the next pure-coding turn sees full
                // control immediately (no race with rapid send after mode change).
                await SetCodingWorkbenchPermission(projectPath, next);
                const saved = await GetCodingWorkbenchPermission(projectPath);
                // Skip UI update if user left pure-coding mid-flight.
                if (!pureCodingTabRef.current) return;
                if (saved === "full" || saved === "workspace" || saved === "request") {
                    setPermissionMode(saved);
                }
            } catch {
                if (pureCodingTabRef.current) {
                    setPermissionMode(previous);
                }
            }
            return;
        }
        const next = mode === "full" ? "full" : "request";
        setPermissionMode(next);
        try {
            const saved = await PatchConfigFields({ subagent_full_access: next === "full" });
            if (!pureCodingTabRef.current) {
                setPermissionMode(saved?.subagent_full_access === true ? "full" : "request");
            }
        } catch {
            if (!pureCodingTabRef.current) {
                setPermissionMode(previous);
            }
        }
    }, [permissionMode, activeTab?.agentMode, activeTab?.projectPath, activeTab?.type]);
    activeTabIdForRecRef.current = activeTab?.id || "local";
    const activateTabRef = useRef(activateTab);
    activateTabRef.current = activateTab;
    const [renameGroupTargetTabId, setRenameGroupTargetTabId] = useState<string | null>(null);
    const [renameGroupValue, setRenameGroupValue] = useState("");
    const [renameGroupError, setRenameGroupError] = useState("");
    const [renameGroupSaving, setRenameGroupSaving] = useState(false);
    const renameGroupTargetTab = renameGroupTargetTabId ? tabState.tabs.find(tab => tab.id === renameGroupTargetTabId && tab.type === "group" && !tab.readOnly) : undefined;
    const openRenameGroupDialog = useCallback((tab: typeof tabState.tabs[number]) => {
        if (tab.type !== "group" || tab.readOnly) return;
        setRenameGroupTargetTabId(tab.id);
        setRenameGroupValue(getAITabDisplayTitle(tab, lang));
        setRenameGroupError("");
    }, [lang]);
    const closeRenameGroupDialog = useCallback(() => {
        setRenameGroupTargetTabId(null);
        setRenameGroupValue("");
        setRenameGroupError("");
        setRenameGroupSaving(false);
    }, []);
    useEffect(() => {
        if (!panelActive) closeRenameGroupDialog();
    }, [closeRenameGroupDialog, panelActive]);
    const submitRenameGroupDialog = useCallback(async () => {
        if (!renameGroupTargetTab || renameGroupSaving) return;
        const title = renameGroupValue.trim();
        if (!title) {
            setRenameGroupError(localizeText(lang, "Group name cannot be empty", "群名不能为空", "群名不能為空"));
            return;
        }
        if (title.length > 60) {
            setRenameGroupError(localizeText(lang, "Group name must be 60 characters or fewer", "群名不能超过 60 个字符", "群名不能超過 60 個字元"));
            return;
        }
        setRenameGroupSaving(true);
        try {
            if (renameGroupTargetTab.discussionId) {
                await GroupDiscussionRenameConsultation(renameGroupTargetTab.discussionId, title);
            }
            renameGroupTab(renameGroupTargetTab.id, title);
            closeRenameGroupDialog();
        } catch (error) {
            setRenameGroupSaving(false);
            setRenameGroupError(error instanceof Error ? error.message : String(error || localizeText(lang, "Failed to rename group", "修改群名失败", "修改群名失敗")));
        }
    }, [closeRenameGroupDialog, lang, renameGroupSaving, renameGroupTab, renameGroupTargetTab, renameGroupValue]);
    const clearActiveHistory = useCallback(async () => {
        // Close all right-side preview panels (workflow doc, code preview, agent view)
        closeAllPreviewPanelsRef.current?.();
        // Clear any pending skill recording card
        setSkillRecordingCard(null);
        if (activeTab.type === "project") {
            // Clear frontend state first so the welcome view appears immediately.
            clearTabConversation(activeTab.id);
            setProjectTabMessages([]);
            clearedProjectTabIdsRef.current.add(activeTab.id);
            latestProjectCloseSnapshotRef.current = {
                tabId: activeTab.id,
                projectPath: activeTab.projectPath,
                messages: [],
                inputText: "",
                scrollTop: 0,
            };
            // Clear preparing state if tab was in context-restore phase.
            preparingProjectTabIdsRef.current.delete(activeTab.id);
            setPreparingProjectTabIds(prev => {
                if (!prev.has(activeTab.id)) return prev;
                const next = new Set(prev);
                next.delete(activeTab.id);
                return next;
            });
            // Clear any in-flight round tracking for this tab so displayMessages'
            // roundMessages path doesn't pull stale messages from shared state,
            // and hasActiveDetachedProjectRound doesn't keep isBusy=true.
            const roundKey = activeTab.projectPath ? projectSessionKey(activeTab.projectPath) : '';
            let roundsChanged = false;
            if (roundKey && projectTabRoundsRef.current.has(roundKey)) {
                projectTabRoundsRef.current.delete(roundKey);
                roundsChanged = true;
            }
            for (const [key, detached] of detachedProjectRoundsRef.current) {
                if (detached.tabId === activeTab.id) {
                    detachedProjectRoundsRef.current.delete(key);
                    roundsChanged = true;
                }
            }
            if (roundsChanged) {
                setProjectTabRouteVersion(version => version + 1);
                setDetachedProjectRoundVersion(version => version + 1);
            }
            setQueueInteractionStarted(false);
            setQueueEditDraftActive(false);
            setEditingEntryId(null);
            // Fire-and-forget cancel the backend agent. The live-sync effect guard
            // and displayMessages guard prevent cancel responses from resurrecting.
            if (activeTab.projectPath) {
                const sessionKey = activeTab.sessionKey || `desktop-user:${activeTab.projectPath}`;
                // Invalidate all session-owned transient state before the async
                // cancel returns. Otherwise an open file picker or late stream
                // event can revive attachments/messages after a clear.
                forgetAIAssistantSessionRounds(sessionKey);
                CancelAIAssistantSessionForSession(sessionKey).catch(() => {});
            }
            return;
        }
        if (activeTab.type === "ve" || activeTab.type === "group") {
            clearTabConversation(activeTab.id);
            return;
        }
        if (activeTab.type === "expert") {
            // Expert tab: clear the tab-local history and cancel any running
            // backend session for this expert (fire-and-forget).
            clearTabConversation(activeTab.id);
            setProjectTabMessages([]);
            // Mark as explicitly cleared: blocks residual session events from
            // resurrecting the old conversation until the user sends again.
            clearedProjectTabIdsRef.current.add(activeTab.id);
            setQueueInteractionStarted(false);
            setQueueEditDraftActive(false);
            setEditingEntryId(null);
            const sessionKey = expertSessionKey(activeTab.expertId);
            if (sessionKey) {
                forgetAIAssistantSessionRounds(sessionKey);
                CancelAIAssistantSessionForSession(sessionKey).catch(() => {});
            }
            return;
        }
        // Local tab: full reset to show welcome/guide page.
        setQueueInteractionStarted(false);
        setQueueEditDraftActive(false);
        setEditingEntryId(null);
        await clearHistory();
    }, [activeTab.id, activeTab.type, activeTab.projectPath, activeTab.expertId, clearHistory, clearTabConversation]);
    const isLocalTabActive = activeTab.id === "local";
    const isProjectTabActive = activeTab.type === "project";
    const isExpertTabActive = activeTab.type === "expert";
    const isCodingDevEnvironment = isProjectTabActive && activeTab.agentMode === "coding_dev";
    const isRemoteCodingDevEnvironment = isProjectTabActive && activeTab.agentMode === "remote_coding_dev";
    const isPureCodingEnvironment = isCodingDevEnvironment || isRemoteCodingDevEnvironment;
    // The remote coding engine is reused for maintenance diagnostics, but the
    // visible label must describe the user's job rather than that implementation.
    const isRemoteMaintenanceEnvironment = isRemoteCodingDevEnvironment && activeTab.remoteSafety === "diagnosis";
    // The tab carries the launch-time reconnect signal synchronously. Status RPC
    // hydration follows asynchronously, so include both sources to avoid briefly
    // announcing a connected remote workbench with an enabled composer.
    const activeRemoteProjectPath = isRemoteCodingDevEnvironment ? (activeTab.projectPath || "") : "";
    const remoteCodingNeedsReconnect = isRemoteCodingDevEnvironment && (
        remoteReconnectStatusPath === activeRemoteProjectPath
            ? remoteReconnect.needsReconnect
            : !!activeTab.remoteNeedsReconnect
    );
    // Float control chrome: see buildCodingBannerChrome (dark local = muted sage, not neon green).
    const codingBannerChrome = useMemo(
        () => buildCodingBannerChrome({
            isDark: !!t.isDark,
            remote: isRemoteCodingDevEnvironment,
            theme: t,
        }),
        [isRemoteCodingDevEnvironment, t],
    );
    // Auto-expand floating coding controls on interrupts.
    // Keyed by project tab so switching pure-coding tasks does not skip / re-use the previous edge state.
    const codingInterruptScopeKey = isProjectTabActive
        ? `project:${activeTab.projectPath || activeTab.id}`
        : (isPureCodingEnvironment ? `tab:${activeTab.id}` : "");
    const prevCodingInterruptRef = useRef({ key: "", pending: false, conflicts: false, reconnect: false });
    useEffect(() => {
        if (!isPureCodingEnvironment || !codingInterruptScopeKey) {
            setCodingControlExpanded(false);
            setCodingConflictOpen(false);
            prevCodingInterruptRef.current = { key: "", pending: false, conflicts: false, reconnect: false };
            return;
        }
        const pending = !!codingPendingApproval;
        const conflicts = codingConflictCount > 0;
        const reconnect = remoteCodingNeedsReconnect;
        const prev = prevCodingInterruptRef.current;
        if (prev.key !== codingInterruptScopeKey) {
            // New coding tab/session: open float only for plan/SSH; conflicts use the side panel.
            setCodingControlExpanded(pending || reconnect);
            setCodingConflictOpen(conflicts);
            // Drop previous tab's active conflict UI until the new session status loads.
            setCodingConflictActiveId("");
            setCodingConflictDiffs([]);
            setCodingConflictSelected([]);
            codingConflictSelectedRef.current = [];
            setCodingConflictPreview(null);
            setCodingConflictEditDraft("");
            setCodingConflictTriple(null);
            setCodingConflictFocusFile("");
            codingConflictFocusFileRef.current = "";
            setCodingConflictPeak(conflicts ? codingConflictCount : 0);
            prevCodingInterruptRef.current = { key: codingInterruptScopeKey, pending, conflicts, reconnect };
            return;
        }
        // Rising edge only so the user can re-collapse without being forced open again.
        // Conflicts open the side panel (not the float) — see below.
        if ((pending && !prev.pending) || (reconnect && !prev.reconnect)) {
            setCodingControlExpanded(true);
        }
        // New isolation conflicts open the dedicated side panel (three-way lives there).
        if (conflicts && !prev.conflicts) {
            setCodingConflictOpen(true);
        }
        prevCodingInterruptRef.current = { key: codingInterruptScopeKey, pending, conflicts, reconnect };
    }, [isPureCodingEnvironment, codingInterruptScopeKey, codingPendingApproval, codingConflictCount, remoteCodingNeedsReconnect]);
    const showChatUI = isLocalTabActive || isProjectTabActive || isExpertTabActive;
    const activeSessionKey = isProjectTabActive && activeTab.projectPath
        ? (activeTab.sessionKey || `desktop-user:${activeTab.projectPath}`)
        : (isExpertTabActive && activeTab.expertId ? expertSessionKey(activeTab.expertId) : 'desktop-user');
    const {
        handlePaste: handlePasteBase,
        handleDragOver: handleDragOverBase,
        handleDrop: handleDropBase,
        pendingAttachments,
        setPendingAttachments,
    } = usePastedImageAttachments(activeSessionKey, { disabled: !ready || cancelPending });
    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, extractEntry } = useBufferQueue(activeSessionKey);
    const firingEntryIdsRef = useRef<Set<string>>(new Set());
    const drainingEntryIdsRef = useRef<Set<string>>(new Set());
    const [queueInFlightVersion, setQueueInFlightVersion] = useState(0);
    const [queueInteractionStarted, setQueueInteractionStarted] = useState(false);
    const refreshQueueInFlight = useCallback(() => setQueueInFlightVersion(version => version + 1), []);
    useEffect(() => {
        setActiveSessionKey(activeSessionKey);
        return () => {
            if (getActiveSessionKey() === activeSessionKey) setActiveSessionKey('');
        };
    }, [activeSessionKey]);
    const [projectTabMessages, setProjectTabMessages] = useState<ChatMessage[]>([]);
    const [projectTabRouteVersion, setProjectTabRouteVersion] = useState(0);
    const [panelSendInFlightSessionKeys, setPanelSendInFlightSessionKeys] = useState<Set<string>>(() => new Set());
    const markPanelSendInFlight = useCallback((sessionKey: string, inFlight: boolean) => {
        const key = String(sessionKey || '').trim();
        if (!key) return;
        setPanelSendInFlightSessionKeys(prev => {
            const has = prev.has(key);
            if (has === inFlight) return prev;
            const next = new Set(prev);
            if (inFlight) next.add(key);
            else next.delete(key);
            return next;
        });
    }, []);
    const [preparingProjectTabIds, setPreparingProjectTabIds] = useState<Set<string>>(() => new Set());
    const preparingProjectTabIdsRef = useRef<Set<string>>(new Set());
    const [preparingProjectTabModes, setPreparingProjectTabModes] = useState<Map<string, NonNullable<PendingProjectTabOpen["prepareMode"]>>>(() => new Map());
    const deferredProjectInitialSendsRef = useRef<Map<string, Array<{ text: string; options?: Record<string, unknown> }>>>(new Map());
    /** IM remote prompts awaiting SSH reconnect, keyed by their project tab. */
    const pendingRemoteInitialSendRef = useRef<Map<string, { text: string; projectPath: string }>>(new Map());
    const projectPrepareTimersRef = useRef<Map<string, number>>(new Map());
    const sendMessageForTabRef = useRef<((text: string, options?: Record<string, unknown>) => Promise<boolean>) | null>(null);
    /** Event-driven entry points share the same lifecycle gate as the composer. */
    const dispatchTaskIntentRef = useRef<((text: string, options?: Record<string, unknown>) => Promise<boolean>) | null>(null);
    const activeTabIdRef = useRef<string>(activeTab.id);
    const activeTabRef = useRef(activeTab);
    const latestDisplayMessagesRef = useRef<ChatMessage[]>([]);
    const clearedProjectTabIdsRef = useRef<Set<string>>(new Set());
    const latestProjectCloseSnapshotRef = useRef<{
        tabId: string;
        projectPath?: string;
        messages: ChatMessage[];
        inputText: string;
        scrollTop: number;
    } | null>(null);
    activeTabIdRef.current = activeTab.id;
    activeTabRef.current = activeTab;
    useEffect(() => () => {
        for (const timer of projectPrepareTimersRef.current.values()) {
            window.clearTimeout(timer);
        }
        projectPrepareTimersRef.current.clear();
    }, []);
    const prevActiveTabIdRef = useRef<string>(activeTab.id);
    const restoredPreviewStateRef = useRef<StoredAssistantPreviewState | null | undefined>(undefined);
    if (restoredPreviewStateRef.current === undefined) {
        restoredPreviewStateRef.current = readStoredAssistantPreviewState();
    }
    const restoredPreviewApplyingRef = useRef(false);
    const restoredPreviewOwnerProjectPathRef = useRef<string | undefined>(restoredPreviewStateRef.current?.ownerProjectPath);
    const previewStateMapRef = useRef<Map<string, { workflow: WorkflowUIState; code: CodePreviewUIState; previewMode: "workflow" | "code" }>>(
        restoredPreviewStateRef.current
            ? new Map([[restoredPreviewStateRef.current.ownerTabId, {
                workflow: cloneWorkflowUIState(restoredPreviewStateRef.current.workflow),
                code: cloneCodePreviewState(restoredPreviewStateRef.current.code),
                previewMode: restoredPreviewStateRef.current.previewMode,
            }]])
            : new Map(),
    );
    const previewOwnerTabRef = useRef<string>(restoredPreviewStateRef.current?.ownerTabId || (canShowAssistantCodingPreviewForTab(activeTab) ? activeTab.id : "local"));
    const previewOwnerResetPendingRef = useRef(false);
    const agentViewOwnerTabRef = useRef<string>(activeTab.id);
    // Declared early so the tab-switch effect can snapshot/restore without TDZ issues.
    // Updated after useCodePreviewState below.
    const codePreviewStateRef = useRef<CodePreviewUIState>(
        restoredPreviewStateRef.current
            ? cloneCodePreviewState(restoredPreviewStateRef.current.code)
            : initialCodePreviewState(),
    );
    const autoOpenedCodePreviewTabRef = useRef<string | null>(null);
    useEffect(() => {
        const prevTabId = prevActiveTabIdRef.current;
        const currentTabId = activeTab.id;
        if (prevTabId === currentTabId) return;
        const multipleTabsExist = tabState.tabs.length > 1;
        if (multipleTabsExist) {
            const currentTabCanOwnPreview = canShowAssistantCodingPreviewForTab(activeTab);
            // Ref is synced every render after useCodePreviewState; effect deps only
            // list activeTab.id, so read the ref for the latest open files/active flag.
            const liveCode = codePreviewStateRef.current;
            const ownerTabId = previewOwnerTabRef.current;
            const ownerTab = tabState.tabs.find(t => t.id === ownerTabId);
            const leavingTab = tabState.tabs.find(t => t.id === prevTabId);

            // Prefer snapshotting the tab we leave; fall back to the preview owner when
            // leaving a non-owning tab (e.g. VE) while path-scoped events still updated live state.
            const snapshotTabId =
                canShowAssistantCodingPreviewForTab(leavingTab) && prevTabId !== currentTabId
                    ? prevTabId
                    : (canShowAssistantCodingPreviewForTab(ownerTab) && ownerTabId !== currentTabId
                        ? ownerTabId
                        : null);
            if (snapshotTabId) {
                previewStateMapRef.current.set(snapshotTabId, {
                    workflow: getWorkflowSnapshot(),
                    code: cloneCodePreviewState(liveCode),
                    previewMode: codePreviewModeFromState(liveCode),
                });
            }

            if (currentTabCanOwnPreview && ownerTabId !== currentTabId) {
                const savedState = previewStateMapRef.current.get(currentTabId);
                if (savedState) {
                    restoreWorkflowState(savedState.workflow);
                    // Sync ref + map before setState flushes so same-tick pure-coding
                    // auto-open / persist see restored userClosed and active.
                    const codeClone = commitRestoredCodePreview(
                        savedState.code,
                        restoreCodePreviewState,
                        codePreviewStateRef,
                    );
                    previewStateMapRef.current.set(currentTabId, {
                        ...savedState,
                        code: codeClone,
                        previewMode: codePreviewModeFromState(codeClone),
                    });
                } else {
                    resetWorkflowState();
                    resetCodePreviewState();
                    codePreviewStateRef.current = initialCodePreviewState();
                }
                previewOwnerTabRef.current = currentTabId;
                // Do not clear autoOpenedCodePreviewTabRef: reopen would wipe userClosed.
                // Re-emit full workflow state from backend for the new active tab.
                if (activeTab.type === "project" && activeTab.projectPath) {
                    RefreshWorkflowV2StateForTab(activeTab.projectPath, [activeTab.id]).catch(() => {});
                }
            }
        }

        const prevTab = tabState.tabs.find(t => t.id === prevTabId);
        if (prevTab && (prevTab.type === "project" || prevTab.type === "expert")) {
            const scrollTop = outputContainerRef.current?.scrollTop || 0;
            let historyToSave = projectTabMessages;
            const prevRound = findProjectRoundForTab(prevTabId, prevTab.projectPath);
            if (sending && prevRound) {
                const prevSessionKey = prevTab.sessionKey || prevRound.sessionKey || projectSessionKey(prevTab.projectPath);
                const inFlightMessages = prevSessionKey
                    ? messages.slice(prevRound.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, prevSessionKey))
                    : [];
                if (inFlightMessages.length > 0) {
                    const existingIds = new Set(projectTabMessages.map((m: ChatMessage) => m.id));
                    const unique = inFlightMessages.filter((m: ChatMessage) => !existingIds.has(m.id));
                    if (unique.length > 0) {
                        historyToSave = [...projectTabMessages, ...unique];
                    }
                }
            }
            saveTabState(prevTabId, {
                history: historyToSave,
                scrollTop,
                inputText: localDraftInputValue,
            });
        }
        if (activeTab.type === "project" || activeTab.type === "expert") {
            const restored = getTabState(currentTabId);
            const hasPendingRoundForTab = activeTab.type === "project" && !!findProjectRoundForTab(currentTabId, activeTab.projectPath);
            if (!sending && !hasPendingRoundForTab) {
                projectTabRoundsRef.current.clear();
            }
            if (restored) {
                setProjectTabMessages((restored.history || []) as ChatMessage[]);
                setLocalDraftInputValue(restored.inputText || "");
                requestAnimationFrame(() => {
                    if (outputContainerRef.current && restored.scrollTop) {
                        outputContainerRef.current.scrollTop = restored.scrollTop;
                    }
                });
            } else {
                setProjectTabMessages([]);
                setLocalDraftInputValue("");
            }
        } else if (activeTab.id === "local") {
            setLocalDraftInputValue(draftInputValue);
        }
        // Compose mode is session-UI state, not per-tab draft — clear on switch.
        setComposeAction(null);
        prevActiveTabIdRef.current = currentTabId;
    }, [activeTab.id]); // eslint-disable-line react-hooks/exhaustive-deps
    // Track which tab owns the agentView — when agentView is set, record the
    // current active tab as its owner. Only show agentView when owning tab is active.
    useEffect(() => {
        if (agentView) {
            agentViewOwnerTabRef.current = activeTab.id;
        }
    }, [agentView]); // eslint-disable-line react-hooks/exhaustive-deps
    const projectTabRoundSeqRef = useRef(0);
    const projectTabRoundsRef = useRef<Map<string, { tabId: string | null; projectPath: string; sessionKey: string; baseline: number; seq: number }>>(new Map());
    const findProjectRoundForTab = useCallback((tabId: string, projectPath?: string | null) => {
		const tabSessionKey = getTabs().find(tab => tab.id === tabId)?.sessionKey || "";
		if (tabSessionKey) {
			const bySession = projectTabRoundsRef.current.get(tabSessionKey);
			if (bySession && bySession.tabId === tabId) return bySession;
		}
        const sessionKey = projectSessionKey(projectPath);
        if (sessionKey) {
            const byPath = projectTabRoundsRef.current.get(sessionKey);
            if (byPath && (byPath.tabId === tabId || byPath.projectPath === projectPath)) return byPath;
        }
        for (const round of projectTabRoundsRef.current.values()) {
            if (round.tabId === tabId) return round;
        }
        return undefined;
    }, [getTabs]);
    const detachedProjectRoundsRef = useRef<Map<string, { tabId: string; messageIds: Set<string> }>>(new Map());
    const [detachedProjectRoundVersion, setDetachedProjectRoundVersion] = useState(0);
    const projectTabMsgIdsRef = useRef<Set<string>>(null!);
    if (!projectTabMsgIdsRef.current) {
        projectTabMsgIdsRef.current = loadProjectTabMsgIds();
    }
    const projectTabIdsPersistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const persistProjectTabMsgIds = useCallback(() => {
        if (projectTabIdsPersistTimerRef.current) clearTimeout(projectTabIdsPersistTimerRef.current);
        projectTabIdsPersistTimerRef.current = setTimeout(() => {
            projectTabIdsPersistTimerRef.current = null;
            try {
                const ids = projectTabMsgIdsRef.current;
                if (ids.size === 0) {
                    localStorage.removeItem(PROJECT_TAB_MSG_IDS_KEY);
                } else {
                    const arr = [...ids];
                    const toStore = arr.length > 200 ? arr.slice(-200) : arr;
                    localStorage.setItem(PROJECT_TAB_MSG_IDS_KEY, JSON.stringify(toStore));
                }
            } catch { /* ignore */ }
        }, 500);
    }, []);
    useEffect(() => {
        if (detachedProjectRoundsRef.current.size === 0) return;
        let changed = false;
        const latestById = new Map(messages.map((m: ChatMessage) => [m.id, m]));
        for (const [key, detached] of detachedProjectRoundsRef.current) {
            const latestDetachedMessages = [...detached.messageIds]
                .map(id => latestById.get(id))
                .filter((m): m is ChatMessage => !!m);
            if (latestDetachedMessages.length === 0) {
                detachedProjectRoundsRef.current.delete(key);
                changed = true;
                continue;
            }
            const existingState = getTabState(detached.tabId);
            const existingHistory = (Array.isArray(existingState?.history) ? existingState?.history : []) as ChatMessage[];
            const nextById = new Map(existingHistory.map((m: ChatMessage) => [m.id, m]));
            for (const message of latestDetachedMessages) {
                nextById.set(message.id, message);
            }
            const nextHistory = [
                ...existingHistory.map((m: ChatMessage) => nextById.get(m.id) || m),
                ...latestDetachedMessages.filter(message => !existingHistory.some((m: ChatMessage) => m.id === message.id)),
            ];
            saveTabState(detached.tabId, {
                ...existingState,
                history: nextHistory,
            });
            if (activeTabIdRef.current === detached.tabId) {
                setProjectTabMessages(nextHistory);
            }
            const assistantStillPending = latestDetachedMessages.some(message => message.role === "assistant" && !message.content && !message.fields?.length && !message.actions?.length && !message.localFilePath && !message.localFilePaths?.length && !message.thumbnailBase64);
            if (!assistantStillPending) {
                detachedProjectRoundsRef.current.delete(key);
                changed = true;
            }
        }
        if (changed) setDetachedProjectRoundVersion(version => version + 1);
    }, [getTabState, messages, saveTabState]);
    useEffect(() => {
        const projectTabs = getTabs().filter(tab => (tab.type === "project" && tab.projectPath) || (tab.type === "expert" && tab.expertId));
        if (projectTabs.length === 0) return;
        for (const tab of projectTabs) {
            const sessionKey = tab.type === "expert" ? expertSessionKey(tab.expertId) : (tab.sessionKey || `desktop-user:${tab.projectPath}`);
            const liveMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, sessionKey));
            if (liveMessages.length === 0) continue;
            const existingState = getTabState(tab.id);
            // Skip syncing into a cleared/empty tab — prevents cancel responses or
            // residual streaming tokens from resurrecting after "New Task" clears
            // the conversation. New rounds populate tab state via the wasSending
            // effect and displayMessages' liveProjectMessages merge instead.
            // Expert tabs: only block when the tab was explicitly cleared (tracked
            // in clearedProjectTabIdsRef). A fresh expert tab may backfill live
            // session messages into an empty state — this is the channel that
            // persists the new conversation after clear → re-chat.
            const existingHistory = existingState?.history as unknown[] | undefined;
            if (!existingHistory || existingHistory.length === 0) {
                if (tab.type === "project" || clearedProjectTabIdsRef.current.has(tab.id)) continue;
            }
            const nextHistory = mergeChatMessages(existingHistory, liveMessages);
            if (chatHistoriesEquivalent(existingHistory as ChatMessage[] | undefined, nextHistory)) continue;
            saveTabState(tab.id, {
                ...existingState,
                history: nextHistory,
            });
            if (activeTabIdRef.current === tab.id) {
                setProjectTabMessages(nextHistory);
            }
            for (const message of liveMessages) {
                projectTabMsgIdsRef.current.add(message.id);
            }
            persistProjectTabMsgIds();
        }
    }, [getTabState, getTabs, messages, persistProjectTabMsgIds, saveTabState]);
    const displayMessages = useMemo(() => {
        if (!isProjectTabActive && !isExpertTabActive) {
            if (projectTabRoundsRef.current.size > 0) {
                const earliestProjectBaseline = Math.min(...Array.from(projectTabRoundsRef.current.values()).map(round => round.baseline));
                return messages.filter((message: ChatMessage, index: number) => {
                    if (!messageIsLocalSession(message)) return false;
                    const owner = typeof message.sessionKey === "string" ? message.sessionKey.trim() : "";
                    return !!owner || index < earliestProjectBaseline;
                });
            }
            if (projectTabMsgIdsRef.current.size > 0) {
                return messages.filter((m: ChatMessage) => messageIsLocalSession(m) && !projectTabMsgIdsRef.current.has(m.id));
            }
            return messages.filter(messageIsLocalSession);
        }
        if (isExpertTabActive) {
            // Expert tab: tab-state history (welcome seed + persisted conversation)
            // merged with live session events keyed by desktop-user:expert:<id>.
            const liveExpertMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, activeSessionKey));
            if (liveExpertMessages.length === 0) return projectTabMessages;
            if (projectTabMessages.length === 0) {
                // After an explicit clear, ignore residual session events until the user sends again.
                return clearedProjectTabIdsRef.current.has(activeTab.id) ? projectTabMessages : liveExpertMessages;
            }
            return mergeChatMessages(projectTabMessages, liveExpertMessages);
        }
        const liveProjectMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, activeSessionKey));
        // When projectTabMessages is empty (tab was cleared), don't merge residual
        // messages from shared state — they are stale cancel responses or streaming
        // leftovers. New rounds will populate projectTabMessages via wasSending effect.
        const mergedProjectMessages = liveProjectMessages.length > 0 && projectTabMessages.length > 0
            ? mergeChatMessages(projectTabMessages, liveProjectMessages)
            : projectTabMessages;
        const activeProjectRound = findProjectRoundForTab(activeTab.id, activeTab.projectPath);
        if (!sending || !activeProjectRound) return mergedProjectMessages;
        // During an active round, also include messages that arrived since the
        // round's baseline and belong to this session but may not carry an
        // explicit sessionKey (messageBelongsToSessionOrLegacy accepts both).
        // Use mergeChatMessages so that messages already present in
        // mergedProjectMessages are *replaced* rather than appended again.
        const roundMessages = messages.slice(activeProjectRound.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, activeSessionKey));
        if (roundMessages.length === 0) return mergedProjectMessages;
        return mergeChatMessages(mergedProjectMessages, roundMessages);
    }, [activeSessionKey, activeTab.id, activeTab.projectPath, findProjectRoundForTab, isProjectTabActive, isExpertTabActive, messages, projectTabMessages, projectTabRouteVersion, sending]);
    latestDisplayMessagesRef.current = displayMessages;
    displayMessagesForPlanRef.current = displayMessages as Array<{ role?: string; content?: string }>;
    const prevSendingRef = useRef(sending);
    useEffect(() => {
        const wasSending = prevSendingRef.current;
        prevSendingRef.current = sending;
        if (!wasSending && sending && isProjectTabActive && activeTab.projectPath && !findProjectRoundForTab(activeTab.id, activeTab.projectPath)) {
            const sessionKey = activeTab.sessionKey || projectSessionKey(activeTab.projectPath);
            if (sessionKey) {
                projectTabRoundsRef.current.set(sessionKey, {
                    tabId: activeTab.id,
                    projectPath: activeTab.projectPath,
                    sessionKey,
                    baseline: messages.length,
                    seq: projectTabRoundSeqRef.current,
                });
                setProjectTabRouteVersion(version => version + 1);
            }
        }
        if (wasSending && !sending && projectTabRoundsRef.current.size > 0) {
            const rounds = Array.from(projectTabRoundsRef.current.entries());
            for (const [roundKey, round] of rounds) {
                const roundSessionKey = round.sessionKey || roundKey;
                const newMessages = roundSessionKey
                    ? messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, roundSessionKey))
                    : [];
                if (newMessages.length > 0 && round.tabId) {
                    // Use mergeChatMessages (not appendUnique) so that messages
                    // whose IDs are already present in the stored history are
                    // *replaced* with the latest version from `messages`.
                    // appendUnique (old code) only added new IDs and silently
                    // dropped updated versions (e.g. a streaming placeholder
                    // that already had its ID written by the live-sync effect
                    // but still held empty content would never get the final
                    // response text, leaving a ghost "思考中..." placeholder
                    // alongside the actual response).
                    if (isProjectTabActive && activeTab.id === round.tabId) {
                        setProjectTabMessages(prev => mergeChatMessages(prev, newMessages));
                    }
                    const existingState = getTabState(round.tabId);
                    // For the active tab, existingState?.history is the persisted
                    // snapshot; projectTabMessages is the live React state. Use
                    // the live state as the base when saving so the persisted copy
                    // does not lag behind what the user is currently seeing.
                    // For inactive tabs we have no in-memory live state, so use
                    // the persisted snapshot directly.
                    const baseHistory = isProjectTabActive && activeTab.id === round.tabId
                        ? projectTabMessages
                        : existingState?.history;
                    saveTabState(round.tabId, {
                        ...existingState,
                        history: mergeChatMessages(baseHistory, newMessages),
                    });
                    for (const m of newMessages) {
                        projectTabMsgIdsRef.current.add(m.id);
                    }
                }
                projectTabRoundsRef.current.delete(roundKey);
            }
            setProjectTabRouteVersion(version => version + 1);
            persistProjectTabMsgIds();
        }
    }, [activeTab.id, activeTab.projectPath, findProjectRoundForTab, getTabState, isProjectTabActive, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sending]);
    const { loadProjectContext } = useProjectContextLoader();
    const setProjectTabPreparing = useCallback((tabId: string, preparing: boolean, mode: PendingProjectTabOpen["prepareMode"] = "restore-context") => {
        const currentRef = preparingProjectTabIdsRef.current;
        if (preparing) {
            currentRef.add(tabId);
        } else {
            currentRef.delete(tabId);
        }
        setPreparingProjectTabIds(prev => {
            const has = prev.has(tabId);
            if (has === preparing) return prev;
            const next = new Set(prev);
            if (preparing) next.add(tabId);
            else next.delete(tabId);
            return next;
        });
        setPreparingProjectTabModes(prev => {
            const current = prev.get(tabId);
            if (preparing && current === (mode || "restore-context")) return prev;
            if (!preparing && !prev.has(tabId)) return prev;
            const next = new Map(prev);
            if (preparing) next.set(tabId, mode || "restore-context");
            else next.delete(tabId);
            return next;
        });
    }, []);
    const finishProjectTabPreparing = useCallback((tabId: string, projectPath?: string) => {
        const timer = projectPrepareTimersRef.current.get(tabId);
        if (timer !== undefined) {
            window.clearTimeout(timer);
            projectPrepareTimersRef.current.delete(tabId);
        }
        setProjectTabPreparing(tabId, false);
        const deferred = deferredProjectInitialSendsRef.current.get(tabId) || [];
        deferredProjectInitialSendsRef.current.delete(tabId);
        for (const deferredSend of deferred) {
            void sendMessageForTabRef.current?.(deferredSend.text, {
                ...deferredSend.options,
                tabId,
                project_path: projectPath,
            });
        }
        const pendingRemote = pendingRemoteInitialSendRef.current.get(tabId);
        if (pendingRemote) {
            pendingRemoteInitialSendRef.current.delete(tabId);
            void sendMessageForTabRef.current?.(pendingRemote.text, {
                tabId,
                project_path: projectPath || pendingRemote.projectPath,
            }).then((sent) => {
                if (sent === false) return;
                const current = getTabState(tabId);
                if (current?.pendingRemoteInitialMessage?.text === pendingRemote.text) {
                    saveTabState(tabId, { ...current, pendingRemoteInitialMessage: undefined });
                }
            }).catch((error) => console.warn("[AIAssistantPanel] pending remote initial send after prepare failed", {
                tabId,
                projectPath: projectPath || pendingRemote.projectPath,
                error,
            }));
        }
    }, [getTabState, saveTabState, setProjectTabPreparing]);
    const createProjectTabWithContext = useCallback((projectPath: string, taskTitle: string, options?: { prepareMode?: PendingProjectTabOpen["prepareMode"]; agentMode?: PendingProjectTabOpen["agentMode"]; remoteHost?: string; remoteSafety?: "diagnosis"; remoteNeedsReconnect?: boolean; sessionKey?: string } | boolean) => {
        const sessionKey = typeof options === "object" ? String(options.sessionKey || "").trim() : "";
        const tabExisted = sessionKey
            ? getTabs().some(tab => tab.type === "project" && tab.sessionKey === sessionKey)
            : hasProjectTab(projectPath);
        const prepareMode = typeof options === "object" ? options.prepareMode : "restore-context";
        const agentMode = typeof options === "object" ? options.agentMode : undefined;
        const remoteHost = typeof options === "object" ? options.remoteHost : undefined;
        const remoteSafety = typeof options === "object" ? options.remoteSafety : undefined;
        const remoteNeedsReconnect = typeof options === "object" ? options.remoteNeedsReconnect : undefined;
        const startedAt = performance.now();
        const scheduleNewAgentReady = (readyTab: { id: string; projectPath?: string }, delayMs: number, reason: string, options?: { skipInitialOpenCheck?: boolean }) => {
            const isStillOpen = () => getTabs().some(openTab => openTab.id === readyTab.id && openTab.type === "project" && openTab.projectPath === readyTab.projectPath);
            if (!options?.skipInitialOpenCheck && !isStillOpen()) return;
            const existingTimer = projectPrepareTimersRef.current.get(readyTab.id);
            if (existingTimer !== undefined) window.clearTimeout(existingTimer);
            const timer = window.setTimeout(() => {
                projectPrepareTimersRef.current.delete(readyTab.id);
                if (!isStillOpen()) return;
                finishProjectTabPreparing(readyTab.id, readyTab.projectPath);
                console.info("[AIAssistantPanel] project tab prepared", { tabId: readyTab.id, projectPath: readyTab.projectPath, prepareMode, agentMode: agentMode || null, reason, elapsedMs: Math.round(performance.now() - startedAt) });
            }, Math.max(0, delayMs));
            projectPrepareTimersRef.current.set(readyTab.id, timer);
        };
        const tab = createProjectTab(projectPath, taskTitle, {
            agentMode,
            remoteHost,
            remoteSafety,
            remoteNeedsReconnect,
            sessionKey,
            ...(prepareMode === "new-agent" ? {
                onSessionReady: (readyTab: { id: string; projectPath?: string }) => {
                    const minimumVisibleMs = Math.max(0, 120 - (performance.now() - startedAt));
                    scheduleNewAgentReady(readyTab, minimumVisibleMs, "session-ready");
                },
            } : {}),
        });
		// ACP's owner and transcript live exclusively in its external session.
		// Never hydrate a path-owned project transcript/context into this mirror.
		if (tab && sessionKey) return tab;
        if (tab && tab.projectPath && !tabExisted) {
            const projectPathForTab = tab.projectPath;
            setProjectTabPreparing(tab.id, true, prepareMode || "restore-context");
            console.info("[AIAssistantPanel] project tab preparing", { tabId: tab.id, projectPath: projectPathForTab, prepareMode: prepareMode || "restore-context" });
            if (prepareMode === "new-agent") {
                scheduleNewAgentReady(tab, 5000, "session-ready-timeout", { skipInitialOpenCheck: true });
                return tab;
            }
            void (async () => {
                const initialState = getTabState(tab.id);
                if (!hasRestorableProjectConversation(withoutProjectContextMessages(initialState?.history))) {
                    try {
                        const restoredHistory = await loadRestoredProjectConversationHistory(projectPathForTab);
                        if (restoredHistory.length > 0) {
                            const existing = getTabState(tab.id);
                            const nextHistory = mergeChatMessages(withoutProjectContextMessages(existing?.history), restoredHistory);
                            saveTabState(tab.id, {
                                ...existing,
                                history: nextHistory,
                            });
                            if (activeTabIdRef.current === tab.id) {
                                setProjectTabMessages(nextHistory);
                            }
                        }
                    } catch (error) {
                        console.warn("[AIAssistantPanel] failed to restore backend project history", { projectPath: projectPathForTab, error });
                    }
                }
                loadProjectContext(projectPathForTab, (msg) => {
                    const existing = getTabState(tab.id);
                    const nextHistory = [msg, ...withoutProjectContextMessages(existing?.history)];
                    saveTabState(tab.id, {
                        ...existing,
                        history: nextHistory,
                    });
                    if (activeTabIdRef.current === tab.id) {
                        setProjectTabMessages(nextHistory);
                    }
                }, () => {
                    finishProjectTabPreparing(tab.id, projectPathForTab);
                    console.info("[AIAssistantPanel] project tab prepared", { tabId: tab.id, projectPath: projectPathForTab, elapsedMs: Math.round(performance.now() - startedAt) });
                });
            })();
        }
        return tab;
    }, [createProjectTab, finishProjectTabPreparing, getTabs, hasProjectTab, loadProjectContext, getTabState, saveTabState, setProjectTabPreparing]);

    // Mode B: VS Code programming agent → open/activate matching project tab so
    // acp- foreground rounds land in the same workspace session as the agent.
    useEffect(() => {
        const off = EventsOn("acp-mode-b-message", (payload: unknown) => {
            let data: any = payload;
            try {
                if (typeof payload === "string") data = JSON.parse(payload);
            } catch {
                return;
            }
            if (!data || data.role !== "user") return;
            const projectPath = normalizeProjectSessionPath(String(data.project_path || data.projectPath || ""));
            if (!projectPath) return;
            const sessionKey = String(data.session_key || data.sessionKey || "").trim();
            if (!sessionKey) return;
            const tab = createProjectTabWithContext(projectPath, "VS Code / ACP", {
                prepareMode: "restore-context",
                agentMode: "coding_dev",
                sessionKey,
            });
            if (tab?.id) {
                activateTab(tab.id);
            }
            setActiveSessionKey(sessionKey);
            logAIPanelDiagnostic({
                event: "acp_mode_b_open_project",
                projectPath,
                sessionKey,
                requestId: String(data.request_id || ""),
                tabId: tab?.id || "",
            });
        });
        return () => {
            if (typeof off === "function") off();
            else EventsOff("acp-mode-b-message");
        };
    }, [activateTab, createProjectTabWithContext]);

    const messagesLengthRef = useRef(messages.length);
    messagesLengthRef.current = messages.length;
    const sendMessageForTab = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        // Queue sends carry their durable session owner explicitly. Keep this
        // panel-only routing hint out of the backend payload after using it.
        const queueSessionKey = typeof options?.queue_session_key === "string" ? options.queue_session_key.trim() : "";
        const sendOptions = { ...(options || {}) };
        delete sendOptions.queue_session_key;
        const queuedProjectPath = projectPathFromSessionKey(queueSessionKey) || undefined;
        const queuedExpertId = expertIdFromSessionKey(queueSessionKey) || undefined;
        const forceLocalQueueRoute = queueSessionKey === "desktop-user";
        const optionProjectPath = typeof sendOptions.project_path === "string" ? sendOptions.project_path : undefined;
        const optionTabId = typeof sendOptions.tabId === "string" ? sendOptions.tabId : undefined;
        const liveActiveTab = activeTabRef.current;
        // ACP owns the turn lifecycle outside the desktop project assistant.
        // Its mirror may share a working directory with a normal project tab, so
        // never let composer input fall through to that path-owned conversation.
        if (liveActiveTab.type === "project" && isACPAssistantSessionKey(liveActiveTab.sessionKey)) {
            return Promise.resolve(false);
        }
        const activeSessionProjectPath = projectPathFromSessionKey(getActiveSessionKey());
        const resolvedACPProjectPath = queueSessionKey.startsWith("desktop-user:acp:")
            ? getTabs().find(tab => tab.type === "project" && tab.sessionKey === queueSessionKey)?.projectPath
            : undefined;
        const resolvedProjectPath = queuedProjectPath
			|| resolvedACPProjectPath
            || optionProjectPath
            || (!forceLocalQueueRoute && !queuedExpertId && liveActiveTab.type === "project" ? liveActiveTab.projectPath : undefined)
            || (!forceLocalQueueRoute && !queuedExpertId ? activeSessionProjectPath : undefined)
            || undefined;
        const resolvedTab = resolvedProjectPath
            ? getTabs().find(t => t.type === "project" && t.projectPath === resolvedProjectPath)
            : undefined;
        const resolvedTabId = optionTabId || resolvedTab?.id || (liveActiveTab.type === "project" ? liveActiveTab.id : undefined);
        const isProjectSend = !!resolvedProjectPath;
        if (isProjectSend && resolvedProjectPath) {
            if (resolvedTabId) clearedProjectTabIdsRef.current.delete(resolvedTabId);
            const mergedOptions: Record<string, unknown> = {
                ...sendOptions,
                tabId: resolvedTabId,
                project_path: resolvedProjectPath,
            };
			// ACP turns are already executing through the external host. Never route
			// a queued UI action into the path-owned desktop project conversation.
			if (queueSessionKey.startsWith("desktop-user:acp:")) {
				return Promise.resolve(false);
			}
            const pendingIMCompletion = resolvedTabId ? getTabState(resolvedTabId)?.pendingIMCompletion : undefined;
            if (pendingIMCompletion) {
                if (!mergedOptions.im_platform) mergedOptions.im_platform = pendingIMCompletion.platform;
                if (!mergedOptions.im_target_uid) mergedOptions.im_target_uid = pendingIMCompletion.targetUID;
				if (mergedOptions.im_is_group === undefined) mergedOptions.im_is_group = pendingIMCompletion.isGroup;
                if (!mergedOptions.im_task_title) mergedOptions.im_task_title = pendingIMCompletion.taskTitle;
            }
            const contextTabId = String(mergedOptions.tabId || '');
            const contextHistory = contextTabId === liveActiveTab.id
                ? projectTabMessages
                : ((getTabState(contextTabId)?.history || []) as ChatMessage[]);
            (mergedOptions as Record<string, unknown>).recentMessages = buildProjectTabRecentMessages(contextHistory);
            console.info("[AIAssistantPanel] send route project", {
                tabId: mergedOptions.tabId,
                projectPath: mergedOptions.project_path,
                activeTabId: liveActiveTab.id,
                activeTabType: liveActiveTab.type,
                activeSessionProjectPath: activeSessionProjectPath || undefined,
                textLength: text.trim().length,
                recentMessages: Array.isArray((mergedOptions as Record<string, unknown>).recentMessages) ? ((mergedOptions as Record<string, unknown>).recentMessages as unknown[]).length : 0,
            });
            logAIPanelDiagnostic({
                event: "send_route_project",
                tabId: mergedOptions.tabId,
                projectPath: mergedOptions.project_path,
                activeTabId: liveActiveTab.id,
                activeTabType: liveActiveTab.type,
                activeSessionProjectPath: activeSessionProjectPath || "",
                textLength: text.trim().length,
                recentMessages: Array.isArray((mergedOptions as Record<string, unknown>).recentMessages) ? ((mergedOptions as Record<string, unknown>).recentMessages as unknown[]).length : 0,
            });
            const roundSeq = projectTabRoundSeqRef.current + 1;
            projectTabRoundSeqRef.current = roundSeq;
            const roundKey = projectSessionKey(resolvedProjectPath);
            if (roundKey) {
                projectTabRoundsRef.current.set(roundKey, {
                    tabId: typeof mergedOptions.tabId === "string" ? mergedOptions.tabId : null,
                    projectPath: resolvedProjectPath,
                    sessionKey: roundKey,
                    baseline: messagesLengthRef.current,
                    seq: roundSeq,
                });
                setProjectTabRouteVersion(version => version + 1);
            }
            return sendMessage(text, mergedOptions).then((sent: boolean) => {
                const currentRound = roundKey ? projectTabRoundsRef.current.get(roundKey) : undefined;
                if (sent === false && currentRound?.seq === roundSeq) {
                    projectTabRoundsRef.current.delete(roundKey);
                    setProjectTabRouteVersion(version => version + 1);
                }
                if (sent !== false && pendingIMCompletion && resolvedTabId) {
                    const current = getTabState(resolvedTabId);
                    if (current?.pendingIMCompletion) {
                        saveTabState(resolvedTabId, { ...current, pendingIMCompletion: undefined });
                    }
                }
                return sent;
            }, (err: unknown) => {
                const currentRound = roundKey ? projectTabRoundsRef.current.get(roundKey) : undefined;
                if (currentRound?.seq === roundSeq) {
                    projectTabRoundsRef.current.delete(roundKey);
                    setProjectTabRouteVersion(version => version + 1);
                }
                throw err;
            });
        }
        const resolvedExpertId = queuedExpertId || (!forceLocalQueueRoute && liveActiveTab.type === "expert" ? liveActiveTab.expertId : undefined);
        if (resolvedExpertId) {
            // Expert route: no project path — the backend derives the session
            // userID (desktop-user:expert:<id>) and persona from expert_id.
            const expertTab = getTabs().find(tab => tab.type === "expert" && tab.expertId === resolvedExpertId);
            const expertTabId = optionTabId || expertTab?.id || (liveActiveTab.type === "expert" ? liveActiveTab.id : undefined);
            const expertKey = expertSessionKey(resolvedExpertId);
            if (expertTabId) clearedProjectTabIdsRef.current.delete(expertTabId);
            const mergedOptions: Record<string, unknown> = {
                ...sendOptions,
                tabId: expertTabId,
                expert_id: resolvedExpertId,
            };
            const expertHistory = expertTabId === liveActiveTab.id
                ? projectTabMessages
                : ((expertTabId ? getTabState(expertTabId)?.history : []) || []);
            (mergedOptions as Record<string, unknown>).recentMessages = buildProjectTabRecentMessages(expertHistory as ChatMessage[]);
            console.info("[AIAssistantPanel] send route expert", {
                tabId: expertTabId,
                expertId: resolvedExpertId,
                sessionKey: expertKey,
                textLength: text.trim().length,
            });
            logAIPanelDiagnostic({
                event: "send_route_expert",
                tabId: expertTabId,
                expertId: resolvedExpertId,
                sessionKey: expertKey,
                textLength: text.trim().length,
            });
            markPanelSendInFlight(expertKey, true);
            return sendMessage(text, mergedOptions).finally(() => markPanelSendInFlight(expertKey, false));
        }
        const activeProjectRounds = Array.from(projectTabRoundsRef.current.values());
        console.info("[AIAssistantPanel] send route local", {
            activeTabId: liveActiveTab.id,
            textLength: text.trim().length,
            detachedProjectTabIds: activeProjectRounds.map(round => round.tabId).filter(Boolean),
            detachedProjectPaths: activeProjectRounds.map(round => round.projectPath).filter(Boolean),
        });
        logAIPanelDiagnostic({
            event: "send_route_local",
            activeTabId: liveActiveTab.id,
            activeTabType: liveActiveTab.type,
            activeSessionProjectPath: activeSessionProjectPath || "",
            textLength: text.trim().length,
            detachedProjectTabId: activeProjectRounds.map(round => round.tabId).filter(Boolean).join(","),
            detachedProjectPath: activeProjectRounds.map(round => round.projectPath).filter(Boolean).join(","),
        });
        const localSessionKey = 'desktop-user';
        markPanelSendInFlight(localSessionKey, true);
        // Only include tabId for tabs that can own a workflow preview (local/project).
        // VE/group tabs don't run workflows — sending their tab ID would overwrite
        // the local tab's event_scope_id mapping on the backend.
        const localTab = forceLocalQueueRoute ? getTabs().find(tab => tab.type === "local") : liveActiveTab;
        const shouldIncludeTabScope = !!localTab && canShowAssistantCodingPreviewForTab(localTab);
        const localOptions = shouldIncludeTabScope
            ? { ...sendOptions, tabId: localTab.id }
            : sendOptions;
        const localSend = sendMessage(text, localOptions as any);
        return localSend.finally(() => markPanelSendInFlight(localSessionKey, false));
    }, [getTabState, getTabs, markPanelSendInFlight, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sendMessage]);
    sendMessageForTabRef.current = sendMessageForTab;
    const sendProjectMessageAfterPrepare = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const tabId = typeof options?.tabId === "string" ? options.tabId : "";
        const projectPath = typeof options?.project_path === "string" ? options.project_path : "";
        if (tabId && projectPath && preparingProjectTabIdsRef.current.has(tabId)) {
            const deferred = deferredProjectInitialSendsRef.current.get(tabId) || [];
            deferred.push({ text, options });
            deferredProjectInitialSendsRef.current.set(tabId, deferred);
            console.info("[AIAssistantPanel] defer project send until prepare completes", { tabId, projectPath, textLength: text.trim().length });
            return Promise.resolve(true);
        }
        return sendMessageForTab(text, options);
    }, [sendMessageForTab]);
    const clearProjectRoundTrackingForTab = useCallback((tabId: string) => {
        let changed = false;
        const tab = getTabs().find(t => t.id === tabId);
        if (tab?.type === "project" && tab.projectPath) {
            forgetAIAssistantSessionRounds(tab.sessionKey || `desktop-user:${tab.projectPath}`);
            const prepareTimer = projectPrepareTimersRef.current.get(tabId);
            if (prepareTimer !== undefined) {
                window.clearTimeout(prepareTimer);
                projectPrepareTimersRef.current.delete(tabId);
            }
            setProjectTabPreparing(tabId, false);
            deferredProjectInitialSendsRef.current.delete(tabId);
            pendingRemoteInitialSendRef.current.delete(tabId);
        }
        for (const [roundKey, round] of projectTabRoundsRef.current) {
            if (round.tabId !== tabId) continue;
            const sessionKey = tab?.type === "project" ? (tab.sessionKey || projectSessionKey(tab.projectPath)) : projectSessionKey(round.projectPath);
            const messagesToMark = sessionKey
                ? messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, sessionKey))
                : [];
            for (const message of messagesToMark) {
                projectTabMsgIdsRef.current.add(message.id);
            }
            projectTabRoundsRef.current.delete(roundKey);
            changed = true;
        }
        for (const [key, detached] of detachedProjectRoundsRef.current) {
            if (detached.tabId !== tabId) continue;
            for (const messageId of detached.messageIds) {
                projectTabMsgIdsRef.current.add(messageId);
            }
            detachedProjectRoundsRef.current.delete(key);
            changed = true;
        }
        if (changed) {
            persistProjectTabMsgIds();
            setProjectTabRouteVersion(version => version + 1);
            setDetachedProjectRoundVersion(version => version + 1);
        }
    }, [getTabs, messages, persistProjectTabMsgIds, setProjectTabPreparing]);
    const closeTabWithProjectCleanup = useCallback((tabId: string) => {
        const tab = getTabs().find(t => t.id === tabId);
        if (tab?.type === "project") {
            const snapshot = latestProjectCloseSnapshotRef.current;
            const existingState = getTabState(tabId);
            if (snapshot?.tabId === tabId) {
                const wasCleared = clearedProjectTabIdsRef.current.has(tabId);
                const snapshotMessages = !wasCleared && activeTabIdRef.current === tabId ? latestDisplayMessagesRef.current : snapshot.messages;
                const nextHistory = wasCleared
                    ? withoutProjectContextMessages(snapshotMessages)
                    : mergeChatMessages(
                        withoutProjectContextMessages(existingState?.history),
                        withoutProjectContextMessages(snapshotMessages),
                    );
                saveTabState(tabId, {
                    ...existingState,
                    history: nextHistory,
                    scrollTop: snapshot.scrollTop,
                    inputText: snapshot.inputText,
                    projectPath: snapshot.projectPath || tab.projectPath,
                    lastActiveAt: Date.now(),
                });
            }
        }
        // Closing an expert removes its UI owner. Revoke all hook-owned work
        // before the tab disappears, so a late file-picker result or stream
        // cannot restore data into a subsequently reopened expert session.
        if (tab?.type === "expert") {
            const sessionKey = expertSessionKey(tab.expertId);
            if (sessionKey) forgetAIAssistantSessionRounds(sessionKey);
        }
        clearProjectRoundTrackingForTab(tabId);
        previewStateMapRef.current.delete(tabId);
        if (previewOwnerTabRef.current === tabId) { previewOwnerTabRef.current = "local"; previewOwnerResetPendingRef.current = true; }
        // If the closed tab is currently recording, auto-stop recording
        if (skillRecordingTabId === tabId) {
            setSkillRecordingTabId(null);
            (window as any).go?.main?.App?.StopSkillRecording?.().catch(() => {});
        }
        closeTab(tabId);
    }, [clearProjectRoundTrackingForTab, closeTab, getTabState, getTabs, saveTabState, skillRecordingTabId]);
    const createProjectTabFromSearch = useCallback((
        projectPath: string,
        taskTitle: string,
        options?: {
            autoSend?: boolean;
            agentMode?: "coding_dev" | "remote_coding_dev";
            remoteHost?: string;
            remoteSafety?: "diagnosis";
            tags?: string[];
        },
    ) => {
        const agentMode = options?.agentMode || agentModeFromTaskTags(options?.tags);
        const remoteHost = options?.remoteHost || remoteHostFromTaskTags(options?.tags);
        // Await Ensure before opening the tab so the first user message cannot
        // race past sticky re-arm (history/search restore of pure coding).
        // Also re-arm when tags are missing but sticky/index still classifies
        // the path as pure coding (Ensure returns kind).
        void (async () => {
            const tabExistedInList = hasProjectTab(projectPath);
            let resolvedMode = agentMode;
            let resolvedHost = remoteHost;
            let resolvedRemoteSafety = options?.remoteSafety;
            let remoteNeedsReconnect = false;
            try {
                const arm = await EnsureCodingWorkbenchArmed(projectPath);
                if (!resolvedMode) {
                    if (arm?.kind === "remote") resolvedMode = "remote_coding_dev";
                    else if (arm?.kind === "local") resolvedMode = "coding_dev";
                }
                if (resolvedMode === "remote_coding_dev") {
                    remoteNeedsReconnect = !!(arm?.needs_reconnect);
                    resolvedRemoteSafety = arm?.remote_safety === "diagnosis" ? "diagnosis" : resolvedRemoteSafety;
                    if (!resolvedHost && arm?.remote_host) {
                        resolvedHost = arm.remote_host;
                    }
                }
            } catch (err) {
                console.warn("[AIAssistantPanel] EnsureCodingWorkbenchArmed from search failed", err);
                if (resolvedMode === "remote_coding_dev") remoteNeedsReconnect = true;
            }
            const tab = createProjectTabWithContext(projectPath, taskTitle, {
                prepareMode: "restore-context",
                agentMode: resolvedMode,
                remoteHost: resolvedHost,
                remoteSafety: resolvedMode === "remote_coding_dev" ? resolvedRemoteSafety : undefined,
                remoteNeedsReconnect: resolvedMode === "remote_coding_dev" ? remoteNeedsReconnect : undefined,
            });
            if (!tab || !options?.autoSend || tabExistedInList) return;
            const existingState = getTabState(tab.id);
            const hasExistingConversation = existingState?.history?.some((m: any) => m && (m.role === "user" || m.role === "assistant"));
            if (!hasExistingConversation) {
                void sendProjectMessageAfterPrepare(taskTitle, { tabId: tab.id, project_path: tab.projectPath });
            }
        })();
        return null;
    }, [createProjectTabWithContext, getTabState, hasProjectTab, sendProjectMessageAfterPrepare]);
    const closeProjectTabByPath = useCallback((projectPath: string) => {
        const normalizedPath = normalizeProjectSessionPath(projectPath);
        const tab = getTabs().find(t => t.type === "project" && normalizeProjectSessionPath(t.projectPath) === normalizedPath);
        if (tab) {
            console.info("[AIAssistantPanel] closing project tab", { projectPath, tabId: tab.id });
            closeTabWithProjectCleanup(tab.id);
        }
    }, [closeTabWithProjectCleanup, getTabs]);
    useEffect(() => {
        const off = EventsOn(EVENT_PROJECT_TASK_CLOSED, (projectPath: string) => {
            if (typeof projectPath === "string" && projectPath.trim()) {
                closeProjectTabByPath(projectPath);
            }
        });
        return () => {
            if (typeof off === "function") off();
            else EventsOff(EVENT_PROJECT_TASK_CLOSED);
        };
    }, [closeProjectTabByPath]);
    useEffect(() => {
        const off = EventsOn(EVENT_PROJECT_TASK_DELETED, (projectPath: string) => {
            if (typeof projectPath === "string" && projectPath.trim()) {
                purgeDeletedProjectTabLocalCache(projectPath);
                discardDeletedProjectTabs(projectPath);
            }
        });
        return () => {
            if (typeof off === "function") off();
            else EventsOff(EVENT_PROJECT_TASK_DELETED);
        };
    }, [discardDeletedProjectTabs]);
    const addParticipantToTab = useAddGroupParticipantToTab({ getTabState, upgradeVETabToGroup });
    const addLocalMaclawToTab = useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup });
    const [participantInviteTargetTabId, setParticipantInviteTargetTabId] = useState<string | null>(null);
    const participantInviteTargetTab = participantInviteTargetTabId ? tabState.tabs.find(t => t.id === participantInviteTargetTabId) || null : null;
    useEffect(() => {
        if (!panelActive) setParticipantInviteTargetTabId(null);
    }, [panelActive]);
    usePendingAssistantTabOpen({
        lang,
        createVETab,
        createGroupTab,
        createProjectTab: createProjectTabWithContext,
        createExpertTab,
        activateTab,
        getTabState,
        saveTabState,
        getTabList: getTabs,
        hasProjectTab,
        sendMessage: sendProjectMessageAfterPrepare,
        pendingVEOpen,
        onPendingVEOpenHandled,
        pendingHistoryDiscussionOpen,
        onPendingHistoryDiscussionOpenHandled,
        pendingProjectTabOpen: props.pendingProjectTabOpen,
        onPendingProjectTabOpenHandled: props.onPendingProjectTabOpenHandled,
        pendingExpertOpen: props.pendingExpertOpen,
        onPendingExpertOpenHandled: props.onPendingExpertOpenHandled,
        onEnsureExpertTask: props.onEnsureExpertTask,
    });
    useEffect(() => {
        if (!tabLimitError) return;
        const timer = setTimeout(clearTabLimitError, 3000);
        return () => clearTimeout(timer);
    }, [tabLimitError, clearTabLimitError]);
    const codingPreviewOwnerTab = tabState.tabs.find(tab => tab.id === previewOwnerTabRef.current);
    // The event scope determines which workflow events are accepted by useWorkflowState.
    // Each tab has a unique scope ID (tab.id): "local" for the AI assistant tab,
    // "proj-{hash}" for project tabs. Events carry this scope ID from the backend,
    // ensuring perfect per-tab isolation without path comparison.
    const codingPreviewEventScope = canShowAssistantCodingPreviewForTab(activeTab) ? activeTab.id : (codingPreviewOwnerTab?.id || "local");
    // Code preview events still use project_path for routing (they don't carry event_scope_id yet).
    const codePreviewPathScope = (canShowAssistantCodingPreviewForTab(activeTab) ? activeTab.projectPath : codingPreviewOwnerTab?.projectPath) || undefined;
    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion, getSnapshot: getWorkflowSnapshot, restoreState: restoreWorkflowState, resetState: resetWorkflowState } = useWorkflowState(codingPreviewEventScope, codePreviewPathScope);
    const sourcePreviewAllowed = shouldShowSourcePreviewForWorkflow(workflowState.workflowType)
        || shouldShowSourcePreviewForAgentMode(activeTab.agentMode);
    const { state: codePreviewState, closePanel: closeCodePreview, reopenPanel: reopenCodePreview, activatePassive: activateCodePreviewPassive, selectFile: selectCodeFile, openWorkspaceFile, closeFile: closeCodeFile, closeOtherFiles: closeOtherCodeFiles, closeFilesToTheRight: closeCodeFilesToTheRight, closeAllFiles: closeAllCodeFiles, moveFile: moveCodeFile, toggleFilePinned: toggleCodeFilePinned, restoreState: restoreCodePreviewState, resetSession: resetCodePreviewState } = useCodePreviewState(codePreviewPathScope, sourcePreviewAllowed);
    useEffect(() => {
        if (!previewOwnerResetPendingRef.current) return;
        previewOwnerResetPendingRef.current = false;
        const state = previewStateMapRef.current.get("local");
        if (state) {
            restoreWorkflowState(state.workflow);
            commitRestoredCodePreview(state.code, restoreCodePreviewState, codePreviewStateRef);
        } else {
            resetWorkflowState();
            resetCodePreviewState();
            codePreviewStateRef.current = initialCodePreviewState();
        }
    }, [activeTab.id, restoreWorkflowState, restoreCodePreviewState, resetWorkflowState, resetCodePreviewState]);
    useEffect(() => {
        const restored = restoredPreviewStateRef.current;
        if (!restored) return;
        const currentOwner = tabState.tabs.find(tab => tab.id === previewOwnerTabRef.current);
        if (!canShowAssistantCodingPreviewForTab(currentOwner)) {
            const restoredPath = normalizeProjectSessionPath(restored.ownerProjectPath);
            const ownerTab = restoredPath
                ? tabState.tabs.find(tab => tab.type === "project" && normalizeProjectSessionPath(tab.projectPath) === restoredPath)
                : tabState.tabs.find(tab => tab.id === "local");
            if (!ownerTab) return;
            const nextOwnerTabId = ownerTab.id;
            if (nextOwnerTabId !== restored.ownerTabId) {
                const saved = previewStateMapRef.current.get(restored.ownerTabId);
                if (saved) {
                    previewStateMapRef.current.delete(restored.ownerTabId);
                    previewStateMapRef.current.set(nextOwnerTabId, saved);
                }
                previewOwnerTabRef.current = nextOwnerTabId;
            }
        }
        if (previewOwnerTabRef.current !== activeTab.id) return;
        // Guard against painting a pending old-project snapshot onto a brand-new
        // coding tab that reassigned preview ownership on tab switch.
        if (!shouldApplyRestoredAssistantPreview({
            restoredOwnerTabId: restored.ownerTabId,
            restoredOwnerProjectPath: restored.ownerProjectPath,
            activeTabId: activeTab.id,
            activeTabType: activeTab.type,
            activeTabProjectPath: activeTab.projectPath,
        })) {
            return;
        }
        restoredPreviewApplyingRef.current = true;
        restoredPreviewStateRef.current = null;
        restoredPreviewOwnerProjectPathRef.current = undefined;
        const workflowClone = cloneWorkflowUIState(restored.workflow);
        workflowStateRef.current = workflowClone;
        const codeClone = commitRestoredCodePreview(
            restored.code,
            restoreCodePreviewState,
            codePreviewStateRef,
        );
        previewStateMapRef.current.set(previewOwnerTabRef.current, {
            workflow: workflowClone,
            code: codeClone,
            previewMode: codePreviewModeFromState(codeClone),
        });
        restoreWorkflowState(restored.workflow);
    }, [activeTab.id, activeTab.type, activeTab.projectPath, tabState.tabs, restoreWorkflowState, restoreCodePreviewState]);
    const workflowStateRef = useRef(workflowState);
    workflowStateRef.current = workflowState;
    codePreviewStateRef.current = codePreviewState;
    const currentPreviewModeRef = useRef<"workflow" | "code">("workflow");
    const persistPreviewStateRef = useRef<number | null>(null);
    const previewPersistTabsRef = useRef(tabState.tabs);
    previewPersistTabsRef.current = tabState.tabs;
    const persistPreviewState = useCallback((options?: { immediate?: boolean }) => {
        const save = () => {
            persistPreviewStateRef.current = null;
            const ownerTabId = previewOwnerTabRef.current;
            const ownerTab = previewPersistTabsRef.current.find(tab => tab.id === ownerTabId);
            if (!canShowAssistantCodingPreviewForTab(ownerTab)) return;
            const savedOwnerState = previewStateMapRef.current.get(ownerTabId);
            const ownerIsActiveTab = ownerTabId === activeTabIdRef.current;
            const workflowToPersist = !ownerIsActiveTab && savedOwnerState ? savedOwnerState.workflow : workflowStateRef.current;
            const codeToPersist = !ownerIsActiveTab && savedOwnerState ? savedOwnerState.code : codePreviewStateRef.current;
            writeStoredAssistantPreviewState({
                ownerTabId,
                ownerProjectPath: ownerTab?.projectPath || restoredPreviewOwnerProjectPathRef.current,
                previewMode: !ownerIsActiveTab && savedOwnerState ? savedOwnerState.previewMode : currentPreviewModeRef.current,
                workflow: cloneWorkflowUIState(workflowToPersist),
                code: cloneCodePreviewState(codeToPersist),
            });
        };
        if (persistPreviewStateRef.current) {
            window.clearTimeout(persistPreviewStateRef.current);
            persistPreviewStateRef.current = null;
        }
        if (options?.immediate) {
            save();
            return;
        }
        persistPreviewStateRef.current = window.setTimeout(save, 250);
    }, []);
    useEffect(() => {
        if (restoredPreviewApplyingRef.current) {
            restoredPreviewApplyingRef.current = false;
            return;
        }
        persistPreviewState();
    }, [workflowState, codePreviewState, persistPreviewState]);
    useEffect(() => () => {
        if (persistPreviewStateRef.current) {
            window.clearTimeout(persistPreviewStateRef.current);
            persistPreviewStateRef.current = null;
        }
        persistPreviewState({ immediate: true });
    }, [persistPreviewState]);
    const showAgentView = !!agentView && (agentViewOwnerTabRef.current === activeTab.id || (agentView.id?.startsWith("workflow:form:") ?? false));
    const codingPreviewAllowed = canShowAssistantCodingPreviewForTab(activeTab);
    // Suppress workflow doc preview when a workflow form is showing — the form
    // is the current phase's data collection step; the doc panel has no content
    // yet (document is produced only after form submission + agent loop).
    const workflowFormActive = showAgentView && (agentView?.id?.startsWith("workflow:form:") ?? false);
    const [workflowFormGeneratingPhaseID, setWorkflowFormGeneratingPhaseID] = useState<string | null>(null);
    const workflowAwaitingForm = workflowState.active && workflowState.awaitingForm;
    const showWorkflowPreview = codingPreviewAllowed && workflowState.splitMode && !workflowFormActive;
    // Source files are neither rendered nor retained outside an active programming
    // workflow, avoiding unnecessary updates and persisted source content.
    useEffect(() => {
        if (!sourcePreviewAllowed && (codePreviewState.active || codePreviewState.files.size > 0)) {
            resetCodePreviewState();
            codePreviewStateRef.current = initialCodePreviewState();
        }
    }, [sourcePreviewAllowed, codePreviewState.active, codePreviewState.files.size, resetCodePreviewState]);
    // Pure coding: open empty right-hand source panel once per tab. After that,
    // only recover when files remain and the user did not close (activatePassive
    // preserves userClosed; reopen would clear it).
    useEffect(() => {
        if (!isPureCodingEnvironment || !codingPreviewAllowed || !sourcePreviewAllowed) {
            if (autoOpenedCodePreviewTabRef.current === activeTab.id) {
                autoOpenedCodePreviewTabRef.current = null;
            }
            return;
        }
        if (autoOpenedCodePreviewTabRef.current === activeTab.id) {
            // Prefer ref: same-tick tab restore updates the ref before setState flushes.
            const cp = codePreviewStateRef.current;
            if (!cp.active && !cp.userClosed && cp.files.size > 0) {
                activateCodePreviewPassive();
            }
            return;
        }
        autoOpenedCodePreviewTabRef.current = activeTab.id;
        // First pure-coding open: drop orphan files from another project when we
        // own the panel and have no per-tab snapshot (including localStorage owner).
        if (
            previewOwnerTabRef.current === activeTab.id
            && !previewStateMapRef.current.has(activeTab.id)
            && codePreviewStateRef.current.files.size > 0
        ) {
            resetCodePreviewState();
            codePreviewStateRef.current = initialCodePreviewState();
        }
        if (!codePreviewStateRef.current.userClosed) {
            reopenCodePreview();
        }
    }, [isPureCodingEnvironment, codingPreviewAllowed, sourcePreviewAllowed, activeTab.id, codePreviewState.active, codePreviewState.files.size, codePreviewState.userClosed, activateCodePreviewPassive, reopenCodePreview, resetCodePreviewState]);
    const showCodePreview = codingPreviewAllowed && sourcePreviewAllowed && codePreviewState.active;
    // Isolation conflicts open a dedicated right-hand side panel (not the float popover).
    const showCodingConflictPanel = isPureCodingEnvironment && codingConflictOpen && codingConflictCount > 0;
    // When the side panel is open without an active conflict, load the first one for diffs/3-way.
    useEffect(() => {
        if (!showCodingConflictPanel) return;
        if (codingConflictActiveId) return;
        const firstId = codingConflicts[0]?.id;
        if (!firstId) return;
        void openCodingConflict(firstId);
    }, [showCodingConflictPanel, codingConflictActiveId, codingConflicts, openCodingConflict]);
    // Drop stale "open" flag once the last conflict is gone so a later conflict can rising-edge open again.
    useEffect(() => {
        if (codingConflictCount === 0 && codingConflictOpen) {
            setCodingConflictOpen(false);
        }
    }, [codingConflictCount, codingConflictOpen]);
    // Track peak conflict count for resolution progress (resets when wave clears).
    useEffect(() => {
        if (codingConflictCount === 0) {
            setCodingConflictPeak(0);
            return;
        }
        setCodingConflictPeak((prev) => Math.max(prev, codingConflictCount));
    }, [codingConflictCount]);
    const anySplitActive = showWorkflowPreview || showCodePreview || showAgentView || showCodingConflictPanel;
    const splitRatio = anySplitActive ? workflowState.splitRatio : 1;
    currentPreviewModeRef.current = showCodePreview && !showWorkflowPreview ? "code" : "workflow";
    const workflowCurrentPhaseMeta = useMemo(
        () => workflowState.phases.find(phase => phase.id === workflowState.currentPhaseID),
        [workflowState.currentPhaseID, workflowState.phases],
    );
    const workflowCurrentPhaseStatus = normalizeWorkflowPhaseStatus(workflowCurrentPhaseMeta?.status);
    const workflowCurrentPhaseRunning = isWorkflowPhaseRunningStatus(workflowCurrentPhaseStatus);
    const workflowCurrentPhaseTerminal = isWorkflowPhaseTerminalStatus(workflowCurrentPhaseStatus);
    const workflowCurrentPhaseExpectsDocument = workflowCurrentPhaseMeta?.expectsDocument !== false;
    const workflowAwaitingReview = workflowState.active && workflowCurrentPhaseStatus === "waiting_confirm";
    const workflowReviewPhaseName = workflowCurrentPhaseMeta?.name || workflowState.currentPhaseID;
    const workflowFormPhaseName = workflowCurrentPhaseMeta?.name || workflowState.currentPhaseID;
    const workflowCurrentPhaseID = workflowState.currentPhaseID || "";
    const lastWorkflowFormRouteRef = useRef<{ viewID: string; phaseID: string; workflowID: string; userID: string; eventScopeID: string } | null>(null);
    useEffect(() => {
        if (!workflowState.active) {
            lastWorkflowFormRouteRef.current = null;
            return;
        }
        const cachedWorkflowID = lastWorkflowFormRouteRef.current?.workflowID;
        if (cachedWorkflowID && workflowState.workflowID && cachedWorkflowID !== workflowState.workflowID) {
            lastWorkflowFormRouteRef.current = null;
        }
        if (!workflowFormActive || !agentView?.id?.startsWith("workflow:form:")) return;
        const phaseID = agentViewHiddenFieldValue(agentView, "_workflow_phase") || workflowCurrentPhaseID;
        lastWorkflowFormRouteRef.current = {
            viewID: agentView.id,
            phaseID,
            workflowID: agentViewHiddenFieldValue(agentView, "_workflow_id") || workflowState.workflowID,
            userID: agentViewHiddenFieldValue(agentView, "_workflow_user_id") || activeSessionKey,
            eventScopeID: agentViewHiddenFieldValue(agentView, "_workflow_event_scope_id"),
        };
    }, [activeSessionKey, agentView, workflowCurrentPhaseID, workflowFormActive, workflowState.active, workflowState.workflowID]);
    const startPreviewResize = useAssistantPreviewResize(setWorkflowSplitRatio);
    // Toggle the entire right-side area (workflow / code / conflict) open/closed
    const handleTogglePreviewPanel = useCallback(() => {
        if (!codingPreviewAllowed) return;
        if (workflowState.splitMode || codePreviewStateRef.current.active || codingConflictOpen) {
            closeDocPreview();
            closeCodePreview();
            closeCodingConflictSidePanel();
        } else {
            openDocPreview();
            const cp = codePreviewStateRef.current;
            if (sourcePreviewAllowed && cp.files.size > 0 && !cp.active) {
                activateCodePreviewPassive();
            }
        }
    }, [codingPreviewAllowed, sourcePreviewAllowed, workflowState.splitMode, codingConflictOpen, closeDocPreview, closeCodePreview, closeCodingConflictSidePanel, openDocPreview, activateCodePreviewPassive]);
    // Keep ref updated so clearActiveHistory (defined earlier) can close all preview panels
    closeAllPreviewPanelsRef.current = () => {
        closeDocPreview();
        closeCodePreview();
        closeCodingConflictSidePanel();
        resetWorkflowState();
        if (agentView) dismissAgentView(agentView.id, undefined, { force: true });
    };
    // Panel chrome title (not per-tab); same i18n source as the local main tab label.
    const title = localAssistantTabTitle(lang);
    const thinkingText = lang === "en" ? "Working... (you can keep typing)" : "\u5904\u7406\u4e2d\u2026\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const processingText = lang === "en" ? "Running tools... (you can keep typing)" : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u2026\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const idlePlaceholderText = getComposeActionPlaceholder(composeAction, !lang?.startsWith("en"))
        || (isRemoteMaintenanceEnvironment
            ? localizeText(lang, "Describe the maintenance task on the remote host...", "描述远程主机上的维护任务…", "描述遠端主機上的維護任務…")
            : isRemoteCodingDevEnvironment
            ? localizeText(lang, "Describe coding work on the remote host...", "描述远程主机上的开发任务…", "描述遠端主機上的開發任務…")
            : isCodingDevEnvironment
            ? localizeText(lang, "Describe coding work in this programming environment...", "在编程环境中描述你的开发任务…", "在程式開發環境中描述你的開發任務…")
            : (lang === "en" ? "Enter a task or command..." : "\u8f93\u5165\u4efb\u52a1\u6216\u6307\u4ee4\u2026"));
    const savedFileLabel = lang === "en" ? "Saved file" : "\u6587\u4ef6\u5df2\u4fdd\u5b58";
    const hasActiveDetachedProjectRound = useMemo(() => (
        isProjectTabActive && Array.from(detachedProjectRoundsRef.current.values()).some(detached => detached.tabId === activeTab.id)
    ), [activeTab.id, detachedProjectRoundVersion, isProjectTabActive]);
    const hasForegroundProjectRound = projectTabRoundsRef.current.size > 0;
    const hasForegroundRoundForActiveProject = isProjectTabActive && !!findProjectRoundForTab(activeTab.id, activeTab.projectPath);
    const sendingSessionKey = typeof rawSendingSessionKey === "string" && rawSendingSessionKey.trim() ? rawSendingSessionKey.trim() : "";
    const streamingSessionKey = typeof rawStreamingSessionKey === "string" && rawStreamingSessionKey.trim() ? rawStreamingSessionKey.trim() : "";
    const busySessionKeys = useMemo(
        () => Array.isArray(rawBusySessionKeys) ? rawBusySessionKeys.map(key => String(key || '').trim()).filter(Boolean) : [],
        [rawBusySessionKeys],
    );
    const streamingSessionKeys = useMemo(
        () => Array.isArray(rawStreamingSessionKeys) ? rawStreamingSessionKeys.map(key => String(key || '').trim()).filter(Boolean) : [],
        [rawStreamingSessionKeys],
    );
    useEffect(() => {
        if (projectTabRoundsRef.current.size === 0 || !Array.isArray(rawBusySessionKeys)) return;
        const busySet = new Set(busySessionKeys);
        let changed = false;
        for (const [roundKey, round] of Array.from(projectTabRoundsRef.current.entries())) {
            const roundSessionKey = projectSessionKey(round.projectPath);
            if (!roundSessionKey || busySet.has(roundSessionKey)) continue;
            const newMessages = messages.slice(round.baseline).filter((message: ChatMessage) => messageBelongsToSessionOrLegacy(message, roundSessionKey));
            if (newMessages.length > 0 && round.tabId) {
                const existingState = getTabState(round.tabId);
                const nextHistory = mergeChatMessages(existingState?.history, newMessages);
                saveTabState(round.tabId, {
                    ...existingState,
                    history: nextHistory,
                });
                if (activeTabIdRef.current === round.tabId) {
                    setProjectTabMessages(nextHistory);
                }
                for (const message of newMessages) {
                    projectTabMsgIdsRef.current.add(message.id);
                }
                persistProjectTabMsgIds();
            }
            projectTabRoundsRef.current.delete(roundKey);
            changed = true;
            console.info("[AIAssistantPanel] project round session idle", {
                tabId: round.tabId,
                projectPath: round.projectPath,
                sessionKey: roundSessionKey,
                messageCount: newMessages.length,
            });
            logAIPanelDiagnostic({
                event: "project_round_session_idle",
                tabId: round.tabId || "",
                projectPath: round.projectPath,
                sessionKey: roundSessionKey,
                messageCount: newMessages.length,
            });
        }
        if (changed) setProjectTabRouteVersion(version => version + 1);
    }, [busySessionKeys, getTabState, messages, persistProjectTabMsgIds, rawBusySessionKeys, saveTabState]);
    const hasExplicitBusySessionList = Array.isArray(rawBusySessionKeys);
    const hasExplicitStreamingSessionList = Array.isArray(rawStreamingSessionKeys);
    const panelSessionIsSending = panelSendInFlightSessionKeys.has(activeSessionKey);
    const activeSessionIsSending = panelSessionIsSending || (hasExplicitBusySessionList
        ? busySessionKeys.includes(activeSessionKey)
        : sending && (sendingSessionKey
            ? sendingSessionKey === activeSessionKey
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : ((isLocalTabActive || isExpertTabActive) && !hasForegroundProjectRound))));
    const activeSessionIsStreaming = hasExplicitStreamingSessionList
        ? streamingSessionKeys.includes(activeSessionKey)
        : streaming && (streamingSessionKey
            ? streamingSessionKey === activeSessionKey
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : ((isLocalTabActive || isExpertTabActive) && !hasForegroundProjectRound)));
    const isBusy = (hasExplicitBusySessionList ? activeSessionIsSending : hasActiveDetachedProjectRound || activeSessionIsSending);
    useEffect(() => {
        if (!workflowFormGeneratingPhaseID) return;
        if (
            workflowAwaitingReview
            || workflowCurrentPhaseTerminal
            || !workflowCurrentPhaseExpectsDocument
            || workflowFormGeneratingPhaseID !== workflowCurrentPhaseID
            || (!workflowAwaitingForm && !workflowCurrentPhaseRunning && !isBusy)
        ) {
            setWorkflowFormGeneratingPhaseID(null);
        }
    }, [isBusy, workflowAwaitingForm, workflowAwaitingReview, workflowCurrentPhaseExpectsDocument, workflowCurrentPhaseID, workflowCurrentPhaseRunning, workflowCurrentPhaseTerminal, workflowFormGeneratingPhaseID]);
    const workflowFormGeneratingDocument = !!workflowCurrentPhaseID
        && workflowFormGeneratingPhaseID === workflowCurrentPhaseID
        && workflowCurrentPhaseExpectsDocument
        && !workflowFormActive
        && !workflowAwaitingReview
        && (isBusy || workflowCurrentPhaseRunning);
    const activeSessionHasWork = isBusy || activeSessionIsStreaming;
    const displayProgressMessages = activeSessionHasWork ? progressMessages : [];
    useEffect(() => {
        if (!(sending || streaming) || isBusy || activeSessionIsStreaming) return;
        console.info("[AIAssistantPanel] active session idle while another session is busy", {
            activeTabId: activeTab.id,
            activeTabType: activeTab.type,
            activeSessionKey,
            sending,
            sendingSessionKey: sendingSessionKey || undefined,
            busySessionKeys,
            streaming,
            streamingSessionKey: streamingSessionKey || undefined,
            streamingSessionKeys,
        });
    }, [activeSessionIsStreaming, activeSessionKey, activeTab.id, activeTab.type, busySessionKeys, isBusy, panelSessionIsSending, sending, sendingSessionKey, streaming, streamingSessionKey, streamingSessionKeys]);
    const activeProjectPreparing = isProjectTabActive && preparingProjectTabIds.has(activeTab.id);
    const activeProjectPrepareMode = activeProjectPreparing ? (preparingProjectTabModes.get(activeTab.id) || "restore-context") : "restore-context";
    const codingTaskPhase = resolveCodingTaskPhase({
        agentMode: isPureCodingEnvironment ? activeTab.agentMode : undefined,
        preparing: activeProjectPreparing,
        remoteNeedsReconnect: remoteCodingNeedsReconnect,
    });
    const codingTaskReadyForIntents = canDispatchCodingIntent(codingTaskPhase);
    const codingEnvDescription = useMemo(() => {
        if (!isPureCodingEnvironment) return "";
        if (isRemoteCodingDevEnvironment) {
            if (remoteCodingNeedsReconnect) {
                return isRemoteMaintenanceEnvironment
                    ? localizeText(lang, "SSH session is not connected. Reconnect below to continue this remote maintenance task.", "SSH 未连接或已断开。请在下方重连以继续此远程维护任务。", "SSH 未連線或已中斷。請在下方重新連線以繼續此遠端維護任務。")
                    : localizeText(lang, "SSH session is not connected. Reconnect below (password is remembered on this device) to continue multi-turn remote coding.", "SSH 未连接或已断开。请在下方重连（本机已记忆密码时会自动填充/重连）以继续多轮远程编程。", "SSH 未連線或已中斷。請在下方重連（本機已記憶密碼時會自動填入/重連）以繼續多輪遠端程式開發。");
            }
            if (activeProjectPreparing && activeProjectPrepareMode === "new-agent") {
                return isRemoteMaintenanceEnvironment
                    ? localizeText(lang, "Connecting SSH and preparing remote maintenance diagnostics (read-only inspection first)…", "正在连接 SSH 并准备远程维护诊断（先进行只读检查）…", "正在連線 SSH 並準備遠端維護診斷（先進行唯讀檢查）…")
                    : localizeText(lang, "Connecting SSH and preparing full remote coding workbench (source preview + local Skill/MCP)…", "正在连接 SSH 并建立全功能远程编程工作台（源码预览 + 本机 Skill/MCP）…", "正在連線 SSH 並建立全功能遠端程式工作台（原始碼預覽 + 本機 Skill/MCP）…");
            }
            return isRemoteMaintenanceEnvironment
                ? localizeText(lang, "Remote maintenance task: inspect the host via SSH before making changes. Diagnostics start read-only; risky repairs require confirmation. Skill/MCP/web search run on this machine.", "远程维护任务：通过 SSH 检查主机后再执行变更。诊断默认只读；高风险修复需要确认。Skill、MCP、联网检索在本机运行。", "遠端維護任務：透過 SSH 檢查主機後再執行變更。診斷預設唯讀；高風險修復需要確認。Skill、MCP、聯網檢索在本機執行。")
                : localizeText(lang, "Full remote workbench: code runs on the remote host via SSH; Skill/MCP/web_search run on this machine. Multi-turn continues until you leave the tab. Source preview is on the right.", "全功能远程工作台：改码/命令在远端 SSH 执行；Skill、MCP、联网检索在本机。同一 Tab 可多轮续写。右侧为源码预览。", "全功能遠端工作台：改碼/命令在遠端 SSH 執行；Skill、MCP、聯網檢索在本機。同一 Tab 可多輪續寫。右側為原始碼預覽。");
        }
        if (activeProjectPreparing && activeProjectPrepareMode === "new-agent") {
            return localizeText(lang, "Starting full coding workbench (tools, Skill/MCP, source preview)…", "正在启动全功能编程工作台（工具 / Skill / MCP / 源码预览）…", "正在啟動全功能程式工作台（工具 / Skill / MCP / 原始碼預覽）…");
        }
        return localizeText(lang, "Full coding workbench (Claude Code / Codex–level intent). Tools, Skill/MCP, web research, multi-turn session memory, and source preview are active. Follow-up messages continue in this coding environment.", "全功能编程工作台（对齐 Claude Code / Codex）。工具、Skill/MCP、联网检索、多轮会话记忆与源码预览已启用；后续消息仍在本编程环境中续写。", "全功能程式工作台（對齊 Claude Code / Codex）。工具、Skill/MCP、聯網檢索、多輪工作階段記憶與原始碼預覽已啟用；後續訊息仍在本程式環境中續寫。");
    }, [isPureCodingEnvironment, isRemoteCodingDevEnvironment, isRemoteMaintenanceEnvironment, remoteCodingNeedsReconnect, activeProjectPreparing, activeProjectPrepareMode, lang]);
    // Live record_audio card: hard-lock composer (no type-ahead / no queue) so the
    // user only uses pause/stop on the recording card until it finishes.
    const recordingActive = useMemo(
        () => isRecordingInputLocked(displayMessages, activeSessionIsStreaming),
        [activeSessionIsStreaming, displayMessages],
    );
    // Block paste/drop attachments while mic is live (composer hard-lock alone is not enough).
    const handlePaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
        if (recordingActive) {
            event.preventDefault();
            return;
        }
        handlePasteBase(event);
    }, [handlePasteBase, recordingActive]);
    const handleDragOver = useCallback((event: DragEvent<HTMLElement>) => {
        if (recordingActive) {
            event.preventDefault();
            event.dataTransfer.dropEffect = "none";
            return;
        }
        handleDragOverBase(event);
    }, [handleDragOverBase, recordingActive]);
    const handleDrop = useCallback((event: DragEvent<HTMLElement>) => {
        if (recordingActive) {
            event.preventDefault();
            return;
        }
        handleDropBase(event);
    }, [handleDropBase, recordingActive]);
    const isACPMirrorTabActive = activeTab.type === "project" && isACPAssistantSessionKey(activeTab.sessionKey);
    const inputLocked = isBusy || cancelPending || !codingTaskReadyForIntents || recordingActive || isACPMirrorTabActive;
    // Busy agent allows type-ahead queue; live mic does not — keep submitLocked true
    // for lock UI, but send paths hard-return when recordingActive.
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const prevShowChatUIRef = useRef(showChatUI);
    // Drain intent is session-owned. A tab switch must not let one session's
    // completed send or busy-submit arm another session's manual queue.
    const continueQueueDrainSessionKeysRef = useRef<Set<string>>(new Set());
    const queueAutoDrainArmedSessionKeysRef = useRef<Set<string>>(new Set());
    const latestSubmitLockedRef = useRef(submitLocked);
    const latestShowChatUIRef = useRef(showChatUI);
    latestSubmitLockedRef.current = submitLocked;
    latestShowChatUIRef.current = showChatUI;
    const showThinkingState = activeSessionIsStreaming;
    const showProcessingState = workflowAwaitingForm || workflowFormGeneratingDocument || (isBusy && (!activeSessionIsStreaming || hasActiveDetachedProjectRound));
    const inputVisualBusy = isBusy || workflowAwaitingForm || workflowFormGeneratingDocument;
    const showBusySpinner = inputVisualBusy;
    const codingAgentTurnSnapshot = useMemo(() => activeSessionHasWork ? latestCodingAgentTurnSnapshot(displayProgressMessages) : null, [activeSessionHasWork, displayProgressMessages]);
    const codingAgentProgress = useMemo(() => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(displayProgressMessages, activeSessionHasWork), [activeSessionHasWork, codingAgentTurnSnapshot, displayProgressMessages]);
    const latestToolProgress = useMemo(() => findLatestToolProgressText(displayProgressMessages, activeSessionHasWork), [activeSessionHasWork, displayProgressMessages]);
    const workflowFormStatusText = workflowFormActive
        ? (lang === "en" ? "Waiting for the workflow form on the right..." : "等待右侧工作流表单填写…")
        : (lang === "en" ? "Opening the workflow form on the right..." : "正在打开右侧工作流表单…");
    const workflowFormGeneratingText = lang === "en"
        ? "Generating the workflow document... (you can type ahead)"
        : "正在生成工作流文档…（可继续输入）";
    const activeProcessingText = workflowFormGeneratingDocument
        ? workflowFormGeneratingText
        : workflowAwaitingForm
        ? workflowFormStatusText
        : codingAgentProgress
        ? codingAgentCompactText(codingAgentProgress, lang)
        : latestToolProgress
            ? `${formatToolProgressStatus(latestToolProgress, lang)} · ${lang === "en" ? "you can type ahead" : "\u53ef\u7ee7\u7eed\u8f93\u5165"}`
            : processingText;
    const projectSearch = useProjectSearch(lang);
    useEffect(() => {
        if (!panelActive) projectSearch.close();
    }, [panelActive, projectSearch.close]);
    const handleProjectSearchSwitch = useCallback(async (msg: string) => {
        if (isBusy && cancelSession) {
            const ok = await showConfirm(
                localizeText(lang, "A task is running. Stop it and switch tasks?", "\u5f53\u524d\u6709\u4efb\u52a1\u6b63\u5728\u6267\u884c\u3002\u662f\u5426\u4e2d\u6b62\u5f53\u524d\u4efb\u52a1\u5e76\u5207\u6362\uff1f"),
                localizeText(lang, "Stop current task?", "\u4e2d\u6b62\u5f53\u524d\u4efb\u52a1\uff1f"),
                {
                    confirmText: localizeText(lang, "Stop and switch", "\u4e2d\u6b62\u5e76\u5207\u6362"),
                    cancelText: localizeText(lang, "Cancel", "\u53d6\u6d88"),
                    confirmVariant: 'danger',
                },
            );
            if (!ok) return;
            await cancelSession();
        }
        await sendMessageForTab(msg);
    }, [cancelSession, isBusy, lang, sendMessageForTab, showConfirm]);
    const deriveTaskNameFromMessages = useCallback(() => {
        const firstUser = messages.find((m: ChatMessage) => m.role === "user");
        const text = firstUser && typeof firstUser.content === "string" ? firstUser.content.trim() : "";
        const runes = [...text];
        return runes.length > 30 ? runes.slice(0, 30).join("") + "..." : text || (lang === "en" ? "Saved task" : "\u5df2\u4fdd\u5b58\u4efb\u52a1");
    }, [lang, messages]);

    const openSaveTaskDialog = useCallback(async () => {
        if (!isLocalTabActive) return;
        let suggested = deriveTaskNameFromMessages();
        try {
            const { SuggestCurrentTaskName } = await getWailsAppModule();
            if (typeof SuggestCurrentTaskName === "function") {
                const backendName = String(await SuggestCurrentTaskName() || "").trim();
                if (backendName) suggested = backendName;
            }
        } catch (error) {
            console.warn("[AIAssistantPanel] SuggestCurrentTaskName failed:", error);
        }
        setSaveTaskName(suggested);
        setSaveTaskDialogOpen(true);
    }, [deriveTaskNameFromMessages, isLocalTabActive]);
    useEffect(() => {
        const handler = () => { void openSaveTaskDialog(); };
        window.addEventListener('ai-save-current-chat-as-task', handler);
        return () => window.removeEventListener('ai-save-current-chat-as-task', handler);
    }, [openSaveTaskDialog]);

    // Branch command listener: triggered by the branch button on user messages.
    useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (typeof detail?.command === "string" && detail.command.trim()) {
                void dispatchTaskIntentRef.current?.(detail.command);
            }
        };
        window.addEventListener('ai-send-branch-command', handler);
        return () => window.removeEventListener('ai-send-branch-command', handler);
    }, []);

    // Handle external "run skill" requests (from Skills Management Panel Run button)
    useEffect(() => {
        const handler = (e: Event) => {
            const text = (e as CustomEvent).detail?.text;
            if (typeof text === "string" && text.trim()) {
                e.preventDefault(); // Signal to sender that injection was accepted
                void dispatchTaskIntentRef.current?.(text);
            }
        };
        window.addEventListener('maclaw:inject-chat-message', handler);
        return () => window.removeEventListener('maclaw:inject-chat-message', handler);
    }, []);

    // Check for workflow-starting indicator from the Workflows panel.
    // Uses sessionStorage as a cross-tab-switch communication channel because
    // this panel may not be mounted when the tile is clicked.
    // Checks on mount + listens for a custom event for the already-mounted case.
    useEffect(() => {
        const consume = () => {
            const raw = sessionStorage.getItem('maclaw:workflow-starting');
            if (!raw) return;
            try {
                const data = JSON.parse(raw);
                if (data.ts && Date.now() - data.ts < 5000) {
                    setWorkflowStartingLabel(data.label || '...');
                    sessionStorage.removeItem('maclaw:workflow-starting');
                    // Switch to local tab so workflow events are received correctly
                    if (data.activateLocal) {
                        activateTabRef.current('local');
                    }
                } else {
                    sessionStorage.removeItem('maclaw:workflow-starting');
                }
            } catch {
                sessionStorage.removeItem('maclaw:workflow-starting');
            }
        };
        // Check immediately (covers the case where panel was just mounted)
        consume();
        // Also listen for nudge events (covers the case where panel is already mounted)
        window.addEventListener('maclaw:workflow-starting-nudge', consume);
        return () => {
            window.removeEventListener('maclaw:workflow-starting-nudge', consume);
        };
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    // Auto-clear the workflow starting label after 8s (fallback timeout)
    useEffect(() => {
        if (!workflowStartingLabel) return;
        const timer = setTimeout(() => setWorkflowStartingLabel(null), 8000);
        return () => clearTimeout(timer);
    }, [workflowStartingLabel]);

    const submitSaveTask = useCallback(async () => {
        if (savingTask) return;
        const taskName = saveTaskName.trim() || deriveTaskNameFromMessages();
        setSavingTask(true);
        try {
            const { SaveCurrentChatAsTask } = await getWailsAppModule();
            if (typeof SaveCurrentChatAsTask !== "function") return;
            await SaveCurrentChatAsTask(taskName);
            setSaveTaskDialogOpen(false);
            onTaskPrefsChanged?.();
        } catch (err) {
            console.error("[SaveCurrentChatAsTask] failed:", err);
        } finally {
            setSavingTask(false);
        }
    }, [deriveTaskNameFromMessages, onTaskPrefsChanged, saveTaskName, savingTask]);

    const handleForkCurrentChat = useCallback(async (taskName: string) => {
        let derivedName = taskName;
        if (!derivedName) {
            derivedName = deriveTaskNameFromMessages();
        }
        try {
            const { SaveCurrentChatAsTask } = await getWailsAppModule();
            const result = await SaveCurrentChatAsTask(derivedName);
            if (!result || !result.project_path) return;
            const tab = createProjectTab(result.project_path, result.name || derivedName);
            if (tab) {
                saveTabState(tab.id, { history: [...messages], scrollTop: 0, inputText: "" });
            }
            onTaskPrefsChanged?.();
        } catch (err) {
            console.error("[SaveCurrentChat] failed:", err);
        }
    }, [createProjectTab, deriveTaskNameFromMessages, messages, onTaskPrefsChanged, saveTabState]);
    const initLabel = getAssistantInitLabel(initStatus, lang);
    const preparingPlaceholderText = activeProjectPrepareMode === "new-agent"
        ? (isRemoteCodingDevEnvironment
            ? (isRemoteMaintenanceEnvironment
                ? localizeText(lang, "Preparing remote maintenance... type ahead, Enter will wait", "正在准备远程维护… 可预输入，Enter 会等待", "正在準備遠端維護… 可預輸入，Enter 會等待")
                : localizeText(lang, "Creating remote coding environment... type ahead, Enter will wait", "正在创建远程编程环境… 可预输入，Enter 会等待", "正在建立遠端程式開發環境… 可預輸入，Enter 會等待"))
            : isCodingDevEnvironment
            ? localizeText(lang, "Creating coding environment... type ahead, Enter will wait", "正在创建编程环境… 可预输入，Enter 会等待", "正在建立程式開發環境… 可預輸入，Enter 會等待")
            : (lang === "en" ? "Creating project session... type ahead, Enter will wait" : "正在创建项目会话… 可预输入，Enter 会等待"))
        : (lang === "en" ? "Restoring task context... type ahead, Enter will wait" : "正在恢复任务上下文… 可预输入，Enter 会等待");
    const placeholderText = !ready
        ? initLabel
        : isACPMirrorTabActive
            ? (lang === "en" ? "This ACP session is controlled by its external client" : "此 ACP 会话由外部客户端控制")
        : recordingActive
            ? (lang === "en"
                ? "Recording in progress — use Pause / Stop on the card above"
                : "录音进行中 — 请使用上方录音卡片的暂停/停止")
            : activeProjectPreparing
            ? preparingPlaceholderText
            : remoteCodingNeedsReconnect
            ? localizeText(lang, "Reconnect SSH above... type ahead, Enter will wait", "请先在上方重新连接 SSH… 可预输入，Enter 会等待", "請先在上方重新連線 SSH… 可預輸入，Enter 會等待")
            : workflowAwaitingForm
            ? (workflowFormGeneratingDocument
                ? (lang === "en" ? "Generating the workflow document..." : "正在生成工作流文档…")
                : workflowFormActive
                ? (lang === "en" ? "Fill in the workflow form on the right to continue" : "请在右侧工作流表单填写并提交")
                : (lang === "en" ? "Opening the workflow form on the right..." : "正在打开右侧工作流表单…"))
            : workflowAwaitingReview
            ? (lang === "en" ? "Review the document on the right, then confirm or provide feedback" : "请先查看右侧文档，再确认推进或输入补充意见")
            : showThinkingState
            ? thinkingText
            : showProcessingState
                ? activeProcessingText
                : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        if (activeTab.type === "project" || activeTab.type === "expert") {
            saveTabState(activeTab.id, { inputText: nextValue });
            return;
        }
        setDraftInputValue?.(nextValue);
    }, [activeTab.id, activeTab.type, saveTabState, setDraftInputValue]);
    const canSend = ready && !recordingActive && !isACPMirrorTabActive && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
    const [welcomeTemplateOffer, setWelcomeTemplateOffer] = useState<WelcomeTemplateSaveOffer | null>(null);
    // Drop the save-offer when switching tabs.
    useEffect(() => {
        setWelcomeTemplateOffer(null);
    }, [activeTab.id]);
    const handleWelcomePromptSelect = useCallback((text: string, _meta?: WelcomePromptSubmitMeta) => {
        // Filled prompts from WelcomePromptParamDialog — free-form text, not slash commands.
        setComposeAction(null);
        setWelcomeTemplateOffer(null);
        updateInputValue(text);
        requestAnimationFrame(() => {
            if (inputRef.current) {
                inputRef.current.focus();
                inputRef.current.style.height = "auto";
                inputRef.current.style.height = inputRef.current.scrollHeight + "px";
                // Params are already filled in the dialog; place caret at end for review/send.
                inputRef.current.selectionStart = text.length;
                inputRef.current.selectionEnd = text.length;
            }
        });
    }, [updateInputValue, inputRef]);
    const selectedFileName = selectedFilePath ? selectedFilePath.split(/[/\\]/).pop() || selectedFilePath : "";
    const { pinnedNews, otherMessages } = useMemo(() => {
        const pinned: ChatMessage[] = [];
        const other: ChatMessage[] = [];
        for (const m of displayMessages) {
            if (isPinnedNewsMessage(m)) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [displayMessages]);
    const [branchPoints, setBranchPoints] = useState<ConversationBranchPointLike[]>([]);
    useEffect(() => {
        if (!isLocalTabActive || otherMessages.length < 2 || sending || streaming) {
            setBranchPoints([]);
            return;
        }
        let cancelled = false;
        GetConversationBranchPoints()
            .then(points => {
                if (cancelled) return;
                setBranchPoints(Array.isArray(points) ? points : []);
            })
            .catch(() => {
                if (!cancelled) setBranchPoints([]);
            });
        return () => {
            cancelled = true;
        };
    }, [isLocalTabActive, otherMessages.length, sending, streaming]);
    const branchPointByDisplayIndex = useMemo(() => {
        const map = new Map<number, ConversationBranchPointLike>();
        for (const point of branchPoints) {
            const index = Number(point?.index);
            if (Number.isInteger(index) && index >= 0) map.set(index, point);
        }
        return map;
    }, [branchPoints]);
    // Show welcome for an idle, empty conversation on the local tab or a regular
    // project tab. Pure coding tabs are already task workbenches; showing the
    // generic assistant guide after their environment connects hides that state.
    // NOTE: welcome view is shown in both inline (embedded panel) and overlay (standalone window)
    // modes — the embedded panel is now the primary usage mode.
    const showWelcomeView = ready && !onboardingIncomplete && otherMessages.length === 0 && displayProgressMessages.length === 0 && !showThinkingState && !showProcessingState && !activeProjectPreparing && !workflowAwaitingForm && !workflowFormGeneratingDocument && !workflowAwaitingReview && !workflowStartingLabel && queue.length === 0 && !queueEditDraftActive && !queueInteractionStarted && (isLocalTabActive || (isProjectTabActive && !isPureCodingEnvironment));
    const pureCodingEmptyTitle = isRemoteMaintenanceEnvironment
        ? (remoteCodingNeedsReconnect
            ? localizeText(lang, "Remote maintenance needs SSH reconnect", "远程维护需要重新连接 SSH", "遠端維護需要重新連線 SSH")
            : localizeText(lang, "Remote maintenance ready", "远程维护已就绪", "遠端維護已就緒"))
        : isRemoteCodingDevEnvironment
        ? (remoteCodingNeedsReconnect
            ? localizeText(lang, "Remote coding environment needs SSH reconnect", "远程编程环境需要重新连接 SSH", "遠端程式環境需要重新連線 SSH")
            : localizeText(lang, "Remote coding environment connected", "远程编程环境已连接", "遠端程式環境已連線"))
        : localizeText(lang, "Coding environment ready", "编程环境已就绪", "程式環境已就緒");
    const pureCodingEmptyDescription = isRemoteMaintenanceEnvironment
        ? (remoteCodingNeedsReconnect
            ? localizeText(lang, "Reconnect above before continuing this maintenance task.", "请先在上方重新连接，再继续此维护任务。", "請先在上方重新連線，再繼續此維護任務。")
            : localizeText(lang, "Describe the incident or maintenance goal below. The assistant starts with read-only checks and asks before risky repairs.", "在下方描述故障或维护目标。助手会先进行只读检查，并在高风险修复前请求确认。", "在下方描述故障或維護目標。助手會先進行唯讀檢查，並在高風險修復前請求確認。"))
        : remoteCodingNeedsReconnect
            ? localizeText(lang, "Reconnect above before sending a programming task.", "请先在上方重新连接，再发送编程任务。", "請先在上方重新連線，再傳送程式任務。")
            : localizeText(lang, "Enter a programming task below to start working in this repository.", "在下方输入编程任务，即可开始处理此仓库。", "在下方輸入程式任務，即可開始處理此倉庫。");
    const showPureCodingEmptyState = isPureCodingEnvironment
        && ready
        && !onboardingIncomplete
        && !activeProjectPreparing
        && displayMessages.length === 0
        && !showThinkingState
        && !showProcessingState;
    const pureCodingEmptyContent = isPureCodingEnvironment ? (
        <div
            data-testid={isRemoteCodingDevEnvironment ? "remote-coding-workbench-empty" : "coding-workbench-empty"}
            style={{
                minHeight: "100%",
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
                gap: 7,
                padding: "28px 20px",
                boxSizing: "border-box",
                textAlign: "center",
                color: t.textMuted,
            }}
        >
            <div style={{ color: t.text, fontSize: 14, fontWeight: 600 }}>
                {pureCodingEmptyTitle}
            </div>
            {isRemoteCodingDevEnvironment && activeTab.remoteHost ? (
                <div style={{ color: t.headingColor, fontSize: 12 }}>{activeTab.remoteHost}</div>
            ) : null}
            <div style={{ maxWidth: 520, color: t.emptyHint, fontSize: 12, lineHeight: 1.55 }}>
                {pureCodingEmptyDescription}
            </div>
        </div>
    ) : undefined;
    // Returning to an empty welcome state should clear any leftover save offer.
    useEffect(() => {
        if (showWelcomeView) setWelcomeTemplateOffer(null);
    }, [showWelcomeView]);
    const hasConversation = otherMessages.length + displayProgressMessages.length > 0;

    // Clear the workflow-starting indicator only when a definitive workflow UI takes over.
    // Do NOT clear on transient states (isBusy, showThinkingState) — they flash briefly
    // and the welcome page would return before the real workflow UI arrives.
    useEffect(() => {
        if (workflowStartingLabel && (hasConversation || workflowAwaitingForm || workflowFormGeneratingDocument || workflowAwaitingReview)) {
            setWorkflowStartingLabel(null);
        }
    }, [workflowStartingLabel, hasConversation, workflowAwaitingForm, workflowFormGeneratingDocument, workflowAwaitingReview]);
    const { handleScroll, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef } = useAssistantOutputScroll({ hasConversation, messages: displayMessages, ready, scrollToTopSeq });
    useEffect(() => {
        if (activeTab.type !== "project") return;
        latestProjectCloseSnapshotRef.current = {
            tabId: activeTab.id,
            projectPath: activeTab.projectPath,
            messages: displayMessages,
            inputText: localDraftInputValue,
            scrollTop: outputContainerRef.current?.scrollTop || 0,
        };
    }, [activeTab.id, activeTab.projectPath, activeTab.type, displayMessages, localDraftInputValue, outputContainerRef]);
    const handleInputResizeEnd = useCallback(() => {
        scrollToBottom("auto", true, 2);
    }, [scrollToBottom]);
    const { inputAreaHeight, resizeInput, startInputResize } = useResizableAssistantInput(inputRef, inputValue, handleInputResizeEnd);
    useEffect(() => {
        if (activeTab.type === "project" || activeTab.type === "expert") return;
        setLocalDraftInputValue(draftInputValue);
    }, [activeTab.type, draftInputValue]);
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);
    useEffect(() => {
        return () => {
            if (projectTabIdsPersistTimerRef.current) {
                clearTimeout(projectTabIdsPersistTimerRef.current);
                projectTabIdsPersistTimerRef.current = null;
            }
            firingEntryIdsRef.current.clear();
            drainingEntryIdsRef.current.clear();
            continueQueueDrainSessionKeysRef.current.clear();
            queueAutoDrainArmedSessionKeysRef.current.clear();
        };
    }, []);
    useEffect(() => {
        if (!maximized && inline) return;
        const handler = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (inline && maximized) {
                onToggleMaximize?.();
                return;
            }
            if (!inline) onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose, inline, maximized, onToggleMaximize]);
    const applyInputValue = useCallback((nextValue: string) => {
        updateInputValue(nextValue);
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = nextValue.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [resizeInput, updateInputValue]);
    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({ applyInputValue, inputRef, inputValue, submittedPrompts });
    const handleComposeActionChange = useCallback((action: ComposeAction | null) => {
        setComposeAction(action);
        requestAnimationFrame(() => inputRef.current?.focus());
    }, []);
    const clearComposerDraft = useCallback((options?: { clearAttachments?: boolean; focus?: boolean }) => {
        resetHistoryBrowsing();
        setComposeAction(null);
        updateInputValue("");
        if (inputRef.current) {
            // Controlled <textarea> keeps the previous DOM value until React re-renders.
            // Clear it immediately so a rapid second Enter cannot re-read the same draft
            // via `inputRef.current?.value ?? inputValue` and double-queue/send.
            inputRef.current.value = "";
            inputRef.current.style.height = "auto";
        }
        if (options?.clearAttachments) {
            setPendingAttachments([]);
            clearSelectedFile?.();
        }
        if (options?.focus !== false) {
            requestAnimationFrame(() => inputRef.current?.focus());
        }
    }, [clearSelectedFile, resetHistoryBrowsing, setPendingAttachments, updateInputValue]);
    const newConversationInFlightRef = useRef(false);
    const handlePlusMenuAction = useCallback((actionId: PlusMenuActionId) => {
        if (actionId === "newConversation") {
            // Same behavior as the title-bar "New conversation" control.
            if (inputLocked || newConversationInFlightRef.current) return;
            newConversationInFlightRef.current = true;
            // Clear local draft/attachments immediately for snappier UI while history resets.
            clearComposerDraft({ clearAttachments: true, focus: true });
            void Promise.resolve(clearActiveHistory()).finally(() => {
                newConversationInFlightRef.current = false;
            });
        }
    }, [clearActiveHistory, clearComposerDraft, inputLocked]);
    /** Shared /btw dispatch used by typed send, voice, queue drain, and queue fire. */
    const dispatchBtwText = useCallback(async (commandText: string): Promise<boolean> => {
        if (!sendBtwMessage || !isBtwCommandText(commandText)) return false;
        const recorded = commandText.trim();
        try {
            await sendBtwMessage(btwQueryFromText(recorded));
            // Only record after a successful handoff so failed sends can be retried cleanly.
            recordSubmittedPrompt?.(recorded);
            return true;
        } catch (err: unknown) {
            console.warn("[AIAssistantPanel] /btw send failed", err);
            return false;
        }
    }, [recordSubmittedPrompt, sendBtwMessage]);
    const handleInsertTemplate = useCallback((template: string) => {
        setComposeAction(null);
        updateInputValue(template);
        requestAnimationFrame(() => {
            if (!inputRef.current) return;
            inputRef.current.focus();
            inputRef.current.style.height = "auto";
            inputRef.current.style.height = inputRef.current.scrollHeight + "px";
            const caret = template.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [updateInputValue]);
    const fireSlashInFlightRef = useRef(false);
    const handleFireSlashCommand = useCallback(async (command: FireSlashCommand) => {
        if (!ready || fireSlashInFlightRef.current) return;
        // Slash palette actions are task intents too.  Do not let this UI-only
        // shortcut bypass a preparing/disconnected remote coding lifecycle.
        if (!codingTaskReadyForIntents) {
            addEntry(command, [], { autoDrain: true, steerWhenBusy: false });
            queueAutoDrainArmedSessionKeysRef.current.add(activeSessionKey);
            return;
        }
        fireSlashInFlightRef.current = true;
        setComposeAction(null);
        // Immediate slash commands (/help, /memory, …) are handled before the agent
        // loop on the backend — always send now, never park them behind the busy queue.
        userScrolledUpRef.current = false;
        try {
            const sent = await sendMessageForTab(command);
            if (sent !== false) recordSubmittedPrompt?.(command);
        } finally {
            fireSlashInFlightRef.current = false;
            requestAnimationFrame(() => inputRef.current?.focus());
        }
    }, [activeSessionKey, addEntry, codingTaskReadyForIntents, ready, recordSubmittedPrompt, sendMessageForTab]);
    const handleClearInput = useCallback(() => {
        clearComposerDraft({ clearAttachments: false });
    }, [clearComposerDraft]);
    const sendInFlightRef = useRef(false);
    /**
     * Single dispatch path for non-composer callers (window events, branch
     * controls, integrations). Those callers do not have an input widget to
     * enforce the lifecycle themselves, so queue their intent until the owning
     * coding environment is prepared and, for remote workbenches, connected.
     */
    const dispatchTaskIntent = useCallback(async (text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const trimmed = text.trim();
        if (!trimmed) return false;
        if (!codingTaskReadyForIntents) {
            const queued = addEntry(trimmed, [], { autoDrain: true, steerWhenBusy: false });
            if (queued) queueAutoDrainArmedSessionKeysRef.current.add(activeSessionKey);
            return queued !== null;
        }
        return sendMessageForTab(trimmed, options);
    }, [activeSessionKey, addEntry, codingTaskReadyForIntents, sendMessageForTab]);
    dispatchTaskIntentRef.current = dispatchTaskIntent;
    /** Welcome param dialog "Send now": dispatch without requiring a second click. */
    const handleWelcomePromptSend = useCallback(async (text: string, meta?: WelcomePromptSubmitMeta) => {
        const trimmed = (text || "").trim();
        if (!trimmed) return;
        setComposeAction(null);
        if (!ready) {
            handleWelcomePromptSelect(trimmed, meta);
            return;
        }
        // Live mic: do not queue — user must finish via the recording card.
        if (recordingActive || isACPMirrorTabActive) return;
        // Busy: queue like voice input so the message is not dropped.
        if (inputLocked || submitLocked) {
            addEntry(trimmed, [], { autoDrain: true });
            // Still offer save after queueing a scenario prompt.
            if (meta && shouldOfferWelcomeTemplateSave({ body: trimmed, title: meta.title })) {
                setWelcomeTemplateOffer({ title: meta.title, body: trimmed });
            }
            return;
        }
        if (sendInFlightRef.current) {
            handleWelcomePromptSelect(trimmed, meta);
            return;
        }
        sendInFlightRef.current = true;
        clearComposerDraft({ clearAttachments: true });
        userScrolledUpRef.current = false;
        try {
            const sent = await sendMessageForTab(trimmed);
            if (sent !== false) {
                recordSubmittedPrompt?.(trimmed);
                if (meta && shouldOfferWelcomeTemplateSave({ body: trimmed, title: meta.title })) {
                    setWelcomeTemplateOffer({ title: meta.title, body: trimmed });
                }
            }
        } catch (err: unknown) {
            console.warn("[AIAssistantPanel] Welcome prompt send failed", err);
            updateInputValue(trimmed);
        } finally {
            sendInFlightRef.current = false;
        }
    }, [
        ready,
        recordingActive,
        inputLocked,
        submitLocked,
        addEntry,
        clearComposerDraft,
        sendMessageForTab,
        recordSubmittedPrompt,
        handleWelcomePromptSelect,
        updateInputValue,
    ]);
    const handleWelcomeTemplateOfferSave = useCallback(() => {
        if (!welcomeTemplateOffer) return;
        const { templates } = saveWelcomeCustomTemplate({
            title: welcomeTemplateOffer.title,
            body: welcomeTemplateOffer.body,
        });
        // This save entry point is outside AssistantWelcomeView; keep its
        // snapshot on the same ordered, password-free sync path.
        syncLocalStartMenuTemplates(templates);
        setWelcomeTemplateOffer(null);
    }, [welcomeTemplateOffer]);
    const handleWelcomeTemplateOfferDismiss = useCallback(() => {
        setWelcomeTemplateOffer(null);
    }, []);
    const submitRecognizedVoiceText = useCallback(async (text: string, _source?: VoiceInputSource) => {
        // Defense-in-depth: never send/queue empty or punctuation-only ASR noise.
        if (!ready || !shouldDispatchASRText(text)) return;
        const trimmed = normalizeASRText(text);
        // Honor active compose mode (goal / btw) so voice matches typed send semantics.
        const composed = applyComposeActionToText(trimmed, composeAction);
        // Remote coding owns every turn, including /btw and install/control
        // commands. Preserve it in the tab queue until SSH is usable again.
        if (!codingTaskReadyForIntents) {
            addEntry(composed, [], { autoDrain: true, steerWhenBusy: false });
            setComposeAction(null);
            return;
        }
        if (isBtwCommandText(composed) && sendBtwMessage) {
            if (sendInFlightRef.current) return;
            sendInFlightRef.current = true;
            // Clear immediately — SendBtwQuery only resolves after the full side-query loop.
            clearComposerDraft({ clearAttachments: false });
            try {
                const ok = await dispatchBtwText(composed);
                if (!ok) {
                    // Restore draft so the user can retry after a hard failure.
                    updateInputValue(composed);
                    setComposeAction("btw");
                }
            } finally {
                sendInFlightRef.current = false;
            }
            return;
        }
        // Live mic: never queue voice as chat (would fight the recording session).
        if (recordingActive) return;
        // Install slash commands: same as typed send — always dispatch now (backend
        // handles them before the agent loop), even when the agent is busy.
        const voiceInstall = normalizeInstallCommandText(composed);
        if (voiceInstall) {
            if (sendInFlightRef.current) {
                addEntry(voiceInstall, [], { autoDrain: true });
                setComposeAction(null);
                return;
            }
            sendInFlightRef.current = true;
            setComposeAction(null);
            clearComposerDraft({ clearAttachments: false });
            try {
                const sent = await sendMessageForTab(voiceInstall);
                if (sent !== false) recordSubmittedPrompt?.(voiceInstall);
            } catch (err: unknown) {
                console.warn("[AIAssistantPanel] Voice install command send failed", err);
                updateInputValue(voiceInstall);
            } finally {
                sendInFlightRef.current = false;
                refreshQueueInFlight();
            }
            return;
        }
        // If agent is busy (inputLocked), queue the transcription for later delivery
        // instead of dropping it. The buffer queue auto-drains when the agent becomes idle.
        if (inputLocked) {
            addEntry(composed, [], { autoDrain: true });
            setComposeAction(null);
            return;
        }
        // A diarized recording can yield several chronological speaker turns.
        // The first turn starts a send; subsequent turns must be preserved for
        // the buffer queue instead of being silently dropped by the duplicate-
        // send guard. The queue drains after the active assistant turn ends.
        if (sendInFlightRef.current) {
            addEntry(composed, [], { autoDrain: true });
            setComposeAction(null);
            return;
        }
        sendInFlightRef.current = true;
        clearComposerDraft({ clearAttachments: false });
        try {
            const sent = await sendMessageForTab(composed);
            if (sent !== false) recordSubmittedPrompt?.(composed);
        } catch (err: unknown) {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        } finally {
            sendInFlightRef.current = false;
            // The send promise can settle before the session's busy state is
            // rendered. Wake the queue explicitly so a diarized follow-up
            // turn waits for this send, then drains promptly afterwards.
            refreshQueueInFlight();
        }
    }, [addEntry, clearComposerDraft, codingTaskReadyForIntents, composeAction, dispatchBtwText, inputLocked, ready, recordSubmittedPrompt, recordingActive, refreshQueueInFlight, sendBtwMessage, sendMessageForTab, updateInputValue]);
    const voiceInput = useVoiceInput(submitRecognizedVoiceText, audioInputDeviceId || '');
    const { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave } = useAIAssistantVoiceControls({
        inputRef,
        petFocusInputSeq,
        petVoiceStartSeq,
        ready,
        voiceInput,
    });
    const handleSend = useCallback(async () => {
        // Live mic session: do not send or queue — user must stop via the card.
        if (recordingActive) return;
        // Prevent duplicate submits before the session reports busy, but keep
        // the type-ahead queue usable while an existing session is running.
        if (sendInFlightRef.current && !submitLocked && !queueEditDraftActive) return;
        // Clear reconnect success once the user continues coding.
        if (remoteReconnect.success) {
            setRemoteReconnect(prev => (prev.success ? { ...prev, success: "" } : prev));
        }
        const rawInputValue = inputRef.current?.value ?? inputValue;
        const text = applyComposeActionToText(rawInputValue, composeAction);
        // Do this before the /btw and install fast paths. Those paths normally
        // bypass busy-state queuing, but a disconnected remote workbench must
        // not dispatch any tab-owned command until SSH reconnect succeeds.
        if (!codingTaskReadyForIntents && !queueEditDraftActive) {
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) return;
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = "." + (fileName.split(".").pop() || "").toLowerCase();
                attachments.push({ filePath: fp, isImage: isImageFilePath(fp), fileName, extension: ext });
            }
            setQueueInteractionStarted(true);
            addEntry(text || rawInputValue, attachments, { autoDrain: true, steerWhenBusy: false });
            queueAutoDrainArmedSessionKeysRef.current.add(activeSessionKey);
            setComposeAction(null);
            clearComposerDraft({ clearAttachments: true });
            return;
        }
        if (isBtwCommandText(text) && sendBtwMessage) {
            sendInFlightRef.current = true;
            // Clear immediately — SendBtwQuery only resolves after the full side-query loop.
            clearComposerDraft({ clearAttachments: true });
            try {
                const ok = await dispatchBtwText(text);
                if (!ok) {
                    // Restore draft so the user can retry after a hard failure.
                    updateInputValue(text);
                    setComposeAction("btw");
                }
            } finally {
                sendInFlightRef.current = false;
            }
            return;
        }
        // /skill /mcp /plugin install commands: always dispatch now (backend
        // handles them before the agent loop). If a send is already in flight,
        // queue the command so it is not silently dropped (matches voice path).
        // Normalize fullwidth slash / CLI prefix / aliases before send.
        const installText = normalizeInstallCommandText(text);
        if (installText) {
            if (sendInFlightRef.current) {
                addEntry(installText, [], { autoDrain: true });
                setComposeAction(null);
                clearComposerDraft({ clearAttachments: true });
                return;
            }
            sendInFlightRef.current = true;
            setComposeAction(null);
            clearComposerDraft({ clearAttachments: true });
            userScrolledUpRef.current = false;
            try {
                const sent = await sendMessageForTab(installText);
                if (sent !== false) recordSubmittedPrompt?.(installText);
            } catch (err: unknown) {
                console.warn("[AIAssistantPanel] install command send failed", err);
                updateInputValue(installText);
            } finally {
                sendInFlightRef.current = false;
                refreshQueueInFlight();
            }
            return;
        }
        if (submitLocked || queueEditDraftActive) {
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) {
                if (queueEditDraftActive) {
                    setQueueEditDraftActive(false);
                    clearComposerDraft({ clearAttachments: false });
                }
                return;
            }
            console.info("[AIAssistantPanel] queue input", {
                activeTabId: activeTab.id,
                activeTabType: activeTab.type,
                projectPath: activeTab.projectPath || "",
                submitLocked,
                queueEditDraftActive,
                sending,
                sendingSessionKey: sendingSessionKey || undefined,
                busySessionKeys,
                streaming,
                streamingSessionKey: streamingSessionKey || undefined,
                streamingSessionKeys,
                activeSessionKey,
                activeSessionIsSending,
                activeSessionIsStreaming,
                cancelPending,
                textLength: text.length,
                attachmentCount: pendingAttachments.length + selectedFilePaths.length,
                composeAction: composeAction || undefined,
            });
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = "." + (fileName.split(".").pop() || "").toLowerCase();
                attachments.push({ filePath: fp, isImage: isImageFilePath(fp), fileName, extension: ext });
            }
            setQueueInteractionStarted(true);
            // Queue stores the composed command text so /goal (and similar) survive drain.
            addEntry(text || rawInputValue, attachments, {
                autoDrain: submitLocked,
                // Enter creates a normal durable next-turn queue item. The
                // user explicitly chooses same-turn steering with this entry's
                // fire/attach button after it appears in the queue.
                steerWhenBusy: false,
            });
            if (submitLocked) {
                queueAutoDrainArmedSessionKeysRef.current.add(activeSessionKey);
            }
            setQueueEditDraftActive(false);
            clearComposerDraft({ clearAttachments: true });
            return;
        }
        if (!text && selectedFilePaths.length === 0 && pendingAttachments.length === 0) return;
        const allFilePaths: string[] = [...selectedFilePaths];
        for (const att of pendingAttachments) {
            if (att.filePath.trim()) allFilePaths.push(att.filePath.trim());
        }
        sendInFlightRef.current = true;
        clearComposerDraft({ clearAttachments: true });
        userScrolledUpRef.current = false;
        try {
            const outgoing = allFilePaths.length > 0 ? buildOutgoingMessageMulti(text, allFilePaths) : text;
            const sent = await sendMessageForTab(outgoing);
            if (sent !== false) recordSubmittedPrompt?.(text);
        } finally {
            sendInFlightRef.current = false;
        }
    }, [activeSessionIsSending, activeSessionIsStreaming, activeSessionKey, activeTab.id, activeTab.projectPath, activeTab.type, addEntry, busySessionKeys, cancelPending, clearComposerDraft, codingTaskReadyForIntents, composeAction, dispatchBtwText, inputValue, pendingAttachments, queueEditDraftActive, recordSubmittedPrompt, recordingActive, refreshQueueInFlight, remoteReconnect.success, selectedFilePaths, sendBtwMessage, sendMessageForTab, sending, sendingSessionKey, streaming, streamingSessionKey, streamingSessionKeys, submitLocked, updateInputValue]);
    useEffect(() => {
        if (queue.length === 0) {
            continueQueueDrainSessionKeysRef.current.delete(activeSessionKey);
            queueAutoDrainArmedSessionKeysRef.current.delete(activeSessionKey);
        }
        // A diarized voice recording calls its speaker turns synchronously.
        // The second turn may enter the buffer before the first send has made
        // submitLocked true, so the ref is the authoritative immediate guard.
        // Install slash commands may drain while the agent is busy (backend
        // handles them before the agent loop); other queue items still wait
        // for !submitLocked.
        const headText = queue[0]?.text?.trim() ?? "";
        const headIsInstall = headText.length > 0 && isInstallCommandText(headText);
        const readyBase = ready && showChatUI && !sendInFlightRef.current;
        // A remote coding queue must never drain through a stale/disconnected SSH
        // workbench. Install commands also wait here: this tab owns a remote
        // coding session, so every queued control turn must retain that ordering.
        const readyToDrainQueue = readyBase && codingTaskReadyForIntents && (!submitLocked || headIsInstall);
        const becameIdle = prevSubmitLockedRef.current && readyToDrainQueue;
        const returnedToChatIdle = !prevShowChatUIRef.current && readyToDrainQueue;
        const continueIdleDrain = continueQueueDrainSessionKeysRef.current.has(activeSessionKey) && readyToDrainQueue;
        const armedIdleDrain = queueAutoDrainArmedSessionKeysRef.current.has(activeSessionKey) && readyToDrainQueue;
        const persistedAutoDrain = !!queue[0]?.autoDrain && readyToDrainQueue;
        if ((becameIdle || returnedToChatIdle || continueIdleDrain || armedIdleDrain || persistedAutoDrain) && queue.length > 0 && !queueEditDraftActive) {
            const entry = queue[0];
            if (firingEntryIdsRef.current.has(entry.id) || drainingEntryIdsRef.current.has(entry.id)) {
                prevSubmitLockedRef.current = submitLocked;
                prevShowChatUIRef.current = showChatUI;
                return;
            }
            continueQueueDrainSessionKeysRef.current.delete(activeSessionKey);
            queueAutoDrainArmedSessionKeysRef.current.delete(activeSessionKey);
            drainingEntryIdsRef.current.add(entry.id);
            refreshQueueInFlight();
            const entryText = entry.text.trim();
            const entrySessionKey = entry.sessionKey?.trim() || activeSessionKey;
            const sendEntryAsTurn = (outgoing: string) => sendMessageForTab(outgoing, { queue_session_key: entrySessionKey });
            console.info("[AIAssistantPanel] drain queued input", {
                activeTabId: activeTab.id,
                activeTabType: activeTab.type,
                projectPath: activeTab.projectPath || "",
                entrySessionKey,
                entryId: entry.id,
                textLength: entryText.length,
                attachmentCount: entry.attachments.length,
            });
            // Preserve /btw side-query semantics when draining the buffer queue.
            const entryIsBtw = isBtwCommandText(entryText) && !!sendBtwMessage;
            // Install commands must go through the plain send path (same as
            // handleFireEntry), not attachment-wrapping multi builders.
            const entryInstall = normalizeInstallCommandText(entryText);
            const drainPromise = entryIsBtw
                ? dispatchBtwText(entryText)
                : entryInstall
                    ? sendEntryAsTurn(entryInstall)
                    : sendEntryAsTurn(buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath)));
            drainPromise.then((sent) => {
                if (sent === false) return;
				// A session forget/queue clear may have removed this item while the
				// send was unresolved. Only the operation that still owns the durable
				// queue item may continue draining or mutate prompt history.
				if (!removeEntry(entry.id)) return;
				continueQueueDrainSessionKeysRef.current.add(entrySessionKey);
                // dispatchBtwText already records the prompt; only record normal sends here.
                if (!entryIsBtw) recordSubmittedPrompt?.(entryInstall ?? entry.text);
            }).catch(() => {}).finally(() => {
                drainingEntryIdsRef.current.delete(entry.id);
                refreshQueueInFlight();
            });
        }
        prevSubmitLockedRef.current = submitLocked;
        prevShowChatUIRef.current = showChatUI;
    }, [activeSessionKey, activeTab.id, activeTab.projectPath, activeTab.type, codingTaskReadyForIntents, dispatchBtwText, queue, queueEditDraftActive, queueInFlightVersion, ready, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendBtwMessage, sendMessageForTab, showChatUI, submitLocked]);
    const handleFireEntry = useCallback(async (id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        const entry = queue.find(item => item.id === id);
        if (!entry) return;
        // "Send now" is still a coding-task intent. A queued remote task may
        // outlive the connection that created it, so it must wait for the same
        // lifecycle gate as typed, voice, slash-palette, and auto-drained input.
        // Leaving the entry intact lets the normal drain resume it after SSH
        // reconnects (or project preparation completes).
        if (!codingTaskReadyForIntents) {
            queueAutoDrainArmedSessionKeysRef.current.add(entry.sessionKey?.trim() || activeSessionKey);
            return;
        }
        const entrySessionKey = entry.sessionKey?.trim() || activeSessionKey;
        const sendEntryAsTurn = (text: string) => sendMessageForTab(text, { queue_session_key: entrySessionKey });
        firingEntryIdsRef.current.add(id);
        refreshQueueInFlight();
        const entryText = entry.text.trim();
        try {
            // /btw is a side query, not a main-loop inject — keep its semantics.
            if (isBtwCommandText(entryText) && sendBtwMessage) {
                const ok = await dispatchBtwText(entryText);
				if (ok) removeEntry(id);
                return;
            }
            // Install slash commands must go through the main send path (backend
            // handles them before the agent loop), not guide/inject.
            const fireInstall = normalizeInstallCommandText(entryText);
            if (fireInstall) {
                const sent = await sendEntryAsTurn(fireInstall);
                if (sent !== false) {
					if (removeEntry(id)) recordSubmittedPrompt?.(fireInstall);
                }
                return;
            }
            const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
            let injected = false;
            if (guideLaunchReference) {
                // Route by the queue item's durable owner, not whichever tab is
                // active when an async click/effect eventually runs.
                injected = await guideLaunchReference(outgoing, entrySessionKey, entry.id);
            } else if (injectSupplementary) {
                injected = await injectSupplementary(outgoing, entrySessionKey);
            }
            if (!injected) {
                // A turn can finish between rendering the busy state and the
                // steer RPC. Keep the entry, stop retrying steer, and let the
                // normal queue drain submit it as the next turn.
                updateEntry(id, entry.text, entry.attachments, { autoDrain: true, steerWhenBusy: false });
                return;
            }
			if (removeEntry(id)) recordSubmittedPrompt?.(entry.text);
        } catch {
            updateEntry(id, entry.text, entry.attachments, { autoDrain: true, steerWhenBusy: false });
            return;
        } finally {
            firingEntryIdsRef.current.delete(id);
            refreshQueueInFlight();
        }
    }, [activeSessionKey, codingTaskReadyForIntents, dispatchBtwText, guideLaunchReference, injectSupplementary, queue, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendBtwMessage, sendMessageForTab, updateEntry]);
    useEffect(() => {
        if (activeProjectPreparing || cancelPending || recordingActive) return;
        // Preserve conversational order. In particular, a later ordinary
        // message must not jump ahead of a queued slash/control turn.
        const pendingSteer = queue[0]?.steerWhenBusy ? queue[0] : undefined;
        if (!pendingSteer) return;
        if (!activeSessionIsSending && !activeSessionIsStreaming) {
            // The turn ended before the effect ran. Convert to an ordinary
            // auto-draining follow-up instead of claiming it was attached.
            updateEntry(pendingSteer.id, pendingSteer.text, pendingSteer.attachments, { autoDrain: true, steerWhenBusy: false });
            return;
        }
        if (firingEntryIdsRef.current.has(pendingSteer.id) || drainingEntryIdsRef.current.has(pendingSteer.id)) return;
        void handleFireEntry(pendingSteer.id);
    }, [activeProjectPreparing, activeSessionIsSending, activeSessionIsStreaming, cancelPending, handleFireEntry, queue, recordingActive, updateEntry]);
    const handleDeleteEntry = useCallback((id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        removeEntry(id);
    }, [removeEntry]);
    // A queued entry cannot be attached or sent until its coding task has
    // completed setup and, for remote workbenches, SSH is connected. Reflect
    // that in the row controls instead of allowing a no-op "Send now" click.
    const isQueueEntryInFlight = useCallback((id: string) => (
        !codingTaskReadyForIntents
        || firingEntryIdsRef.current.has(id)
        || drainingEntryIdsRef.current.has(id)
    ), [codingTaskReadyForIntents, queueInFlightVersion]);
    const handleReorderEntry = useCallback((fromIndex: number, toIndex: number) => {
        const moving = queue[fromIndex];
        const target = queue[toIndex];
        if (!moving || !target) return;
        const isInFlight = (id: string) => firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id);
        if (isInFlight(moving.id) || isInFlight(target.id)) return;
        reorderEntry(fromIndex, toIndex);
    }, [queue, reorderEntry]);
    const handleEditEntry = useCallback((id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        const entry = extractEntry(id);
        if (!entry) return;
        setQueueInteractionStarted(true);
        setEditingEntryId(null);
        setQueueEditDraftActive(true);
        updateInputValue(entry.text);
        setPendingAttachments([...entry.attachments]);
        clearSelectedFile?.();
        resetHistoryBrowsing();
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = entry.text.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [clearSelectedFile, extractEntry, resetHistoryBrowsing, resizeInput, setPendingAttachments, updateInputValue]);
    const handleCancelEdit = useCallback(() => setEditingEntryId(null), []);
    const handleSaveEdit = useCallback((id: string, text: string, attachments: AttachmentInfo[]) => {
        updateEntry(id, text, attachments);
        setEditingEntryId(null);
    }, [updateEntry]);
    const handleCancel = useCallback(async () => {
        if (cancelPending) return;
        // If workflow is awaiting form input (no active agent loop), dismiss the
        // workflow form and cancel the workflow instead of calling cancelSession.
        if (workflowAwaitingForm) {
            const liveWorkflowFormRoute = agentView?.id?.startsWith("workflow:form:")
                ? {
                    viewID: agentView.id,
                    phaseID: agentViewHiddenFieldValue(agentView, "_workflow_phase") || workflowCurrentPhaseID,
                    workflowID: agentViewHiddenFieldValue(agentView, "_workflow_id") || workflowState.workflowID,
                    userID: agentViewHiddenFieldValue(agentView, "_workflow_user_id") || activeSessionKey,
                    eventScopeID: agentViewHiddenFieldValue(agentView, "_workflow_event_scope_id"),
                }
                : null;
            const cachedWorkflowFormRoute = lastWorkflowFormRouteRef.current;
            const canUseCachedWorkflowFormRoute = !!cachedWorkflowFormRoute
                && (!workflowState.workflowID || !cachedWorkflowFormRoute.workflowID || cachedWorkflowFormRoute.workflowID === workflowState.workflowID)
                && (!workflowCurrentPhaseID || !cachedWorkflowFormRoute.phaseID || cachedWorkflowFormRoute.phaseID === workflowCurrentPhaseID);
            const workflowFormRoute = liveWorkflowFormRoute || (canUseCachedWorkflowFormRoute ? cachedWorkflowFormRoute : null);
            const workflowFormViewID = workflowFormRoute
                ? workflowFormRoute.viewID
                : workflowCurrentPhaseID
                    ? `workflow:form:${workflowCurrentPhaseID}`
                    : "";
            if (workflowFormViewID) {
                void dismissAgentView(workflowFormViewID, {
                    __cancel_workflow: true,
                    _workflow_phase: workflowFormRoute ? workflowFormRoute.phaseID : workflowCurrentPhaseID,
                    _workflow_id: workflowFormRoute ? workflowFormRoute.workflowID : workflowState.workflowID,
                    _workflow_user_id: workflowFormRoute ? workflowFormRoute.userID : activeSessionKey,
                    _workflow_event_scope_id: workflowFormRoute ? workflowFormRoute.eventScopeID : "",
                });
                return;
            }
        }
        if (!cancelSession) return;
        const restoreSeq = ++cancelRestoreSeqRef.current;
        const previousInputValue = inputValue;
        setCancelPending(true);
        try {
            // If Computer Use is active, hard-stop it together with generation cancel
            // so desktop click/type loops do not continue after the user hits stop.
            try {
                const cu: any = await GetComputerUseStatus();
                if (cu && (cu.session_active || cu.paused || (cu.step_count ?? 0) > 0) && !cu.stopped) {
                    await ComputerUseStop();
                }
            } catch {
                /* ignore CU stop failures */
            }
            const { canceledText } = await cancelSession();
            if (cancelRestoreSeqRef.current !== restoreSeq) return;
            if (draftInputValue === previousInputValue) {
                updateInputValue(canceledText);
            }
            resetHistoryBrowsing();
            requestAnimationFrame(() => {
                resizeInput();
                inputRef.current?.focus();
            });
        } finally {
            setCancelPending(false);
        }
    }, [activeSessionKey, agentView, cancelPending, cancelSession, draftInputValue, dismissAgentView, inputValue, resetHistoryBrowsing, resizeInput, updateInputValue, workflowAwaitingForm, workflowCurrentPhaseID, workflowState.workflowID]);
    const panelSubmitAgentView = useCallback((viewId: string | undefined, data: Record<string, unknown>) => {
        if (typeof viewId === "string" && viewId.startsWith("workflow:form:")) {
            const submittedPhaseID = typeof data?._workflow_phase === "string" && data._workflow_phase.trim()
                ? data._workflow_phase.trim()
                : workflowCurrentPhaseID;
            const submittedPhase = workflowState.phases.find(phase => phase.id === submittedPhaseID);
            if (submittedPhaseID && submittedPhase?.expectsDocument !== false) {
                setWorkflowFormGeneratingPhaseID(submittedPhaseID);
            }
        }
        return submitAgentView?.(viewId, data);
    }, [submitAgentView, workflowCurrentPhaseID, workflowState.phases]);
    // Wrap executeAction so that on project tabs, a project round is
    // pre-registered before the send. executeAction's internal sendMessage
    // call goes through optionsForActiveSession (which adds project_path),
    // but it doesn't set up projectTabRoundsRef with the correct baseline.
    // Without pre-registration, the wasSending effect's fallback uses a
    // stale baseline (after messages are added), causing responses to not
    // appear in the project tab's message list.
    const panelExecuteAction = useCallback((command: string) => {
        // Workflow review "supplement_focus": focus input box after disabling buttons.
        // This must be handled at the Panel level (not in useAIAssistant hook)
        // because inputRef is a Panel-local ref not accessible from the hook.
        if (command === '__wf_review__ supplement_focus') {
            executeAction(command); // disables buttons in hook
            requestAnimationFrame(() => inputRef.current?.focus());
            return;
        }
        // Message-card actions ultimately invoke the same agent session as a
        // typed task. In a pure coding tab, keep them behind the lifecycle
        // gate so stale confirmation/recovery cards cannot send into a remote
        // workbench while its SSH connection is unavailable.
        if (isPureCodingEnvironment && !codingTaskReadyForIntents) return;
        const routeOptions = isProjectTabActive && activeTab.projectPath
            ? {
                project_path: activeTab.projectPath,
                tabId: activeTab.id,
                recentMessages: buildProjectTabRecentMessages(projectTabMessages),
            }
            : isExpertTabActive && activeTab.expertId
                ? {
                    expert_id: activeTab.expertId,
                    tabId: activeTab.id,
                    recentMessages: buildProjectTabRecentMessages(projectTabMessages),
                }
                : undefined;
        // Preserve the single-argument callback contract for local actions;
        // some panel integrations intentionally expose only that legacy shape.
        const executeRoutedAction = (actionCommand: string) => routeOptions
            ? executeAction(actionCommand, routeOptions)
            : executeAction(actionCommand);
        if (!isProjectTabActive || !activeTab.projectPath) {
            return executeRoutedAction(command);
        }
        // Commands handled entirely in the frontend — they call Wails bindings
        // directly without sendMessage. Do NOT pre-register a project round
        // for these (it would create a stale round that's never consumed).
        if (command.startsWith('__resolve_critical_confirm__') || command.startsWith('__view_trace__')) {
            return executeRoutedAction(command);
        }
        // Pre-register the project round with correct baseline (current
        // messages.length BEFORE executeAction adds user+placeholder).
        const roundKey = activeTab.sessionKey || projectSessionKey(activeTab.projectPath);
        if (roundKey && !projectTabRoundsRef.current.has(roundKey)) {
            const roundSeq = projectTabRoundSeqRef.current + 1;
            projectTabRoundSeqRef.current = roundSeq;
            projectTabRoundsRef.current.set(roundKey, {
                tabId: activeTab.id,
                projectPath: activeTab.projectPath,
                sessionKey: roundKey,
                baseline: messagesLengthRef.current,
                seq: roundSeq,
            });
            setProjectTabRouteVersion(version => version + 1);
        }
        // Call the original executeAction which handles all special command
        // logic (__workflow_choice__, __confirm_execution__, etc.) and
        // internally calls sendMessage with proper options.
        return executeRoutedAction(command);
    }, [activeTab.expertId, activeTab.id, activeTab.projectPath, codingTaskReadyForIntents, executeAction, isExpertTabActive, isProjectTabActive, isPureCodingEnvironment, projectTabMessages]);

    const handleRecordingComplete = useCallback((result: RecordingCompleteResult, messageId: string) => {
        // Deactivate before sending completion so input unlocks and the card
        // does not re-arm if the list re-renders mid-upload.
        try {
            deactivateRecordingSession?.(messageId);
        } catch {
            /* ignore */
        }
        const payload = formatRecordingCompletionMessage(result);
        // A recording card can complete while a remote coding workbench is
        // reconnecting. Route through the same lifecycle-aware dispatcher so
        // its completion never jumps ahead of that task's SSH recovery.
        void dispatchTaskIntent(payload, { uiAction: true, displayText: formatRecordingCompletionDisplay(result, lang) });
    }, [deactivateRecordingSession, dispatchTaskIntent, lang]);

    const lastAssistantIdx = useMemo(() => findLastIndex(otherMessages, m => m.role === 'assistant'), [otherMessages]);

    // Per-message render cache: avoids re-rendering unchanged messages during
    // streaming. During streaming, only the LAST assistant message changes
    // (every 33ms). Without this cache, all N messages get full Markdown
    // re-parsing on every token batch flush.
    const msgRenderCacheRef = useRef<Map<string, { contentKey: string; node: ReturnType<typeof renderMessage> }>>(new Map());
    // Incremental Markdown render state for the streaming (last) assistant message.
    // This avoids re-parsing the entire message content on every 33ms token flush.
    // Only the "active tail" (last incomplete paragraph) is re-parsed each frame;
    // completed paragraphs are frozen as cached React nodes.
    const incrementalStateRef = useRef<{ messageId: string; state: IncrementalRenderState }>({
        messageId: '', state: createIncrementalRenderState(),
    });
    // Track render-config: when theme/lang/callback change, invalidate entire cache
    // to avoid returning stale renders with old styles or stale closures.
    // NOTE: isBusy is NOT included here — it only affects the last assistant message's
    // <details open> state, handled via the per-message contentKey below.
    const prevRenderConfigRef = useRef<{ t: Theme; lang: string; savedFileLabel: string; execAction: typeof panelExecuteAction } | null>(null);
    if (!prevRenderConfigRef.current || prevRenderConfigRef.current.t !== t || prevRenderConfigRef.current.lang !== lang || prevRenderConfigRef.current.savedFileLabel !== savedFileLabel || prevRenderConfigRef.current.execAction !== panelExecuteAction) {
        prevRenderConfigRef.current = { t, lang, savedFileLabel, execAction: panelExecuteAction };
        msgRenderCacheRef.current.clear();
        // Also invalidate incremental state on config change (theme colors affect rendered nodes)
        incrementalStateRef.current = { messageId: '', state: createIncrementalRenderState() };
    }
    const renderedOtherMessages = useMemo(() => {
        const cache = msgRenderCacheRef.current;
        // Cap cache size to prevent unbounded growth across long sessions.
        // When the cache exceeds 200 entries (well above typical conversation
        // length), clear it entirely — the cost of one full re-render is
        // negligible vs the risk of memory bloat.
        if (cache.size > 200) {
            cache.clear();
        }
        return otherMessages.map((msg: ChatMessage, idx: number) => {
            const isLast = idx === lastAssistantIdx;
            // Content key captures message-specific fields that affect render.
            // isBusy is included only for the last assistant (affects <details open>).
            // Include the actual text, not just its length. Tool retries and stream
            // corrections can replace content in place without changing the length.
            const contentKey = `${msg.content ?? '__undefined__'}|${msg.kind ?? ''}|${msg.reasoning ?? ''}|${msg.actions?.length ?? 0}|${isLast ? 1 : 0}|${isLast && isBusy ? 1 : 0}|${msg.confirmation ? 1 : 0}|${msg.unfinishedSlot ? 1 : 0}|${msg.localFilePath ?? ''}|${msg.thumbnailBase64 ? 1 : 0}|${msg.recordingSession ? `${msg.recordingSession.active ? 1 : 0}:${msg.recordingSession.title}` : ''}`;
            const cached = cache.get(msg.id);
            if (cached && cached.contentKey === contentKey) {
                return cached.node;
            }
            // For the last assistant message during streaming (isBusy), use incremental
            // Markdown rendering to avoid O(content.length) full re-parse every 33ms.
            // The incremental renderer freezes completed paragraphs and only re-parses
            // the active tail (~20 lines), keeping per-frame cost < 1ms regardless of
            // total message length.
            let node: ReturnType<typeof renderMessage>;
            if (isLast && isBusy && msg.role === 'assistant' && msg.content && msg.content.length > 2000) {
                node = renderMessage(suppressWorkflowReviewActions(msg), panelExecuteAction, t, isLast, savedFileLabel, lang, isBusy, (formattedContent: string) => {
                    // Incremental render callback: called by renderMessage for the
                    // content section of the last streaming assistant message.
                    const incRef = incrementalStateRef.current;
                    if (incRef.messageId !== msg.id) {
                        incRef.messageId = msg.id;
                        incRef.state = createIncrementalRenderState();
                    }
                    return renderContentIncremental(formattedContent, t, incRef.state);
                }, handleRecordingComplete);
            } else {
                // Reset incremental state when streaming ends (isBusy becomes false)
                // so the final render is a clean full parse (100% correct).
                if (isLast && msg.role === 'assistant' && incrementalStateRef.current.messageId === msg.id && !isBusy) {
                    incrementalStateRef.current = { messageId: '', state: createIncrementalRenderState() };
                }
                node = renderMessage(suppressWorkflowReviewActions(msg), panelExecuteAction, t, isLast, savedFileLabel, lang, isBusy, undefined, handleRecordingComplete);
            }
            cache.set(msg.id, { contentKey, node });
            const branchPoint = msg.role === 'user' ? branchPointByDisplayIndex.get(idx) : undefined;
            if (branchPoint) {
                const branchIdx = Number(branchPoint.index ?? idx);
                const branchCount = Number(branchPoint.branches || 0);
                const branchTitle = lang === 'en'
                    ? branchCount > 1
                        ? `Branch from here (#${branchIdx}, ${branchCount} paths)`
                        : `Branch from here (#${branchIdx})`
                    : branchCount > 1
                        ? `从此处分支 (#${branchIdx}，${branchCount} 条路径)`
                        : `从此处分支 (#${branchIdx})`;
                const wrappedNode = (
                    <div key={`branch-wrap-${msg.id}`} style={{ position: 'relative' }} className="branch-hover-container">
                        {node}
                        <button
                            type="button"
                            className="branch-btn"
                            aria-label={branchTitle}
                            title={branchTitle}
                            onClick={() => {
                                window.dispatchEvent(new CustomEvent('ai-send-branch-command', { detail: { command: `/branch ${branchIdx}` } }));
                            }}
                            style={{
                                position: 'absolute', top: 2, right: 4,
                                background: 'transparent', border: 'none',
                                cursor: 'pointer', fontSize: 11, opacity: 0,
                                transition: 'opacity 0.15s',
                                color: t.textMuted, padding: '2px 6px', borderRadius: 4,
                            }}
                        ><IconBranch size={12} color="currentColor" /></button>
                    </div>
                );
                cache.set(msg.id, { contentKey, node: wrappedNode });
                return wrappedNode;
            }
            return node;
        });
    }, [otherMessages, panelExecuteAction, t, lastAssistantIdx, savedFileLabel, lang, isBusy, branchPointByDisplayIndex, handleRecordingComplete]);
    const chatProgressMessages = useMemo(
        () => activeSessionHasWork ? displayProgressMessages.filter((msg: ChatMessage) => !isToolProgressMessage(msg)) : displayProgressMessages,
        [activeSessionHasWork, displayProgressMessages],
    );
    const compactProgressMessages = useMemo(() => compactCodingAgentProgressMessages(chatProgressMessages), [chatProgressMessages]);
    // Group consecutive coding-agent tool rows into one activity feed (Codex-style trail).
    const renderedProgressMessages = useMemo(() => {
        const items = groupCodingAgentProgressForRender(compactProgressMessages);
        return items.map((item) => {
            if (item.kind === "coding-feed") {
                return (
                    <Fragment key={item.key}>
                        {renderCodingAgentActivityFeed(item.messages, t, lang)}
                    </Fragment>
                );
            }
            return (
                <Fragment key={item.message.id}>
                    {renderMessage(suppressWorkflowReviewActions(item.message), panelExecuteAction, t, false, savedFileLabel, lang)}
                </Fragment>
            );
        });
    }, [compactProgressMessages, panelExecuteAction, t, savedFileLabel, lang]);
    // maximizedInlineStyle uses inset:0 (not 100vw/100vh) so UI scale transform cannot clip chrome.
    const containerStyle: React.CSSProperties = !inline
        ? overlayStyle
        : maximized
            ? { ...maximizedInlineStyle, background: t.bg }
            : {
                display: "flex",
                flex: "1 1 0%",
                flexDirection: "column",
                minWidth: 0,
                minHeight: 0,
                boxSizing: "border-box",
                overflow: "hidden",
                background: t.bg,
                textAlign: "left",
                width: "100%",
                height: "100%",
                position: "relative",
            };
    const scopeApprovalIsHighRisk = scopeApprovalPending?.kind === REMOTE_HIGH_RISK_APPROVAL_KIND || scopeApprovalPending?.kind === LOCAL_HIGH_RISK_APPROVAL_KIND;
    const scopeApprovalIsRemoteHighRisk = scopeApprovalPending?.kind === REMOTE_HIGH_RISK_APPROVAL_KIND;
    const scopeApprovalIsRemoteScope = scopeApprovalPending?.kind === REMOTE_DIRECTORY_WRITE_APPROVAL_KIND || scopeApprovalPending?.kind === REMOTE_PATH_ACCESS_APPROVAL_KIND;
    const scopeApprovalIsRemoteMaintenance = (scopeApprovalIsRemoteHighRisk || scopeApprovalIsRemoteScope) && scopeApprovalPending?.maintenance === true;
    return (
        <div data-testid="ai-panel-root" style={containerStyle}>
            <style>{`.branch-hover-container:hover .branch-btn { opacity: 0.7 !important; } .branch-hover-container .branch-btn:hover { opacity: 1 !important; background: ${t.fieldBg} !important; }`}</style>
            {inline && <AssistantDragHandle />}
            <AssistantTitleBar clearHistory={clearActiveHistory} clearHistoryDisabled={inputLocked} inline={!!inline} lang={lang} maximized={!!maximized} onClose={onClose} onDismissAppUpdate={onDismissAppUpdate} onHideWindow={onHideWindow} onOpenAppUpdate={onOpenAppUpdate} onOpenKnowledge={() => setKnowledgeDialogOpen(true)} onOpenTutorial={onOpenTutorial} onSaveCurrentTask={isLocalTabActive ? openSaveTaskDialog : undefined} onToggleMaximize={onToggleMaximize} onTogglePreviewPanel={handleTogglePreviewPanel} onToggleSkillRecording={handleToggleSkillRecording} previewPanelOpen={showWorkflowPreview || showCodePreview || showCodingConflictPanel} projectSearchOpen={projectSearch.open} refreshNews={refreshNews} showMaximizeToggle={showMaximizeToggle} skillRecording={skillRecordingTabId === activeTab?.id} skillRecordingCount={skillRecordingCount} skillRecordingAnyTab={!!skillRecordingTabId} theme={t} themeMode={themeMode} title={title} trialReflectEnabled={trialReflectEnabled} toggleProjectSearch={projectSearch.toggle} updateAvailable={appUpdateAvailable} workflowActive={workflowState.active} />
            {/* Column shell: chat|preview row on top, full-bleed bottom chrome under both
                (so the quick-settings / status strip spans into the code-preview column). */}
            <div data-testid="ai-panel-main" style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0, minWidth: 0, overflow: "hidden" }}>
            <div data-testid="ai-panel-content-row" style={{ display: "flex", flexDirection: "row", flex: 1, minHeight: 0, minWidth: 0, overflow: "hidden" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }} onDragOver={handleDragOver} onDrop={handleDrop}>
            <KnowledgeDialog open={panelActive && knowledgeDialogOpen} onClose={() => setKnowledgeDialogOpen(false)} lang={lang} theme={t} />
            {panelActive && scopeApprovalPending && (
                <div data-testid="scope-approval-backdrop" style={{ position: "fixed", inset: 0, zIndex: 50001, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(15, 23, 42, 0.35)", padding: 16 }}>
                    <div role="alertdialog" aria-modal="true" aria-labelledby="scope-approval-title" style={{ width: 440, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 14, boxShadow: (t.bg.startsWith("#0") || t.bg.startsWith("#1") || t.bg.startsWith("#2")) ? "0 1px 2px rgba(0, 0, 0, 0.30), 0 4px 12px -2px rgba(0, 0, 0, 0.40)" : "0 1px 2px rgba(30, 58, 95, 0.05), 0 4px 12px -2px rgba(30, 58, 95, 0.10)", color: t.text, overflow: "hidden" }} onMouseDown={e => e.stopPropagation()}>
                        <div style={{ padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                            <h3 id="scope-approval-title" style={{ margin: 0, fontSize: 14, fontWeight: 700, color: "#f59e0b" }}>{scopeApprovalIsHighRisk ? (scopeApprovalIsRemoteHighRisk ? localizeText(lang, "Remote Command Approval", "远程命令确认", "遠程命令確認") : localizeText(lang, "Command Approval", "命令确认", "命令確認")) : scopeApprovalIsRemoteMaintenance ? localizeText(lang, "Remote Maintenance Approval", "远程维护确认", "遠端維護確認") : localizeText(lang, "Scope Approval", "目录越权确认", "目錄越權確認")}</h3>
                        </div>
                        <div style={{ padding: "12px 14px", fontSize: 13, lineHeight: 1.6 }}>
                            <div style={{ marginBottom: 8 }}>{scopeApprovalIsHighRisk ? (scopeApprovalIsRemoteMaintenance ? localizeText(lang, "Remote maintenance is requesting a high-risk command:", "远程维护请求执行高风险命令：", "遠端維護請求執行高風險命令：") : scopeApprovalIsRemoteHighRisk ? localizeText(lang, "Remote CodingSubAgent is trying to run a blocked high-risk command:", "远程编码 SubAgent 尝试执行被拦截的高风险命令：", "遠程編碼 SubAgent 嘗試執行被攔截的高風險命令：") : localizeText(lang, "CodingSubAgent is trying to run a blocked high-risk command:", "编码 SubAgent 尝试执行被拦截的高风险命令：", "編碼 SubAgent 嘗試執行被攔截的高風險命令：")) : scopeApprovalIsRemoteMaintenance ? localizeText(lang, "Remote maintenance is requesting access outside the project scope:", "远程维护请求访问项目范围外的路径：", "遠端維護請求存取專案範圍外的路徑：") : localizeText(lang, "CodingSubAgent is trying to access a path outside the project:", "编码 SubAgent 尝试访问项目目录外的路径：", "編碼 SubAgent 嘗試訪問項目目錄外的路徑：")}</div>
                            <div style={{ background: t.fieldBg, borderRadius: 4, padding: "6px 8px", fontSize: 12, fontFamily: "monospace", wordBreak: "break-all", marginBottom: 6 }}>
                                <div><strong>{localizeText(lang, "Tool", "工具", "工具")}:</strong> {scopeApprovalPending.tool}</div>
                                <div><strong>{scopeApprovalIsHighRisk ? localizeText(lang, "Command", "命令", "命令") : localizeText(lang, "Path", "路径", "路徑")}:</strong> {scopeApprovalPending.path}</div>
                                <div><strong>{scopeApprovalIsHighRisk ? localizeText(lang, "Working dir", "工作目录", "工作目錄") : localizeText(lang, "Project", "项目范围", "項目範圍")}:</strong> {scopeApprovalPending.projectPath}</div>
                            </div>
                            <div style={{ fontSize: 12, color: t.textMuted }}>{scopeApprovalIsHighRisk ? localizeText(lang, "Only approve this if you understand the command and trust its effect. If you do nothing, it will be rejected.", "仅在你理解该命令并信任其影响时放行。不操作将自动拒绝。", "僅在你理解該命令並信任其影響時放行。不操作將自動拒絕。") : localizeText(lang, `Allow directory "${scopeApprovalPending.directory}" for the remainder of this task?`, `允许目录「${scopeApprovalPending.directory}」在本任务中后续操作？`, `允許目錄「${scopeApprovalPending.directory}」在本任務中後續操作？`)}</div>
                        </div>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "10px 14px", borderTop: `1px solid ${t.titleBarBorder}` }}>
                            <button type="button" onClick={() => void handleScopeApprovalResolve("deny")} style={{ padding: "5px 14px", borderRadius: 4, border: `1px solid ${t.fieldBorder}`, background: "transparent", color: t.text, fontSize: 12, cursor: "pointer" }}>{localizeText(lang, "Deny", "拒绝", "拒絕")}</button>
                            <button type="button" onClick={() => void handleScopeApprovalResolve("full_access")} style={{ padding: "5px 14px", borderRadius: 4, border: `1px solid ${t.fieldBorder}`, background: "transparent", color: "#22c55e", fontSize: 12, cursor: "pointer" }}>{scopeApprovalIsHighRisk ? localizeText(lang, "Allow Later", "以后放行", "以後放行") : localizeText(lang, "Full Access", "完全访问", "完全訪問")}</button>
                            <button type="button" onClick={() => void handleScopeApprovalResolve(scopeApprovalIsHighRisk ? "allow_once" : "allow_dir")} style={{ padding: "5px 14px", borderRadius: 4, border: "none", background: "#f59e0b", color: "#fff", fontSize: 12, fontWeight: 600, cursor: "pointer" }}>{scopeApprovalIsHighRisk ? localizeText(lang, `Allow Once (${scopeApprovalCountdown}s)`, `本次放行 (${scopeApprovalCountdown}s)`, `本次放行 (${scopeApprovalCountdown}s)`) : localizeText(lang, `Allow Directory (${scopeApprovalCountdown}s)`, `允许该目录 (${scopeApprovalCountdown}s)`, `允許該目錄 (${scopeApprovalCountdown}s)`)}</button>
                        </div>
                    </div>
                </div>
            )}
            {panelActive && saveTaskDialogOpen && (
                <div data-testid="save-task-dialog-backdrop" style={{ position: "fixed", inset: 0, zIndex: 50000, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(15, 23, 42, 0.28)", padding: 16 }} onMouseDown={event => { if (event.target === event.currentTarget && !savingTask) setSaveTaskDialogOpen(false); }}>
                    <form role="dialog" aria-modal="true" aria-labelledby="save-task-dialog-title" onSubmit={event => { event.preventDefault(); void submitSaveTask(); }} style={{ width: 390, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 14, boxShadow: (t.bg.startsWith("#0") || t.bg.startsWith("#1") || t.bg.startsWith("#2")) ? "0 1px 2px rgba(0, 0, 0, 0.30), 0 4px 12px -2px rgba(0, 0, 0, 0.40)" : "0 1px 2px rgba(30, 58, 95, 0.05), 0 4px 12px -2px rgba(30, 58, 95, 0.10)", color: t.text, overflow: "hidden" }} onMouseDown={event => event.stopPropagation()}>
                        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                            <h3 id="save-task-dialog-title" style={{ margin: 0, fontSize: 14, fontWeight: 700, color: t.text }}>{localizeText(lang, "Save as Task", "\u4fdd\u5b58\u4e3a\u4efb\u52a1", "\u4fdd\u5b58\u70ba\u4efb\u52d9")}</h3>
                            <button type="button" disabled={savingTask} onClick={() => setSaveTaskDialogOpen(false)} style={{ border: "none", background: "transparent", color: t.text, opacity: 0.62, cursor: savingTask ? "default" : "pointer", fontSize: 14, lineHeight: 1 }}>x</button>
                        </div>
                        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: "14px" }}>
                            <label htmlFor="save-task-name" style={{ fontSize: 12, fontWeight: 700, color: formFieldLabelColor(t) }}>{localizeText(lang, "Task name", "\u4efb\u52a1\u540d\u79f0", "\u4efb\u52d9\u540d\u7a31")}</label>
                            <input id="save-task-name" autoFocus value={saveTaskName} disabled={savingTask} onChange={event => setSaveTaskName(event.target.value)} onKeyDown={event => { if (event.key === "Escape" && !savingTask) setSaveTaskDialogOpen(false); }} style={{ width: "100%", boxSizing: "border-box", borderRadius: 6, fontSize: 13, padding: "7px 9px", fontFamily: "inherit", ...formFieldInputStyle(t) }} />
                            <p style={{ margin: "4px 0 0", fontSize: 12, lineHeight: 1.45, color: formFieldLabelColor(t) }}>{localizeText(lang, "The current main conversation history and task context will be saved. Double-click it in Task Management to continue in a separate tab.", "\u5c06\u4fdd\u5b58\u5f53\u524d\u4e3b\u5bf9\u8bdd\u5386\u53f2\u548c\u4efb\u52a1\u4e0a\u4e0b\u6587\u3002\u4e4b\u540e\u53ef\u5728\u4efb\u52a1\u7ba1\u7406\u4e2d\u53cc\u51fb\uff0c\u4ee5\u72ec\u7acb Tab \u7ee7\u7eed\u3002", "\u5c07\u4fdd\u5b58\u76ee\u524d\u4e3b\u5c0d\u8a71\u6b77\u53f2\u548c\u4efb\u52d9\u4e0a\u4e0b\u6587\u3002\u4e4b\u5f8c\u53ef\u5728\u4efb\u52d9\u7ba1\u7406\u4e2d\u96d9\u64ca\uff0c\u4ee5\u7368\u7acb Tab \u7e7c\u7e8c\u3002")}</p>
                        </div>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "10px 14px 12px", borderTop: `1px solid ${t.titleBarBorder}` }}>
                            <button type="button" disabled={savingTask} onClick={() => setSaveTaskDialogOpen(false)} style={{ border: `1px solid ${t.titleBarBorder}`, borderRadius: 6, background: t.fieldBg, color: t.text, cursor: savingTask ? "default" : "pointer", fontSize: 12, padding: "5px 12px" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88", "\u53d6\u6d88")}</button>
                            <button type="submit" disabled={savingTask || !saveTaskName.trim()} style={primaryFilledButtonStyle(t, { borderRadius: 6, cursor: savingTask || !saveTaskName.trim() ? "default" : "pointer", opacity: savingTask || !saveTaskName.trim() ? 0.62 : 1, fontSize: 12, padding: "5px 12px" })}>{savingTask ? localizeText(lang, "Saving...", "\u4fdd\u5b58\u4e2d...", "\u4fdd\u5b58\u4e2d...") : localizeText(lang, "Save", "\u4fdd\u5b58", "\u4fdd\u5b58")}</button>
                        </div>
                    </form>
                </div>
            )}
            <AITabBar tabs={tabState.tabs} activeTabId={tabState.activeTabId} theme={t} onActivate={activateTab} onClose={closeTabWithProjectCleanup} onInviteToTab={(tab) => {
                if (tab.type === "ve") {
                    const tabSt = getTabState(tab.id);
                    const sessionId = tabSt?.sessionId || tab.discussionId;
                    const currentParticipants = tab.participants || (tab.veId ? [tab.veId] : []);
                    const title = String(tab.title || "").trim();
                    const veId = String(tab.veId || "").trim();
                    const titleLooksRaw = looksLikeRawParticipantId(title);
                    const participantNames = veId && title && title !== veId && !titleLooksRaw ? { [veId]: title } : undefined;
                    upgradeVETabToGroup(tab.id, currentParticipants, sessionId, participantNames);
                }
                setParticipantInviteTargetTabId(tab.id);
                activateTab(tab.id);
            }} onAddLocalMaclawToTab={addLocalMaclawToTab} onRenameGroupTab={openRenameGroupDialog} lang={lang} recordingTabId={skillRecordingTabId} />
            {tabLimitError && <div data-testid="ai-tab-limit-error" style={{ padding: "6px 12px", fontSize: 12, color: t.errorText, background: t.errorBg, borderBottom: `1px solid ${t.errorBorder}`, textAlign: "center" }}>{tabLimitError}</div>}
            {(activeTab?.type === "local" || activeTab?.type === "project" || activeTab?.type === "expert") && (
                <ProjectDirBar
                    key={activeTab?.type === "project" ? activeTab.id : "local"}
                    // Expert sessions do not own a task path, so they share the
                    // current desktop working directory with the local assistant.
                    tabId={activeTab?.type === "project" ? activeTab.id : ""}
                    theme={t}
                    lang={lang}
                />
            )}
            {showChatUI && (
            <div data-testid="ai-chat-column" style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column", position: "relative", overflow: "hidden" }}>
                {isPureCodingEnvironment && (
                    <CodingWorkbenchControlPanel
                        lang={lang}
                        theme={t}
                        chrome={codingBannerChrome}
                        remote={isRemoteCodingDevEnvironment}
                        intent={isRemoteMaintenanceEnvironment ? "remote_maintenance" : undefined}
                        remoteHost={activeTab?.remoteHost}
                        preparing={activeProjectPreparing}
                        prepareMode={activeProjectPrepareMode || undefined}
                        stepStatuses={codingStepStatuses}
                        pendingApproval={codingPendingApproval}
                        conflictCount={codingConflictCount}
                        lockExpanded={remoteCodingNeedsReconnect}
                        expanded={codingControlExpanded}
                        onExpandedChange={setCodingControlExpanded}
                        envDescription={codingEnvDescription}
                    >
                        {/* Defer heavy control tree until expanded — parent would otherwise rebuild it every chat render. */}
                        {!codingControlExpanded ? null : remoteCodingNeedsReconnect ? (
                            <div data-testid="remote-coding-reconnect-form" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                                <div style={{ fontSize: 12, fontWeight: 600, color: t.headingColor || t.text }}>
                                    {localizeText(lang, "Reconnect remote SSH", "重新连接远程 SSH", "重新連線遠端 SSH")}
                                </div>
                                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Host", "主机", "主機")}
                                        <input
                                            data-testid="remote-reconnect-host"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.host}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, host: e.target.value, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "User", "用户名", "使用者")}
                                        <input
                                            data-testid="remote-reconnect-user"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.user}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, user: e.target.value, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Port", "端口", "連接埠")}
                                        <input
                                            data-testid="remote-reconnect-port"
                                            type="number"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.port || 22}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, port: Number(e.target.value) || 22, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Password", "密码", "密碼")}
                                        <input
                                            data-testid="remote-reconnect-password"
                                            type="password"
                                            disabled={remoteReconnect.connecting}
                                            autoComplete="current-password"
                                            value={remoteReconnect.password}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, password: e.target.value }))}
                                            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void handleRemoteCodingReconnect({}); } }}
                                            placeholder={localizeText(lang, "Remembered on this device", "本机记忆，下次自动填充", "本機記憶，下次自動填入")}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                </div>
                                <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                    {localizeText(lang, "Remote work directory", "远程工作目录", "遠端工作目錄")}
                                    <input
                                        data-testid="remote-reconnect-workdir"
                                        disabled={remoteReconnect.connecting}
                                        value={remoteReconnect.workDir}
                                        onChange={(e) => setRemoteReconnect(prev => ({ ...prev, workDir: e.target.value }))}
                                        style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                    />
                                </label>
                                {remoteReconnect.sessionPlan && (
                                    <div style={{ fontSize: 11, color: formFieldLabelColor(t), lineHeight: 1.45 }}>
                                        {localizeText(lang, "Continuing session plan: ", "将延续会话目标：", "將延續工作階段目標：")}
                                        {remoteReconnect.sessionPlan.length > 160 ? `${remoteReconnect.sessionPlan.slice(0, 160)}…` : remoteReconnect.sessionPlan}
                                    </div>
                                )}
                                {remoteReconnect.error && (
                                    <div data-testid="remote-reconnect-error" style={{ fontSize: 11, color: t.errorText || "#c43d34" }}>{remoteReconnect.error}</div>
                                )}
                                {remoteReconnect.connecting && remoteReconnect.success && (
                                    <div
                                        data-testid="remote-reconnect-progress"
                                        role="status"
                                        aria-live="polite"
                                        style={{ fontSize: 11, color: formFieldLabelColor(t) }}
                                    >
                                        {remoteReconnect.success}
                                    </div>
                                )}
                                <div style={{ display: "flex", justifyContent: "flex-end" }}>
                                    <button
                                        type="button"
                                        data-testid="remote-reconnect-submit"
                                        disabled={remoteReconnect.connecting}
                                        onClick={() => { void handleRemoteCodingReconnect({}); }}
                                        style={primaryFilledButtonStyle(t, {
                                            height: 28,
                                            padding: "0 14px",
                                            borderRadius: 4,
                                            fontSize: 12,
                                            fontWeight: 600,
                                            cursor: remoteReconnect.connecting ? "wait" : "pointer",
                                            opacity: remoteReconnect.connecting ? 0.75 : 1,
                                        })}
                                    >
                                        {remoteReconnect.connecting
                                            ? localizeText(lang, "Connecting…", "连接中…", "連線中…")
                                            : localizeText(lang, "Reconnect", "重新连接", "重新連線")}
                                    </button>
                                </div>
                            </div>
                        ) : (
                            <>
                                <CodingControlSection title={localizeText(lang, "Status & controls", "状态与控制", "狀態與控制")} chrome={codingBannerChrome}>
                                <div data-testid="coding-session-plan" style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                                    <span style={{ fontSize: 11, fontWeight: 600, color: codingBannerChrome.muted }}>
                                        {localizeText(lang, "Session plan", "会话目标", "工作階段目標")}
                                    </span>
                                    {!codingSessionPlanEditing && (
                                        <>
                                            <button
                                                type="button"
                                                data-testid="coding-session-plan-edit"
                                                onClick={() => {
                                                    setCodingSessionPlanDraft(codingSessionPlan);
                                                    setCodingSessionPlanEditing(true);
                                                }}
                                                style={{ border: "none", background: "transparent", color: codingBannerChrome.accentStrong, fontSize: 11, cursor: "pointer", padding: 0, fontWeight: 600 }}
                                            >
                                                {codingSessionPlan
                                                    ? localizeText(lang, "Edit", "编辑", "編輯")
                                                    : localizeText(lang, "Set goal", "设置目标", "設定目標")}
                                            </button>
                                            <button
                                                type="button"
                                                data-testid="coding-session-plan-extract"
                                                onClick={handleExtractCodingSessionPlan}
                                                style={{ border: "none", background: "transparent", color: codingBannerChrome.accentStrong, fontSize: 11, cursor: "pointer", padding: 0, fontWeight: 600 }}
                                                title={localizeText(lang, "Fill from the earliest user message in this chat", "用本对话最早的用户消息填充目标", "用本對話最早的使用者訊息填入目標")}
                                            >
                                                {localizeText(lang, "From chat", "从对话提取", "從對話擷取")}
                                            </button>
                                        </>
                                    )}
                                </div>
                                {codingSessionPlanEditing ? (
                                    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                                        <textarea
                                            data-testid="coding-session-plan-input"
                                            value={codingSessionPlanDraft}
                                            onChange={(e) => setCodingSessionPlanDraft(e.target.value)}
                                            rows={2}
                                            placeholder={localizeText(lang, "Overall coding goal for this multi-turn session…", "本多轮编程会话的总体目标…", "本多輪程式工作階段的總體目標…")}
                                            style={{ width: "100%", resize: "vertical", minHeight: 44, padding: "6px 8px", borderRadius: 4, border: `1px solid ${t.fieldBorder}`, background: t.fieldBg, color: t.text, fontSize: 12, lineHeight: 1.4, boxSizing: "border-box" }}
                                        />
                                        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
                                            <button type="button" data-testid="coding-session-plan-cancel" onClick={() => { setCodingSessionPlanEditing(false); setCodingSessionPlanDraft(codingSessionPlan); }} style={{ height: 24, padding: "0 10px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: "pointer" }}>
                                                {localizeText(lang, "Cancel", "取消", "取消")}
                                            </button>
                                            <button type="button" data-testid="coding-session-plan-extract-edit" onClick={handleExtractCodingSessionPlan} style={{ height: 24, padding: "0 10px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: t.text, fontSize: 11, cursor: "pointer" }}>
                                                {localizeText(lang, "From chat", "从对话提取", "從對話擷取")}
                                            </button>
                                            <button type="button" data-testid="coding-session-plan-save" disabled={codingSessionPlanSaving} onClick={() => { void handleSaveCodingSessionPlan(); }} style={{ height: 24, padding: "0 10px", borderRadius: 4, border: "none", background: codingBannerChrome.btnPrimaryBg, color: codingBannerChrome.btnPrimaryFg, fontSize: 11, fontWeight: 600, cursor: codingSessionPlanSaving ? "wait" : "pointer", opacity: codingSessionPlanSaving ? 0.7 : 1 }}>
                                                {localizeText(lang, "Save", "保存", "儲存")}
                                            </button>
                                        </div>
                                    </div>
                                ) : (
                                    <>
                                        <div data-testid="coding-session-plan-text" style={{ fontSize: 11, opacity: 0.95, color: codingBannerChrome.muted, whiteSpace: "pre-wrap" }}>
                                            {codingSessionPlan
                                                ? (codingSessionPlan.length > 160 ? `${codingSessionPlan.slice(0, 160)}…` : codingSessionPlan)
                                                : localizeText(lang, "No session plan yet — set one to keep multi-turn focus.", "尚未设置会话目标 — 设置后多轮续写会始终对齐目标。", "尚未設定工作階段目標 — 設定後多輪續寫會始終對齊目標。")}
                                        </div>
                                        <div data-testid="coding-plan-mode" style={{ marginTop: 4, display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", fontSize: 11 }}>
                                            <span style={{ fontWeight: 600, color: codingBannerChrome.muted }}>
                                                {localizeText(lang, "Task handling", "任务处理", "任務處理")}
                                            </span>
                                            {(["auto", "approve", "off"] as const).map((mode) => (
                                                <button
                                                    key={mode}
                                                    type="button"
                                                    data-testid={`coding-plan-mode-${mode}`}
                                                    onClick={() => { void handleCodingPlanModeChange(mode); }}
                                                    style={{
                                                        height: 22,
                                                        padding: "0 8px",
                                                        borderRadius: 4,
                                                        border: `1px solid ${codingPlanMode === mode ? codingBannerChrome.accent : codingBannerChrome.chipIdleBorder}`,
                                                        background: codingPlanMode === mode ? codingBannerChrome.chipActiveBg : codingBannerChrome.chipIdleBg,
                                                        color: codingPlanMode === mode ? codingBannerChrome.accentStrong : codingBannerChrome.muted,
                                                        fontSize: 11,
                                                        fontWeight: codingPlanMode === mode ? 600 : 400,
                                                        cursor: "pointer",
                                                    }}
                                                >
                                                    {mode === "auto"
                                                        ? localizeText(lang, "Adaptive", "自动决策", "自動決策")
                                                        : mode === "approve"
                                                            ? localizeText(lang, "Plan first", "先给计划", "先給計畫")
                                                            : localizeText(lang, "Fast execute", "快速执行", "快速執行")}
                                                </button>
                                            ))}
                                            <span style={{ fontWeight: 600, color: codingBannerChrome.muted, marginLeft: 4 }}>
                                                {localizeText(lang, "Worktree", "Worktree", "Worktree")}
                                            </span>
                                            {(["auto", "always", "off"] as const).map((mode) => (
                                                <button
                                                    key={`wt-${mode}`}
                                                    type="button"
                                                    data-testid={`coding-worktree-mode-${mode}`}
                                                    onClick={() => { void handleCodingWorktreeModeChange(mode); }}
                                                    title={mode === "auto"
                                                        ? localizeText(lang, "Isolate write steps only when parallel", "仅在并行写改时隔离", "僅在並行寫改時隔離")
                                                        : mode === "always"
                                                            ? localizeText(lang, "Every write step in a git worktree", "每个写步骤使用 worktree", "每個寫步驟使用 worktree")
                                                            : localizeText(lang, "Never use worktrees", "不使用 worktree", "不使用 worktree")}
                                                    style={{
                                                        height: 22,
                                                        padding: "0 8px",
                                                        borderRadius: 4,
                                                        border: `1px solid ${codingWorktreeMode === mode ? codingBannerChrome.accent : codingBannerChrome.chipIdleBorder}`,
                                                        background: codingWorktreeMode === mode ? codingBannerChrome.chipActiveBg : codingBannerChrome.chipIdleBg,
                                                        color: codingWorktreeMode === mode ? codingBannerChrome.accentStrong : codingBannerChrome.muted,
                                                        fontSize: 11,
                                                        fontWeight: codingWorktreeMode === mode ? 600 : 400,
                                                        cursor: "pointer",
                                                    }}
                                                >
                                                    {mode === "auto"
                                                        ? localizeText(lang, "Auto", "自动", "自動")
                                                        : mode === "always"
                                                            ? localizeText(lang, "Always", "总是", "總是")
                                                            : localizeText(lang, "Off", "关闭", "關閉")}
                                                </button>
                                            ))}
                                            {codingPendingApproval && (
                                                <span data-testid="coding-pending-approval" style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                                                    <span style={{ color: codingBannerChrome.accentStrong, fontWeight: 600 }}>
                                                        {localizeText(lang, "Plan awaiting approval", "计划待批准", "計畫待批准")}
                                                    </span>
                                                    <button
                                                        type="button"
                                                        data-testid="coding-plan-edit"
                                                        onClick={() => {
                                                            setCodingPendingPlanDraft(codingExecutionPlan || codingPendingPlanDraft);
                                                            setCodingPendingPlanEditing((v) => !v);
                                                        }}
                                                        title={localizeText(lang, "Edit pending plan steps before approve", "批准前编辑待批步骤", "批准前編輯待批步驟")}
                                                        style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingPendingPlanEditing ? codingBannerChrome.chipActiveBg : codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: "pointer" }}
                                                    >
                                                        {codingPendingPlanEditing
                                                            ? localizeText(lang, "Hide editor", "收起编辑", "收起編輯")
                                                            : localizeText(lang, "Edit plan", "编辑计划", "編輯計畫")}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        data-testid="coding-plan-approve"
                                                        onClick={() => { void handleCodingPlanGate("approve"); }}
                                                        disabled={!codingTaskReadyForIntents}
                                                        title={localizeText(lang, "Start the confirmed multi-step plan", "开始实施多步计划", "開始實施多步計畫")}
                                                        style={{ height: 22, padding: "0 8px", borderRadius: 4, border: "none", background: codingBannerChrome.btnPrimaryBg, color: codingBannerChrome.btnPrimaryFg, fontSize: 11, fontWeight: 600, cursor: codingTaskReadyForIntents ? "pointer" : "not-allowed", opacity: codingTaskReadyForIntents ? 1 : 0.55 }}
                                                    >
                                                        {localizeText(lang, "Start", "开始实施", "開始實施")}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        data-testid="coding-plan-skip"
                                                        onClick={() => { void handleCodingPlanGate("skip"); }}
                                                        disabled={!codingTaskReadyForIntents}
                                                        title={localizeText(lang, "Skip multi-step plan; run original request as one step", "跳过多步规划，按原请求单步执行", "跳過多步規劃，按原請求單步執行")}
                                                        style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: codingTaskReadyForIntents ? "pointer" : "not-allowed", opacity: codingTaskReadyForIntents ? 1 : 0.55 }}
                                                    >
                                                        {localizeText(lang, "Skip plan", "跳过规划", "跳過規劃")}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        data-testid="coding-plan-reject"
                                                        onClick={() => { void handleCodingPlanGate("reject"); }}
                                                        disabled={!codingTaskReadyForIntents}
                                                        title={localizeText(lang, "Reject and clear pending plan", "拒绝并清除待批计划", "拒絕並清除待批計畫")}
                                                        style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid #dc262655`, background: codingBannerChrome.chipIdleBg, color: "#dc2626", fontSize: 11, cursor: codingTaskReadyForIntents ? "pointer" : "not-allowed", opacity: codingTaskReadyForIntents ? 1 : 0.55 }}
                                                    >
                                                        {localizeText(lang, "Reject", "拒绝", "拒絕")}
                                                    </button>
                                                </span>
                                            )}
                                        </div>
                                        {codingStepStatuses.length > 0 && (
                                            <div data-testid="coding-step-statuses" style={{ marginTop: 4, padding: "6px 8px", borderRadius: 6, border: `1px solid ${codingBannerChrome.border}`, background: codingBannerChrome.insetBg, fontSize: 11, color: codingBannerChrome.muted, maxHeight: 100, overflow: "auto" }}>
                                                <div style={{ fontWeight: 600, marginBottom: 4, color: codingBannerChrome.accentStrong }}>
                                                    {localizeText(lang, "Steps", "执行步骤", "執行步驟")}
                                                </div>
                                                {codingStepStatuses.map((st) => {
                                                    // Claude Code / Codex-style checklist marks.
                                                    const icon = st.status === "passed"
                                                        ? "☑"
                                                        : (st.status === "failed" || st.status === "verify_failed")
                                                            ? "✗"
                                                            : st.status === "running"
                                                                ? "…"
                                                                : st.status === "skipped"
                                                                    ? "–"
                                                                    : "☐";
                                                    const color = codingStepStatusColor(st.status, !!t.isDark, codingBannerChrome);
                                                    return (
                                                        <div key={st.index} data-testid={`coding-step-${st.index}`} data-status={st.status} style={{ display: "flex", gap: 6, alignItems: "baseline", marginBottom: 2, color }}>
                                                            <span style={{ width: 14, flexShrink: 0 }}>{icon}</span>
                                                            <span style={{ fontWeight: 600 }}>T{st.index}</span>
                                                            <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{st.title || st.status}</span>
                                                            <span style={{ opacity: 0.8, flexShrink: 0 }}>{st.status}</span>
                                                        </div>
                                                    );
                                                })}
                                            </div>
                                        )}
                                        {(codingSessionCost || codingRouteInfo || codingConflictCount > 0 || codingCheckpointLabel || codingBackgroundVerify) ? (
                                            <div data-testid="coding-session-cost" style={{ marginTop: 4, fontSize: 11, color: codingBannerChrome.muted, display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center" }}>
                                                {codingSessionCost ? (
                                                    <span>{localizeText(lang, "Session usage", "会话用量", "工作階段用量")}: {codingSessionCost}</span>
                                                ) : null}
                                                {codingRouteInfo ? (
                                                    <span data-testid="coding-route-info">{localizeText(lang, "Route", "路由", "路由")}: {codingRouteInfo}</span>
                                                ) : null}
                                                {codingCheckpointLabel ? (
                                                    <span
                                                        data-testid="coding-checkpoint-label"
                                                        title={codingCheckpointFiles.length > 0
                                                            ? codingCheckpointFiles.slice(0, 12).join("\n") + (codingCheckpointFiles.length > 12 ? `\n…(+${codingCheckpointFiles.length - 12})` : "")
                                                            : undefined}
                                                    >
                                                        {localizeText(lang, "Checkpoint", "检查点", "檢查點")}: {codingCheckpointLabel}
                                                        {codingCheckpointFiles.length > 0 ? ` · ${codingCheckpointFiles.length} files` : ""}
                                                        {codingCheckpointSnapshots > 0 ? ` · ${codingCheckpointSnapshots} snaps` : ""}
                                                    </span>
                                                ) : null}
                                                {codingBackgroundVerify ? (
                                                    <span data-testid="coding-bg-verify" title={codingBackgroundVerify} style={{ maxWidth: 280, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                                                        {localizeText(lang, "BG verify", "后台验证", "背景驗證")}: {codingBackgroundVerify.length > 48 ? `${codingBackgroundVerify.slice(0, 48)}…` : codingBackgroundVerify}
                                                    </span>
                                                ) : null}
                                                {codingConflictCount > 0 ? (
                                                    <button
                                                        type="button"
                                                        data-testid="coding-conflicts"
                                                        onClick={() => {
                                                            // Open dedicated right-hand conflict side panel (three-way lives there).
                                                            setCodingConflictOpen(true);
                                                            if (codingConflicts[0]?.id) {
                                                                void openCodingConflict(codingConflicts[0].id);
                                                            }
                                                        }}
                                                        style={{ border: "none", background: "transparent", color: "#dc2626", fontWeight: 600, fontSize: 11, cursor: "pointer", padding: 0 }}
                                                        title={localizeText(lang, "Open conflict side panel", "打开冲突侧栏", "開啟衝突側欄")}
                                                    >
                                                        {codingConflictOpen
                                                            ? localizeText(lang, `Conflicts: ${codingConflictCount} (side panel)`, `冲突: ${codingConflictCount}（侧栏）`, `衝突: ${codingConflictCount}（側欄）`)
                                                            : localizeText(lang, `Conflicts: ${codingConflictCount} →`, `冲突: ${codingConflictCount} →`, `衝突: ${codingConflictCount} →`)}
                                                    </button>
                                                ) : null}
                                            </div>
                                        ) : null}
                                        <div data-testid="coding-checkpoint-bg" style={{ marginTop: 4, display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", fontSize: 11 }}>
                                            <button
                                                type="button"
                                                data-testid="coding-checkpoint-save"
                                                disabled={codingCheckpointBusy}
                                                onClick={() => { void handleSaveCodingCheckpoint(); }}
                                                style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: codingCheckpointBusy ? "wait" : "pointer" }}
                                            >
                                                {localizeText(lang, "Save checkpoint", "保存检查点", "儲存檢查點")}
                                            </button>
                                            <button
                                                type="button"
                                                data-testid="coding-checkpoint-restore"
                                                disabled={codingCheckpointBusy || !codingCheckpointLabel}
                                                onClick={() => { void handleRestoreCodingCheckpoint(false); }}
                                                title={codingCheckpointLabel
                                                    ? localizeText(lang, `Restore plan: ${codingCheckpointLabel}`, `恢复计划: ${codingCheckpointLabel}`, `還原計畫: ${codingCheckpointLabel}`)
                                                    : localizeText(lang, "No checkpoint yet", "尚无检查点", "尚無檢查點")}
                                                style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: (codingCheckpointBusy || !codingCheckpointLabel) ? "not-allowed" : "pointer", opacity: codingCheckpointLabel ? 1 : 0.5 }}
                                            >
                                                {localizeText(lang, "Restore plan", "恢复计划", "還原計畫")}
                                            </button>
                                            <button
                                                type="button"
                                                data-testid="coding-checkpoint-restore-files"
                                                disabled={codingCheckpointBusy || !codingCheckpointLabel || codingCheckpointSnapshots <= 0}
                                                onClick={() => { void handleRestoreCodingCheckpoint(true); }}
                                                title={codingCheckpointSnapshots > 0
                                                    ? localizeText(lang, `Restore plan + ${codingCheckpointSnapshots} file snapshots`, `恢复计划 + ${codingCheckpointSnapshots} 个文件快照`, `還原計畫 + ${codingCheckpointSnapshots} 個檔案快照`)
                                                    : localizeText(lang, "No file snapshots in checkpoint", "检查点无文件内容快照", "檢查點無檔案內容快照")}
                                                style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: (codingCheckpointBusy || !codingCheckpointLabel || codingCheckpointSnapshots <= 0) ? "not-allowed" : "pointer", opacity: codingCheckpointSnapshots > 0 ? 1 : 0.5 }}
                                            >
                                                {localizeText(lang, "Restore files", "恢复文件", "還原檔案")}
                                            </button>
                                            {codingCheckpointHistory.length > 1 ? (
                                                <select
                                                    data-testid="coding-checkpoint-history"
                                                    disabled={codingCheckpointBusy}
                                                    defaultValue=""
                                                    onChange={(e) => {
                                                        const raw = e.target.value;
                                                        e.target.value = "";
                                                        if (!raw) return;
                                                        // Values: "plan:<label>" | "files:<label>"
                                                        const colon = raw.indexOf(":");
                                                        const mode = colon > 0 ? raw.slice(0, colon) : "plan";
                                                        const lab = colon > 0 ? raw.slice(colon + 1) : raw;
                                                        if (!lab) return;
                                                        void handleRestoreCodingCheckpoint(mode === "files", lab);
                                                    }}
                                                    title={localizeText(lang, "Restore plan or files from a checkpoint", "从检查点恢复计划或文件", "從檢查點還原計畫或檔案")}
                                                    style={{
                                                        height: 22,
                                                        maxWidth: 200,
                                                        fontSize: 11,
                                                        borderRadius: 4,
                                                        border: `1px solid ${codingBannerChrome.chipIdleBorder}`,
                                                        background: codingBannerChrome.chipIdleBg,
                                                        color: codingBannerChrome.muted,
                                                        cursor: codingCheckpointBusy ? "wait" : "pointer",
                                                    }}
                                                >
                                                    <option value="">
                                                        {localizeText(lang, `History (${codingCheckpointHistory.length})`, `历史 (${codingCheckpointHistory.length})`, `歷史 (${codingCheckpointHistory.length})`)}
                                                    </option>
                                                    <optgroup label={localizeText(lang, "Restore plan", "恢复计划", "還原計畫")}>
                                                        {codingCheckpointHistory.map((e) => (
                                                            <option key={`plan-${e.label}`} value={`plan:${e.label}`} title={e.summary || e.label}>
                                                                {e.current ? "★ " : ""}{e.label}
                                                            </option>
                                                        ))}
                                                    </optgroup>
                                                    <optgroup label={localizeText(lang, "Restore plan + files", "恢复计划+文件", "還原計畫+檔案")}>
                                                        {codingCheckpointHistory.map((e) => (
                                                            <option
                                                                key={`files-${e.label}`}
                                                                value={`files:${e.label}`}
                                                                title={e.summary || e.label}
                                                                disabled={!e.snapshot_count}
                                                            >
                                                                {e.current ? "★ " : ""}{e.label}{e.snapshot_count ? ` · ${e.snapshot_count}s` : " · no snaps"}
                                                            </option>
                                                        ))}
                                                    </optgroup>
                                                </select>
                                            ) : null}
                                            <button
                                                type="button"
                                                data-testid="coding-bg-verify-run"
                                                disabled={codingBgVerifyBusy}
                                                onClick={() => { void handleRunCodingBgVerify(); }}
                                                style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: codingBgVerifyBusy ? "wait" : "pointer" }}
                                            >
                                                {codingBgVerifyBusy
                                                    ? localizeText(lang, "Verifying…", "验证中…", "驗證中…")
                                                    : localizeText(lang, "BG verify", "后台验证", "背景驗證")}
                                            </button>
                                            <button
                                                type="button"
                                                data-testid="coding-checkpoint-prune"
                                                disabled={codingCheckpointBusy}
                                                onClick={() => { void handlePruneCheckpoints(); }}
                                                title={localizeText(lang, "Prune old checkpoint sidecar files", "清理非当前检查点侧车文件", "清理非目前檢查點側車檔案")}
                                                style={{ height: 22, padding: "0 8px", borderRadius: 4, border: `1px solid ${codingBannerChrome.chipIdleBorder}`, background: codingBannerChrome.chipIdleBg, color: codingBannerChrome.muted, fontSize: 11, cursor: codingCheckpointBusy ? "wait" : "pointer" }}
                                            >
                                                {localizeText(lang, "Prune snaps", "清理快照", "清理快照")}
                                            </button>
                                            {codingSidecarStats && (Number(codingSidecarStats.max_bytes) > 0) ? (
                                                <span
                                                    data-testid="coding-sidecar-stats"
                                                    title={localizeText(lang, "Checkpoint sidecar disk usage", "检查点侧车磁盘用量", "檢查點側車磁碟用量")}
                                                    style={{
                                                        fontSize: 11,
                                                        color: (Number(codingSidecarStats.usage_ratio) >= 0.85) ? "#dc2626" : codingBannerChrome.muted,
                                                        fontWeight: (Number(codingSidecarStats.usage_ratio) >= 0.85) ? 600 : 400,
                                                    }}
                                                >
                                                    {`sidecar ${(Number(codingSidecarStats.total_bytes) / (1024 * 1024)).toFixed(1)}/${(Number(codingSidecarStats.max_bytes) / (1024 * 1024)).toFixed(0)}MB`}
                                                    {Number(codingSidecarStats.dir_count) > 0 ? ` · ${codingSidecarStats.dir_count}` : ""}
                                                </span>
                                            ) : null}
                                            {codingHooksInfo?.active ? (
                                                <span
                                                    data-testid="coding-hooks-info"
                                                    title={[
                                                        localizeText(lang, "Project hooks from .maclaw/hooks.json", "项目钩子来自 .maclaw/hooks.json", "專案鉤子來自 .maclaw/hooks.json"),
                                                        codingHooksInfo.phases.join(", "),
                                                        codingHooksInfo.failOnError ? "fail_on_error" : "",
                                                    ].filter(Boolean).join(" · ")}
                                                    style={{
                                                        fontSize: 11,
                                                        color: codingHooksInfo.failOnError ? "#dc2626" : codingBannerChrome.accentStrong,
                                                        fontWeight: codingHooksInfo.failOnError ? 600 : 400,
                                                    }}
                                                >
                                                    {`hooks ${codingHooksInfo.count}`}
                                                    {codingHooksInfo.phases.length > 0
                                                        ? ` · ${codingHooksInfo.phases.slice(0, 4).join(",")}${codingHooksInfo.phases.length > 4 ? "…" : ""}`
                                                        : ""}
                                                    {codingHooksInfo.failOnError ? " · fail" : ""}
                                                </span>
                                            ) : null}
                                        </div>
                                        <div data-testid="coding-route-pref" style={{ marginTop: 4, display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", fontSize: 11 }}>
                                            <span style={{ fontWeight: 600, color: codingBannerChrome.muted }}>
                                                {localizeText(lang, "Model", "选模", "選模")}
                                            </span>
                                            {(["auto", "primary", "reasoning", "vision"] as const).map((pref) => {
                                                const cap = codingRouteCaps.find((c) => c.pref === pref);
                                                const isFallback = !!cap?.source && (cap.source === "fallback" || cap.source === "primary") && (pref === "reasoning" || pref === "vision") && !!cap?.note;
                                                const title = [
                                                    cap?.model ? `model: ${cap.model}` : "",
                                                    cap?.source ? `source: ${cap.source}` : "",
                                                    cap?.note || "",
                                                    (pref === "reasoning" || pref === "vision")
                                                        ? localizeText(lang, "Configure in Settings → LLM Cache → Model routes", "在 设置 → LLM 缓存 → 模型路由 配置", "在 設定 → LLM 快取 → 模型路由 配置")
                                                        : "",
                                                ].filter(Boolean).join(" · ") || pref;
                                                return (
                                                <button
                                                    key={pref}
                                                    type="button"
                                                    data-testid={`coding-route-pref-${pref}`}
                                                    title={title}
                                                    onClick={() => { void handleCodingRoutePrefChange(pref); }}
                                                    style={{
                                                        height: 22,
                                                        padding: "0 8px",
                                                        borderRadius: 4,
                                                        border: `1px solid ${codingRoutePref === pref ? codingBannerChrome.accent : codingBannerChrome.chipIdleBorder}`,
                                                        background: codingRoutePref === pref ? codingBannerChrome.chipActiveBg : codingBannerChrome.chipIdleBg,
                                                        color: codingRoutePref === pref ? codingBannerChrome.accentStrong : codingBannerChrome.muted,
                                                        fontSize: 11,
                                                        fontWeight: codingRoutePref === pref ? 600 : 400,
                                                        cursor: "pointer",
                                                        opacity: isFallback && codingRoutePref !== pref ? 0.72 : 1,
                                                    }}
                                                >
                                                    {pref}{cap?.model && pref !== "auto" ? ` · ${cap.model.length > 18 ? `${cap.model.slice(0, 16)}…` : cap.model}` : ""}
                                                </button>
                                                );
                                            })}
                                        </div>
                                        {/* Full conflict log + three-way live in the side panel; keep a mini strip only when side is closed. */}
                                        {codingConflictLog.length > 0 && !showCodingConflictPanel ? (
                                            <div data-testid="coding-conflict-log" style={{ marginTop: 4, fontSize: 10, color: t.textMuted || t.promptColor, maxHeight: 56, overflow: "auto", opacity: 0.9 }}>
                                                <div style={{ display: "flex", justifyContent: "space-between", gap: 8, marginBottom: 2, alignItems: "center" }}>
                                                    <span style={{ fontWeight: 600 }}>{localizeText(lang, "Conflict log", "冲突日志", "衝突日誌")}</span>
                                                    <span style={{ display: "flex", gap: 8 }}>
                                                        <button
                                                            type="button"
                                                            data-testid="coding-conflict-log-export"
                                                            onClick={() => { void handleExportConflictLog(); }}
                                                            title={localizeText(lang, "Export log into worktree notes", "导出日志到 worktree notes", "匯出日誌到 worktree notes")}
                                                            style={{ border: "none", background: "transparent", color: t.headingColor || t.btnColor || t.textMuted, fontSize: 10, cursor: "pointer", padding: 0 }}
                                                        >
                                                            {localizeText(lang, "Export", "导出", "匯出")}
                                                        </button>
                                                        <button
                                                            type="button"
                                                            data-testid="coding-conflict-log-clear"
                                                            onClick={() => {
                                                                const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
                                                                if (!projectPath) return;
                                                                void ClearCodingWorkbenchConflictLog(projectPath).then(() => setCodingConflictLog([])).catch(() => { /* ignore */ });
                                                            }}
                                                            style={{ border: "none", background: "transparent", color: t.textMuted, fontSize: 10, cursor: "pointer", padding: 0 }}
                                                        >
                                                            {localizeText(lang, "Clear", "清空", "清空")}
                                                        </button>
                                                    </span>
                                                </div>
                                                {codingConflictLog.slice().reverse().slice(0, 4).map((line, i) => (
                                                    <div key={`${i}-${line.slice(0, 24)}`} style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{line}</div>
                                                ))}
                                            </div>
                                        ) : null}
                                        {codingExecutionPlan || (codingPendingApproval && codingPendingPlanEditing) ? (
                                            <div data-testid="coding-execution-plan" style={{ marginTop: 4, padding: "6px 8px", borderRadius: 6, border: `1px solid ${codingPendingApproval ? "#dc262655" : (t.fieldBorder || "rgba(127,127,127,0.25)")}`, background: t.fieldBg || "transparent", fontSize: 11, color: t.textMuted || t.promptColor, lineHeight: 1.35 }}>
                                                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 4 }}>
                                                    <div style={{ fontWeight: 600, color: t.headingColor || t.btnColor || t.text }}>
                                                        {localizeText(lang, "Execution plan", "执行计划", "執行計畫")}
                                                        {codingPendingApproval ? ` · ${localizeText(lang, "pending", "待批准", "待批准")}` : ""}
                                                    </div>
                                                    {codingPendingApproval && codingPendingPlanEditing ? (
                                                        <button
                                                            type="button"
                                                            data-testid="coding-plan-edit-save"
                                                            disabled={codingPendingPlanSaving || !codingPendingPlanDraft.trim()}
                                                            onClick={() => { void handleSavePendingPlanEdit(); }}
                                                            style={primaryFilledButtonStyle(t, { height: 20, padding: "0 8px", borderRadius: 4, fontSize: 10, cursor: (codingPendingPlanSaving || !codingPendingPlanDraft.trim()) ? "not-allowed" : "pointer", opacity: codingPendingPlanDraft.trim() ? 1 : 0.5 })}
                                                        >
                                                            {codingPendingPlanSaving
                                                                ? localizeText(lang, "Saving…", "保存中…", "儲存中…")
                                                                : localizeText(lang, "Save plan", "保存计划", "儲存計畫")}
                                                        </button>
                                                    ) : null}
                                                </div>
                                                {codingPendingApproval && codingPendingPlanEditing ? (
                                                    <textarea
                                                        data-testid="coding-pending-plan-draft"
                                                        value={codingPendingPlanDraft}
                                                        onChange={(e) => setCodingPendingPlanDraft(e.target.value)}
                                                        rows={8}
                                                        spellCheck={false}
                                                        placeholder={localizeText(lang, "T1: …\nT2: …", "T1: …\nT2: …", "T1: …\nT2: …")}
                                                        style={{
                                                            width: "100%",
                                                            boxSizing: "border-box",
                                                            margin: 0,
                                                            fontSize: 11,
                                                            fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                                                            lineHeight: 1.35,
                                                            resize: "vertical",
                                                            minHeight: 100,
                                                            maxHeight: 220,
                                                            border: `1px solid ${t.fieldBorder || "rgba(127,127,127,0.3)"}`,
                                                            borderRadius: 4,
                                                            padding: 6,
                                                            background: "transparent",
                                                            color: t.text || t.promptColor,
                                                        }}
                                                    />
                                                ) : (
                                                    <div style={{ whiteSpace: "pre-wrap", maxHeight: 120, overflow: "auto" }}>{codingExecutionPlan}</div>
                                                )}
                                            </div>
                                        ) : null}
                                    </>
                                )}
                            </div>
                                </CodingControlSection>
                            </>
                        )}
                    </CodingWorkbenchControlPanel>
                )}
                {isRemoteCodingDevEnvironment && remoteReconnect.success && !remoteCodingNeedsReconnect && (
                    <div
                        data-testid="remote-coding-reconnect-success"
                        data-coding-float-ignore-outside=""
                        role="status"
                        style={{
                            position: "absolute",
                            top: 44,
                            right: 10,
                            // Above coding float root (zIndex 40) so dismiss stays clickable.
                            zIndex: 45,
                            maxWidth: "min(320px, calc(100% - 20px))",
                            padding: "8px 12px",
                            borderRadius: 8,
                            border: `1px solid ${t.titleBarBorder}`,
                            background: `color-mix(in srgb, #22c55e 12%, ${t.bg || "#fff"})`,
                            color: t.text,
                            fontSize: 12,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "space-between",
                            gap: 8,
                            boxShadow: "0 8px 20px rgba(15,23,42,0.12)",
                            pointerEvents: "auto",
                        }}
                    >
                        <span>{remoteReconnect.success}</span>
                        <button
                            type="button"
                            data-testid="remote-coding-reconnect-success-dismiss"
                            onClick={() => setRemoteReconnect(prev => ({ ...prev, success: "" }))}
                            style={{ border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", fontSize: 11 }}
                        >
                            {localizeText(lang, "Dismiss", "关闭", "關閉")}
                        </button>
                    </div>
                )}

                <AssistantWorkflowMaximizeSuggestion inline={!!inline} lang={lang} maximized={!!maximized} onDismiss={dismissMaximizeSuggestion} onToggleMaximize={onToggleMaximize} suggestMaximize={workflowState.suggestMaximize} theme={t} themeMode={themeMode} />
                <ProjectSearchPanel search={projectSearch} lang={lang} theme={t} inline={!!inline} active={panelActive} onProjectSwitch={handleProjectSearchSwitch} onCreateProjectTab={createProjectTabFromSearch} onCloseProjectTab={closeProjectTabByPath} onForkCurrentChat={handleForkCurrentChat} onTaskPrefsChanged={onTaskPrefsChanged} />
                {workflowStartingLabel && !hasConversation && !showThinkingState && !showProcessingState && (
                    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '12px', background: t.bg, color: t.textMuted }}>
                        <div style={{ opacity: 0.7, display: 'inline-flex' }}><IconRocket size={28} color="currentColor" /></div>
                        <div style={{ fontSize: '0.88rem', fontWeight: 600 }}>
                            {lang?.startsWith('en') ? `Starting workflow: ${workflowStartingLabel}` : `正在启动工作流：${workflowStartingLabel}`}
                        </div>
                        <div style={{ fontSize: '0.75rem', opacity: 0.7 }}>
                            {lang?.startsWith('en') ? 'Please wait...' : '请稍候...'}
                        </div>
                    </div>
                )}
                {showWelcomeView ? (
                    <div data-testid="ai-welcome-container" style={{ flex: 1, minHeight: 0, overflow: "auto", background: t.bg, boxSizing: "border-box" }}>
                        <AssistantWelcomeView
                            lang={lang}
                            theme={t}
                            themeMode={themeMode}
                            active={panelActive}
                            onPromptSelect={handleWelcomePromptSelect}
                            onPromptSend={handleWelcomePromptSend}
                            pinnedNews={pinnedNews}
                            composer={{
                                browseFile,
                                canSend,
                                cancelPending,
                                cancelSession,
                                clearSelectedFile,
                                composeAction,
                                exitHistoryBrowsing,
                                finishVoicePointer,
                                handleCancel,
                                handleClearInput,
                                handleDragOver,
                                handleDrop,
                                handlePaste,
                                handleSend,
                                handleVoiceClick,
                                handleVoicePointerDown,
                                handleVoicePointerLeave,
                                inputLocked,
                                inputRef,
                                inputValue,
                                isBusy: inputVisualBusy,
                                isSelectionCollapsedAtBoundary,
                                onComposeActionChange: handleComposeActionChange,
                                onFireSlashCommand: handleFireSlashCommand,
                                onInsertTemplate: handleInsertTemplate,
                                onPlusMenuAction: handlePlusMenuAction,
                                onPermissionModeChange: handlePermissionModeChange,
                                pendingAttachments,
                                permissionMode,
                                showWorkspacePermissionOption: isPureCodingEnvironment,
                                ready,
                                recallHistory,
                                rememberHistoryEdit,
                                removeSelectedFile,
                                resizeInput,
                                selectedFilePaths,
                                setPendingAttachments,
                                showBusySpinner,
                                submittedPrompts,
                                updateInputValue,
                                voiceInput,
                            }}
                        />
                    </div>
                ) : (
                <div ref={outputContainerRef} data-testid="ai-output-container" className="ai-chat-scrollbar" style={{ flex: 1, minHeight: 0, maxHeight: "none", padding: isPureCodingEnvironment ? "40px 10px 8px" : "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5, overflowY: "auto", overflowX: "hidden", scrollbarGutter: "stable", textAlign: "left", color: t.text, background: t.bg, fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace", whiteSpace: "normal", overflowWrap: "anywhere", wordBreak: "normal" }} onScroll={handleScroll}>
                    {isExpertTabActive && displayMessages.length === 0 ? (
                        // Expert tab empty state (e.g. after a conversation clear):
                        // expert name + intro instead of the generic welcome view.
                        <div data-testid="ai-expert-empty" style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 10, padding: "40px 20px", textAlign: "center", color: t.textMuted }}>
                            <div aria-hidden="true" style={{ fontSize: 36, lineHeight: 1 }}>{activeTab.expertIcon || DEFAULT_EXPERT_ICON}</div>
                            <div style={{ fontSize: "0.95rem", fontWeight: 600, color: t.text }}>{activeTab.title}</div>
                            {activeTab.expertDescription ? (
                                <div style={{ fontSize: "0.8rem", maxWidth: 420, lineHeight: 1.5 }}>{activeTab.expertDescription}</div>
                            ) : null}
                            <div style={{ fontSize: "0.78rem", opacity: 0.8, maxWidth: 420, lineHeight: 1.5 }}>
                                {expertWelcomeMessageText({ name: activeTab.title, description: activeTab.expertDescription || "" }, lang)}
                            </div>
                        </div>
                    ) : null}
                    {showPureCodingEmptyState ? pureCodingEmptyContent : null}
                    <AssistantConversationBody emptyContent={isPureCodingEnvironment ? null : undefined} initLabel={initLabel} lang={lang} messages={displayMessages} onOpenOnboarding={onOpenOnboarding} onboardingIncomplete={onboardingIncomplete} pinnedNews={pinnedNews} processingText={activeProcessingText} ready={ready} renderedOtherMessages={renderedOtherMessages} renderedProgressMessages={renderedProgressMessages} showProcessingState={showProcessingState} showThinkingState={showThinkingState} theme={t} thinkingText={thinkingText} />
                    <div ref={outputEndRef} />
                </div>
                )}
                {activeProjectPreparing && <div data-testid="project-tab-restore-progress" style={{ flexShrink: 0, padding: "7px 10px 8px", borderTop: `1px solid ${t.inputBarBorder}`, background: t.inputBarBg, color: t.textMuted, fontSize: 12 }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, marginBottom: 6 }}>
                        <span>{activeProjectPrepareMode === "new-agent"
                            ? (isRemoteCodingDevEnvironment
                                ? (isRemoteMaintenanceEnvironment
                                    ? localizeText(lang, "Preparing remote maintenance", "正在准备远程维护", "正在準備遠端維護")
                                    : localizeText(lang, "Creating remote coding environment", "正在创建远程编程环境", "正在建立遠端程式開發環境"))
                                : isCodingDevEnvironment
                                ? localizeText(lang, "Creating coding environment", "正在创建编程环境", "正在建立程式開發環境")
                                : (lang === "en" ? "Creating project session" : "正在创建项目会话"))
                            : (lang === "en" ? "Restoring task context" : "正在恢复任务上下文")}</span>
                        <span style={{ opacity: 0.82 }}>{lang === "en" ? "Input will wait" : "输入会先等待"}</span>
                    </div>
                    <div style={{ height: 3, overflow: "hidden", borderRadius: 999, background: `color-mix(in srgb, ${t.headingColor} 16%, transparent)` }}>
                        <div style={{ width: "38%", height: "100%", borderRadius: "inherit", background: t.headingColor, animation: "sidebar-task-restore-progress 0.9s ease-in-out infinite alternate" }} />
                    </div>
                </div>}
                {!showWelcomeView && skillRecordingCard && <InlineChatCard card={skillRecordingCard} onResolve={(cardId, action, values) => { const resolvedCardId = cardId; setSkillRecordingCard((prev: any) => prev ? { ...prev, resolved: true, resolvedAction: action, resolvedValues: values } : null); handleResolveSkillRecordingCard(action, values); setTimeout(() => setSkillRecordingCard((prev: any) => prev && prev.id === resolvedCardId ? null : prev), 2000); }} theme={{ cardBg: t.fieldBg, cardBorder: t.titleBarBorder, textColor: t.text, mutedColor: t.promptColor, accentColor: "#4f7f6f", inputBg: t.fieldBg, inputBorder: t.titleBarBorder, buttonBg: "#4f7f6f", buttonText: "#fff", dangerColor: "#dc2626" }} lang={lang} />}
                {!showWelcomeView && codingPreviewAllowed && (workflowAwaitingForm || workflowFormGeneratingDocument) && (
                    <WorkflowFormInlinePrompt
                        formActive={workflowFormActive}
                        generatingDocument={workflowFormGeneratingDocument}
                        lang={lang}
                        phaseName={workflowFormPhaseName}
                        theme={t}
                    />
                )}
                {!showWelcomeView && codingPreviewAllowed && workflowAwaitingReview && (
                    <WorkflowReviewInlinePrompt
                        lang={lang}
                        onAbort={() => panelExecuteAction('__wf_review__ abort')}
                        onConfirm={() => panelExecuteAction('__wf_review__ confirm')}
                        onRequestRevision={() => panelExecuteAction('__wf_review__ supplement_focus')}
                        onViewDocument={() => openDocPreview(workflowState.currentPhaseID)}
                        phaseName={workflowReviewPhaseName}
                        theme={t}
                    />
                )}
                {!showWelcomeView && welcomeTemplateOffer && (
                    <WelcomeTemplateSaveOfferBanner
                        lang={lang}
                        theme={t}
                        title={welcomeTemplateOffer.title}
                        onSave={handleWelcomeTemplateOfferSave}
                        onDismiss={handleWelcomeTemplateOfferDismiss}
                    />
                )}
                {!showWelcomeView && <ComputerUseReadinessBanner lang={lang} theme={t} />}
                {!showWelcomeView && <ComputerUseQuickBar lang={lang} theme={t} themeMode={themeMode} />}
                {!showWelcomeView && <AssistantInputStack active={panelActive} browseFile={browseFile} canSend={canSend} cancelPending={cancelPending} cancelSession={cancelSession} clearSelectedFile={clearSelectedFile} composeAction={composeAction} editingEntryId={editingEntryId} exitHistoryBrowsing={exitHistoryBrowsing} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleCancelEdit={handleCancelEdit} handleClearInput={handleClearInput} handleDragOver={handleDragOver} handleDrop={handleDrop} handleEditEntry={handleEditEntry} handlePaste={handlePaste} handleSaveEdit={handleSaveEdit} handleFireEntry={handleFireEntry} handleSend={handleSend} isEntryInFlight={isQueueEntryInFlight} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputAreaHeight={inputAreaHeight} inputLocked={inputLocked} hardLockInput={recordingActive} inputRef={inputRef} inputValue={inputValue} inline={false} flushBottom isBusy={inputVisualBusy} isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary} lang={lang} onComposeActionChange={handleComposeActionChange} onFireSlashCommand={handleFireSlashCommand} onInsertTemplate={handleInsertTemplate} onPlusMenuAction={handlePlusMenuAction} onPermissionModeChange={handlePermissionModeChange} pendingAttachments={pendingAttachments} permissionMode={permissionMode} showWorkspacePermissionOption={isPureCodingEnvironment} placeholderText={placeholderText} queue={queue} ready={ready} recallHistory={recallHistory} rememberHistoryEdit={rememberHistoryEdit} removeEntry={handleDeleteEntry} removeSelectedFile={removeSelectedFile} reorderEntry={handleReorderEntry} resizeInput={resizeInput} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} showBusySpinner={showBusySpinner} startInputResize={startInputResize} submittedPrompts={submittedPrompts} theme={t} themeMode={themeMode} updateInputValue={updateInputValue} voiceInput={voiceInput} />}
            </div>
            )}
            <AssistantActiveTabContent activeTab={activeTab} tabs={tabState.tabs} isLocalTabActive={isLocalTabActive} isProjectTabActive={isProjectTabActive} lang={lang} theme={t} getTabState={getTabState} saveTabState={saveTabState} onAddParticipantToTab={addParticipantToTab} />
            {panelActive && renameGroupTargetTab && (
                <AIAssistantRenameGroupDialog
                    error={renameGroupError}
                    lang={lang}
                    onClose={closeRenameGroupDialog}
                    onSubmit={submitRenameGroupDialog}
                    onValueChange={value => { setRenameGroupValue(value); if (renameGroupError) setRenameGroupError(""); }}
                    saving={renameGroupSaving}
                    theme={t}
                    value={renameGroupValue}
                />
            )}
            {panelActive && participantInviteTargetTab && <TabParticipantInviteDialog key={participantInviteTargetTab.id} tab={participantInviteTargetTab} lang={lang} theme={t} onClose={() => setParticipantInviteTargetTabId(null)} onAddParticipantToTab={addParticipantToTab} />}
            </div>
            {(showCodingConflictPanel || showWorkflowPreview || showCodePreview || showAgentView) ? (
                <Suspense fallback={null}>
                    <AssistantPreviewPane
                        agentView={agentView}
                        codePreviewState={codePreviewState}
                        closeCodePreview={closeCodePreview}
                        closeCodeFile={closeCodeFile}
                        closeOtherCodeFiles={closeOtherCodeFiles}
                        closeCodeFilesToTheRight={closeCodeFilesToTheRight}
                        closeAllCodeFiles={closeAllCodeFiles}
                        moveCodeFile={moveCodeFile}
                        toggleCodeFilePinned={toggleCodeFilePinned}
                        closeDocPreview={closeDocPreview}
                        dismissAgentView={dismissAgentView}
                        lang={lang}
                        selectCodeFile={selectCodeFile}
                        projectPath={isPureCodingEnvironment ? activeTab.projectPath : undefined}
                        workspaceRefreshToken={isRemoteCodingDevEnvironment ? remoteWorkspaceRefreshToken : undefined}
                        openWorkspaceFile={openWorkspaceFile}
                        submitAgentView={panelSubmitAgentView}
                        showCodePreview={showCodePreview}
                        showAgentView={showAgentView}
                        showWorkflowPreview={showWorkflowPreview}
                        showConflict={showCodingConflictPanel}
                        conflictCount={codingConflictCount}
                        onCloseConflict={closeCodingConflictSidePanel}
                        conflictContent={showCodingConflictPanel ? (
                            <CodingConflictSidePanel
                                lang={lang}
                                theme={t}
                                embedded
                                busy={codingConflictBusy}
                                progressTotal={codingConflictPeak}
                                conflicts={codingConflicts}
                                activeId={codingConflictActiveId}
                                diffs={codingConflictDiffs}
                                selected={codingConflictSelected}
                                focusFile={codingConflictFocusFile}
                                preview={codingConflictPreview}
                                previewSide={codingConflictPreviewSide}
                                editDraft={codingConflictEditDraft}
                                triple={codingConflictTriple}
                                conflictLog={codingConflictLog}
                                onClose={closeCodingConflictSidePanel}
                                onOpenConflict={(id) => { void openCodingConflict(id); }}
                                onDiscardAll={() => { void handleDiscardAllConflicts(); }}
                                onDiscard={(id) => { void handleDiscardConflict(id); }}
                                onResolveBatch={(id, action) => { void handleResolveConflictBatch(id, action); }}
                                onToggleFile={toggleCodingConflictFile}
                                onSelectAll={(paths) => {
                                    setCodingConflictSelected(paths);
                                    codingConflictSelectedRef.current = paths;
                                    persistCodingConflictUIState(codingConflictActiveId, paths, codingConflictFocusFileRef.current);
                                }}
                                onClearSelection={() => {
                                    setCodingConflictSelected([]);
                                    codingConflictSelectedRef.current = [];
                                    persistCodingConflictUIState(codingConflictActiveId, [], codingConflictFocusFileRef.current);
                                }}
                                onResolveSelected={(action) => { void handleResolveSelectedConflictFiles(action); }}
                                onAdoptFile={(id, path) => { void handleAdoptConflict(id, path); }}
                                onKeepMainFile={(id, path) => { void handleKeepMainConflictFile(id, path); }}
                                onAdoptBaseFile={(id, path) => { void handleAdoptBaseConflictFile(id, path); }}
                                onOpenFile={(path, side) => { void handleOpenConflictFile(path, side); }}
                                onLoadPreview={(path, side) => { void loadCodingConflictPreview(path, side); }}
                                onApplyPreviewSide={() => { void handleApplyPreviewSide(); }}
                                onWriteEdit={() => { void handleWriteConflictEdit(); }}
                                onEditDraftChange={setCodingConflictEditDraft}
                                onExportLog={() => { void handleExportConflictLog(); }}
                                onClearLog={() => {
                                    const projectPath = activeTab?.type === "project" ? (activeTab.projectPath || "") : "";
                                    if (!projectPath) return;
                                    void ClearCodingWorkbenchConflictLog(projectPath).then(() => setCodingConflictLog([])).catch(() => { /* ignore */ });
                                }}
                                syncTripleScroll={syncCodingConflictTripleScroll}
                                tripleScrollRefs={codingConflictTripleScrollRefs}
                            />
                        ) : null}
                        splitRatio={splitRatio}
                        startPreviewResize={startPreviewResize}
                        onToggleMaximize={onToggleMaximize}
                        theme={t}
                        workflowState={workflowState}
                    />
                </Suspense>
            ) : null}
            <ComputerUseOperatorPanel lang={lang} />
            </div>
            {/* Full-bleed footer under content-row (chat|preview). Always mounted so
                welcome/guide and VE/group tabs keep the same chrome as normal chat. */}
            <AssistantQuickSettingsBar active={panelActive} lang={lang} theme={t} themeMode={themeMode} onToggleTheme={handleQuickThemeToggle} workflowEnabled={workflowEnabled} onToggleWorkflow={handleToggleWorkflow} ttsEnabled={ttsEnabled} ttsPlaying={ttsPlaying} onToggleTts={handleQuickTtsToggle} availableProviders={availableProviders} currentModel={currentModel} modelOptions={modelOptions} modelsLoading={modelsLoading} onSwitchProvider={onSwitchProvider} onSwitchModel={onSwitchModel} onOpenModelMenu={onOpenModelMenu} onLanguageChange={onLanguageChange} statusSlot={statusSlot} />
            </div>
        </div>
    );
}

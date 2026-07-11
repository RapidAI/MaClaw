import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti, setActiveSessionKey, getActiveSessionKey, forgetAIAssistantSessionRounds, buildGuideReferenceAcceptedNotice, buildGuideReferenceRejectedNotice } from "./useAIAssistant";
import { useVoiceInput, type VoiceInputSource } from "./useVoiceInput";
import { normalizeASRText, shouldDispatchASRText } from "./asrTextUtils";
import { cloneWorkflowUIState, useWorkflowState, type WorkflowUIState } from "./useWorkflowState";
import { cloneCodePreviewState, useCodePreviewState, type CodePreviewUIState } from "./useCodePreviewState";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { renderMessage } from "./aiAssistantMarkdown";
import { createIncrementalRenderState, renderContentIncremental, type IncrementalRenderState } from "./IncrementalMarkdownRenderer";
import { lightTheme, maximizedInlineStyle, overlayStyle, overlayTheme, type Theme } from "./aiAssistantPanelTheme";
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
import { applyComposeActionToText, btwQueryFromText, getComposeActionPlaceholder, isBtwCommandText } from "./composeAction";
import { InlineChatCard } from "./InlineChatCard";
import { AssistantWelcomeView } from "./AssistantWelcomeView";
import { AssistantWorkflowMaximizeSuggestion } from "./AssistantWorkflowMaximizeSuggestion";
import { useAssistantThemeMode } from "./useAssistantThemeMode";
import { AssistantPreviewPane } from "./AssistantPreviewPane";
import { activeCodingAgentProgress, codingAgentCompactText, latestCodingAgentTurnSnapshot } from "./CodingAgentProgressStatus";
import { findLatestToolProgressText, formatToolProgressStatus, isToolProgressMessage } from "./aiAssistantProgressUtils";
import { AITabBar } from "./AITabBar";
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
import { compactCodingAgentProgressMessages } from "./compactCodingAgentProgressMessages";
import { TabParticipantInviteDialog } from "./TabParticipantInviteDialog";
import { AIAssistantRenameGroupDialog } from "./AIAssistantRenameGroupDialog";
import { WorkflowFormInlinePrompt, WorkflowReviewInlinePrompt } from "./WorkflowInlinePrompts";
import { buildProjectTabRecentMessages, chatHistoriesEquivalent, logAIPanelDiagnostic, messageBelongsToSession, messageBelongsToSessionOrLegacy, messageIsLocalSession, projectPathFromSessionKey, projectSessionKey } from "./aiAssistantPanelSessionUtils";
import { CancelAIAssistantSessionForSession, GetConversationBranchPoints, GroupDiscussionRenameConsultation, LoadConfig, PatchConfigFields, RefreshWorkflowV2StateForTab } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { EVENT_PROJECT_TASK_CLOSED } from "../../constants/events";
import { getWailsAppModule } from "../../utils/wailsAppModule";
import { useDialog } from "../CustomDialog";
export { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";

const LOCAL_HIGH_RISK_APPROVAL_KIND = "local_high_risk_bash";
const REMOTE_HIGH_RISK_APPROVAL_KIND = "remote_high_risk_bash";

type ConversationBranchPointLike = {
    index?: number;
    entry_id?: string;
    role?: string;
    preview?: string;
    branches?: number;
    labels?: string[];
};

export function canShowAssistantCodingPreviewForTab(tab: Pick<AITab, "type"> | null | undefined): boolean { return tab?.type === "local" || tab?.type === "project"; }

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
    const { onClose, lang, chatFontSize = 14, themeMode: controlledThemeMode, darkSchemeId, lightSchemeId, onThemeModeChange, audioInputDeviceId, audioOutputDeviceId, petVoiceStartSeq = 0, petFocusInputSeq = 0, pendingVEOpen, onPendingVEOpenHandled, pendingHistoryDiscussionOpen, onPendingHistoryDiscussionOpenHandled, appUpdateAvailable, onOpenAppUpdate, onDismissAppUpdate } = props;
    const state = props.state || props;
    const actions = props.actions || props;
    const panelWindow = props.window || props;
    const { messages, progressMessages = [], sending, sendingSessionKey: rawSendingSessionKey, busySessionKeys: rawBusySessionKeys, streaming, streamingSessionKey: rawStreamingSessionKey, streamingSessionKeys: rawStreamingSessionKeys, visualBusy, ready, initStatus, selectedFilePath: selectedFilePathFromState = "", submittedPrompts = [], draftInputValue = "", trialReflectEnabled = false, scrollToTopSeq, onboardingIncomplete, showTraceEntry = false, agentView = null } = state;
    const { browseFile, clearSelectedFile, removeSelectedFile, sendMessage, sendBtwMessage, injectSupplementary, guideLaunchReference, clearHistory, recordSubmittedPrompt, setDraftInputValue, executeAction, refreshNews, onOpenOnboarding, cancelSession, onOpenTutorial, onTaskPrefsChanged, submitAgentView, dismissAgentView } = actions;
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
    const [workflowStartingLabel, setWorkflowStartingLabel] = useState<string | null>(null);
    const [skillRecordingTabId, setSkillRecordingTabId] = useState<string | null>(null);
    const [skillRecordingCount, setSkillRecordingCount] = useState(0);
    const [skillRecordingCard, setSkillRecordingCard] = useState<any>(null);
    const { showConfirm } = useDialog();
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
    // Workflow toggle: load initial state from config, sync on config-changed event
    useEffect(() => {
        LoadConfig().then((cfg) => {
            setWorkflowEnabled(cfg?.workflow_enabled === true);
            setPermissionMode(cfg?.subagent_full_access === true ? "full" : "request");
        }).catch(() => { /* ignore */ });
        const off = EventsOn("config-changed", (cfg: any) => {
            if (cfg && typeof cfg.workflow_enabled === "boolean") {
                setWorkflowEnabled(cfg.workflow_enabled);
            } else if (cfg && cfg.workflow_enabled === undefined) {
                setWorkflowEnabled(false);
            }
            if (cfg && typeof cfg.subagent_full_access === "boolean") {
                setPermissionMode(cfg.subagent_full_access ? "full" : "request");
            }
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    const handlePermissionModeChange = useCallback((mode: AssistantPermissionMode) => {
        const next = mode === "full" ? "full" : "request";
        const previous = permissionMode;
        setPermissionMode(next);
        void PatchConfigFields({ subagent_full_access: next === "full" }).then((saved) => {
            setPermissionMode(saved?.subagent_full_access === true ? "full" : "request");
        }).catch(() => {
            setPermissionMode(previous);
        });
    }, [permissionMode]);
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
    const [scopeApprovalPending, setScopeApprovalPending] = useState<{ id: string; tool: string; path: string; projectPath: string; directory: string; timeoutSeconds: number; kind: string; message: string; autoAllow: boolean } | null>(null);
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
                            ? `🎬 Recording complete! ${data.count} operations captured.`
                            : `🎬 录制完成！共记录 ${data.count} 个操作步骤。`,
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
                            ? "⚠️ Recording stopped — no operations captured."
                            : "⚠️ 录制已停止 — 没有记录到可用操作。",
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
                        title: lang === "en" ? "🔴 Skill Recording Started" : "🔴 Skill 录制已开始",
                        description: lang === "en"
                            ? "All commands, file writes, and edits will be recorded. Click REC again to stop.\n\n💡 Just tell the AI what to do as usual — recording runs silently."
                            : "所有命令执行、文件写入、文件编辑将被记录。再次点击录制按钮停止。\n\n💡 像平时一样让 AI 工作即可，录制在后台静默进行。",
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
    const { tabState, activeTab, activateTab, createVETab, createGroupTab, createProjectTab, closeTab, clearTabConversation, saveTabState, getTabState, getLastActiveAt, getTabs, hasProjectTab, upgradeVETabToGroup, renameGroupTab, tabLimitError, clearTabLimitError } = useAITabManager();
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
                CancelAIAssistantSessionForSession(`desktop-user:${activeTab.projectPath}`).catch(() => {});
            }
            return;
        }
        if (activeTab.type === "ve" || activeTab.type === "group") {
            clearTabConversation(activeTab.id);
            return;
        }
        // Local tab: full reset to show welcome/guide page.
        setQueueInteractionStarted(false);
        setQueueEditDraftActive(false);
        setEditingEntryId(null);
        await clearHistory();
    }, [activeTab.id, activeTab.type, activeTab.projectPath, clearHistory, clearTabConversation]);
    const isLocalTabActive = activeTab.id === "local";
    const isProjectTabActive = activeTab.type === "project";
    const showChatUI = isLocalTabActive || isProjectTabActive;
    const activeSessionKey = isProjectTabActive && activeTab.projectPath ? `desktop-user:${activeTab.projectPath}` : 'desktop-user';
    const { handlePaste, handleDragOver, handleDrop, pendingAttachments, setPendingAttachments } = usePastedImageAttachments(activeSessionKey, { disabled: !ready || cancelPending });
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
    const deferredProjectInitialSendsRef = useRef<Map<string, string[]>>(new Map());
    const projectPrepareTimersRef = useRef<Map<string, number>>(new Map());
    const sendMessageForTabRef = useRef<((text: string, options?: Record<string, unknown>) => Promise<boolean>) | null>(null);
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
    useEffect(() => {
        const prevTabId = prevActiveTabIdRef.current;
        const currentTabId = activeTab.id;
        if (prevTabId === currentTabId) return;
        const multipleTabsExist = tabState.tabs.length > 1;
        if (multipleTabsExist) {
            const currentTabCanOwnPreview = canShowAssistantCodingPreviewForTab(activeTab);
            const currentPreviewMode: "workflow" | "code" = codePreviewState.active ? "code" : "workflow";
            const ownerTabId = previewOwnerTabRef.current;
            const ownerTab = tabState.tabs.find(t => t.id === ownerTabId);

            if (canShowAssistantCodingPreviewForTab(ownerTab) && ownerTabId !== currentTabId) {
                previewStateMapRef.current.set(ownerTabId, {
                    workflow: getWorkflowSnapshot(),
                    code: cloneCodePreviewState(codePreviewState),
                    previewMode: currentPreviewMode,
                });
            }

            if (currentTabCanOwnPreview && ownerTabId !== currentTabId) {
                const savedState = previewStateMapRef.current.get(currentTabId);
                if (savedState) {
                    restoreWorkflowState(savedState.workflow);
                    restoreCodePreviewState(savedState.code);
                } else {
                    resetWorkflowState();
                    resetCodePreviewState();
                }
                previewOwnerTabRef.current = currentTabId;
                // Re-emit full workflow state from backend for the new active tab.
                // Background agent loops emit events that were rejected by the inactive
                // tab's event filter — this refresh bridges that gap.
                if (activeTab.type === "project" && activeTab.projectPath) {
                    RefreshWorkflowV2StateForTab(activeTab.projectPath, activeTab.id).catch(() => {});
                }
            }
        }

        const prevTab = tabState.tabs.find(t => t.id === prevTabId);
        if (prevTab && prevTab.type === "project") {
            const scrollTop = outputContainerRef.current?.scrollTop || 0;
            let historyToSave = projectTabMessages;
            const prevRound = findProjectRoundForTab(prevTabId, prevTab.projectPath);
            if (sending && prevRound) {
                const prevSessionKey = projectSessionKey(prevTab.projectPath);
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
        if (activeTab.type === "project") {
            const restored = getTabState(currentTabId);
            const hasPendingRoundForTab = !!findProjectRoundForTab(currentTabId, activeTab.projectPath);
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
    const projectTabRoundsRef = useRef<Map<string, { tabId: string | null; projectPath: string; baseline: number; seq: number }>>(new Map());
    const findProjectRoundForTab = useCallback((tabId: string, projectPath?: string | null) => {
        const sessionKey = projectSessionKey(projectPath);
        if (sessionKey) {
            const byPath = projectTabRoundsRef.current.get(sessionKey);
            if (byPath && (byPath.tabId === tabId || byPath.projectPath === projectPath)) return byPath;
        }
        for (const round of projectTabRoundsRef.current.values()) {
            if (round.tabId === tabId) return round;
        }
        return undefined;
    }, []);
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
        const projectTabs = getTabs().filter(tab => tab.type === "project" && tab.projectPath);
        if (projectTabs.length === 0) return;
        for (const tab of projectTabs) {
            const sessionKey = `desktop-user:${tab.projectPath}`;
            const liveMessages = messages.filter((message: ChatMessage) => messageBelongsToSession(message, sessionKey));
            if (liveMessages.length === 0) continue;
            const existingState = getTabState(tab.id);
            // Skip syncing into a cleared/empty tab — prevents cancel responses or
            // residual streaming tokens from resurrecting after "New Task" clears
            // the conversation. New rounds populate tab state via the wasSending
            // effect and displayMessages' liveProjectMessages merge instead.
            const existingHistory = existingState?.history as unknown[] | undefined;
            if (!existingHistory || existingHistory.length === 0) continue;
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
        if (!isProjectTabActive) {
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
    }, [activeSessionKey, activeTab.id, activeTab.projectPath, findProjectRoundForTab, isProjectTabActive, messages, projectTabMessages, projectTabRouteVersion, sending]);
    latestDisplayMessagesRef.current = displayMessages;
    const prevSendingRef = useRef(sending);
    useEffect(() => {
        const wasSending = prevSendingRef.current;
        prevSendingRef.current = sending;
        if (!wasSending && sending && isProjectTabActive && activeTab.projectPath && !findProjectRoundForTab(activeTab.id, activeTab.projectPath)) {
            const sessionKey = projectSessionKey(activeTab.projectPath);
            if (sessionKey) {
                projectTabRoundsRef.current.set(sessionKey, {
                    tabId: activeTab.id,
                    projectPath: activeTab.projectPath,
                    baseline: messages.length,
                    seq: projectTabRoundSeqRef.current,
                });
                setProjectTabRouteVersion(version => version + 1);
            }
        }
        if (wasSending && !sending && projectTabRoundsRef.current.size > 0) {
            const rounds = Array.from(projectTabRoundsRef.current.entries());
            for (const [roundKey, round] of rounds) {
                const roundSessionKey = projectSessionKey(round.projectPath);
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
        for (const text of deferred) {
            void sendMessageForTabRef.current?.(text, { tabId, project_path: projectPath });
        }
    }, [setProjectTabPreparing]);
    const createProjectTabWithContext = useCallback((projectPath: string, taskTitle: string, options?: { prepareMode?: PendingProjectTabOpen["prepareMode"] } | boolean) => {
        const tabExisted = hasProjectTab(projectPath);
        const prepareMode = typeof options === "object" ? options.prepareMode : "restore-context";
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
                console.info("[AIAssistantPanel] project tab prepared", { tabId: readyTab.id, projectPath: readyTab.projectPath, prepareMode, reason, elapsedMs: Math.round(performance.now() - startedAt) });
            }, Math.max(0, delayMs));
            projectPrepareTimersRef.current.set(readyTab.id, timer);
        };
        const tab = createProjectTab(projectPath, taskTitle, prepareMode === "new-agent" ? {
            onSessionReady: (readyTab) => {
                const minimumVisibleMs = Math.max(0, 120 - (performance.now() - startedAt));
                scheduleNewAgentReady(readyTab, minimumVisibleMs, "session-ready");
            },
        } : undefined);
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
    const messagesLengthRef = useRef(messages.length);
    messagesLengthRef.current = messages.length;
    const sendMessageForTab = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const optionProjectPath = typeof options?.project_path === "string" ? options.project_path : undefined;
        const optionTabId = typeof options?.tabId === "string" ? options.tabId : undefined;
        const liveActiveTab = activeTabRef.current;
        const activeSessionProjectPath = projectPathFromSessionKey(getActiveSessionKey());
        const resolvedProjectPath = optionProjectPath
            || (liveActiveTab.type === "project" ? liveActiveTab.projectPath : undefined)
            || activeSessionProjectPath
            || undefined;
        const resolvedTab = resolvedProjectPath
            ? getTabs().find(t => t.type === "project" && t.projectPath === resolvedProjectPath)
            : undefined;
        const resolvedTabId = optionTabId || resolvedTab?.id || (liveActiveTab.type === "project" ? liveActiveTab.id : undefined);
        const isProjectSend = !!resolvedProjectPath;
        if (isProjectSend && resolvedProjectPath) {
            if (resolvedTabId) clearedProjectTabIdsRef.current.delete(resolvedTabId);
            const mergedOptions = {
                ...options,
                tabId: resolvedTabId,
                project_path: resolvedProjectPath,
            };
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
        const shouldIncludeTabScope = canShowAssistantCodingPreviewForTab(liveActiveTab);
        const localOptions = shouldIncludeTabScope
            ? (options ? { ...options, tabId: liveActiveTab.id } : { tabId: liveActiveTab.id })
            : (options || {});
        const localSend = sendMessage(text, localOptions as any);
        return localSend.finally(() => markPanelSendInFlight(localSessionKey, false));
    }, [getTabState, getTabs, markPanelSendInFlight, messages, persistProjectTabMsgIds, projectTabMessages, saveTabState, sendMessage]);
    sendMessageForTabRef.current = sendMessageForTab;
    const sendProjectMessageAfterPrepare = useCallback((text: string, options?: Record<string, unknown>): Promise<boolean> => {
        const tabId = typeof options?.tabId === "string" ? options.tabId : "";
        const projectPath = typeof options?.project_path === "string" ? options.project_path : "";
        if (tabId && projectPath && preparingProjectTabIdsRef.current.has(tabId)) {
            const deferred = deferredProjectInitialSendsRef.current.get(tabId) || [];
            deferred.push(text);
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
            forgetAIAssistantSessionRounds(`desktop-user:${tab.projectPath}`);
            const prepareTimer = projectPrepareTimersRef.current.get(tabId);
            if (prepareTimer !== undefined) {
                window.clearTimeout(prepareTimer);
                projectPrepareTimersRef.current.delete(tabId);
            }
            setProjectTabPreparing(tabId, false);
            deferredProjectInitialSendsRef.current.delete(tabId);
        }
        for (const [roundKey, round] of projectTabRoundsRef.current) {
            if (round.tabId !== tabId) continue;
            const sessionKey = projectSessionKey(tab?.type === "project" ? tab.projectPath : round.projectPath);
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
    const createProjectTabFromSearch = useCallback((projectPath: string, taskTitle: string, options?: { autoSend?: boolean }) => {
        const tabExistedInList = hasProjectTab(projectPath);
        const tab = createProjectTabWithContext(projectPath, taskTitle);
        if (!tab || !options?.autoSend || tabExistedInList) return tab;
        const existingState = getTabState(tab.id);
        const hasExistingConversation = existingState?.history?.some((m: any) => m && (m.role === "user" || m.role === "assistant"));
        if (!hasExistingConversation) {
            void sendProjectMessageAfterPrepare(taskTitle, { tabId: tab.id, project_path: tab.projectPath });
        }
        return tab;
    }, [createProjectTabWithContext, getTabState, hasProjectTab, sendProjectMessageAfterPrepare]);
    const closeProjectTabByPath = useCallback((projectPath: string) => {
        const tab = getTabs().find(t => t.type === "project" && t.projectPath === projectPath);
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
    const addParticipantToTab = useAddGroupParticipantToTab({ getTabState, upgradeVETabToGroup });
    const addLocalMaclawToTab = useAddLocalMaclawToTab({ getTabState, upgradeVETabToGroup });
    const [participantInviteTargetTabId, setParticipantInviteTargetTabId] = useState<string | null>(null);
    const participantInviteTargetTab = participantInviteTargetTabId ? tabState.tabs.find(t => t.id === participantInviteTargetTabId) || null : null;
    usePendingAssistantTabOpen({
        lang,
        createVETab,
        createGroupTab,
        createProjectTab: createProjectTabWithContext,
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
    const { state: codePreviewState, closePanel: closeCodePreview, activatePassive: activateCodePreviewPassive, selectFile: selectCodeFile, restoreState: restoreCodePreviewState, resetSession: resetCodePreviewState } = useCodePreviewState(codePreviewPathScope);
    useEffect(() => {
        if (!previewOwnerResetPendingRef.current) return; previewOwnerResetPendingRef.current = false;
        const state = previewStateMapRef.current.get("local");
        if (state) { restoreWorkflowState(state.workflow); restoreCodePreviewState(state.code); }
        else { resetWorkflowState(); resetCodePreviewState(); }
    }, [activeTab.id, restoreWorkflowState, restoreCodePreviewState, resetWorkflowState, resetCodePreviewState]);
    useEffect(() => {
        const restored = restoredPreviewStateRef.current;
        if (!restored) return;
        const currentOwner = tabState.tabs.find(tab => tab.id === previewOwnerTabRef.current);
        if (!canShowAssistantCodingPreviewForTab(currentOwner)) {
            const ownerTab = restored.ownerProjectPath
                ? tabState.tabs.find(tab => tab.type === "project" && tab.projectPath === restored.ownerProjectPath)
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
        restoredPreviewApplyingRef.current = true;
        restoredPreviewStateRef.current = null;
        restoredPreviewOwnerProjectPathRef.current = undefined;
        workflowStateRef.current = cloneWorkflowUIState(restored.workflow);
        codePreviewStateRef.current = cloneCodePreviewState(restored.code);
        restoreWorkflowState(restored.workflow);
        restoreCodePreviewState(restored.code);
    }, [activeTab.id, tabState.tabs, restoreWorkflowState, restoreCodePreviewState]);
    const workflowStateRef = useRef(workflowState);
    workflowStateRef.current = workflowState;
    const codePreviewStateRef = useRef(codePreviewState);
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
    const showCodePreview = codingPreviewAllowed && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview || showAgentView;
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
    // Toggle the entire right-side area (workflow doc preview + code preview) open/closed
    const handleTogglePreviewPanel = useCallback(() => {
        if (!codingPreviewAllowed) return;
        if (workflowState.splitMode || codePreviewStateRef.current.active) {
            closeDocPreview();
            closeCodePreview();
        } else {
            openDocPreview();
            const cp = codePreviewStateRef.current;
            if (cp.files.size > 0 && !cp.active) {
                activateCodePreviewPassive();
            }
        }
    }, [codingPreviewAllowed, workflowState.splitMode, closeDocPreview, closeCodePreview, openDocPreview, activateCodePreviewPassive]);
    // Keep ref updated so clearActiveHistory (defined earlier) can close all preview panels
    closeAllPreviewPanelsRef.current = () => {
        closeDocPreview();
        closeCodePreview();
        resetWorkflowState();
        if (agentView) dismissAgentView(agentView.id, undefined, { force: true });
    };
    const title = lang === "en" ? "AI Assistant" : "AI \u52a9\u624b";
    const thinkingText = lang === "en" ? "Thinking... (you can type ahead)" : "\u6b63\u5728\u601d\u8003...\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const processingText = lang === "en" ? "Running tools... (you can type ahead)" : "\u6b63\u5728\u6267\u884c\u5de5\u5177\u2026\uff08\u53ef\u7ee7\u7eed\u8f93\u5165\uff09";
    const idlePlaceholderText = getComposeActionPlaceholder(composeAction, !lang?.startsWith("en"))
        || (lang === "en" ? "Type a message..." : "\u8f93\u5165\u6d88\u606f...");
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
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : (isLocalTabActive && !hasForegroundProjectRound))));
    const activeSessionIsStreaming = hasExplicitStreamingSessionList
        ? streamingSessionKeys.includes(activeSessionKey)
        : streaming && (streamingSessionKey
            ? streamingSessionKey === activeSessionKey
            : (isProjectTabActive ? hasForegroundRoundForActiveProject : (isLocalTabActive && !hasForegroundProjectRound)));
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
    const inputLocked = isBusy || cancelPending || activeProjectPreparing;
    const submitLocked = inputLocked;
    const prevSubmitLockedRef = useRef(submitLocked);
    const prevShowChatUIRef = useRef(showChatUI);
    const continueQueueDrainRef = useRef(false);
    const queueAutoDrainArmedRef = useRef(false);
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

    // Branch command listener: triggered by the 🔀 button on user messages.
    useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (detail?.command && sendMessageForTabRef.current) {
                void sendMessageForTabRef.current(detail.command);
            }
        };
        window.addEventListener('ai-send-branch-command', handler);
        return () => window.removeEventListener('ai-send-branch-command', handler);
    }, []);

    // Handle external "run skill" requests (from Skills Management Panel ▶ button)
    useEffect(() => {
        const handler = (e: Event) => {
            const text = (e as CustomEvent).detail?.text;
            if (typeof text === "string" && text.trim()) {
                e.preventDefault(); // Signal to sender that injection was accepted
                void sendMessageForTabRef.current?.(text.trim());
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
        ? (lang === "en" ? "Creating agent instance... type ahead, Enter will wait" : "正在创建 Agent 实例... 可预输入，Enter 会等待")
        : (lang === "en" ? "Restoring task context... type ahead, Enter will wait" : "正在恢复任务上下文... 可预输入，Enter 会等待");
    const placeholderText = !ready
        ? initLabel
        : activeProjectPreparing
            ? preparingPlaceholderText
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
        if (activeTab.type === "project") {
            saveTabState(activeTab.id, { inputText: nextValue });
            return;
        }
        setDraftInputValue?.(nextValue);
    }, [activeTab.id, activeTab.type, saveTabState, setDraftInputValue]);
    const canSend = ready && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
    const handleWelcomePromptSelect = useCallback((text: string) => {
        // Scenario templates are free-form prompts, not slash-command arguments.
        setComposeAction(null);
        updateInputValue(text);
        requestAnimationFrame(() => {
            if (inputRef.current) {
                inputRef.current.focus();
                // Auto-grow textarea height to fit multi-line template
                inputRef.current.style.height = "auto";
                inputRef.current.style.height = inputRef.current.scrollHeight + "px";
                // Select the first [placeholder] so user can immediately type the value
                const firstBracket = text.indexOf('[');
                const closeBracket = text.indexOf(']', firstBracket);
                if (firstBracket >= 0 && closeBracket > firstBracket) {
                    inputRef.current.selectionStart = firstBracket;
                    inputRef.current.selectionEnd = closeBracket + 1;
                } else {
                    // No placeholder — move cursor to end
                    inputRef.current.selectionStart = text.length;
                    inputRef.current.selectionEnd = text.length;
                }
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
    // Show welcome for an idle, empty conversation on local tab or a cleared project tab.
    // NOTE: welcome view is shown in both inline (embedded panel) and overlay (standalone window)
    // modes — the embedded panel is now the primary usage mode.
    const showWelcomeView = ready && !onboardingIncomplete && otherMessages.length === 0 && displayProgressMessages.length === 0 && !showThinkingState && !showProcessingState && !activeProjectPreparing && !workflowAwaitingForm && !workflowFormGeneratingDocument && !workflowAwaitingReview && !workflowStartingLabel && queue.length === 0 && !queueEditDraftActive && !queueInteractionStarted && (isLocalTabActive || isProjectTabActive);
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
        if (activeTab.type === "project") return;
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
            continueQueueDrainRef.current = false;
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
    }, [ready, recordSubmittedPrompt, sendMessageForTab]);
    const handleClearInput = useCallback(() => {
        clearComposerDraft({ clearAttachments: false });
    }, [clearComposerDraft]);
    const sendInFlightRef = useRef(false);
    const submitRecognizedVoiceText = useCallback(async (text: string, _source?: VoiceInputSource) => {
        // Defense-in-depth: never send/queue empty or punctuation-only ASR noise.
        if (!ready || !shouldDispatchASRText(text)) return;
        const trimmed = normalizeASRText(text);
        // Honor active compose mode (goal / btw) so voice matches typed send semantics.
        const composed = applyComposeActionToText(trimmed, composeAction);
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
        // If agent is busy (inputLocked), queue the transcription for later delivery
        // instead of dropping it. The buffer queue auto-drains when the agent becomes idle.
        if (inputLocked) {
            addEntry(composed, [], { autoDrain: true });
            setComposeAction(null);
            return;
        }
        if (sendInFlightRef.current) return;
        sendInFlightRef.current = true;
        clearComposerDraft({ clearAttachments: false });
        try {
            const sent = await sendMessageForTab(composed);
            if (sent !== false) recordSubmittedPrompt?.(composed);
        } catch (err: unknown) {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        } finally {
            sendInFlightRef.current = false;
        }
    }, [addEntry, clearComposerDraft, composeAction, dispatchBtwText, inputLocked, ready, recordSubmittedPrompt, sendBtwMessage, sendMessageForTab, updateInputValue]);
    const voiceInput = useVoiceInput(submitRecognizedVoiceText, audioInputDeviceId || '');
    const { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave } = useAIAssistantVoiceControls({
        inputRef,
        petFocusInputSeq,
        petVoiceStartSeq,
        ready,
        voiceInput,
    });
    const handleSend = useCallback(async () => {
        // Prevent duplicate submits before the session reports busy, but keep
        // the type-ahead queue usable while an existing session is running.
        if (sendInFlightRef.current && !submitLocked && !queueEditDraftActive) return;
        const rawInputValue = inputRef.current?.value ?? inputValue;
        const text = applyComposeActionToText(rawInputValue, composeAction);
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
            addEntry(text || rawInputValue, attachments, { autoDrain: submitLocked });
            if (submitLocked) {
                queueAutoDrainArmedRef.current = true;
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
    }, [activeSessionIsSending, activeSessionIsStreaming, activeSessionKey, activeTab.id, activeTab.projectPath, activeTab.type, addEntry, busySessionKeys, cancelPending, clearComposerDraft, composeAction, dispatchBtwText, inputValue, pendingAttachments, queueEditDraftActive, recordSubmittedPrompt, selectedFilePaths, sendBtwMessage, sendMessageForTab, sending, sendingSessionKey, streaming, streamingSessionKey, streamingSessionKeys, submitLocked, updateInputValue]);
    useEffect(() => {
        if (queue.length === 0) {
            continueQueueDrainRef.current = false;
            queueAutoDrainArmedRef.current = false;
        }
        const readyToDrainQueue = ready && showChatUI && !submitLocked;
        const becameIdle = prevSubmitLockedRef.current && readyToDrainQueue;
        const returnedToChatIdle = !prevShowChatUIRef.current && readyToDrainQueue;
        const continueIdleDrain = continueQueueDrainRef.current && readyToDrainQueue;
        const armedIdleDrain = queueAutoDrainArmedRef.current && readyToDrainQueue;
        const persistedAutoDrain = !!queue[0]?.autoDrain && readyToDrainQueue;
        if ((becameIdle || returnedToChatIdle || continueIdleDrain || armedIdleDrain || persistedAutoDrain) && queue.length > 0 && !queueEditDraftActive) {
            const entry = queue[0];
            if (firingEntryIdsRef.current.has(entry.id) || drainingEntryIdsRef.current.has(entry.id)) {
                prevSubmitLockedRef.current = submitLocked;
                prevShowChatUIRef.current = showChatUI;
                return;
            }
            continueQueueDrainRef.current = false;
            queueAutoDrainArmedRef.current = false;
            drainingEntryIdsRef.current.add(entry.id);
            refreshQueueInFlight();
            const entryText = entry.text.trim();
            console.info("[AIAssistantPanel] drain queued input", {
                activeTabId: activeTab.id,
                activeTabType: activeTab.type,
                projectPath: activeTab.projectPath || "",
                entryId: entry.id,
                textLength: entryText.length,
                attachmentCount: entry.attachments.length,
            });
            // Preserve /btw side-query semantics when draining the buffer queue.
            const entryIsBtw = isBtwCommandText(entryText) && !!sendBtwMessage;
            const drainPromise = entryIsBtw
                ? dispatchBtwText(entryText)
                : sendMessageForTab(buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath)));
            drainPromise.then((sent) => {
                if (sent === false) return;
                continueQueueDrainRef.current = true;
                removeEntry(entry.id);
                // dispatchBtwText already records the prompt; only record normal sends here.
                if (!entryIsBtw) recordSubmittedPrompt?.(entry.text);
            }).catch(() => {}).finally(() => {
                drainingEntryIdsRef.current.delete(entry.id);
                refreshQueueInFlight();
            });
        }
        prevSubmitLockedRef.current = submitLocked;
        prevShowChatUIRef.current = showChatUI;
    }, [activeTab.id, activeTab.projectPath, activeTab.type, dispatchBtwText, queue, queueEditDraftActive, ready, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendBtwMessage, sendMessageForTab, showChatUI, submitLocked]);
    const appendProjectGuideReferenceEcho = useCallback((text: string, targetTabId: string | null, accepted = true) => {
        if (!targetTabId) return;
        const targetTab = tabState.tabs.find(tab => tab.id === targetTabId);
        if (!targetTab || targetTab.type !== "project") return;
        const echo: ChatMessage = {
            id: `guide-reference-${Date.now()}-${Math.random().toString(36).slice(2)}`,
            role: 'system',
            kind: accepted ? 'guideReceipt' : 'guideRejection',
            content: accepted ? buildGuideReferenceAcceptedNotice(text, lang || "en") : buildGuideReferenceRejectedNotice(text, lang || "en"),
            timestamp: Date.now(),
        };
        const existingState = getTabState(targetTabId);
        const nextHistory = mergeChatMessages(existingState?.history, [echo]);
        saveTabState(targetTabId, {
            ...existingState,
            history: nextHistory,
        });
        if (activeTabIdRef.current === targetTabId) {
            setProjectTabMessages(nextHistory);
        }
    }, [getTabState, lang, saveTabState, tabState.tabs]);
    const handleFireEntry = useCallback(async (id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        const entry = queue.find(item => item.id === id);
        if (!entry) return;
        const guideTargetTabId = isProjectTabActive ? activeTab.id : null;
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
            const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
            let injected = false;
            if (guideLaunchReference) {
                injected = await guideLaunchReference(outgoing, activeSessionKey);
            } else if (injectSupplementary) {
                injected = await injectSupplementary(outgoing);
            }
            if (!injected) {
                appendProjectGuideReferenceEcho(outgoing, guideTargetTabId, false);
                return;
            }
            appendProjectGuideReferenceEcho(outgoing, guideTargetTabId, true);
            removeEntry(id);
            recordSubmittedPrompt?.(entry.text);
        } catch {
            if (!isBtwCommandText(entryText)) {
                const outgoing = buildOutgoingMessageMulti(entry.text, entry.attachments.map(att => att.filePath));
                appendProjectGuideReferenceEcho(outgoing, guideTargetTabId, false);
            }
            return;
        } finally {
            firingEntryIdsRef.current.delete(id);
            refreshQueueInFlight();
        }
    }, [activeSessionKey, activeTab.id, appendProjectGuideReferenceEcho, dispatchBtwText, guideLaunchReference, injectSupplementary, isProjectTabActive, queue, recordSubmittedPrompt, refreshQueueInFlight, removeEntry, sendBtwMessage]);
    const handleDeleteEntry = useCallback((id: string) => {
        if (firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id)) return;
        removeEntry(id);
    }, [removeEntry]);
    const isQueueEntryInFlight = useCallback((id: string) => activeProjectPreparing || firingEntryIdsRef.current.has(id) || drainingEntryIdsRef.current.has(id), [activeProjectPreparing, queueInFlightVersion]);
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
        if (!isProjectTabActive || !activeTab.projectPath) {
            return executeAction(command);
        }
        // Commands handled entirely in the frontend — they call Wails bindings
        // directly without sendMessage. Do NOT pre-register a project round
        // for these (it would create a stale round that's never consumed).
        if (command.startsWith('__resolve_critical_confirm__') || command.startsWith('__view_trace__')) {
            return executeAction(command);
        }
        // Pre-register the project round with correct baseline (current
        // messages.length BEFORE executeAction adds user+placeholder).
        const roundKey = projectSessionKey(activeTab.projectPath);
        if (roundKey && !projectTabRoundsRef.current.has(roundKey)) {
            const roundSeq = projectTabRoundSeqRef.current + 1;
            projectTabRoundSeqRef.current = roundSeq;
            projectTabRoundsRef.current.set(roundKey, {
                tabId: activeTab.id,
                projectPath: activeTab.projectPath,
                baseline: messagesLengthRef.current,
                seq: roundSeq,
            });
            setProjectTabRouteVersion(version => version + 1);
        }
        // Call the original executeAction which handles all special command
        // logic (__workflow_choice__, __confirm_execution__, etc.) and
        // internally calls sendMessage with proper options.
        return executeAction(command);
    }, [activeTab.id, activeTab.projectPath, executeAction, isProjectTabActive]);
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
            // Use -1 for undefined content to distinguish from empty string (length 0).
            const contentLen = msg.content == null ? -1 : msg.content.length;
            const contentKey = `${contentLen}|${msg.reasoning?.length ?? 0}|${msg.actions?.length ?? 0}|${isLast ? 1 : 0}|${isLast && isBusy ? 1 : 0}|${msg.confirmation ? 1 : 0}|${msg.unfinishedSlot ? 1 : 0}|${msg.localFilePath ?? ''}|${msg.thumbnailBase64 ? 1 : 0}`;
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
                });
            } else {
                // Reset incremental state when streaming ends (isBusy becomes false)
                // so the final render is a clean full parse (100% correct).
                if (isLast && msg.role === 'assistant' && incrementalStateRef.current.messageId === msg.id && !isBusy) {
                    incrementalStateRef.current = { messageId: '', state: createIncrementalRenderState() };
                }
                node = renderMessage(suppressWorkflowReviewActions(msg), panelExecuteAction, t, isLast, savedFileLabel, lang, isBusy);
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
                        >🔀</button>
                    </div>
                );
                cache.set(msg.id, { contentKey, node: wrappedNode });
                return wrappedNode;
            }
            return node;
        });
    }, [otherMessages, panelExecuteAction, t, lastAssistantIdx, savedFileLabel, lang, isBusy, branchPointByDisplayIndex]);
    const chatProgressMessages = useMemo(
        () => activeSessionHasWork ? displayProgressMessages.filter((msg: ChatMessage) => !isToolProgressMessage(msg)) : displayProgressMessages,
        [activeSessionHasWork, displayProgressMessages],
    );
    const compactProgressMessages = useMemo(() => compactCodingAgentProgressMessages(chatProgressMessages), [chatProgressMessages]);
    const renderedProgressMessages = useMemo(() => compactProgressMessages.map((msg: ChatMessage) => renderMessage(suppressWorkflowReviewActions(msg), panelExecuteAction, t, false, savedFileLabel, lang)), [compactProgressMessages, panelExecuteAction, t, savedFileLabel, lang]);
    const containerStyle: React.CSSProperties = inline ? (maximized ? { ...maximizedInlineStyle, background: t.bg } : { display: "flex", flex: "1 1 0%", flexDirection: "column", minWidth: 0, minHeight: 0, boxSizing: "border-box", overflow: "hidden", background: t.bg, textAlign: "left", width: "100%", height: "100%", position: "relative" }) : overlayStyle;
    const scopeApprovalIsHighRisk = scopeApprovalPending?.kind === REMOTE_HIGH_RISK_APPROVAL_KIND || scopeApprovalPending?.kind === LOCAL_HIGH_RISK_APPROVAL_KIND;
    const scopeApprovalIsRemoteHighRisk = scopeApprovalPending?.kind === REMOTE_HIGH_RISK_APPROVAL_KIND;
    return (
        <div data-testid="ai-panel-root" style={containerStyle}>
            <style>{`.branch-hover-container:hover .branch-btn { opacity: 0.7 !important; } .branch-hover-container .branch-btn:hover { opacity: 1 !important; background: ${t.fieldBg} !important; }`}</style>
            {inline && <AssistantDragHandle />}
            <AssistantTitleBar clearHistory={clearActiveHistory} clearHistoryDisabled={inputLocked} inline={!!inline} lang={lang} maximized={!!maximized} onClose={onClose} onDismissAppUpdate={onDismissAppUpdate} onHideWindow={onHideWindow} onOpenAppUpdate={onOpenAppUpdate} onOpenKnowledge={() => setKnowledgeDialogOpen(true)} onOpenTutorial={onOpenTutorial} onSaveCurrentTask={isLocalTabActive ? openSaveTaskDialog : undefined} onToggleMaximize={onToggleMaximize} onTogglePreviewPanel={handleTogglePreviewPanel} onToggleSkillRecording={handleToggleSkillRecording} onToggleWorkflow={handleToggleWorkflow} previewPanelOpen={showWorkflowPreview || showCodePreview} projectSearchOpen={projectSearch.open} refreshNews={refreshNews} setThemeMode={setThemeMode} setTtsEnabled={setTtsEnabled} showMaximizeToggle={showMaximizeToggle} skillRecording={skillRecordingTabId === activeTab?.id} skillRecordingCount={skillRecordingCount} skillRecordingAnyTab={!!skillRecordingTabId} theme={t} themeMode={themeMode} title={title} trialReflectEnabled={trialReflectEnabled} ttsEnabled={ttsEnabled} ttsPlaying={ttsPlaying} toggleProjectSearch={projectSearch.toggle} updateAvailable={appUpdateAvailable} workflowActive={workflowState.active} workflowEnabled={workflowEnabled} />
            <div data-testid="ai-panel-content-row" style={{ display: "flex", flexDirection: "row", flex: 1, minHeight: 0, minWidth: 0, overflow: "hidden" }}>
            <div data-testid="ai-panel-body" style={{ display: "flex", flexDirection: "column", flex: splitRatio, minWidth: 0, minHeight: 0, height: "100%", boxSizing: "border-box", overflow: "hidden", position: "relative" }} onDragOver={handleDragOver} onDrop={handleDrop}>
            <KnowledgeDialog open={knowledgeDialogOpen} onClose={() => setKnowledgeDialogOpen(false)} lang={lang} theme={t} />
            {scopeApprovalPending && (
                <div data-testid="scope-approval-backdrop" style={{ position: "fixed", inset: 0, zIndex: 50001, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(15, 23, 42, 0.35)", padding: 16 }}>
                    <div role="alertdialog" aria-modal="true" aria-labelledby="scope-approval-title" style={{ width: 440, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 8, boxShadow: "0 12px 32px rgba(15, 23, 42, 0.22)", color: t.text, overflow: "hidden" }} onMouseDown={e => e.stopPropagation()}>
                        <div style={{ padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                            <h3 id="scope-approval-title" style={{ margin: 0, fontSize: 14, fontWeight: 700, color: "#f59e0b" }}>{scopeApprovalIsHighRisk ? (scopeApprovalIsRemoteHighRisk ? localizeText(lang, "Remote Command Approval", "远程命令确认", "遠程命令確認") : localizeText(lang, "Command Approval", "命令确认", "命令確認")) : localizeText(lang, "Scope Approval", "目录越权确认", "目錄越權確認")}</h3>
                        </div>
                        <div style={{ padding: "12px 14px", fontSize: 13, lineHeight: 1.6 }}>
                            <div style={{ marginBottom: 8 }}>{scopeApprovalIsHighRisk ? (scopeApprovalIsRemoteHighRisk ? localizeText(lang, "Remote CodingSubAgent is trying to run a blocked high-risk command:", "远程编码 SubAgent 尝试执行被拦截的高风险命令：", "遠程編碼 SubAgent 嘗試執行被攔截的高風險命令：") : localizeText(lang, "CodingSubAgent is trying to run a blocked high-risk command:", "编码 SubAgent 尝试执行被拦截的高风险命令：", "編碼 SubAgent 嘗試執行被攔截的高風險命令：")) : localizeText(lang, "CodingSubAgent is trying to access a path outside the project:", "编码 SubAgent 尝试访问项目目录外的路径：", "編碼 SubAgent 嘗試訪問項目目錄外的路徑：")}</div>
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
            {saveTaskDialogOpen && (
                <div data-testid="save-task-dialog-backdrop" style={{ position: "fixed", inset: 0, zIndex: 50000, display: "flex", alignItems: "center", justifyContent: "center", background: "rgba(15, 23, 42, 0.28)", padding: 16 }} onMouseDown={event => { if (event.target === event.currentTarget && !savingTask) setSaveTaskDialogOpen(false); }}>
                    <form role="dialog" aria-modal="true" aria-labelledby="save-task-dialog-title" onSubmit={event => { event.preventDefault(); void submitSaveTask(); }} style={{ width: 390, maxWidth: "calc(100vw - 32px)", background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: 8, boxShadow: "0 12px 32px rgba(15, 23, 42, 0.22)", color: t.text, overflow: "hidden" }} onMouseDown={event => event.stopPropagation()}>
                        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "12px 14px", borderBottom: `1px solid ${t.titleBarBorder}` }}>
                            <h3 id="save-task-dialog-title" style={{ margin: 0, fontSize: 14, fontWeight: 700, color: t.text }}>{localizeText(lang, "Save as Task", "\u4fdd\u5b58\u4e3a\u4efb\u52a1", "\u4fdd\u5b58\u70ba\u4efb\u52d9")}</h3>
                            <button type="button" disabled={savingTask} onClick={() => setSaveTaskDialogOpen(false)} style={{ border: "none", background: "transparent", color: t.text, opacity: 0.62, cursor: savingTask ? "default" : "pointer", fontSize: 14, lineHeight: 1 }}>x</button>
                        </div>
                        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: "14px" }}>
                            <label htmlFor="save-task-name" style={{ fontSize: 12, fontWeight: 700, color: t.promptColor }}>{localizeText(lang, "Task name", "\u4efb\u52a1\u540d\u79f0", "\u4efb\u52d9\u540d\u7a31")}</label>
                            <input id="save-task-name" autoFocus value={saveTaskName} disabled={savingTask} onChange={event => setSaveTaskName(event.target.value)} onKeyDown={event => { if (event.key === "Escape" && !savingTask) setSaveTaskDialogOpen(false); }} style={{ width: "100%", boxSizing: "border-box", border: `1px solid ${t.fieldBorder}`, borderRadius: 6, background: t.fieldBg, color: t.text, fontSize: 13, padding: "7px 9px", outline: "none", fontFamily: "inherit" }} />
                            <p style={{ margin: "4px 0 0", fontSize: 12, lineHeight: 1.45, color: t.promptColor }}>{localizeText(lang, "The current main conversation history and task context will be saved. Double-click it in Task Management to continue in a separate tab.", "\u5c06\u4fdd\u5b58\u5f53\u524d\u4e3b\u5bf9\u8bdd\u5386\u53f2\u548c\u4efb\u52a1\u4e0a\u4e0b\u6587\u3002\u4e4b\u540e\u53ef\u5728\u4efb\u52a1\u7ba1\u7406\u4e2d\u53cc\u51fb\uff0c\u4ee5\u72ec\u7acb Tab \u7ee7\u7eed\u3002", "\u5c07\u4fdd\u5b58\u76ee\u524d\u4e3b\u5c0d\u8a71\u6b77\u53f2\u548c\u4efb\u52d9\u4e0a\u4e0b\u6587\u3002\u4e4b\u5f8c\u53ef\u5728\u4efb\u52d9\u7ba1\u7406\u4e2d\u96d9\u64ca\uff0c\u4ee5\u7368\u7acb Tab \u7e7c\u7e8c\u3002")}</p>
                        </div>
                        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, padding: "10px 14px 12px", borderTop: `1px solid ${t.titleBarBorder}` }}>
                            <button type="button" disabled={savingTask} onClick={() => setSaveTaskDialogOpen(false)} style={{ border: `1px solid ${t.titleBarBorder}`, borderRadius: 6, background: t.fieldBg, color: t.text, cursor: savingTask ? "default" : "pointer", fontSize: 12, padding: "5px 12px" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88", "\u53d6\u6d88")}</button>
                            <button type="submit" disabled={savingTask || !saveTaskName.trim()} style={{ border: `1px solid ${t.btnBorder}`, borderRadius: 6, background: t.sendBtnBg, color: t.sendBtnColor, cursor: savingTask || !saveTaskName.trim() ? "default" : "pointer", opacity: savingTask || !saveTaskName.trim() ? 0.62 : 1, fontSize: 12, padding: "5px 12px" }}>{savingTask ? localizeText(lang, "Saving...", "\u4fdd\u5b58\u4e2d...", "\u4fdd\u5b58\u4e2d...") : localizeText(lang, "Save", "\u4fdd\u5b58", "\u4fdd\u5b58")}</button>
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
            }} onAddLocalMaclawToTab={addLocalMaclawToTab} onRenameGroupTab={openRenameGroupDialog} lang={lang} getLastActiveAt={getLastActiveAt} recordingTabId={skillRecordingTabId} />
            {tabLimitError && <div data-testid="ai-tab-limit-error" style={{ padding: "6px 12px", fontSize: 12, color: t.errorText, background: t.errorBg, borderBottom: `1px solid ${t.errorBorder}`, textAlign: "center" }}>{tabLimitError}</div>}
            {(activeTab?.type === "local" || activeTab?.type === "project") && (
                <ProjectDirBar key={activeTab?.type === "local" ? "local" : activeTab?.id || "local"} tabId={activeTab?.type === "local" ? "" : (activeTab?.id || "")} theme={t} lang={lang} />
            )}
            {showChatUI && <>
                <AssistantWorkflowMaximizeSuggestion inline={!!inline} lang={lang} maximized={!!maximized} onDismiss={dismissMaximizeSuggestion} onToggleMaximize={onToggleMaximize} suggestMaximize={workflowState.suggestMaximize} theme={t} themeMode={themeMode} />
                <ProjectSearchPanel search={projectSearch} lang={lang} theme={t} inline={!!inline} onProjectSwitch={handleProjectSearchSwitch} onCreateProjectTab={createProjectTabFromSearch} onCloseProjectTab={closeProjectTabByPath} onForkCurrentChat={handleForkCurrentChat} onTaskPrefsChanged={onTaskPrefsChanged} />
                {workflowStartingLabel && !hasConversation && !showThinkingState && !showProcessingState && (
                    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '12px', background: t.bg, color: t.textMuted }}>
                        <div style={{ fontSize: '2rem' }}>🚀</div>
                        <div style={{ fontSize: '0.88rem', fontWeight: 600 }}>
                            {lang?.startsWith('en') ? `Starting workflow: ${workflowStartingLabel}` : `正在启动工作流：${workflowStartingLabel}`}
                        </div>
                        <div style={{ fontSize: '0.75rem', opacity: 0.7 }}>
                            {lang?.startsWith('en') ? 'Please wait...' : '请稍候...'}
                        </div>
                    </div>
                )}
                {showWelcomeView ? (
                    <div data-testid="ai-welcome-container" style={{ flex: 1, minHeight: 0, overflow: "auto", background: t.bg }}>
                        <AssistantWelcomeView
                            lang={lang}
                            theme={t}
                            themeMode={themeMode}
                            onPromptSelect={handleWelcomePromptSelect}
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
                <div ref={outputContainerRef} data-testid="ai-output-container" className="ai-chat-scrollbar" style={{ flex: 1, minHeight: 0, maxHeight: "none", padding: "8px 10px", fontSize: `${chatFontSize}px`, lineHeight: 1.5, overflowY: "auto", overflowX: "hidden", scrollbarGutter: "stable", textAlign: "left", color: t.text, background: t.bg, fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace", whiteSpace: "normal", overflowWrap: "anywhere", wordBreak: "normal" }} onScroll={handleScroll}>
                    <AssistantConversationBody initLabel={initLabel} lang={lang} messages={displayMessages} onOpenOnboarding={onOpenOnboarding} onboardingIncomplete={onboardingIncomplete} pinnedNews={pinnedNews} processingText={activeProcessingText} ready={ready} renderedOtherMessages={renderedOtherMessages} renderedProgressMessages={renderedProgressMessages} showProcessingState={showProcessingState} showThinkingState={showThinkingState} theme={t} thinkingText={thinkingText} />
                    <div ref={outputEndRef} />
                </div>
                )}
                {activeProjectPreparing && <div data-testid="project-tab-restore-progress" style={{ flexShrink: 0, padding: "7px 10px 8px", borderTop: `1px solid ${t.inputBarBorder}`, background: t.inputBarBg, color: t.textMuted, fontSize: 12 }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, marginBottom: 6 }}>
                        <span>{activeProjectPrepareMode === "new-agent" ? (lang === "en" ? "Creating agent instance" : "正在创建 Agent 实例") : (lang === "en" ? "Restoring task context" : "正在恢复任务上下文")}</span>
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
                {!showWelcomeView && <AssistantInputStack browseFile={browseFile} canSend={canSend} cancelPending={cancelPending} cancelSession={cancelSession} clearSelectedFile={clearSelectedFile} composeAction={composeAction} editingEntryId={editingEntryId} exitHistoryBrowsing={exitHistoryBrowsing} finishVoicePointer={finishVoicePointer} handleCancel={handleCancel} handleCancelEdit={handleCancelEdit} handleClearInput={handleClearInput} handleDragOver={handleDragOver} handleDrop={handleDrop} handleEditEntry={handleEditEntry} handlePaste={handlePaste} handleSaveEdit={handleSaveEdit} handleFireEntry={handleFireEntry} handleSend={handleSend} isEntryInFlight={isQueueEntryInFlight} handleVoiceClick={handleVoiceClick} handleVoicePointerDown={handleVoicePointerDown} handleVoicePointerLeave={handleVoicePointerLeave} inputAreaHeight={inputAreaHeight} inputLocked={inputLocked} inputRef={inputRef} inputValue={inputValue} inline={false} isBusy={inputVisualBusy} isSelectionCollapsedAtBoundary={isSelectionCollapsedAtBoundary} lang={lang} onComposeActionChange={handleComposeActionChange} onFireSlashCommand={handleFireSlashCommand} onInsertTemplate={handleInsertTemplate} onPlusMenuAction={handlePlusMenuAction} onPermissionModeChange={handlePermissionModeChange} pendingAttachments={pendingAttachments} permissionMode={permissionMode} placeholderText={placeholderText} queue={queue} ready={ready} recallHistory={recallHistory} rememberHistoryEdit={rememberHistoryEdit} removeEntry={handleDeleteEntry} removeSelectedFile={removeSelectedFile} reorderEntry={handleReorderEntry} resizeInput={resizeInput} selectedFilePaths={selectedFilePaths} setPendingAttachments={setPendingAttachments} showBusySpinner={showBusySpinner} startInputResize={startInputResize} submittedPrompts={submittedPrompts} theme={t} themeMode={themeMode} updateInputValue={updateInputValue} voiceInput={voiceInput} />}
            </>}
            <AssistantActiveTabContent activeTab={activeTab} tabs={tabState.tabs} isLocalTabActive={isLocalTabActive} isProjectTabActive={isProjectTabActive} lang={lang} theme={t} getTabState={getTabState} saveTabState={saveTabState} onAddParticipantToTab={addParticipantToTab} />
            {renameGroupTargetTab && (
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
            {participantInviteTargetTab && <TabParticipantInviteDialog key={participantInviteTargetTab.id} tab={participantInviteTargetTab} lang={lang} theme={t} onClose={() => setParticipantInviteTargetTabId(null)} onAddParticipantToTab={addParticipantToTab} />}
            </div>
            <AssistantPreviewPane agentView={agentView} codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} closeDocPreview={closeDocPreview} dismissAgentView={dismissAgentView} lang={lang} selectCodeFile={selectCodeFile} submitAgentView={panelSubmitAgentView} showCodePreview={showCodePreview} showAgentView={showAgentView} showWorkflowPreview={showWorkflowPreview} splitRatio={splitRatio} startPreviewResize={startPreviewResize} theme={t} themeMode={themeMode} workflowState={workflowState} />
            </div>
        </div>
    );
}

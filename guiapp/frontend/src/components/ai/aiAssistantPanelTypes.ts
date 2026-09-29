import type { AIAssistantInitStatus, CancelAIAssistantResult, ChatMessage, AIAssistantActionRouteOptions } from "./useAIAssistant";
import type { AgentView } from "./agentViewTypes";
export type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./groupDiscussionTypes";
import type { GroupDiscussionPanelControl } from "./groupDiscussionTypes";
import type { PendingHistoryDiscussionOpen, PendingProjectTabOpen, PendingProjectTabOpenResult, PendingExpertOpen, EnsuredExpertTask } from "./usePendingAssistantTabOpen";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import type { ExpertDefinition } from "./expertTypes";
import type { AssistantUpdatePayload } from "./AssistantUpdateNotice";
import type { AssistantDarkSchemeId } from "./assistantDarkSchemes";
import type { AssistantLightSchemeId } from "./assistantLightSchemes";
import type { SidebarLLMProviderSummary } from "../../types/appShell";
import type { AIExecutionProfile } from "./AITabTypes";
import type { ActiveAssistantTaskIdentity } from "./aiAssistantPanelSessionUtils";
import type { TaskManagementItem } from "../layout/SidebarTaskManagement";

export type { ActiveAssistantTaskIdentity };

/**
 * State fields provided by useAIAssistant hook.
 * All fields are required — TypeScript will error if the hook omits any.
 * This eliminates the "forgot to wire a field" class of bugs.
 */
export interface AIAssistantPanelHookState {
    messages: ChatMessage[];
    progressMessages: ChatMessage[];
    sending: boolean;
    sendingSessionKey?: string;
    busySessionKeys?: string[];
    streaming: boolean;
    streamingSessionKey?: string;
    streamingSessionKeys?: string[];
    visualBusy: boolean;
    ready: boolean;
    initStatus: AIAssistantInitStatus;
    selectedFilePaths: string[];
    submittedPrompts: string[];
    draftInputValue: string;
    trialReflectEnabled: boolean;
    scrollToTopSeq: number;
    agentView: AgentView | null;
}

/**
 * State fields provided by the App shell (not the hook).
 * These depend on external state (config, routing) that the hook doesn't own.
 */
export interface AIAssistantPanelAppState {
    selectedFilePath?: string;
    onboardingIncomplete?: boolean;
    showTraceEntry?: boolean;
    /** Whether the assistant is the visible application page. */
    active?: boolean;
}

export interface AIAssistantPanelStateProps extends Partial<AIAssistantPanelHookState>, AIAssistantPanelAppState {
    messages: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    ready: boolean;
}

/**
 * Action callbacks provided by useAIAssistant hook.
 * All fields are required — TypeScript will error if the hook omits any.
 */
export interface AIAssistantPanelHookActions {
    browseFile: () => Promise<void>;
    clearSelectedFile: () => void;
    /** Replace the active session's picker files. Used to restore a covered composer. */
    setSelectedFilePaths?: (paths: string[]) => void;
    removeSelectedFile: (filePath: string) => void;
    sendMessage: (text: string, options?: Record<string, unknown>) => Promise<boolean>;
    sendBtwMessage: (query: string) => Promise<void>;
    sendMessageInBackground: (text: string) => Promise<void>;
    injectSupplementary: (text: string, sessionKey?: string) => Promise<boolean>;
    guideLaunchReference: (text: string, sessionKey?: string, launchId?: string) => Promise<boolean>;
    clearHistory: () => Promise<void>;
    recordSubmittedPrompt: (text: string) => void;
    setDraftInputValue: (text: string) => void;
    executeAction: (command: string, routeOptions?: AIAssistantActionRouteOptions) => Promise<boolean | undefined | void>;
    refreshNews: () => void;
    cancelSession: () => Promise<CancelAIAssistantResult>;
    submitAgentView: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    dismissAgentView: (viewId: string | undefined, data?: Record<string, unknown>, options?: { force?: boolean }) => void | Promise<void>;
    /** Mark a record_audio card inactive so the mic UI does not re-open. */
    deactivateRecordingSession: (messageId: string) => void;
}

export interface AIAssistantPanelAppActions {
    onOpenOnboarding?: () => void;
    onOpenTutorial?: () => void;
    onTaskPrefsChanged?: () => void;
}

export interface AIAssistantPanelActionProps extends Partial<AIAssistantPanelHookActions>, AIAssistantPanelAppActions {}

export interface AIAssistantPanelWindowProps {
    inline?: boolean;
    maximized?: boolean;
    onToggleMaximize?: () => void;
    onHideWindow?: () => void;
}

export interface AIAssistantPanelProps {
    onClose: () => void;
    lang: string; // 'zh-Hans' | 'zh-Hant' | 'en'
    /** Start the embedded assistant on the redesigned workbench landing page.
     * Existing local conversation history remains available after the user
     * sends a new prompt or opens a concrete task from the sidebar. */
    startOnWorkbenchHome?: boolean;
    activeAssistantTask?: ActiveAssistantTaskIdentity | null;
    chatFontSize?: number;
    state: AIAssistantPanelStateProps;
    actions: AIAssistantPanelActionProps;
    window?: AIAssistantPanelWindowProps;
    groupDiscussion?: GroupDiscussionPanelControl;
    themeMode?: 'light' | 'dark';
    darkSchemeId?: AssistantDarkSchemeId;
    lightSchemeId?: AssistantLightSchemeId;
    onThemeModeChange?: (mode: 'light' | 'dark') => void;
    audioInputDeviceId?: string;
    audioOutputDeviceId?: string;
    petVoiceStartSeq?: number;
    petFocusInputSeq?: number;
    pendingVEOpen?: VirtualEmployeeEntry | null;
    onPendingVEOpenHandled?: () => void;
    pendingHistoryDiscussionOpen?: PendingHistoryDiscussionOpen | null;
    onPendingHistoryDiscussionOpenHandled?: () => void;
    pendingProjectTabOpen?: PendingProjectTabOpen | null;
    onPendingProjectTabOpenHandled?: (result: PendingProjectTabOpenResult) => void;
    pendingExpertOpen?: PendingExpertOpen | null;
    onPendingExpertOpenHandled?: () => void;
    onEnsureExpertTask?: (expert: ExpertDefinition, existing?: { relativePath?: string }) => Promise<EnsuredExpertTask | void> | EnsuredExpertTask | void;
    /** Persist every non-main assistant tab before it is opened. */
    onEnsureAssistantTabTask?: (tabType: string, tabIdentity: string, title: string, projectPath?: string) => Promise<void> | void;
    appUpdateAvailable?: AssistantUpdatePayload | null;
    onOpenAppUpdate?: () => void;
    onOpenAppReleaseNotes?: () => void;
    onDismissAppUpdate?: (latestVersion: string) => void;
    /** Bottom quick-settings bar: provider/model quick-switch data and language change handler. */
    availableProviders?: SidebarLLMProviderSummary[];
    currentModel?: string;
    /** Provider and model the in-flight turn will actually call. */
    contactProviderName?: string;
    contactModelId?: string;
    contactIsHubService?: boolean;
    modelOptions?: string[];
    modelsLoading?: boolean;
    onSwitchProvider?: (providerName: string) => void;
    onSwitchModel?: (modelId: string) => void;
    onOpenModelMenu?: () => void;
    onDismissModelMenu?: () => void;
    activeExecutionProfile?: AIExecutionProfile;
    codingInheritsAssistant?: boolean;
    providerSelectionPending?: boolean;
    onOpenLLMSettings?: () => void;
    /** Published from the active task's explicit execution metadata. */
    onActiveExecutionProfileChange?: (profile: AIExecutionProfile) => void;
    onLanguageChange?: (lang: string) => void;
    /**
     * Notifies the shell when project tabs open/close so the task list can block
     * removing tasks that still have an open tab.
     */
    onOpenProjectTabsChange?: (projectPaths: string[]) => void;
    /** Publishes stable identities so cloud workspaces survive cache-path changes. */
    onOpenProjectTabIdentitiesChange?: (identities: Array<{ projectPath: string; cloudWorkspaceId?: string }>) => void;
    /** Notifies the shell about open expert tabs so their durable task rows cannot be removed mid-session. */
    onOpenExpertTabsChange?: (expertIDs: string[]) => void;
    /**
     * The currently visible assistant tab's durable task identity.
     * Null when the local AI assistant (or any non-task tab) is active so the
     * sidebar highlight can clear.
     */
    onActiveAssistantTaskChange?: (identity: ActiveAssistantTaskIdentity | null) => void;
    /**
     * Live "running" signal for the currently visible tab: true while its task
     * is actively executing (agent loop streaming or workflow phase running),
     * mirroring the execution header badge. The sidebar merges this with the
     * durable task snapshot so in-progress runs are not misfiled as completed.
     */
    onActiveTaskRunningChange?: (running: boolean) => void;
    /**
     * Live "running" signal for EVERY assistant tab with an in-flight run, not
     * just the visible one: concurrent runs (a previous task still executing
     * after the user opens/switches to another) keep streaming as detached
     * rounds, and the sidebar needs each busy identity so all in-progress
     * tasks land in the 进行中 bucket instead of only the active tab's row.
     * Project paths and expert IDs are normalized, deduped and sorted.
     */
    onBusyTaskRunsChange?: (runs: { projectPaths: string[]; expertIds: string[] }) => void;
    /** Sidebar-visible task list mirrored by the header task switcher. */
    tasks?: TaskManagementItem[];
    /**
     * False until the first ListTasks settles. The orphan-tab reconcile stays
     * off while false so a slow initial load cannot prune legitimate tabs.
     * Undefined (standalone/test usage) means "treat as loaded".
     */
    tasksLoaded?: boolean;
    /** Opens a task-list row that has no open assistant tab yet. */
    onOpenTask?: (projectPath: string, task?: TaskManagementItem) => void;
    brandId?: string | null;
    brandDisplayNameCN?: string | null;
}

export type AIAssistantPanelCompatProps = AIAssistantPanelProps & AIAssistantPanelStateProps & AIAssistantPanelActionProps & AIAssistantPanelWindowProps;

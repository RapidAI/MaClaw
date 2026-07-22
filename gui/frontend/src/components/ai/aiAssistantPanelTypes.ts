import type { AIAssistantInitStatus, CancelAIAssistantResult, ChatMessage } from "./useAIAssistant";
import type { AgentView } from "./agentViewTypes";
export type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./groupDiscussionTypes";
import type { GroupDiscussionPanelControl } from "./groupDiscussionTypes";
import type { PendingHistoryDiscussionOpen, PendingProjectTabOpen, PendingExpertOpen } from "./usePendingAssistantTabOpen";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import type { AssistantUpdatePayload } from "./AssistantUpdateNotice";
import type { AssistantDarkSchemeId } from "./assistantDarkSchemes";
import type { AssistantLightSchemeId } from "./assistantLightSchemes";

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
    removeSelectedFile: (index: number) => void;
    sendMessage: (text: string, options?: Record<string, unknown>) => Promise<boolean>;
    sendBtwMessage: (query: string) => Promise<void>;
    sendMessageInBackground: (text: string) => Promise<void>;
    injectSupplementary: (text: string, sessionKey?: string) => Promise<boolean>;
    guideLaunchReference: (text: string, sessionKey?: string, launchId?: string) => Promise<boolean>;
    clearHistory: () => Promise<void>;
    recordSubmittedPrompt: (text: string) => void;
    setDraftInputValue: (text: string) => void;
    executeAction: (command: string) => Promise<boolean | undefined | void>;
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
    onPendingProjectTabOpenHandled?: () => void;
    pendingExpertOpen?: PendingExpertOpen | null;
    onPendingExpertOpenHandled?: () => void;
    appUpdateAvailable?: AssistantUpdatePayload | null;
    onOpenAppUpdate?: () => void;
    onDismissAppUpdate?: (latestVersion: string) => void;
    /**
     * Notifies the shell when project tabs open/close so the task list can block
     * removing tasks that still have an open tab.
     */
    onOpenProjectTabsChange?: (projectPaths: string[]) => void;
}

export type AIAssistantPanelCompatProps = AIAssistantPanelProps & AIAssistantPanelStateProps & AIAssistantPanelActionProps & AIAssistantPanelWindowProps;

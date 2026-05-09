import type { AIAssistantInitStatus, CancelAIAssistantResult, ChatMessage } from "./useAIAssistant";
import type { AgentView } from "./agentViewTypes";

export interface AIAssistantPanelStateProps {
    messages: ChatMessage[];
    progressMessages?: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    visualBusy?: boolean;
    ready: boolean;
    initStatus?: AIAssistantInitStatus;
    selectedFilePath?: string;
    selectedFilePaths?: string[];
    submittedPrompts?: string[];
    draftInputValue?: string;
    trialReflectEnabled?: boolean;
    scrollToTopSeq?: number;
    onboardingIncomplete?: boolean;
    showTraceEntry?: boolean;
    agentView?: AgentView | null;
}

export interface AIAssistantPanelActionProps {
    browseFile?: () => Promise<void>;
    clearSelectedFile?: () => void;
    removeSelectedFile?: (index: number) => void;
    sendMessage: (text: string) => Promise<void>;
    sendMessageInBackground?: (text: string) => Promise<void>;
    injectSupplementary?: (text: string) => Promise<boolean>;
    clearHistory: () => Promise<void>;
    recordSubmittedPrompt?: (text: string) => void;
    setDraftInputValue?: (text: string) => void;
    executeAction: (command: string) => Promise<void>;
    refreshNews: () => void;
    onOpenOnboarding?: () => void;
    cancelSession?: () => Promise<CancelAIAssistantResult>;
    onOpenTutorial?: () => void;
    onTaskPrefsChanged?: () => void;
    submitAgentView?: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    dismissAgentView?: (viewId: string | undefined) => void | Promise<void>;
}

export interface AIAssistantPanelWindowProps {
    inline?: boolean;
    maximized?: boolean;
    onToggleMaximize?: () => void;
    onHideWindow?: () => void;
}

export interface GroupDiscussionPanelStatus {
    enabled?: boolean;
    discoverable?: boolean;
    profile?: { agent_id?: string; display_name?: string } | null;
    experts?: Array<unknown>;
    discussions?: Array<any>;
    pending_invites?: Array<any>;
    active_discussion_count?: number;
    ready_discussion_count?: number;
    waiting_discussion_count?: number;
    stale_discussion_count?: number;
    recommended_focus_context?: Record<string, unknown>;
    recommended_tool_call?: Record<string, unknown>;
    non_executing_boundary?: string;
    error?: string;
}

export interface GroupDiscussionPanelControl {
    config?: any;
    status?: GroupDiscussionPanelStatus | null;
    onRefreshStatus?: () => void | Promise<void>;
    onPublishProfile?: () => void | Promise<void>;
    onAcceptInvite?: (inviteId: string) => void | Promise<void>;
    onRejectInvite?: (inviteId: string) => void | Promise<void>;
    onOpenExperienceTrace?: (focus?: string) => void;
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
    onThemeModeChange?: (mode: 'light' | 'dark') => void;
    audioInputDeviceId?: string;
    audioOutputDeviceId?: string;
    petVoiceStartSeq?: number;
    petFocusInputSeq?: number;
}

/**
 * AI Assistant Panel Tab System Types
 *
 * Defines the tab data model for the multi-conversation AI panel.
 * - First tab (id="local", type="local") is always present and not closable.
 * - Digital employee tabs have type="ve" and include veId.
 * - Group tabs have type="group" and include participants array.
 * - Expert tabs have type="expert" and include expertId (title holds the expert name).
 * - Max 8 digital employee tabs (configurable via maxVETabs).
 */
/** Tab type discriminator */
export type AITabType = "local" | "ve" | "group" | "project" | "expert";
/**
 * The persisted execution scope for profile-scoped model controls. This is
 * intentionally task metadata, never inferred from prompt content or visible
 * UI. `none` means an older/ambiguous task and disables quick writes.
 */
export type AIExecutionProfile = "assistant" | "coding" | "none";
/** A single tab in the AI Assistant Panel */
export interface AITab {
    /** Unique tab identifier. "local" for the fixed AI assistant tab. */
    id: string;
    /** Tab type: local AI assistant, VE conversation, or group chat */
    type: AITabType;
    /** Profile whose new requests this task is allowed to create. */
    executionProfile?: AIExecutionProfile;
    /** Primary conversation title. For VE/group tabs this remains the primary VE name or history topic. */
    title: string;
    /** User-defined title for the fixed local AI assistant tab. */
    customTitle?: string;
    /** Explicit group topic/name, separate from participant names. */
    groupTitle?: string;
    /** Digital Employee ID (only for type="ve") */
    veId?: string;
    /** Group chat participant IDs (only for type="group") */
    participants?: string[];
    /** Display names for group chat participants keyed by participant ID */
    participantNames?: Record<string, string>;
    /** Canonical participant IDs that represent the local AI in this group tab */
    localParticipantIds?: string[];
    /** Hub discussion/consultation ID for group history tabs */
    discussionId?: string;
    /** Whether this group history tab is view-only */
    readOnly?: boolean;
    /** Local relationship to the discussion, e.g. initiated or participated */
    role?: string;
    /** Bound project path (required when type="project") */
    projectPath?: string;
    /** Exact external ACP owner. It is unique even when projectPath is shared. */
    sessionKey?: string;
    /** AI expert ID (required when type="expert"); title carries the expert name. */
    expertId?: string;
    /** Expert emoji badge (only for type="expert"). */
    expertIcon?: string;
    /** Short expert description (only for type="expert"; used by the empty-tab intro). */
    expertDescription?: string;
    /**
     * Agent execution mode for project tabs.
     * "coding_dev" — local pure coding environment.
     * "remote_coding_dev" — SSH remote pure coding with right-hand source preview.
     */
    agentMode?: "coding_dev" | "remote_coding_dev";
    /** Optional remote host label for remote_coding_dev tabs (display only). */
    remoteHost?: string;
    /** Remote incident diagnosis: keep the first SSH turn evidence-only. */
    remoteSafety?: "diagnosis";
    /** Remote coding: SSH session missing/expired; show reconnect form. */
    remoteNeedsReconnect?: boolean;
    /** Whether this tab is archived (read-only mode) */
    archived?: boolean;
    /** Online status of the VE (only for type="ve"). Updated via ve:status_change events. */
    onlineStatus?: "online" | "offline";
    /** Safe data URL avatar for a digital employee tab. */
    avatarDataURL?: string;
    /** Short skill/profile description for local-only intro copy. */
    veSkillDescription?: string;
    /** Bumped when the visible conversation should clear and start a fresh session. */
    conversationResetSeq?: number;
    /** Whether this tab can be closed. The local tab is always false. */
    closable: boolean;
}
/** Preserved state for an inactive tab */
export interface AITabState {
    /** Conversation history messages */
    history: unknown[];
    /** Scroll position (pixels from top) */
    scrollTop: number;
    /** Input text draft */
    inputText: string;
    /** A2A session ID (for VE/group tabs) */
    sessionId?: string;
    /** Hub discussion/consultation ID for group history tabs */
    discussionId?: string;
    /** Whether this group history tab is view-only */
    readOnly?: boolean;
    /** Bound project path (redundant storage for persistence recovery) */
    projectPath?: string;
    /** Last active timestamp (ms since epoch), used for overflow sorting and cleanup */
    lastActiveAt?: number;
    /** One-shot IM completion route for a task opened by Hub /startmenu. */
    pendingIMCompletion?: {
        platform: string;
        targetUID: string;
		isGroup?: boolean;
        taskTitle: string;
    };
    /** Initial IM prompt held locally until remote SSH is ready. */
    pendingRemoteInitialMessage?: {
        text: string;
    };
}
/** Overall state of the AI Assistant Panel tab system */
export interface AIAssistantPanelTabState {
    /** Ordered list of tabs. First tab is always the local AI assistant. */
    tabs: AITab[];
    /** ID of the currently active (visible) tab */
    activeTabId: string;
    /** Maximum number of digital employee tabs allowed (default 8) */
    maxVETabs: number;
}
/** Default max digital employee tabs */
export const DEFAULT_MAX_VE_TABS = 8;
/**
 * Fixed local AI assistant tab (main session surface).
 * `title` is a persistence/default fallback only — UI must render via
 * `localAssistantTabTitle(lang)` / `getAITabDisplayTitle` so English never sticks on Chinese
 * unless the user explicitly supplied `customTitle`.
 */
export const LOCAL_TAB: AITab = {
    id: "local",
    type: "local",
    title: "AI \u52a9\u624b",
    executionProfile: "assistant",
    closable: false,
};
/** Create initial tab state with only the local tab (cloned so mutations cannot corrupt LOCAL_TAB). */
export function createInitialTabState(maxVETabs = DEFAULT_MAX_VE_TABS): AIAssistantPanelTabState {
    return {
        tabs: [{ ...LOCAL_TAB }],
        activeTabId: "local",
        maxVETabs,
    };
}

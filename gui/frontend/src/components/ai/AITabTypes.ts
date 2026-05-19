/**
 * AI Assistant Panel Tab System Types
 *
 * Defines the tab data model for the multi-conversation AI panel.
 * - First tab (id="local", type="local") is always present and not closable.
 * - Digital employee tabs have type="ve" and include veId.
 * - Group tabs have type="group" and include participants array.
 * - Max 8 digital employee tabs (configurable via maxVETabs).
 */
/** Tab type discriminator */
export type AITabType = "local" | "ve" | "group" | "project";
/** A single tab in the AI Assistant Panel */
export interface AITab {
    /** Unique tab identifier. "local" for the fixed AI assistant tab. */
    id: string;
    /** Tab type: local AI assistant, VE conversation, or group chat */
    type: AITabType;
    /** Display title shown in the tab bar */
    title: string;
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
    /** Whether this tab is archived (read-only mode) */
    archived?: boolean;
    /** Online status of the VE (only for type="ve"). Updated via ve:status_change events. */
    onlineStatus?: "online" | "offline";
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
/** The fixed local AI assistant tab */
export const LOCAL_TAB: AITab = {
    id: "local",
    type: "local",
    title: "AI \u52a9\u624b",
    closable: false,
};
/** Create initial tab state with only the local tab */
export function createInitialTabState(maxVETabs = DEFAULT_MAX_VE_TABS): AIAssistantPanelTabState {
    return {
        tabs: [LOCAL_TAB],
        activeTabId: "local",
        maxVETabs,
    };
}

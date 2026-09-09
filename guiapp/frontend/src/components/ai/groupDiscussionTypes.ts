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

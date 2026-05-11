import type { GroupDiscussionPanelStatus } from "./groupDiscussionTypes";

export function buildGroupDiscussionStatusSafeHandoff(status: GroupDiscussionPanelStatus | null | undefined, primaryTraceFocus: string): string {
    if (!status) return "";
    const backendFocus = status.recommended_focus_context;
    const backendToolCall = status.recommended_tool_call;
    const backendBoundary = String(status.non_executing_boundary || "").trim();
    const pendingInvites = Array.isArray(status.pending_invites) ? status.pending_invites : [];
    const firstInvite = pendingInvites[0] as Record<string, unknown> | undefined;
    const focusContext = backendFocus || {
        action_kind: "inspect_group_discussion_status",
        reason: "read-only A2A status inspection from the assistant title bar",
        enabled: Boolean(status.enabled),
        discoverable: Boolean(status.discoverable),
        expert_count: Array.isArray(status.experts) ? status.experts.length : 0,
        discussion_count: Array.isArray(status.discussions) ? status.discussions.length : 0,
        active_count: status.active_discussion_count || 0,
        ready_count: status.ready_discussion_count || 0,
        waiting_count: status.waiting_discussion_count || 0,
        stale_count: status.stale_discussion_count || 0,
        pending_invite_count: pendingInvites.length,
        primary_trace_focus: primaryTraceFocus || "",
    };
    if (!backendFocus && firstInvite && !primaryTraceFocus) addInviteRecommendation(focusContext, firstInvite);
    const targetDiscussionID = primaryTraceFocus.replace(/^discussion:/, "");
    const recommendedToolCall = backendToolCall || {
        tool: "group_discussion",
        args: targetDiscussionID ? { action: "get_detail", consultation_id: targetDiscussionID } : { action: "status" },
        recommended_focus_context: focusContext,
        discussion_focus_context: focusContext,
        non_executing: true,
        non_executing_boundary: "recommended status follow-up only; it may inspect discussion detail or repeat status, and must not start a discussion, invite experts, send messages, mutate Hub state, mutate memory, or change routing",
    };
    const boundary = backendBoundary || "read-only group discussion status inspection; no discussion was started, no experts were invited, no messages were sent, no Hub state changed, no memory was promoted, and no routing changed";
    return [
        "recommended_focus_context:\n" + safeStringify(focusContext),
        "recommended_tool_call:\n" + safeStringify(recommendedToolCall),
        "non_executing_boundary:\n" + boundary,
    ].join("\n\n");
}

function addInviteRecommendation(focusContext: Record<string, unknown>, firstInvite: Record<string, unknown>) {
    focusContext.recommended_action_kind = "review_pending_invites";
    const inviteID = String(firstInvite.id || firstInvite.invite_id || "").trim();
    const sessionID = String(firstInvite.session_id || firstInvite.consultation_id || "").trim();
    const fromID = String(firstInvite.from_id || firstInvite.from_name || "").trim();
    const role = String(firstInvite.role || "").trim();
    const topic = String(firstInvite.topic || "").trim();
    if (inviteID) focusContext.recommended_invite_id = inviteID;
    if (sessionID) focusContext.recommended_consultation_id = sessionID;
    if (fromID) focusContext.recommended_from_id = fromID;
    if (role) focusContext.recommended_role = role;
    if (topic) focusContext.recommended_topic = topic;
}

function safeStringify(value: unknown): string {
    try {
        return JSON.stringify(value, null, 2);
    } catch {
        return String(value);
    }
}

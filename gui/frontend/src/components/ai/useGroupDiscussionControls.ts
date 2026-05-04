import { useCallback, useMemo, useState } from "react";
import type { GroupDiscussionPanelControl } from "./aiAssistantPanelTypes";
import { localizeText } from "./aiAssistantI18n";

export function useGroupDiscussionControls(groupDiscussion: GroupDiscussionPanelControl | undefined, inline: boolean | undefined, lang: string) {
    const [groupDiscussionOpen, setGroupDiscussionOpen] = useState(false);
    const [groupDiscussionBusy, setGroupDiscussionBusy] = useState("");

    const state = useMemo(() => {
        const groupDiscussionStatus = groupDiscussion?.status;
        const groupDiscussionConfig = groupDiscussion?.config || {};
        const groupDiscussionEnabled = groupDiscussionStatus?.enabled ?? groupDiscussionConfig.enabled ?? groupDiscussionConfig.group_discussion_enabled ?? false;
        const groupDiscussionDiscoverable = groupDiscussionStatus?.discoverable ?? groupDiscussionConfig.discoverable ?? groupDiscussionConfig.group_discussion_discoverable ?? false;
        const groupPendingInvites = groupDiscussionStatus?.pending_invites || [];
        const groupReadyTalks = groupDiscussionStatus?.ready_discussion_count ?? 0;
        const groupWaitingTalks = groupDiscussionStatus?.waiting_discussion_count ?? 0;
        const groupActiveTalks = groupDiscussionStatus?.active_discussion_count ?? 0;
        const groupStaleTalks = groupDiscussionStatus?.stale_discussion_count ?? 0;
        const groupDiscussionLabel = lang === "en"
            ? (groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "Group Listed" : "Group Private") : "Group Off")
            : (groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "\u7fa4\u7ec4\u53ef\u89c1" : "\u7fa4\u7ec4\u79c1\u5bc6") : "\u7fa4\u7ec4\u5173\u95ed");
        const groupDiscussionScopeText = localizeText(lang, "Current Hub only", "\u4ec5\u5f53\u524d Hub");
        return {
            groupActiveTalks,
            groupDiscussionConfig,
            groupDiscussionDiscoverable,
            groupDiscussionEnabled,
            groupDiscussionLabel,
            groupDiscussionScopeText,
            groupDiscussionStatus,
            groupPendingInvites,
            groupReadyTalks,
            groupStaleTalks,
            groupWaitingTalks,
        };
    }, [groupDiscussion, lang]);

    const runGroupDiscussionAction = useCallback(async (name: string, action?: () => void | Promise<void>) => {
        if (!action || groupDiscussionBusy) return;
        setGroupDiscussionBusy(name);
        try {
            await action();
        } finally {
            setGroupDiscussionBusy("");
        }
    }, [groupDiscussionBusy]);

    const bindGroupDiscussionPress = useCallback((handler: () => void) => {
        if (!inline) return { onClick: handler };
        return {
            onMouseDown: (event: React.MouseEvent) => {
                event.preventDefault();
                event.stopPropagation();
                handler();
            },
        };
    }, [inline]);

    return { bindGroupDiscussionPress, groupDiscussionBusy, groupDiscussionOpen, runGroupDiscussionAction, setGroupDiscussionOpen, ...state };
}

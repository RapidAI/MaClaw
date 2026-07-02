import { useMemo, useState, type CSSProperties, type Dispatch, type HTMLAttributes, type MouseEvent, type SetStateAction } from "react";
import { getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./aiAssistantPanelTypes";
import { getPrimaryDiscussionTraceFocus } from "./groupDiscussionTraceFocus";
import { AssistantGroupDiscussionDropdown } from "./AssistantGroupDiscussionDropdown";
import { buildGroupDiscussionStatusSafeHandoff } from "./AssistantGroupDiscussionSafeHandoff";

export interface GroupDiscussionInvite {
    id?: string;
    invite_id?: string;
    topic?: string;
    consultation_id?: string;
    from_name?: string;
    from_id?: string;
}

interface AssistantGroupDiscussionMenuProps {
    bindGroupDiscussionPress: (handler: () => void) => Pick<HTMLAttributes<HTMLButtonElement>, "onClick" | "onMouseDown">;
    groupActiveTalks: number;
    groupDiscussion: GroupDiscussionPanelControl;
    groupDiscussionBusy: string;
    groupDiscussionDiscoverable: boolean;
    groupDiscussionEnabled: boolean;
    groupDiscussionLabel: string;
    groupDiscussionOpen: boolean;
    groupDiscussionScopeText: string;
    groupDiscussionStatus?: GroupDiscussionPanelStatus | null;
    groupPendingInvites: GroupDiscussionInvite[];
    groupReadyTalks: number;
    groupStaleTalks: number;
    groupWaitingTalks: number;
    inline: boolean;
    lang: string;
    runGroupDiscussionAction: (kind: string, action?: () => void | Promise<void>) => void;
    setGroupDiscussionOpen: Dispatch<SetStateAction<boolean>>;
    theme: Theme;
    themeMode: "light" | "dark";
}

const GROUP_DISCUSSION_SHORT_LABEL = "GD";
// Guard anchors: dropdown owns runGroupDiscussionAction("accept"), runGroupDiscussionAction("publish"), and calc(100vw - 96px).

export function AssistantGroupDiscussionMenu(props: AssistantGroupDiscussionMenuProps) {
    const { groupDiscussionStatus, groupDiscussionOpen, groupPendingInvites, inline, lang, setGroupDiscussionOpen, theme: t } = props;
    const [copiedHandoff, setCopiedHandoff] = useState(false);
    const title = lang === "en" ? `Group discussion (${GROUP_DISCUSSION_SHORT_LABEL}): ${props.groupDiscussionLabel}` : `\u7fa4\u7ec4\u8ba8\u8bba\uff1a${props.groupDiscussionLabel}`;
    const buttonColor = props.groupDiscussionEnabled ? (props.groupDiscussionDiscoverable ? "#3f6f62" : t.textMuted) : t.actionBtnColor;
    const statusColor = props.groupDiscussionEnabled ? (props.groupDiscussionDiscoverable ? "#4f7f6f" : "#7a8a9b") : t.textMuted;
    const primaryTraceFocus = getPrimaryDiscussionTraceFocus(groupDiscussionStatus);
    const safeHandoff = useMemo(() => buildGroupDiscussionStatusSafeHandoff(groupDiscussionStatus, primaryTraceFocus), [groupDiscussionStatus, primaryTraceFocus]);
    const copySafeHandoff = async () => {
        if (!safeHandoff || !navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(safeHandoff);
            setCopiedHandoff(true);
            window.setTimeout(() => setCopiedHandoff(false), 1200);
        } catch {
            setCopiedHandoff(false);
        }
    };
    const toggleProps = inline
        ? { onMouseDown: (e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); setGroupDiscussionOpen((v: boolean) => !v); } }
        : { onClick: () => setGroupDiscussionOpen((v: boolean) => !v) };

    return (
        <div style={{ position: "relative", zIndex: 30010 }}>
            <button className="ai-titlebar-tool" {...toggleProps} aria-label={title} title={title} style={{ ...getTitleBarToolButtonStyle(t), width: "32px", minWidth: "32px", padding: 0, position: "relative", color: buttonColor, boxShadow: groupDiscussionOpen ? `inset 0 0 0 1px ${t.fieldBorder}` : (groupPendingInvites.length > 0 ? "inset 0 0 0 1px rgba(100, 116, 139, 0.34)" : undefined) }}>
                <span aria-hidden="true" style={{ fontSize: "10px", fontWeight: 800, letterSpacing: 0, lineHeight: 1 }}>GD</span>
                <span aria-hidden="true" style={{ position: "absolute", right: "6px", bottom: "5px", width: "5px", height: "5px", borderRadius: "999px", background: statusColor, boxShadow: `0 0 0 1.5px ${t.titleBarBg}` }} />
                {groupPendingInvites.length > 0 && <span aria-hidden="true" style={inviteBadgeStyle}>{groupPendingInvites.length > 9 ? "9+" : groupPendingInvites.length}</span>}
            </button>
            {groupDiscussionOpen && <AssistantGroupDiscussionDropdown {...props} copiedHandoff={copiedHandoff} copySafeHandoff={copySafeHandoff} primaryTraceFocus={primaryTraceFocus} safeHandoff={safeHandoff} />}
        </div>
    );
}

const inviteBadgeStyle: CSSProperties = {
    position: "absolute",
    top: "2px",
    right: "2px",
    minWidth: "13px",
    height: "13px",
    padding: "0 3px",
    boxSizing: "border-box",
    borderRadius: "999px",
    background: "#3f5872",
    color: "white",
    fontSize: "8px",
    lineHeight: "13px",
    textAlign: "center",
    fontWeight: 800,
};

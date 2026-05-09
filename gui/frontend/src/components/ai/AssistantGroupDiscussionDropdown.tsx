import type { CSSProperties, Dispatch, HTMLAttributes, SetStateAction } from "react";
import { miniActionButtonStyle } from "./aiAssistantControls";
import type { Theme } from "./aiAssistantPanelTheme";
import type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./aiAssistantPanelTypes";
import type { GroupDiscussionInvite } from "./AssistantGroupDiscussionMenu";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "no-drag" };
const wailsDragStyle = (style: WailsDragStyle): CSSProperties => style;

interface Props {
    bindGroupDiscussionPress: (handler: () => void) => Pick<HTMLAttributes<HTMLButtonElement>, "onClick" | "onMouseDown">;
    copiedHandoff: boolean;
    copySafeHandoff: () => void;
    groupActiveTalks: number;
    groupDiscussion: GroupDiscussionPanelControl;
    groupDiscussionBusy: string;
    groupDiscussionEnabled: boolean;
    groupDiscussionScopeText: string;
    groupDiscussionStatus?: GroupDiscussionPanelStatus | null;
    groupPendingInvites: GroupDiscussionInvite[];
    groupReadyTalks: number;
    groupStaleTalks: number;
    groupWaitingTalks: number;
    lang: string;
    primaryTraceFocus: string;
    runGroupDiscussionAction: (kind: string, action?: () => void | Promise<void>) => void;
    safeHandoff: string;
    setGroupDiscussionOpen: Dispatch<SetStateAction<boolean>>;
    theme: Theme;
    themeMode: "light" | "dark";
}

export function AssistantGroupDiscussionDropdown(props: Props) {
    const { bindGroupDiscussionPress, groupDiscussion, groupDiscussionStatus, groupPendingInvites, lang, theme: t, themeMode } = props;
    const actionGridColumns = groupDiscussion.onOpenExperienceTrace ? "repeat(3, minmax(0, 1fr))" : "repeat(2, minmax(0, 1fr))";
    const dangerTextColor = themeMode === "dark" ? "#fbbf24" : "#b91c1c";
    const dangerBorderColor = themeMode === "dark" ? "rgba(251, 191, 36, 0.40)" : "#fecaca";
    return (
        <div style={wailsDragStyle({ position: "absolute", right: 0, top: "30px", width: "min(280px, calc(100vw - 96px))", maxWidth: "calc(100vw - 96px)", padding: "12px", borderRadius: "12px", border: `1px solid ${t.titleBarBorder}`, background: themeMode === "dark" ? "#0f172a" : t.bg, boxShadow: themeMode === "dark" ? "0 22px 60px rgba(0, 0, 0, 0.72), 0 0 0 1px rgba(148, 163, 184, 0.16)" : "0 18px 45px rgba(15, 23, 42, 0.18)", color: t.text, zIndex: 30020, "--wails-draggable": "no-drag" })}>
            <Header {...props} />
            <Stats {...props} />
            {(props.groupActiveTalks > 0 || props.groupReadyTalks > 0 || props.groupWaitingTalks > 0 || props.groupStaleTalks > 0) && <div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "8px", padding: "7px", borderRadius: "9px", background: themeMode === "dark" ? "rgba(148, 163, 184, 0.12)" : "rgba(15, 23, 42, 0.04)" }}>{lang === "en" ? `Active ${props.groupActiveTalks} \u00b7 Ready ${props.groupReadyTalks} \u00b7 Waiting ${props.groupWaitingTalks} \u00b7 Stale ${props.groupStaleTalks}` : `\u8fdb\u884c\u4e2d ${props.groupActiveTalks} \u00b7 \u53ef\u6536\u5c3e ${props.groupReadyTalks} \u00b7 \u7b49\u5f85 ${props.groupWaitingTalks} \u00b7 \u8d85\u65f6 ${props.groupStaleTalks}`}</div>}
            {groupDiscussionStatus?.error && <div style={{ fontSize: "11px", color: dangerTextColor, marginBottom: "8px" }}>{String(groupDiscussionStatus.error)}</div>}
            {props.safeHandoff && <SafeHandoff {...props} />}
            {groupPendingInvites.slice(0, 2).map((invite) => <InviteRow key={invite.invite_id || invite.id} invite={invite} {...props} dangerTextColor={dangerTextColor} dangerBorderColor={dangerBorderColor} />)}
            <div style={{ display: "grid", gridTemplateColumns: actionGridColumns, gap: "6px", marginTop: "10px" }}>
                <button type="button" style={actionButtonStyle(t, themeMode, !props.groupDiscussionBusy)} disabled={!!props.groupDiscussionBusy} {...bindGroupDiscussionPress(() => props.runGroupDiscussionAction("refresh", groupDiscussion.onRefreshStatus))}>{props.groupDiscussionBusy === "refresh" ? (lang === "en" ? "Refreshing..." : "\u5237\u65b0\u4e2d...") : (lang === "en" ? "Refresh" : "\u5237\u65b0")}</button>
                <button type="button" style={actionButtonStyle(t, themeMode, !props.groupDiscussionBusy && props.groupDiscussionEnabled)} disabled={!!props.groupDiscussionBusy || !props.groupDiscussionEnabled} {...bindGroupDiscussionPress(() => props.runGroupDiscussionAction("publish", groupDiscussion.onPublishProfile))}>{props.groupDiscussionBusy === "publish" ? (lang === "en" ? "Publishing..." : "\u53d1\u5e03\u4e2d...") : (lang === "en" ? "Publish" : "\u53d1\u5e03\u8eab\u4efd")}</button>
                {groupDiscussion.onOpenExperienceTrace && <button type="button" style={actionButtonStyle(t, themeMode, true)} {...bindGroupDiscussionPress(() => { groupDiscussion.onOpenExperienceTrace?.(props.primaryTraceFocus); props.setGroupDiscussionOpen(false); })}>{lang === "en" ? "Experience" : "\u7ecf\u9a8c"}</button>}
            </div>
        </div>
    );
}

function Header({ bindGroupDiscussionPress, groupDiscussionScopeText, lang, setGroupDiscussionOpen, theme: t, themeMode }: Props) {
    return <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px", marginBottom: "8px" }}><div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: "2px" }}><strong style={{ fontSize: "12px" }}>{lang === "en" ? "Group Discussion" : "\u7fa4\u7ec4\u8ba8\u8bba"}</strong><span style={{ fontSize: "10px", color: t.textMuted }}>{groupDiscussionScopeText}</span></div><button type="button" {...bindGroupDiscussionPress(() => setGroupDiscussionOpen(false))} aria-label={lang === "en" ? "Close group discussion panel" : "\u5173\u95ed\u7fa4\u7ec4\u8ba8\u8bba\u9762\u677f"} title={lang === "en" ? "Close" : "\u5173\u95ed"} style={wailsDragStyle({ width: "22px", height: "22px", minWidth: "22px", borderRadius: "999px", border: `1px solid ${themeMode === "dark" ? "rgba(148, 163, 184, 0.28)" : "rgba(148, 163, 184, 0.24)"}`, background: themeMode === "dark" ? "rgba(15, 23, 42, 0.88)" : "rgba(255, 255, 255, 0.9)", color: t.textMuted, cursor: "pointer", display: "inline-flex", alignItems: "center", justifyContent: "center", padding: 0, fontSize: "14px", lineHeight: 1, fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif", flexShrink: 0, "--wails-draggable": "no-drag" })}><span aria-hidden="true">&times;</span></button></div>;
}

function Stats({ groupDiscussionStatus, groupPendingInvites, lang, theme: t, themeMode }: Props) {
    const items = [[lang === "en" ? "Experts" : "\u4e13\u5bb6", groupDiscussionStatus?.experts?.length ?? 0], [lang === "en" ? "Talks" : "\u8ba8\u8bba", groupDiscussionStatus?.discussions?.length ?? 0], [lang === "en" ? "Invites" : "\u9080\u8bf7", groupPendingInvites.length]];
    return <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: "6px", marginBottom: "10px" }}>{items.map(([label, value]) => <div key={String(label)} style={{ padding: "7px", borderRadius: "9px", background: themeMode === "dark" ? "rgba(148, 163, 184, 0.14)" : "rgba(148, 163, 184, 0.10)", textAlign: "center", minWidth: 0 }}><div style={{ fontSize: "14px", fontWeight: 700 }}>{value}</div><div style={{ fontSize: "10px", color: t.textMuted }}>{label}</div></div>)}</div>;
}

function SafeHandoff({ bindGroupDiscussionPress, copiedHandoff, copySafeHandoff, lang, safeHandoff, theme: t, themeMode }: Props) {
    return <div style={{ marginBottom: "8px", padding: "7px", borderRadius: "9px", background: themeMode === "dark" ? "rgba(34, 197, 94, 0.10)" : "rgba(16, 185, 129, 0.08)", border: `1px solid ${themeMode === "dark" ? "rgba(34, 197, 94, 0.22)" : "rgba(16, 185, 129, 0.22)"}` }}><div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "6px", marginBottom: "5px" }}><strong style={{ fontSize: "10px", color: themeMode === "dark" ? "#86efac" : "#047857" }}>{lang === "en" ? "Safe Handoff" : "\u5b89\u5168\u4ea4\u63a5"}</strong><button type="button" style={{ ...miniActionButtonStyle, padding: "3px 6px", fontSize: "9px", background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder }} {...bindGroupDiscussionPress(copySafeHandoff)}>{copiedHandoff ? (lang === "en" ? "Copied" : "\u5df2\u590d\u5236") : (lang === "en" ? "Copy" : "\u590d\u5236")}</button></div><pre style={{ margin: 0, whiteSpace: "pre-wrap", wordBreak: "break-word", maxHeight: "92px", overflow: "auto", fontSize: "9px", lineHeight: 1.45, color: t.textMuted }}>{safeHandoff}</pre></div>;
}

function InviteRow(props: Props & { invite: GroupDiscussionInvite; dangerTextColor: string; dangerBorderColor: string }) {
    const { bindGroupDiscussionPress, groupDiscussion, groupDiscussionBusy, invite, lang, runGroupDiscussionAction, theme: t, themeMode } = props;
    const inviteID = invite.invite_id || invite.id || "";
    return <div style={{ padding: "8px 0", borderTop: `1px solid ${t.divider}` }}><div style={{ fontSize: "11px", fontWeight: 600, marginBottom: "2px" }}>{invite.topic || invite.consultation_id || (lang === "en" ? "Discussion invite" : "\u8ba8\u8bba\u9080\u8bf7")}</div><div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "6px" }}>{invite.from_name || invite.from_id || "MaClaw"}</div><div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "6px" }}><button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: themeMode === "dark" ? "#86efac" : "#047857", borderColor: themeMode === "dark" ? "rgba(134, 239, 172, 0.45)" : "#86efac" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("accept", () => groupDiscussion.onAcceptInvite?.(inviteID)))}>{lang === "en" ? "Accept" : "\u63a5\u53d7"}</button><button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: props.dangerTextColor, borderColor: props.dangerBorderColor }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("reject", () => groupDiscussion.onRejectInvite?.(inviteID)))}>{lang === "en" ? "Reject" : "\u62d2\u7edd"}</button></div></div>;
}

function actionButtonStyle(t: Theme, themeMode: "light" | "dark", enabled: boolean): CSSProperties {
    return { ...miniActionButtonStyle, background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder, opacity: enabled ? 1 : (themeMode === "dark" ? 0.68 : 0.55), cursor: enabled ? "pointer" : "default" };
}

import type { CSSProperties, Dispatch, HTMLAttributes, MouseEvent, SetStateAction } from "react";
import { localizeText } from "./aiAssistantI18n";
import { getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { miniActionButtonStyle } from "./aiAssistantControls";
import type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./aiAssistantPanelTypes";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "no-drag" };
const wailsDragStyle = (style: WailsDragStyle): CSSProperties => style;

interface GroupDiscussionInvite {
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

export function AssistantGroupDiscussionMenu({ bindGroupDiscussionPress, groupActiveTalks, groupDiscussion, groupDiscussionBusy, groupDiscussionDiscoverable, groupDiscussionEnabled, groupDiscussionLabel, groupDiscussionOpen, groupDiscussionScopeText, groupDiscussionStatus, groupPendingInvites, groupReadyTalks, groupStaleTalks, groupWaitingTalks, inline, lang, runGroupDiscussionAction, setGroupDiscussionOpen, theme: t, themeMode }: AssistantGroupDiscussionMenuProps) {
    return (
        <div style={{ position: "relative", zIndex: 30010 }}>
            <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: (e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); setGroupDiscussionOpen((v: boolean) => !v); } } : { onClick: () => setGroupDiscussionOpen((v: boolean) => !v) })} style={{ ...getTitleBarToolButtonStyle(t), width: "auto", minWidth: "72px", padding: "0 8px", gap: "5px", color: groupDiscussionEnabled ? (groupDiscussionDiscoverable ? "#047857" : "#92400e") : t.actionBtnColor, borderColor: groupPendingInvites.length > 0 ? "#f59e0b" : undefined }} title={lang === "en" ? "Group discussion" : "\u7fa4\u7ec4\u8ba8\u8bba"}>
                <span aria-hidden="true" style={{ fontSize: "13px", lineHeight: 1 }}>GD</span>
                <span style={{ fontSize: "10px", lineHeight: 1, whiteSpace: "nowrap" }}>{groupDiscussionLabel}</span>
                {groupPendingInvites.length > 0 && <span style={{ minWidth: "14px", height: "14px", borderRadius: "999px", background: "#f59e0b", color: "white", fontSize: "9px", lineHeight: "14px", textAlign: "center", fontWeight: 700 }}>{groupPendingInvites.length > 9 ? "9+" : groupPendingInvites.length}</span>}
            </button>
            {groupDiscussionOpen && (
                <div style={wailsDragStyle({ position: "absolute", right: 0, top: "30px", width: "min(280px, calc(100vw - 96px))", maxWidth: "calc(100vw - 96px)", padding: "12px", borderRadius: "12px", border: `1px solid ${t.titleBarBorder}`, background: themeMode === "dark" ? "#0f172a" : t.bg, boxShadow: themeMode === "dark" ? "0 22px 60px rgba(0, 0, 0, 0.72), 0 0 0 1px rgba(148, 163, 184, 0.16)" : "0 18px 45px rgba(15, 23, 42, 0.18)", color: t.text, zIndex: 30020, "--wails-draggable": "no-drag" })}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px", marginBottom: "8px" }}>
                        <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: "2px" }}>
                            <strong style={{ fontSize: "12px" }}>{lang === "en" ? "Group Discussion" : "\u7fa4\u7ec4\u8ba8\u8bba"}</strong>
                            <span style={{ fontSize: "10px", color: t.textMuted }}>{groupDiscussionScopeText}</span>
                        </div>
                        <button type="button" {...bindGroupDiscussionPress(() => setGroupDiscussionOpen(false))} aria-label={lang === "en" ? "Close group discussion panel" : "\u5173\u95ed\u7fa4\u7ec4\u8ba8\u8bba\u9762\u677f"} title={lang === "en" ? "Close" : "\u5173\u95ed"} style={wailsDragStyle({ width: "22px", height: "22px", minWidth: "22px", borderRadius: "999px", border: `1px solid ${themeMode === "dark" ? "rgba(148, 163, 184, 0.28)" : "rgba(148, 163, 184, 0.24)"}`, background: themeMode === "dark" ? "rgba(15, 23, 42, 0.88)" : "rgba(255, 255, 255, 0.9)", color: t.textMuted, cursor: "pointer", display: "inline-flex", alignItems: "center", justifyContent: "center", padding: 0, fontSize: "14px", lineHeight: 1, fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif", flexShrink: 0, "--wails-draggable": "no-drag" })}>
                            <span aria-hidden="true">&times;</span>
                        </button>
                    </div>
                    <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: "6px", marginBottom: "10px" }}>
                        {[[lang === "en" ? "Experts" : "\u4e13\u5bb6", groupDiscussionStatus?.experts?.length ?? 0], [lang === "en" ? "Talks" : "\u8ba8\u8bba", groupDiscussionStatus?.discussions?.length ?? 0], [lang === "en" ? "Invites" : "\u9080\u8bf7", groupPendingInvites.length]].map(([label, value]) => (
                            <div key={String(label)} style={{ padding: "7px", borderRadius: "9px", background: themeMode === "dark" ? "rgba(148, 163, 184, 0.14)" : "rgba(148, 163, 184, 0.10)", textAlign: "center", minWidth: 0 }}>
                                <div style={{ fontSize: "14px", fontWeight: 700 }}>{value}</div>
                                <div style={{ fontSize: "10px", color: t.textMuted }}>{label}</div>
                            </div>
                        ))}
                    </div>
                    {(groupActiveTalks > 0 || groupReadyTalks > 0 || groupWaitingTalks > 0 || groupStaleTalks > 0) && <div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "8px", padding: "7px", borderRadius: "9px", background: themeMode === "dark" ? "rgba(148, 163, 184, 0.12)" : "rgba(15, 23, 42, 0.04)" }}>{lang === "en" ? `Active ${groupActiveTalks} \u00b7 Ready ${groupReadyTalks} \u00b7 Waiting ${groupWaitingTalks} \u00b7 Stale ${groupStaleTalks}` : `\u8fdb\u884c\u4e2d ${groupActiveTalks} \u00b7 \u53ef\u6536\u5c3e ${groupReadyTalks} \u00b7 \u7b49\u5f85 ${groupWaitingTalks} \u00b7 \u8d85\u65f6 ${groupStaleTalks}`}</div>}
                    {groupDiscussionStatus?.error && <div style={{ fontSize: "11px", color: "#b91c1c", marginBottom: "8px" }}>{String(groupDiscussionStatus.error)}</div>}
                    {groupPendingInvites.slice(0, 2).map((invite) => (
                        <div key={invite.invite_id || invite.id} style={{ padding: "8px 0", borderTop: `1px solid ${t.divider}` }}>
                            <div style={{ fontSize: "11px", fontWeight: 600, marginBottom: "2px" }}>{invite.topic || invite.consultation_id || (lang === "en" ? "Discussion invite" : "\u8ba8\u8bba\u9080\u8bf7")}</div>
                            <div style={{ fontSize: "10px", color: t.textMuted, marginBottom: "6px" }}>{invite.from_name || invite.from_id || "MaClaw"}</div>
                            <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "6px" }}>
                                <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: themeMode === "dark" ? "#86efac" : "#047857", borderColor: themeMode === "dark" ? "rgba(134, 239, 172, 0.45)" : "#86efac" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("accept", () => groupDiscussion.onAcceptInvite?.(invite.invite_id || invite.id || "")))}>{lang === "en" ? "Accept" : "\u63a5\u53d7"}</button>
                                <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: themeMode === "dark" ? "#fca5a5" : "#b91c1c", borderColor: themeMode === "dark" ? "rgba(252, 165, 165, 0.45)" : "#fecaca" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("reject", () => groupDiscussion.onRejectInvite?.(invite.invite_id || invite.id || "")))}>{lang === "en" ? "Reject" : "\u62d2\u7edd"}</button>
                            </div>
                        </div>
                    ))}
                    <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: "6px", marginTop: "10px" }}>
                        <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder, opacity: groupDiscussionBusy ? 0.68 : 1, cursor: groupDiscussionBusy ? "default" : "pointer" }} disabled={!!groupDiscussionBusy} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("refresh", groupDiscussion.onRefreshStatus))}>{groupDiscussionBusy === "refresh" ? (lang === "en" ? "Refreshing..." : "\u5237\u65b0\u4e2d...") : (lang === "en" ? "Refresh" : "\u5237\u65b0")}</button>
                        <button type="button" style={{ ...miniActionButtonStyle, background: t.fieldBg, color: t.text, borderColor: t.titleBarBorder, opacity: groupDiscussionBusy ? 0.68 : (groupDiscussionEnabled ? 1 : 0.55), cursor: (groupDiscussionBusy || !groupDiscussionEnabled) ? "default" : "pointer" }} disabled={!!groupDiscussionBusy || !groupDiscussionEnabled} {...bindGroupDiscussionPress(() => runGroupDiscussionAction("publish", groupDiscussion.onPublishProfile))}>{groupDiscussionBusy === "publish" ? (lang === "en" ? "Publishing..." : "\u53d1\u5e03\u4e2d...") : (lang === "en" ? "Publish" : "\u53d1\u5e03\u8eab\u4efd")}</button>
                    </div>
                </div>
            )}
        </div>
    );
}
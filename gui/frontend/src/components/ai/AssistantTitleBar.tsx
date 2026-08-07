import type { CSSProperties, MouseEvent } from "react";
import { LoadConfig } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { buildHubCardStoreURL } from "../../utils/hubCredits";
import { localizeText } from "./aiAssistantI18n";
import { dotBase, getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { getWindowControlButtonStyle } from "./aiAssistantControls";
import { VEAuthorizationRequestCenter } from "./VEAuthorizationDialog";
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from "../layout/WindowControlIcons";
import { AssistantUpdateNotice, type AssistantUpdatePayload } from "./AssistantUpdateNotice";
import { AssistantMobileDocsControl } from "./AssistantMobileDocsControl";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";
import { NotificationBell } from "./NotificationBell";
import { NotificationPanel } from "./NotificationPanel";
import { useNotifications } from "./useNotifications";
import type { AdminNotification } from "./useNotifications";
import { IconRecord } from "./WorkbenchIcons";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "drag" | "no-drag" };

type CardStoreConfig = {
    remote_email?: string;
    remote_hub_id?: string;
    remote_hub_url?: string;
    remote_hubcenter_url?: string;
    remote_mobile?: string;
    remote_tenant_id?: string;
    remote_tenant_name?: string;
    remote_user_id?: string;
    remote_viewer_token?: string;
};

export async function openCurrentTenantCardStore(loadConfig: () => Promise<CardStoreConfig> = LoadConfig, openURL: (url: string) => void = BrowserOpenURL) {
    try {
        const config = await loadConfig();
        const storeURL = buildHubCardStoreURL(config?.remote_hub_url, config?.remote_tenant_id, config?.remote_email, config?.remote_viewer_token, config?.remote_hubcenter_url, config?.remote_hub_id, undefined, config?.remote_tenant_name, config?.remote_user_id, config?.remote_mobile);
        if (storeURL) openURL(storeURL);
    } catch (error) {
        console.warn("[AIAssistantPanel] Failed to open card store", error);
    }
}

interface AssistantTitleBarProps {
    clearHistory: () => void;
    /** When true, the "New conversation" button is disabled to prevent clearing an in-progress session. */
    clearHistoryDisabled?: boolean;
    inline: boolean;
    lang: string;
    maximized: boolean;
    onClose: () => void;
    onHideWindow?: () => void;
    onDismissAppUpdate?: (latestVersion: string) => void;
    onOpenKnowledge?: () => void;
    onOpenAppUpdate?: () => void;
    onOpenTutorial?: () => void;
    onToggleMaximize?: () => void;
    projectSearchOpen: boolean;
    refreshNews: () => void;
    showMaximizeToggle: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    title: string;
    trialReflectEnabled: boolean;
    toggleProjectSearch: () => void;
    updateAvailable?: AssistantUpdatePayload | null;
    workflowActive?: boolean;
    /** Whether the right-side preview/code panel is currently open. */
    previewPanelOpen?: boolean;
    /** Toggle the right-side preview panel open/closed during an active workflow. */
    onTogglePreviewPanel?: () => void;
    skillRecording?: boolean;
    skillRecordingCount?: number;
    skillRecordingAnyTab?: boolean;
    onToggleSkillRecording?: () => void;
    onSaveCurrentTask?: () => void;
    /** Shown only on expert conversation tabs: distill the session into an optimized expert. */
    onOptimizeExpert?: () => void;
    /** When true, the optimize button is disabled and shows an in-progress label. */
    optimizeExpertBusy?: boolean;
}
const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};
export function AssistantTitleBar({ clearHistory, clearHistoryDisabled, inline, lang, maximized, onClose, onDismissAppUpdate, onHideWindow, onOpenAppUpdate, onOpenKnowledge, onOpenTutorial, onOptimizeExpert, onSaveCurrentTask, onToggleMaximize, onTogglePreviewPanel, onToggleSkillRecording, optimizeExpertBusy, previewPanelOpen, projectSearchOpen, refreshNews, showMaximizeToggle, skillRecording, skillRecordingAnyTab, skillRecordingCount, theme: t, themeMode, title, trialReflectEnabled, toggleProjectSearch, updateAvailable, workflowActive }: AssistantTitleBarProps) {
    // Notification system state
    const {
        notifications,
        unreadCount,
        panelOpen,
        categoryFilter,
        togglePanel,
        setCategoryFilter,
        markRead,
        markAllRead,
        urgentToast,
        dismissUrgentToast,
    } = useNotifications();

    const handleSelectNotification = (notification: AdminNotification) => {
        // Mark the notification as read when selected
        if (!notification.is_read) {
            markRead(notification.id);
        }
    };
    return (
        <>
        <div data-testid="ai-title-bar" onDoubleClick={() => { if (inline) onToggleMaximize?.(); }} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 12px 0 10px", height: "38px", background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`, flexShrink: 0, minWidth: 0, boxSizing: "border-box", gap: "8px", position: "relative", zIndex: 30000, overflow: "visible", ...(inline ? { "--wails-draggable": "drag" } satisfies WailsDragStyle : {}) }}>
            <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: "1 1 auto" }}>
                {!inline && <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}><span style={{ ...dotBase, background: t.closeBtnColor }} onClick={onClose} title={lang === "en" ? "Close" : "\u5173\u95ed"} /></div>}
                <span data-testid="ai-titlebar-title" style={{ color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em", fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif", flex: "0 1 240px", minWidth: 0, maxWidth: "240px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", transform: "translateY(-0.5px)" }}>{title}</span>
                {trialReflectEnabled && <span style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: t.fieldBg, color: t.promptColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{lang === "en" ? "Trial+Reflect" : "\u8bd5\u9519\u53cd\u601d"}</span>}
                <div data-testid="ai-titlebar-primary-actions" style={{ display: "flex", alignItems: "center", gap: "4px", flexShrink: 0 }}>
                    {onSaveCurrentTask && <button className="ai-titlebar-tool save-task-btn" data-testid="save-current-task-btn" aria-label={localizeText(lang, "Save current conversation as task", "保存当前对话为任务", "保存目前對話為任務")} {...(inline ? { onMouseDown: stopMouse(onSaveCurrentTask) } : { onClick: onSaveCurrentTask })} style={{ display: "inline-flex", alignItems: "center", gap: "3px", padding: "2px 8px", borderRadius: "999px", fontSize: "10px", fontWeight: 600, lineHeight: 1, cursor: "pointer", userSelect: "none", border: `1px solid ${t.titleBarBorder}`, background: t.fieldBg, color: t.promptColor, transition: "all 150ms ease", flexShrink: 0, height: "20px", ["--ai-titlebar-tool-hover-bg" as any]: "rgba(148, 163, 184, 0.15)", ...(inline ? { "--wails-draggable": "no-drag" } as WailsDragStyle : {}) }} title={localizeText(lang, "Save current conversation as task", "保存当前对话为任务", "保存目前對話為任務")}><svg width="10" height="10" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block", flexShrink: 0 }}><path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M6 4h10l2 2v14H6z" /><path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M9 4v6h6V4" /><path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M9 16h6" /></svg>{localizeText(lang, "Save as Task", "保存为任务", "保存為任務")}</button>}
                    {onOptimizeExpert && <button className="ai-titlebar-tool optimize-expert-btn" data-testid="optimize-expert-btn" disabled={!!optimizeExpertBusy} aria-label={localizeText(lang, "Distill this conversation into an optimized expert", "从当前对话提炼优化专家", "從目前對話提煉優化專家")} {...(inline ? { onMouseDown: optimizeExpertBusy ? undefined : stopMouse(onOptimizeExpert) } : { onClick: optimizeExpertBusy ? undefined : onOptimizeExpert })} style={{ display: "inline-flex", alignItems: "center", gap: "3px", padding: "2px 8px", borderRadius: "999px", fontSize: "10px", fontWeight: 600, lineHeight: 1, cursor: optimizeExpertBusy ? "wait" : "pointer", userSelect: "none", border: `1px solid ${t.titleBarBorder}`, background: t.fieldBg, color: t.promptColor, opacity: optimizeExpertBusy ? 0.6 : 1, transition: "all 150ms ease", flexShrink: 0, height: "20px", ["--ai-titlebar-tool-hover-bg" as any]: "rgba(148, 163, 184, 0.15)", ...(inline ? { "--wails-draggable": "no-drag" } as WailsDragStyle : {}) }} title={localizeText(lang, "Distill this conversation into an optimized expert", "从当前对话提炼优化专家", "從目前對話提煉優化專家")}><svg width="10" height="10" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block", flexShrink: 0 }}><path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M12 3l1.9 4.8L18.5 9.5l-4.6 1.7L12 16l-1.9-4.8L5.5 9.5l4.6-1.7z" /><path fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M18.5 15.5l.9 2.1 2.1.9-2.1.9-.9 2.1-.9-2.1-2.1-.9 2.1-.9z" /></svg>{optimizeExpertBusy ? localizeText(lang, "Optimizing…", "优化中…", "優化中…") : localizeText(lang, "Optimize Expert", "专家优化", "專家優化")}</button>}
                    {(workflowActive || previewPanelOpen) && onTogglePreviewPanel && <button className="ai-titlebar-tool workflow-preview-toggle-btn" data-testid="workflow-preview-toggle-btn" role="switch" aria-checked={!!previewPanelOpen} aria-label={localizeText(lang, previewPanelOpen ? "Hide preview panel" : "Show preview panel", previewPanelOpen ? "隐藏预览面板" : "显示预览面板", previewPanelOpen ? "隱藏預覽面板" : "顯示預覽面板")} {...(inline ? { onMouseDown: stopMouse(onTogglePreviewPanel) } : { onClick: onTogglePreviewPanel })} style={{ display: "inline-flex", alignItems: "center", gap: "4px", padding: "2px 8px", borderRadius: "999px", fontSize: "10px", fontWeight: 600, lineHeight: 1, cursor: "pointer", userSelect: "none", border: previewPanelOpen ? "1px solid rgba(79, 127, 111, 0.36)" : `1px solid ${t.titleBarBorder}`, background: previewPanelOpen ? "rgba(79, 127, 111, 0.12)" : t.fieldBg, color: previewPanelOpen ? "#4f7f6f" : t.promptColor, transition: "all 150ms ease", flexShrink: 0, height: "20px", ["--ai-titlebar-tool-hover-bg" as any]: previewPanelOpen ? "rgba(79, 127, 111, 0.20)" : "rgba(148, 163, 184, 0.15)", ...(inline ? { "--wails-draggable": "no-drag" } as WailsDragStyle : {}) }} title={localizeText(lang, previewPanelOpen ? "Hide preview panel" : "Show preview panel", previewPanelOpen ? "隐藏预览面板" : "显示预览面板", previewPanelOpen ? "隱藏預覽面板" : "顯示預覽面板")}><span aria-hidden="true" style={{ display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "#4f7f6f", opacity: previewPanelOpen ? 1 : 0.4, transition: "all 150ms ease" }} />{localizeText(lang, "Preview", "预览", "預覽")}</button>}
                    {onToggleSkillRecording && <button className="ai-titlebar-tool skill-recording-btn" data-testid="skill-recording-btn" role="switch" aria-checked={!!skillRecording} disabled={!!(skillRecordingAnyTab && !skillRecording)} aria-label={localizeText(lang, skillRecording ? "Recording... click to stop" : (skillRecordingAnyTab ? "Another tab is recording" : "Record operations as Skill"), skillRecording ? "录制中...点击停止" : (skillRecordingAnyTab ? "其他标签页正在录制" : "录制操作为 Skill"), skillRecording ? "錄製中...點擊停止" : (skillRecordingAnyTab ? "其他標籤頁正在錄製" : "錄製操作為 Skill"))} {...(inline ? { onMouseDown: (skillRecordingAnyTab && !skillRecording) ? undefined : stopMouse(onToggleSkillRecording) } : { onClick: (skillRecordingAnyTab && !skillRecording) ? undefined : onToggleSkillRecording })} style={{ display: "inline-flex", alignItems: "center", gap: "4px", padding: "2px 8px", borderRadius: "999px", fontSize: "10px", fontWeight: 600, lineHeight: 1, cursor: (skillRecordingAnyTab && !skillRecording) ? "not-allowed" : "pointer", userSelect: "none", border: skillRecording ? "1px solid rgba(220, 38, 38, 0.4)" : `1px solid ${t.titleBarBorder}`, background: skillRecording ? "rgba(220, 38, 38, 0.1)" : t.fieldBg, color: skillRecording ? "#dc2626" : t.promptColor, opacity: (skillRecordingAnyTab && !skillRecording) ? 0.4 : 1, transition: "all 150ms ease", flexShrink: 0, height: "20px", ["--ai-titlebar-tool-hover-bg" as any]: skillRecording ? "rgba(220, 38, 38, 0.18)" : "rgba(148, 163, 184, 0.15)", ...(inline ? { "--wails-draggable": "no-drag" } as WailsDragStyle : {}) }} title={localizeText(lang, skillRecording ? `Recording (${skillRecordingCount || 0} ops)... click to stop` : (skillRecordingAnyTab ? "Another tab is recording" : "Record operations as Skill"), skillRecording ? `录制中 (${skillRecordingCount || 0} 步)...点击停止` : (skillRecordingAnyTab ? "其他标签页正在录制" : "录制操作为 Skill"), skillRecording ? `錄製中 (${skillRecordingCount || 0} 步)...点击停止` : (skillRecordingAnyTab ? "其他标签页正在录制" : "录制操作为 Skill"))}><span aria-hidden="true" style={{ display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: skillRecording ? "#dc2626" : t.promptColor, opacity: skillRecording ? 1 : 0.4, transition: "all 150ms ease", animation: skillRecording ? "pulse-recording 1.5s ease-in-out infinite" : "none" }} />{localizeText(lang, skillRecording ? `REC ${skillRecordingCount || 0}` : "REC", skillRecording ? `录制 ${skillRecordingCount || 0}` : "录制", skillRecording ? `錄製 ${skillRecordingCount || 0}` : "錄製")}</button>}
                </div>
            </div>
            {/* flexShrink:0: prefer ellipsizing the title over crushing window controls.
                Do not overflow:hidden tools — NotificationPanel / update menu are absolute. */}
            <div style={{ display: "flex", alignItems: "center", flexShrink: 0, paddingRight: inline ? 0 : 2, ...(inline ? { "--wails-draggable": "no-drag", position: "relative", zIndex: 30010 } satisfies WailsDragStyle : {}) }}>
                <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "4px", alignItems: "center", minWidth: 0, paddingTop: 1 }}>
                    <AssistantUpdateNotice inline={inline} lang={lang} onDismissAppUpdate={onDismissAppUpdate} onOpenAppUpdate={onOpenAppUpdate} theme={t} themeMode={themeMode} updateAvailable={updateAvailable} />
                    <div style={{ position: "relative" }}>
                        <NotificationBell
                            unreadCount={unreadCount}
                            onClick={togglePanel}
                            theme={t}
                            inline={inline}
                        />
                        {panelOpen && (
                            <NotificationPanel
                                notifications={notifications}
                                categoryFilter={categoryFilter as any}
                                onCategoryChange={setCategoryFilter as any}
                                onMarkAllRead={markAllRead}
                                onSelectNotification={handleSelectNotification}
                                onClose={togglePanel}
                                lang={lang}
                                theme={{
                                    bg: t.titleBarBg,
                                    text: t.titleText,
                                    textMuted: t.promptColor,
                                    headingColor: t.headingColor,
                                    divider: t.titleBarBorder,
                                    inputBarBg: t.fieldBg,
                                    inputBarBorder: t.titleBarBorder,
                                }}
                            />
                        )}
                    </div>
                    <AssistantMobileDocsControl lang={lang} theme={t} inline={inline} />
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(() => { void openCurrentTenantCardStore(); }) } : { onClick: () => { void openCurrentTenantCardStore(); } })} style={getTitleBarToolButtonStyle(t)} title={localizeText(lang, "Buy service redemption cards", "\u8d2d\u4e70\u670d\u52a1\u5151\u6362\u5361")}><TitleBarToolIcon name="cart" /></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleProjectSearch) } : { onClick: toggleProjectSearch })} style={getTitleBarToolButtonStyle(t, projectSearchOpen ? "active" : "default")} title={localizeText(lang, "Search tasks", "\u641c\u7d22\u4efb\u52a1")}><TitleBarToolIcon name="search" /></button>
                    <VEAuthorizationRequestCenter theme={t} lang={lang} inline={inline} />
                    {onOpenKnowledge && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(onOpenKnowledge) } : { onClick: onOpenKnowledge })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Knowledge Base" : "\u77e5\u8bc6\u5e93"}><TitleBarToolIcon name="book" /></button>}
                    {onOpenTutorial && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Tutorial" : "\u6559\u7a0b"}><TitleBarToolIcon name="guide" /></button>}
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Refresh news" : "\u5237\u65b0\u6d88\u606f"}><TitleBarToolIcon name="refresh" /></button>
                    <button className="ai-titlebar-tool" disabled={!!clearHistoryDisabled} {...(inline ? { onMouseDown: clearHistoryDisabled ? undefined : clearHistory } : { onClick: clearHistoryDisabled ? undefined : clearHistory })} style={{ ...getTitleBarToolButtonStyle(t), ...(clearHistoryDisabled ? { opacity: 0.4, cursor: "not-allowed" } : {}) }} title={clearHistoryDisabled ? (lang === "en" ? "Please wait for the current task to finish" : "\u8bf7\u7b49\u5f85\u5f53\u524d\u4efb\u52a1\u5b8c\u6210") : (lang === "en" ? "New conversation" : "\u5f00\u59cb\u65b0\u5bf9\u8bdd")}><TitleBarToolIcon name="eraser" /></button>
                </div>
                <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", flexShrink: 0, boxSizing: "border-box", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && <button className="ai-window-control" onMouseDown={stopMouse(onHideWindow)} data-testid="ai-hide-toggle" aria-label={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"} style={getWindowControlButtonStyle(t, "hide")} title={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}><WindowCloseIcon /></button>}
                    {showMaximizeToggle && <button className="ai-window-control" onClick={(e) => { e.preventDefault(); e.stopPropagation(); onToggleMaximize?.(); }} data-testid="ai-maximize-toggle" aria-label={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")} style={getWindowControlButtonStyle(t, "fullscreen", maximized)} title={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}>{maximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}</button>}
                    {!inline && <button className="ai-window-control" onClick={onClose} style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor }} title={lang === "en" ? "Close" : "\u5173\u95ed"}><WindowCloseIcon /></button>}
                </div>
            </div>
        </div>
        {/* Urgent notification toast */}
        {urgentToast && (
            <div
                data-testid="notification-urgent-toast"
                style={{
                    position: "fixed",
                    top: "48px",
                    right: "16px",
                    width: "320px",
                    padding: "12px 16px",
                    background: t.titleBarBg,
                    border: `1px solid #ef4444`,
                    borderRadius: "8px",
                    boxShadow: "0 4px 24px rgba(239, 68, 68, 0.2)",
                    zIndex: 50000,
                    display: "flex",
                    flexDirection: "column",
                    gap: "6px",
                    animation: "notification-toast-in 300ms ease-out",
                }}
            >
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                    <span style={{ fontSize: "12px", fontWeight: 600, color: "#ef4444", display: "inline-flex", alignItems: "center", gap: 6 }}>
                        <IconRecord size={12} color="#ef4444" />
                        {localizeText(lang, "Urgent Notification", "紧急通知", "緊急通知")}
                    </span>
                    <button
                        onClick={dismissUrgentToast}
                        style={{ background: "none", border: "none", cursor: "pointer", color: t.promptColor, fontSize: "14px", padding: "0 2px", lineHeight: 1 }}
                        aria-label={localizeText(lang, "Dismiss", "关闭", "關閉")}
                    >
                        ×
                    </button>
                </div>
                <span style={{ fontSize: "13px", fontWeight: 500, color: t.titleText }}>{urgentToast.title}</span>
                <span style={{ fontSize: "11px", color: t.promptColor, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {urgentToast.content.length > 80 ? urgentToast.content.slice(0, 80) + "..." : urgentToast.content}
                </span>
            </div>
        )}
        </>
    );
}

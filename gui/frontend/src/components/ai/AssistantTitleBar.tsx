import type { CSSProperties, MouseEvent } from "react";
import { LoadConfig } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import { buildHubCardStoreURL } from "../../utils/hubCredits";
import { localizeText } from "./aiAssistantI18n";
import { dotBase, getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { getWindowControlButtonStyle } from "./aiAssistantControls";
import { codingAgentCompactText, codingAgentDisplayText, codingAgentStatusClassName, codingAgentStatusDataAttrs, codingAgentStatusTone, normalizeCodingAgentProgress, type CodingAgentProgress } from "./CodingAgentProgressStatus";
import { TTSLevelBars } from "./TTSLevelBars";
import { VEAuthorizationRequestCenter } from "./VEAuthorizationDialog";
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from "../layout/WindowControlIcons";
import { AssistantUpdateNotice, type AssistantUpdatePayload } from "./AssistantUpdateNotice";
import { TitleBarToolIcon } from "./AssistantTitleBarIcons";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "drag" | "no-drag" };

type CardStoreConfig = {
    remote_email?: string;
    remote_hub_url?: string;
    remote_tenant_id?: string;
    remote_viewer_token?: string;
};

export async function openCurrentTenantCardStore(loadConfig: () => Promise<CardStoreConfig> = LoadConfig, openURL: (url: string) => void = BrowserOpenURL) {
    try {
        const config = await loadConfig();
        const storeURL = buildHubCardStoreURL(config?.remote_hub_url, config?.remote_tenant_id, config?.remote_email, config?.remote_viewer_token);
        if (storeURL) openURL(storeURL);
    } catch (error) {
        console.warn("[AIAssistantPanel] Failed to open card store", error);
    }
}

interface AssistantTitleBarProps {
    clearHistory: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
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
    setThemeMode: (mode: "light" | "dark") => void;
    setTtsEnabled: (enabled: boolean) => void;
    showMaximizeToggle: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    title: string;
    trialReflectEnabled: boolean;
    ttsEnabled: boolean;
    ttsPlaying: boolean;
    toggleProjectSearch: () => void;
    updateAvailable?: AssistantUpdatePayload | null;
    workflowActive?: boolean;
    workflowEnabled?: boolean;
    onToggleWorkflow?: () => void;
    onOpenWorkflowPanel?: () => void;
}

const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};

export function AssistantTitleBar({ clearHistory, codingAgentProgress, inline, lang, maximized, onClose, onDismissAppUpdate, onHideWindow, onOpenAppUpdate, onOpenKnowledge, onOpenTutorial, onToggleMaximize, onOpenWorkflowPanel, onToggleWorkflow, projectSearchOpen, refreshNews, setThemeMode, setTtsEnabled, showMaximizeToggle, theme: t, themeMode, title, trialReflectEnabled, ttsEnabled, ttsPlaying, toggleProjectSearch, updateAvailable, workflowActive, workflowEnabled }: AssistantTitleBarProps) {
    const toggleTts = () => setTtsEnabled(!ttsEnabled);
    const toggleTheme = () => setThemeMode(themeMode === "dark" ? "light" : "dark");
    const normalizedCodingAgentProgress = codingAgentProgress ? normalizeCodingAgentProgress(codingAgentProgress) : null;
    const codingTone = normalizedCodingAgentProgress ? codingAgentStatusTone(normalizedCodingAgentProgress.phase) : null;
    const codingDisplayText = normalizedCodingAgentProgress ? codingAgentDisplayText(normalizedCodingAgentProgress, lang) : "";
    return (
        <div data-testid="ai-title-bar" onDoubleClick={() => { if (inline) onToggleMaximize?.(); }} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 12px 0 10px", height: "38px", background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`, flexShrink: 0, minWidth: 0, boxSizing: "border-box", gap: "8px", position: "relative", zIndex: 30000, overflow: "visible", ...(inline ? { "--wails-draggable": "drag" } satisfies WailsDragStyle : {}) }}>
            <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: 1 }}>
                {!inline && <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}><span style={{ ...dotBase, background: t.closeBtnColor }} onClick={onClose} title={lang === "en" ? "Close" : "\u5173\u95ed"} /></div>}
                <span style={{ color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em", fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", transform: "translateY(-0.5px)" }}>{title}</span>
                {trialReflectEnabled && <span style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: t.fieldBg, color: t.promptColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{lang === "en" ? "Trial+Reflect" : "\u8bd5\u9519\u53cd\u601d"}</span>}
                {onToggleWorkflow && !workflowActive && <button className="ai-titlebar-tool workflow-toggle-btn" data-testid="workflow-toggle-btn" role="switch" aria-checked={!!workflowEnabled} aria-label={localizeText(lang, workflowEnabled ? "Workflow ON - click to disable" : "Workflow OFF - click to enable", workflowEnabled ? "工作流已开启，点击关闭" : "工作流已关闭，点击开启", workflowEnabled ? "工作流已開啟，點擊關閉" : "工作流已關閉，點擊開啟")} {...(inline ? { onMouseDown: stopMouse(onToggleWorkflow) } : { onClick: onToggleWorkflow })} style={{ display: "inline-flex", alignItems: "center", gap: "4px", padding: "2px 8px", borderRadius: "999px", fontSize: "10px", fontWeight: 600, lineHeight: 1, cursor: "pointer", userSelect: "none", border: workflowEnabled ? "1px solid rgba(79, 127, 111, 0.36)" : `1px solid ${t.titleBarBorder}`, background: workflowEnabled ? "rgba(79, 127, 111, 0.12)" : t.fieldBg, color: workflowEnabled ? "#4f7f6f" : t.promptColor, transition: "all 150ms ease", flexShrink: 0, height: "20px", ["--ai-titlebar-tool-hover-bg" as any]: workflowEnabled ? "rgba(79, 127, 111, 0.20)" : "rgba(148, 163, 184, 0.15)", ...(inline ? { "--wails-draggable": "no-drag" } as WailsDragStyle : {}) }} title={localizeText(lang, workflowEnabled ? "Workflow ON - click to disable" : "Workflow OFF - click to enable", workflowEnabled ? "工作流已开启，点击关闭" : "工作流已关闭，点击开启", workflowEnabled ? "工作流已開啟，點擊關閉" : "工作流已關閉，點擊開啟")}><span aria-hidden="true" style={{ display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: workflowEnabled ? "#4f7f6f" : t.promptColor, opacity: workflowEnabled ? 1 : 0.4, transition: "all 150ms ease" }} />{localizeText(lang, "Workflow", "工作流", "工作流")}</button>}
                {workflowActive && <span className="workflow-active-indicator" {...(inline ? { onMouseDown: (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); onOpenWorkflowPanel?.(); } } : { onClick: onOpenWorkflowPanel })} style={{ fontSize: "10px", lineHeight: 1, padding: "3px 8px", borderRadius: "999px", background: "rgba(79, 127, 111, 0.12)", color: "#4f7f6f", border: "1px solid rgba(79, 127, 111, 0.26)", flexShrink: 0, fontWeight: 600, animation: "workflow-pulse 1.5s ease-in-out infinite", cursor: onOpenWorkflowPanel ? "pointer" : "default", userSelect: "none" }} title={lang === "en" ? "Open workflow & preview panel" : "\u6253\u5f00\u5de5\u4f5c\u6d41\u4e0e\u9884\u89c8\u9762\u677f"}>{lang === "en" ? "Workflow" : "\u5de5\u4f5c\u6d41"}</span>}
                {normalizedCodingAgentProgress && codingTone && <span className={codingAgentStatusClassName(normalizedCodingAgentProgress, "title-bar")} data-testid="coding-agent-title-status" {...codingAgentStatusDataAttrs(normalizedCodingAgentProgress, "title-bar")} role="status" aria-live="polite" aria-label={codingDisplayText} title={codingDisplayText} style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: codingTone.bg, color: codingTone.accent, border: `1px solid ${codingTone.border}`, flexShrink: 0, fontWeight: 700 }}>{codingAgentCompactText(normalizedCodingAgentProgress, lang)}</span>}
            </div>
            <div style={{ display: "flex", alignItems: "center", flexShrink: 0, paddingRight: inline ? 0 : 2, ...(inline ? { "--wails-draggable": "no-drag", position: "relative", zIndex: 30010 } satisfies WailsDragStyle : {}) }}>
                <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "4px", alignItems: "center", minWidth: 0, paddingTop: 1 }}>
                    <AssistantUpdateNotice inline={inline} lang={lang} onDismissAppUpdate={onDismissAppUpdate} onOpenAppUpdate={onOpenAppUpdate} theme={t} themeMode={themeMode} updateAvailable={updateAvailable} />
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(() => { void openCurrentTenantCardStore(); }) } : { onClick: () => { void openCurrentTenantCardStore(); } })} style={getTitleBarToolButtonStyle(t)} title={localizeText(lang, "Buy service redemption cards", "\u8d2d\u4e70\u670d\u52a1\u5151\u6362\u5361")}><TitleBarToolIcon name="cart" /></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleProjectSearch) } : { onClick: toggleProjectSearch })} style={getTitleBarToolButtonStyle(t, projectSearchOpen ? "active" : "default")} title={localizeText(lang, "Search tasks", "\u641c\u7d22\u4efb\u52a1")}><TitleBarToolIcon name="search" /></button>
                    <VEAuthorizationRequestCenter theme={t} lang={lang} inline={inline} />
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTts) } : { onClick: toggleTts })} style={{ ...getTitleBarToolButtonStyle(t, ttsEnabled ? "active" : "default"), position: "relative" }} title={ttsEnabled ? localizeText(lang, "Voice readback ON - click to disable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5f00\u542f\uff0c\u70b9\u51fb\u5173\u95ed") : localizeText(lang, "Voice readback OFF - click to enable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5173\u95ed\uff0c\u70b9\u51fb\u5f00\u542f")}><span aria-hidden="true" style={{ opacity: ttsPlaying ? 0 : 1, transition: "opacity 150ms" }}><TitleBarToolIcon name={ttsEnabled ? "volumeOn" : "volumeOff"} /></span>{ttsPlaying && <TTSLevelBars accentColor={t.headingColor} />}</button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTheme) } : { onClick: toggleTheme })} style={getTitleBarToolButtonStyle(t)} title={themeMode === "dark" ? localizeText(lang, "Switch to light mode", "\u5207\u6362\u5230\u666e\u901a\u6a21\u5f0f") : localizeText(lang, "Switch to dark mode", "\u5207\u6362\u5230\u6697\u9ed1\u6a21\u5f0f")}><TitleBarToolIcon name={themeMode === "dark" ? "moon" : "sun"} /></button>
                    {onOpenKnowledge && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(onOpenKnowledge) } : { onClick: onOpenKnowledge })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Knowledge Base" : "\u77e5\u8bc6\u5e93"}><TitleBarToolIcon name="book" /></button>}
                    {onOpenTutorial && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Tutorial" : "\u6559\u7a0b"}><TitleBarToolIcon name="guide" /></button>}
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Refresh news" : "\u5237\u65b0\u6d88\u606f"}><TitleBarToolIcon name="refresh" /></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Clear history" : "\u6e05\u7a7a\u5386\u53f2"}><TitleBarToolIcon name="trash" /></button>
                </div>
                <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", flexShrink: 0, boxSizing: "border-box", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && <button className="ai-window-control" onMouseDown={stopMouse(onHideWindow)} data-testid="ai-hide-toggle" aria-label={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"} style={getWindowControlButtonStyle(t, "hide")} title={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}><WindowCloseIcon /></button>}
                    {showMaximizeToggle && <button className="ai-window-control" onClick={(e) => { e.preventDefault(); e.stopPropagation(); onToggleMaximize?.(); }} data-testid="ai-maximize-toggle" aria-label={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")} style={getWindowControlButtonStyle(t, "fullscreen", maximized)} title={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}>{maximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}</button>}
                    {!inline && <button className="ai-window-control" onClick={onClose} style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor }} title={lang === "en" ? "Close" : "\u5173\u95ed"}><WindowCloseIcon /></button>}
                </div>
            </div>
        </div>
    );
}

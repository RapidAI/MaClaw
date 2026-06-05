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
}

const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};

export function AssistantTitleBar({ clearHistory, codingAgentProgress, inline, lang, maximized, onClose, onDismissAppUpdate, onHideWindow, onOpenAppUpdate, onOpenKnowledge, onOpenTutorial, onToggleMaximize, projectSearchOpen, refreshNews, setThemeMode, setTtsEnabled, showMaximizeToggle, theme: t, themeMode, title, trialReflectEnabled, ttsEnabled, ttsPlaying, toggleProjectSearch, updateAvailable }: AssistantTitleBarProps) {
    const toggleTts = () => setTtsEnabled(!ttsEnabled);
    const toggleTheme = () => setThemeMode(themeMode === "dark" ? "light" : "dark");
    const normalizedCodingAgentProgress = codingAgentProgress ? normalizeCodingAgentProgress(codingAgentProgress) : null;
    const codingTone = normalizedCodingAgentProgress ? codingAgentStatusTone(normalizedCodingAgentProgress.phase) : null;
    const codingDisplayText = normalizedCodingAgentProgress ? codingAgentDisplayText(normalizedCodingAgentProgress, lang) : "";
    return (
        <div data-testid="ai-title-bar" onDoubleClick={() => { if (inline) onToggleMaximize?.(); }} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 12px 0 10px", height: "38px", background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`, flexShrink: 0, minWidth: 0, boxSizing: "border-box", gap: "8px", position: "relative", zIndex: 30000, overflow: "visible", ...(inline ? { "--wails-draggable": "drag" } satisfies WailsDragStyle : {}) }}>
            <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: 1 }}>
                {!inline && <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}><span style={{ ...dotBase, background: "#ff5f57" }} onClick={onClose} title={lang === "en" ? "Close" : "\u5173\u95ed"} /></div>}
                <span style={{ color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em", fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", transform: "translateY(-0.5px)" }}>{title}</span>
                {trialReflectEnabled && <span style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: "rgba(99, 102, 241, 0.12)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{lang === "en" ? "Trial+Reflect" : "\u8bd5\u9519\u53cd\u601d"}</span>}
                {normalizedCodingAgentProgress && codingTone && <span className={codingAgentStatusClassName(normalizedCodingAgentProgress, "title-bar")} data-testid="coding-agent-title-status" {...codingAgentStatusDataAttrs(normalizedCodingAgentProgress, "title-bar")} role="status" aria-live="polite" aria-label={codingDisplayText} title={codingDisplayText} style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: codingTone.bg, color: codingTone.accent, border: `1px solid ${codingTone.border}`, flexShrink: 0, fontWeight: 700 }}>{codingAgentCompactText(normalizedCodingAgentProgress, lang)}</span>}
            </div>
            <div style={{ display: "flex", alignItems: "center", flexShrink: 0, paddingRight: inline ? 0 : 2, ...(inline ? { "--wails-draggable": "no-drag", position: "relative", zIndex: 30010 } satisfies WailsDragStyle : {}) }}>
                <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "4px", alignItems: "center", minWidth: 0, paddingTop: 1 }}>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(() => { void openCurrentTenantCardStore(); }) } : { onClick: () => { void openCurrentTenantCardStore(); } })} style={getTitleBarToolButtonStyle(t)} title={localizeText(lang, "Buy service redemption cards", "\u8d2d\u4e70\u670d\u52a1\u5151\u6362\u5361")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F6D2}"}</span></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleProjectSearch) } : { onClick: toggleProjectSearch })} style={getTitleBarToolButtonStyle(t, projectSearchOpen ? "active" : "default")} title={localizeText(lang, "Search tasks", "\u641c\u7d22\u4efb\u52a1")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F50D}"}</span></button>
                    <AssistantUpdateNotice inline={inline} lang={lang} onDismissAppUpdate={onDismissAppUpdate} onOpenAppUpdate={onOpenAppUpdate} theme={t} themeMode={themeMode} updateAvailable={updateAvailable} />
                    <VEAuthorizationRequestCenter theme={t} lang={lang} inline={inline} />
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTts) } : { onClick: toggleTts })} style={{ ...getTitleBarToolButtonStyle(t, ttsEnabled ? "active" : "default"), position: "relative" }} title={ttsEnabled ? localizeText(lang, "Voice readback ON - click to disable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5f00\u542f\uff0c\u70b9\u51fb\u5173\u95ed") : localizeText(lang, "Voice readback OFF - click to enable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5173\u95ed\uff0c\u70b9\u51fb\u5f00\u542f")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)", opacity: ttsPlaying ? 0 : 1, transition: "opacity 150ms" }}>{ttsEnabled ? "\u{1F50A}" : "\u{1F507}"}</span>{ttsPlaying && <TTSLevelBars accentColor={t.headingColor} />}</button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTheme) } : { onClick: toggleTheme })} style={getTitleBarToolButtonStyle(t)} title={themeMode === "dark" ? localizeText(lang, "Switch to light mode", "\u5207\u6362\u5230\u666e\u901a\u6a21\u5f0f") : localizeText(lang, "Switch to dark mode", "\u5207\u6362\u5230\u6697\u9ed1\u6a21\u5f0f")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{themeMode === "dark" ? "\u{1F319}" : "\u2600\uFE0F"}</span></button>
                    {onOpenKnowledge && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(onOpenKnowledge) } : { onClick: onOpenKnowledge })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Knowledge Base" : "\u77e5\u8bc6\u5e93"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F4DA}"}</span></button>}
                    {onOpenTutorial && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Tutorial" : "\u6559\u7a0b"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F4D6}"}</span></button>}
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Refresh news" : "\u5237\u65b0\u6d88\u606f"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u21bb"}</span></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Clear history" : "\u6e05\u7a7a\u5386\u53f2"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F5D1}"}</span></button>
                </div>
                <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", flexShrink: 0, boxSizing: "border-box", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && <button className="ai-window-control" onMouseDown={stopMouse(onHideWindow)} data-testid="ai-hide-toggle" aria-label={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"} style={getWindowControlButtonStyle(t, "hide")} title={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}><WindowCloseIcon /></button>}
                    {showMaximizeToggle && <button className="ai-window-control" onMouseDown={stopMouse(() => onToggleMaximize?.())} data-testid="ai-maximize-toggle" aria-label={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")} style={getWindowControlButtonStyle(t, "fullscreen", maximized)} title={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}>{maximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}</button>}
                    {!inline && <button className="ai-window-control" onClick={onClose} style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor }} title={lang === "en" ? "Close" : "\u5173\u95ed"}><WindowCloseIcon /></button>}
                </div>
            </div>
        </div>
    );
}

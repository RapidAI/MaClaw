import type { CSSProperties, Dispatch, HTMLAttributes, MouseEvent, SetStateAction } from "react";
import { localizeText } from "./aiAssistantI18n";
import { dotBase, getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { getWindowControlButtonStyle } from "./aiAssistantControls";
import { AssistantGroupDiscussionMenu } from "./AssistantGroupDiscussionMenu";
import type { GroupDiscussionPanelControl, GroupDiscussionPanelStatus } from "./aiAssistantPanelTypes";
import { codingAgentCompactText, codingAgentDisplayText, codingAgentStatusClassName, codingAgentStatusDataAttrs, codingAgentStatusTone, normalizeCodingAgentProgress, type CodingAgentProgress } from "./CodingAgentProgressStatus";
import { TTSLevelBars } from "./TTSLevelBars";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "drag" | "no-drag" };

interface AssistantTitleBarProps {
    bindGroupDiscussionPress: (handler: () => void) => Pick<HTMLAttributes<HTMLButtonElement>, "onClick" | "onMouseDown">;
    clearHistory: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
    groupActiveTalks: number;
    groupDiscussion?: GroupDiscussionPanelControl;
    groupDiscussionBusy: string;
    groupDiscussionDiscoverable: boolean;
    groupDiscussionEnabled: boolean;
    groupDiscussionLabel: string;
    groupDiscussionOpen: boolean;
    groupDiscussionScopeText: string;
    groupDiscussionStatus?: GroupDiscussionPanelStatus | null;
    groupPendingInvites: NonNullable<GroupDiscussionPanelStatus["pending_invites"]>;
    groupReadyTalks: number;
    groupStaleTalks: number;
    groupWaitingTalks: number;
    inline: boolean;
    lang: string;
    maximized: boolean;
    onClose: () => void;
    onHideWindow?: () => void;
    onOpenKnowledge?: () => void;
    onOpenTutorial?: () => void;
    onToggleMaximize?: () => void;
    projectSearchOpen: boolean;
    refreshNews: () => void;
    runGroupDiscussionAction: (kind: string, action?: () => void | Promise<void>) => void;
    setGroupDiscussionOpen: Dispatch<SetStateAction<boolean>>;
    setThemeMode: (mode: "light" | "dark") => void;
    setTtsEnabled: (enabled: boolean) => void;
    showMaximizeToggle: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    title: string;
    trialReflectEnabled: boolean;
    ttsEnabled: boolean;
    ttsPlaying: boolean;
    ttsAudioLevel: number;
    toggleProjectSearch: () => void;
}

const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};

function WindowsMaximizeRestoreIcon({ maximized, activeBg }: { maximized: boolean; activeBg: string }) {
    const box = (left: number, top: number, background = "transparent", zIndex = 1) => <span style={{ position: "absolute", left, top, width: 9, height: 9, border: "2px solid currentColor", borderRadius: 1, background, boxSizing: "border-box", zIndex }} />;
    return <span style={{ position: "relative", width: 15, height: 15, display: "inline-block" }}>{box(maximized ? 4 : 2, maximized ? 5 : 2, maximized ? activeBg : "transparent", 2)}{maximized && box(2, 2)}</span>;
}

export function AssistantTitleBar({ bindGroupDiscussionPress, clearHistory, codingAgentProgress, groupActiveTalks, groupDiscussion, groupDiscussionBusy, groupDiscussionDiscoverable, groupDiscussionEnabled, groupDiscussionLabel, groupDiscussionOpen, groupDiscussionScopeText, groupDiscussionStatus, groupPendingInvites, groupReadyTalks, groupStaleTalks, groupWaitingTalks, inline, lang, maximized, onClose, onHideWindow, onOpenKnowledge, onOpenTutorial, onToggleMaximize, projectSearchOpen, refreshNews, runGroupDiscussionAction, setGroupDiscussionOpen, setThemeMode, setTtsEnabled, showMaximizeToggle, theme: t, themeMode, title, trialReflectEnabled, ttsEnabled, ttsPlaying, ttsAudioLevel, toggleProjectSearch }: AssistantTitleBarProps) {
    const toggleTts = () => setTtsEnabled(!ttsEnabled);
    const toggleTheme = () => setThemeMode(themeMode === "dark" ? "light" : "dark");
    const normalizedCodingAgentProgress = codingAgentProgress ? normalizeCodingAgentProgress(codingAgentProgress) : null;
    const codingTone = normalizedCodingAgentProgress ? codingAgentStatusTone(normalizedCodingAgentProgress.phase) : null;
    const codingDisplayText = normalizedCodingAgentProgress ? codingAgentDisplayText(normalizedCodingAgentProgress, lang) : "";
    return (
        <div data-testid="ai-title-bar" onDoubleClick={() => { if (inline) onToggleMaximize?.(); }} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 12px 0 10px", height: "38px", background: t.titleBarBg, borderBottom: `1px solid ${t.titleBarBorder}`, flexShrink: 0, minWidth: 0, boxSizing: "border-box", gap: "8px", position: "relative", zIndex: 30000, overflow: "visible", ...(inline && !maximized ? { "--wails-draggable": "drag" } satisfies WailsDragStyle : {}) }}>
            <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: 1 }}>
                {!inline && <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}><span style={{ ...dotBase, background: "#ff5f57" }} onClick={onClose} title={lang === "en" ? "Close" : "\u5173\u95ed"} /></div>}
                <span style={{ color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em", fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", transform: "translateY(-0.5px)" }}>{title}</span>
                {trialReflectEnabled && <span style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: "rgba(99, 102, 241, 0.12)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{lang === "en" ? "Trial+Reflect" : "\u8bd5\u9519\u53cd\u601d"}</span>}
                {normalizedCodingAgentProgress && codingTone && <span className={codingAgentStatusClassName(normalizedCodingAgentProgress, "title-bar")} data-testid="coding-agent-title-status" {...codingAgentStatusDataAttrs(normalizedCodingAgentProgress, "title-bar")} role="status" aria-live="polite" aria-label={codingDisplayText} title={codingDisplayText} style={{ fontSize: "10px", lineHeight: 1, padding: "3px 6px", borderRadius: "999px", background: codingTone.bg, color: codingTone.accent, border: `1px solid ${codingTone.border}`, flexShrink: 0, fontWeight: 700 }}>{codingAgentCompactText(normalizedCodingAgentProgress, lang)}</span>}
            </div>
            <div style={{ display: "flex", alignItems: "center", flexShrink: 0, paddingRight: inline ? 0 : 2, ...(inline ? { "--wails-draggable": "no-drag", position: "relative", zIndex: 30010 } satisfies WailsDragStyle : {}) }}>
                <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "4px", alignItems: "center", minWidth: 0, paddingTop: 1 }}>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleProjectSearch) } : { onClick: toggleProjectSearch })} style={getTitleBarToolButtonStyle(t, projectSearchOpen ? "active" : "default")} title={localizeText(lang, "Search tasks", "\u641c\u7d22\u4efb\u52a1")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F50D}"}</span></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTts) } : { onClick: toggleTts })} style={{ ...getTitleBarToolButtonStyle(t, ttsEnabled ? "active" : "default"), position: "relative" }} title={ttsEnabled ? localizeText(lang, "Voice readback ON - click to disable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5f00\u542f\uff0c\u70b9\u51fb\u5173\u95ed") : localizeText(lang, "Voice readback OFF - click to enable", "\u8bed\u97f3\u64ad\u62a5\u5df2\u5173\u95ed\uff0c\u70b9\u51fb\u5f00\u542f")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{ttsEnabled ? "\u{1F50A}" : "\u{1F507}"}</span>{ttsPlaying && <TTSLevelBars level={ttsAudioLevel} accentColor={t.headingColor} />}</button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(toggleTheme) } : { onClick: toggleTheme })} style={getTitleBarToolButtonStyle(t)} title={themeMode === "dark" ? localizeText(lang, "Switch to light mode", "\u5207\u6362\u5230\u666e\u901a\u6a21\u5f0f") : localizeText(lang, "Switch to dark mode", "\u5207\u6362\u5230\u6697\u9ed1\u6a21\u5f0f")}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{themeMode === "dark" ? "\u{1F319}" : "\u2600\uFE0F"}</span></button>
                    {groupDiscussion && <AssistantGroupDiscussionMenu bindGroupDiscussionPress={bindGroupDiscussionPress} groupActiveTalks={groupActiveTalks} groupDiscussion={groupDiscussion} groupDiscussionBusy={groupDiscussionBusy} groupDiscussionDiscoverable={groupDiscussionDiscoverable} groupDiscussionEnabled={groupDiscussionEnabled} groupDiscussionLabel={groupDiscussionLabel} groupDiscussionOpen={groupDiscussionOpen} groupDiscussionScopeText={groupDiscussionScopeText} groupDiscussionStatus={groupDiscussionStatus} groupPendingInvites={groupPendingInvites} groupReadyTalks={groupReadyTalks} groupStaleTalks={groupStaleTalks} groupWaitingTalks={groupWaitingTalks} inline={inline} lang={lang} runGroupDiscussionAction={runGroupDiscussionAction} setGroupDiscussionOpen={setGroupDiscussionOpen} theme={t} themeMode={themeMode} />}
                    {onOpenKnowledge && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: stopMouse(onOpenKnowledge) } : { onClick: onOpenKnowledge })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Knowledge Base" : "\u77e5\u8bc6\u5e93"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F4DA}"}</span></button>}
                    {onOpenTutorial && <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Tutorial" : "\u6559\u7a0b"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F4D6}"}</span></button>}
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })} style={getTitleBarToolButtonStyle(t)} title={lang === "en" ? "Refresh news" : "\u5237\u65b0\u6d88\u606f"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u21bb"}</span></button>
                    <button className="ai-titlebar-tool" {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })} style={getTitleBarToolButtonStyle(t, "danger")} title={lang === "en" ? "Clear history" : "\u6e05\u7a7a\u5386\u53f2"}><span aria-hidden="true" style={{ fontSize: "16px", lineHeight: 1, transform: "translateY(-0.5px)" }}>{"\u{1F5D1}"}</span></button>
                </div>
                <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", flexShrink: 0, boxSizing: "border-box", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && <button className="ai-window-control" onMouseDown={stopMouse(onHideWindow)} data-testid="ai-hide-toggle" aria-label={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"} style={getWindowControlButtonStyle(t, "hide")} title={lang === "en" ? "Minimize window" : "\u6700\u5c0f\u5316\u7a97\u53e3"}><span style={{ width: "10px", borderTop: "1.5px solid currentColor", transform: "translateY(4px)" }} /></button>}
                    {showMaximizeToggle && <button className="ai-window-control" onMouseDown={stopMouse(() => onToggleMaximize?.())} data-testid="ai-maximize-toggle" aria-label={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")} style={getWindowControlButtonStyle(t, "fullscreen", maximized)} title={maximized ? (lang === "en" ? "Restore window" : "\u8fd8\u539f\u7a97\u53e3") : (lang === "en" ? "Maximize window" : "\u6700\u5927\u5316\u7a97\u53e3")}><WindowsMaximizeRestoreIcon maximized={maximized} activeBg={t.divider} /></button>}
                    {!inline && <button className="ai-window-control" onClick={onClose} style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor, fontSize: "14px" }} title={lang === "en" ? "Close" : "\u5173\u95ed"}>{"x"}</button>}
                </div>
            </div>
        </div>
    );
}

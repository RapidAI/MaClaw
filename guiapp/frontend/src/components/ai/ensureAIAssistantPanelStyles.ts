import { AI_PANEL_STATIC_STYLE_ID, AI_PANEL_STATIC_STYLE_TEXT } from "./aiAssistantPanelTheme";

if (typeof document !== "undefined" && !document.getElementById(AI_PANEL_STATIC_STYLE_ID)) {
    const style = document.createElement("style");
    style.id = AI_PANEL_STATIC_STYLE_ID;
    style.textContent = AI_PANEL_STATIC_STYLE_TEXT;
    document.head.appendChild(style);
}

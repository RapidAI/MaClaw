import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";

interface AssistantWorkflowMaximizeSuggestionProps {
    inline: boolean;
    lang: string;
    maximized: boolean;
    onDismiss: () => void;
    onToggleMaximize?: () => void;
    suggestMaximize?: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
}

export function AssistantWorkflowMaximizeSuggestion({ inline, lang, maximized, onDismiss, onToggleMaximize, suggestMaximize, theme: t, themeMode }: AssistantWorkflowMaximizeSuggestionProps) {
    if (!suggestMaximize || maximized || !inline || !onToggleMaximize) return null;
    return (
        <div data-testid="ai-workflow-maximize-suggestion" style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "10px", padding: "8px 14px", background: themeMode === "dark" ? "rgba(99,102,241,0.16)" : "linear-gradient(90deg, rgba(99,102,241,0.08), rgba(59,130,246,0.08))", borderBottom: `1px solid ${t.titleBarBorder}`, fontSize: "13px", flexShrink: 0 }}>
            <span style={{ color: t.text }}>{localizeText(lang, "Workflow is starting. Maximized view is recommended.", "\u6d41\u7a0b\u5373\u5c06\u5f00\u59cb\uff0c\u6700\u5927\u5316\u89c6\u56fe\u4f53\u9a8c\u66f4\u597d")}</span>
            <div style={{ display: "flex", gap: "6px", flexShrink: 0 }}>
                <button type="button" onClick={() => { onToggleMaximize(); onDismiss(); }} style={{ padding: "4px 12px", fontSize: "12px", border: `1px solid ${t.inputBarBorder}`, borderRadius: "4px", background: t.fieldBg, color: t.headingColor, cursor: "pointer", fontWeight: 500 }}>{localizeText(lang, "Maximize", "\u6700\u5927\u5316")}</button>
                <button type="button" onClick={onDismiss} style={{ padding: "4px 8px", fontSize: "12px", border: "none", background: "transparent", color: t.textMuted, cursor: "pointer" }}>{localizeText(lang, "Dismiss", "\u5ffd\u7565")}</button>
            </div>
        </div>
    );
}

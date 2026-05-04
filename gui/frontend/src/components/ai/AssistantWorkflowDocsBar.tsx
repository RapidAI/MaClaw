import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

interface AssistantWorkflowDocsBarProps {
    currentPhaseID?: string;
    docsBarDismissed?: boolean;
    onDismiss: () => void;
    onOpenDocPreview: (phaseId: string) => void;
    onSelectWorkingDir: () => void;
    phaseDocuments: Map<string, unknown>;
    splitMode?: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    lang: string;
    workingDir?: string;
}

export function AssistantWorkflowDocsBar({
    currentPhaseID,
    docsBarDismissed,
    lang,
    onDismiss,
    onOpenDocPreview,
    onSelectWorkingDir,
    phaseDocuments,
    splitMode,
    theme: t,
    themeMode,
    workingDir,
}: AssistantWorkflowDocsBarProps) {
    if (phaseDocuments.size === 0 || docsBarDismissed) return null;
    return (
        <div data-testid="ai-workflow-docs-bar" style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 14px", borderTop: `1px solid ${t.divider}`, background: t.bg, flexShrink: 0, flexWrap: "wrap" }}>
            <span style={{ fontSize: "11px", color: t.textMuted, flexShrink: 0 }}>{"docs"}</span>
            {Array.from(phaseDocuments.keys()).map(pid => (
                <button key={pid} type="button" onClick={() => onOpenDocPreview(pid)} style={{ padding: "3px 8px", fontSize: "11px", borderRadius: "999px", border: `1px solid ${splitMode && currentPhaseID === pid ? t.headingColor : t.titleBarBorder}`, background: splitMode && currentPhaseID === pid ? (themeMode === "dark" ? "rgba(99,102,241,0.18)" : "rgba(99,102,241,0.08)") : "transparent", color: splitMode && currentPhaseID === pid ? t.headingColor : t.textMuted, cursor: "pointer" }}>
                    {pid}
                </button>
            ))}
            <button type="button" onClick={onSelectWorkingDir} style={{ padding: "3px 8px", fontSize: "11px", borderRadius: "999px", border: `1px solid ${t.titleBarBorder}`, background: "transparent", color: t.textMuted, cursor: "pointer" }} title={workingDir || undefined}>{workingDir ? workingDir.split(/[\/\\]/).pop() : localizeText(lang, "Working dir", "\u5de5\u4f5c\u76ee\u5f55")}</button>
            <button type="button" onClick={onDismiss} style={{ marginLeft: "auto", border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", fontSize: "12px" }}>{"x"}</button>
        </div>
    );
}

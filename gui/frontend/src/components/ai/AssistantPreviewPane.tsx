import type React from "react";
import { CodePreviewPanel, darkCodePreviewTheme, lightCodePreviewTheme } from "./CodePreviewPanel";
import { WorkflowDocPreview } from "./WorkflowDocPreview";
import type { Theme } from "./aiAssistantPanelTheme";
import { AgentTaskPanel } from "./AgentTaskPanel";
import type { AgentView } from "./agentViewTypes";
import type { CodePreviewUIState } from "./useCodePreviewState";
import type { WorkflowUIState } from "./useWorkflowState";

interface AssistantPreviewPaneProps {
    agentView?: AgentView | null;
    codePreviewState: CodePreviewUIState;
    closeCodePreview: () => void;
    closeDocPreview: () => void;
    dismissAgentView?: (viewId: string | undefined, data?: Record<string, unknown>) => void | Promise<void>;
    inline: boolean;
    lang: string;
    onToggleMaximize?: () => void;
    selectCodeFile: (filePath: string) => void;
    submitAgentView?: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    showCodePreview: boolean;
    showAgentView: boolean;
    showWorkflowPreview: boolean;
    splitRatio: number;
    startPreviewResize: () => void;
    theme: Theme;
    themeMode: "dark" | "light";
    workflowState: WorkflowUIState;
}

export function AssistantPreviewPane({
    agentView,
    codePreviewState,
    closeCodePreview,
    closeDocPreview,
    dismissAgentView,
    inline,
    lang,
    onToggleMaximize,
    selectCodeFile,
    submitAgentView,
    showCodePreview,
    showAgentView,
    showWorkflowPreview,
    splitRatio,
    startPreviewResize,
    theme,
    themeMode,
    workflowState,
}: AssistantPreviewPaneProps) {
    const paneStyle: React.CSSProperties = {
        flex: Math.max(0.2, 1 - splitRatio),
        minWidth: 0,
        height: "100%",
    };
    if (showAgentView && agentView) {
        return (
            <div style={paneStyle}>
                <AgentTaskPanel
                    view={agentView}
                    onDismiss={dismissAgentView}
                    onResizeStart={startPreviewResize}
                    onToggleMaximize={inline ? onToggleMaximize : undefined}
                    onSubmit={submitAgentView}
                    theme={theme}
                    lang={lang}
                />
            </div>
        );
    }
    if (showWorkflowPreview) {
        return (
            <div style={paneStyle}>
                <WorkflowDocPreview
                    phaseDocuments={workflowState.phaseDocuments}
                    currentPhaseID={workflowState.currentPhaseID}
                    latestDocumentPhaseID={workflowState.latestDocumentPhaseID}
                    phases={workflowState.phases}
                    workflowType={workflowState.workflowType}
                    gateResults={workflowState.gateResults}
                    lang={lang}
                    onClose={closeDocPreview}
                    onToggleMaximize={inline ? onToggleMaximize : undefined}
                    onResizeStart={startPreviewResize}
                    theme={{
                        bg: theme.bg,
                        text: theme.text,
                        textMuted: theme.textMuted,
                        border: theme.divider,
                        headerBg: theme.titleBarBg,
                        accentColor: theme.headingColor,
                        accentBg: themeMode === "dark" ? "rgba(99,102,241,0.15)" : "rgba(99,102,241,0.08)",
                        codeBg: theme.codeBg,
                        codeText: theme.codeText,
                        codeBlockBg: theme.codeBlockBg,
                        codeBlockBorder: theme.codeBlockBorder,
                        headingColor: theme.headingColor,
                        linkColor: theme.linkColor,
                        quoteBorder: theme.quoteBorder,
                        quoteText: theme.quoteText,
                        quoteBg: themeMode === "dark" ? "rgba(99,102,241,0.08)" : "rgba(99,102,241,0.04)",
                    }}
                />
            </div>
        );
    }
    if (!showCodePreview) return null;
    return (
        <div style={paneStyle}>
            <CodePreviewPanel
                files={codePreviewState.files}
                activeFilePath={codePreviewState.activeFilePath}
                onSelectFile={selectCodeFile}
                onClose={closeCodePreview}
                onResizeStart={startPreviewResize}
                onToggleMaximize={inline ? onToggleMaximize : undefined}
                theme={themeMode === "dark" ? darkCodePreviewTheme : lightCodePreviewTheme}
            />
        </div>
    );
}

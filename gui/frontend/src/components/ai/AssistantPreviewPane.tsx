import React, { useEffect, useRef, useState } from "react";
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

type PreviewPaneMode = "workflow" | "code";

function previewTabID(mode: PreviewPaneMode): string {
    return `assistant-preview-tab-${mode}`;
}

function previewPanelID(mode: PreviewPaneMode): string {
    return `assistant-preview-panel-${mode}`;
}

function previewTabLabel(mode: PreviewPaneMode, lang: string): string {
    if (mode === "workflow") {
        return lang === "en" ? "Progress" : "\u6d41\u7a0b/\u8fdb\u5ea6";
    }
    return lang === "en" ? "Source" : "\u6e90\u7801\u67e5\u770b";
}

function PreviewModeTabs({
    activeMode,
    lang,
    onSelectMode,
    theme,
}: {
    activeMode: PreviewPaneMode;
    lang: string;
    onSelectMode: (mode: PreviewPaneMode) => void;
    theme: Theme;
}) {
    const modes: PreviewPaneMode[] = ["workflow", "code"];
    const tabRefs = useRef<Partial<Record<PreviewPaneMode, HTMLButtonElement>>>({});
    const selectMode = (mode: PreviewPaneMode, focusTab = false) => {
        onSelectMode(mode);
        if (focusTab) {
            requestAnimationFrame(() => tabRefs.current[mode]?.focus());
        }
    };
    const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
        if (event.key === "Home") {
            event.preventDefault();
            selectMode(modes[0], true);
            return;
        }
        if (event.key === "End") {
            event.preventDefault();
            selectMode(modes[modes.length - 1], true);
            return;
        }
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
            event.preventDefault();
            const currentIndex = modes.indexOf(activeMode);
            const offset = event.key === "ArrowRight" ? 1 : -1;
            const nextMode = modes[(currentIndex + offset + modes.length) % modes.length];
            selectMode(nextMode, true);
        }
    };
    return (
        <div
            data-testid="assistant-preview-mode-tabs"
            data-preview-no-maximize="true"
            role="tablist"
            aria-label={lang === "en" ? "Preview mode" : "\u9884\u89c8\u6a21\u5f0f"}
            onKeyDown={handleKeyDown}
            style={{
                display: "flex",
                alignItems: "center",
                gap: "6px",
                padding: "6px 10px",
                borderBottom: `1px solid ${theme.divider}`,
                background: theme.titleBarBg,
                flexShrink: 0,
                '--wails-draggable': 'drag',
            } as any}
        >
            {modes.map((mode) => {
                const active = activeMode === mode;
                return (
                    <button
                        key={mode}
                        type="button"
                        role="tab"
                        id={previewTabID(mode)}
                        ref={(node) => {
                            if (node) tabRefs.current[mode] = node;
                            else delete tabRefs.current[mode];
                        }}
                        aria-selected={active}
                        aria-controls={previewPanelID(mode)}
                        tabIndex={active ? 0 : -1}
                        onClick={() => selectMode(mode)}
                        style={{
                            border: `1px solid ${active ? theme.headingColor : theme.divider}`,
                            background: active ? theme.codeBg : "transparent",
                            color: active ? theme.headingColor : theme.textMuted,
                            borderRadius: "6px",
                            cursor: "pointer",
                            fontSize: "12px",
                            fontWeight: active ? 700 : 600,
                            lineHeight: 1,
                            padding: "6px 10px",
                            minHeight: "28px",
                            whiteSpace: "nowrap",
                            '--wails-draggable': 'no-drag',
                        } as any}
                    >
                        {previewTabLabel(mode, lang)}
                    </button>
                );
            })}
        </div>
    );
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
    const [activeMode, setActiveMode] = useState<PreviewPaneMode>("workflow");
    const previousShowCodeRef = useRef(showCodePreview);
    const previousShowWorkflowRef = useRef(showWorkflowPreview);

    useEffect(() => {
        const codeOpened = showCodePreview && !previousShowCodeRef.current;
        const workflowOpened = showWorkflowPreview && !previousShowWorkflowRef.current;
        previousShowCodeRef.current = showCodePreview;
        previousShowWorkflowRef.current = showWorkflowPreview;

        if (!showWorkflowPreview && showCodePreview) {
            setActiveMode("code");
            return;
        }
        if (showWorkflowPreview && !showCodePreview) {
            setActiveMode("workflow");
            return;
        }
        if (codeOpened) {
            setActiveMode("code");
            return;
        }
        if (workflowOpened && activeMode !== "code") {
            setActiveMode("workflow");
        }
    }, [activeMode, showCodePreview, showWorkflowPreview]);

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
    if (showWorkflowPreview && showCodePreview) {
        return (
            <div style={{ ...paneStyle, display: "flex", flexDirection: "column" }}>
                <PreviewModeTabs activeMode={activeMode} lang={lang} onSelectMode={setActiveMode} theme={theme} />
                <div
                    id={previewPanelID(activeMode)}
                    role="tabpanel"
                    aria-labelledby={previewTabID(activeMode)}
                    style={{ flex: "1 1 auto", minHeight: 0, overflow: "hidden" }}
                >
                    {activeMode === "workflow" ? (
                        <WorkflowPreviewContent closeDocPreview={closeDocPreview} inline={inline} lang={lang} onToggleMaximize={onToggleMaximize} startPreviewResize={startPreviewResize} theme={theme} themeMode={themeMode} workflowState={workflowState} />
                    ) : (
                        <CodePreviewContent codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} inline={inline} onToggleMaximize={onToggleMaximize} selectCodeFile={selectCodeFile} startPreviewResize={startPreviewResize} themeMode={themeMode} />
                    )}
                </div>
            </div>
        );
    }
    if (showWorkflowPreview) {
        return (
            <div style={paneStyle}>
                <WorkflowPreviewContent closeDocPreview={closeDocPreview} inline={inline} lang={lang} onToggleMaximize={onToggleMaximize} startPreviewResize={startPreviewResize} theme={theme} themeMode={themeMode} workflowState={workflowState} />
            </div>
        );
    }
    if (!showCodePreview) return null;
    return (
        <div style={paneStyle}>
            <CodePreviewContent codePreviewState={codePreviewState} closeCodePreview={closeCodePreview} inline={inline} onToggleMaximize={onToggleMaximize} selectCodeFile={selectCodeFile} startPreviewResize={startPreviewResize} themeMode={themeMode} />
        </div>
    );
}

function CodePreviewContent({
    codePreviewState,
    closeCodePreview,
    inline,
    onToggleMaximize,
    selectCodeFile,
    startPreviewResize,
    themeMode,
}: {
    codePreviewState: CodePreviewUIState;
    closeCodePreview: () => void;
    inline: boolean;
    onToggleMaximize?: () => void;
    selectCodeFile: (filePath: string) => void;
    startPreviewResize: () => void;
    themeMode: "dark" | "light";
}) {
    return (
        <CodePreviewPanel
            files={codePreviewState.files}
            activeFilePath={codePreviewState.activeFilePath}
            onSelectFile={selectCodeFile}
            onClose={closeCodePreview}
            onResizeStart={startPreviewResize}
            onToggleMaximize={inline ? onToggleMaximize : undefined}
            theme={themeMode === "dark" ? darkCodePreviewTheme : lightCodePreviewTheme}
        />
    );
}

function WorkflowPreviewContent({
    closeDocPreview,
    inline,
    lang,
    onToggleMaximize,
    startPreviewResize,
    theme,
    themeMode,
    workflowState,
}: {
    closeDocPreview: () => void;
    inline: boolean;
    lang: string;
    onToggleMaximize?: () => void;
    startPreviewResize: () => void;
    theme: Theme;
    themeMode: "dark" | "light";
    workflowState: WorkflowUIState;
}) {
    return (
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
    );
}

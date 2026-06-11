import React, { useEffect, useMemo, useRef, useState } from "react";
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
    lang: string;
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

function previewTabIcon(mode: PreviewPaneMode): string {
    return mode === "workflow" ? "WF" : "SRC";
}

function previewTabTooltip(mode: PreviewPaneMode, lang: string): string {
    if (mode === "workflow") {
        return lang === "en" ? "Progress" : "\u6d41\u7a0b/\u8fdb\u5ea6";
    }
    return lang === "en" ? "Source" : "\u6e90\u7801\u67e5\u770b";
}

/**
 * Vertical tab rail on the right edge of the preview pane.
 * Contains mode tabs + close button.
 */
function PreviewTabRail({
    activeMode,
    availableModes,
    lang,
    onClose,
    onSelectMode,
    theme,
}: {
    activeMode: PreviewPaneMode;
    availableModes: PreviewPaneMode[];
    lang: string;
    onClose: () => void;
    onSelectMode: (mode: PreviewPaneMode) => void;
    theme: Theme;
}) {
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
            selectMode(availableModes[0], true);
            return;
        }
        if (event.key === "End") {
            event.preventDefault();
            selectMode(availableModes[availableModes.length - 1], true);
            return;
        }
        if (event.key === "ArrowUp" || event.key === "ArrowDown" || event.key === "ArrowLeft" || event.key === "ArrowRight") {
            event.preventDefault();
            const currentIndex = availableModes.indexOf(activeMode);
            const offset = event.key === "ArrowDown" || event.key === "ArrowRight" ? 1 : -1;
            const nextMode = availableModes[(currentIndex + offset + availableModes.length) % availableModes.length];
            selectMode(nextMode, true);
        }
    };
    return (
        <div
            data-testid="assistant-preview-mode-tabs"
            style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: "4px",
                padding: "8px 4px",
                borderLeft: `1px solid ${theme.divider}`,
                background: theme.titleBarBg,
                flexShrink: 0,
                width: "32px",
            }}
        >
            <button
                type="button"
                onClick={onClose}
                style={{
                    width: "26px",
                    height: "26px",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    background: "none",
                    border: "none",
                    cursor: "pointer",
                    fontSize: "14px",
                    padding: 0,
                    borderRadius: "4px",
                    color: theme.textMuted,
                    lineHeight: 1,
                    marginBottom: "4px",
                }}
                title={lang === "en" ? "Close preview" : "\u5173\u95ed\u9884\u89c8"}
                aria-label={lang === "en" ? "Close preview" : "\u5173\u95ed\u9884\u89c8"}
            >
                X
            </button>
            <div
                role="tablist"
                aria-orientation="vertical"
                aria-label={lang === "en" ? "Preview mode" : "\u9884\u89c8\u6a21\u5f0f"}
                onKeyDown={handleKeyDown}
                style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "4px" }}
            >
            {availableModes.map((mode) => {
                const active = activeMode === mode;
                return (
                    <button
                        key={mode}
                        type="button"
                        role="tab"
                        id={`assistant-preview-tab-${mode}`}
                        ref={(node) => {
                            if (node) tabRefs.current[mode] = node;
                            else delete tabRefs.current[mode];
                        }}
                        aria-selected={active}
                        aria-controls={`assistant-preview-panel-${mode}`}
                        tabIndex={active ? 0 : -1}
                        onClick={() => selectMode(mode)}
                        style={{
                            width: "26px",
                            height: "26px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            border: `1px solid ${active ? theme.headingColor : "transparent"}`,
                            background: active ? theme.codeBg : "transparent",
                            color: active ? theme.headingColor : theme.textMuted,
                            borderRadius: "6px",
                            cursor: "pointer",
                            fontSize: "9px",
                            fontWeight: 700,
                            letterSpacing: "0",
                            lineHeight: 1,
                            padding: 0,
                        }}
                        title={previewTabTooltip(mode, lang)}
                        aria-label={previewTabTooltip(mode, lang)}
                    >
                        {previewTabIcon(mode)}
                    </button>
                );
            })}
            </div>
        </div>
    );
}

export function AssistantPreviewPane({
    agentView,
    codePreviewState,
    closeCodePreview,
    closeDocPreview,
    dismissAgentView,
    lang,
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

    const docPreviewTheme = useMemo(() => ({
        bg: themeMode === "dark" ? theme.bg : "#ffffff",
        text: themeMode === "dark" ? theme.text : "#1f2937",
        textMuted: themeMode === "dark" ? theme.textMuted : "#6b7280",
        border: themeMode === "dark" ? theme.divider : "#e5e7eb",
        headerBg: themeMode === "dark" ? theme.titleBarBg : "#f7f8fa",
        accentColor: themeMode === "dark" ? "#b7d3ef" : "#3f5872",
        accentBg: themeMode === "dark" ? "rgba(91, 120, 152, 0.16)" : "#f5f7fa",
        codeBg: themeMode === "dark" ? theme.codeBg : "#f1f4f7",
        codeText: themeMode === "dark" ? theme.codeText : "#334155",
        codeBlockBg: themeMode === "dark" ? theme.codeBlockBg : "#f8fafc",
        codeBlockBorder: themeMode === "dark" ? theme.codeBlockBorder : "#e5e7eb",
        headingColor: themeMode === "dark" ? "#d9e7f5" : "#1f2937",
        linkColor: themeMode === "dark" ? "#9bc2ea" : "#2f5f98",
        quoteBorder: themeMode === "dark" ? theme.quoteBorder : "#c7d1dc",
        quoteText: themeMode === "dark" ? theme.quoteText : "#4b5563",
        quoteBg: themeMode === "dark" ? "rgba(91, 120, 152, 0.12)" : "#f8fafc",
    }), [theme, themeMode]);

    const codeTheme = useMemo(
        () => themeMode === "dark" ? darkCodePreviewTheme : lightCodePreviewTheme,
        [themeMode],
    );

    const paneStyle: React.CSSProperties = {
        flex: Math.max(0.2, 1 - splitRatio),
        minWidth: 0,
        height: "100%",
        display: "flex",
        flexDirection: "row",
        position: "relative",
    };

    // AgentTaskPanel has its own layout (no tab rail needed)
    if (showAgentView && agentView) {
        return (
            <div style={paneStyle}>
                <AgentTaskPanel
                    view={agentView}
                    onDismiss={dismissAgentView}
                    onResizeStart={startPreviewResize}
                    onSubmit={submitAgentView}
                    theme={theme}
                    lang={lang}
                />
            </div>
        );
    }

    // Determine which modes are available
    const availableModes: PreviewPaneMode[] = [];
    if (showWorkflowPreview) availableModes.push("workflow");
    if (showCodePreview) availableModes.push("code");
    if (availableModes.length === 0) return null;

    // Ensure activeMode is valid
    const effectiveMode = availableModes.includes(activeMode) ? activeMode : availableModes[0];

    const handleClose = () => {
        // Close the entire preview pane (both modes)
        if (showWorkflowPreview) closeDocPreview();
        if (showCodePreview) closeCodePreview();
    };

    return (
        <div style={paneStyle}>
            {/* ── Drag handle for resizing ── */}
            <div
                onMouseDown={(e) => { e.preventDefault(); startPreviewResize(); }}
                style={{
                    position: "absolute",
                    left: 0,
                    top: 0,
                    bottom: 0,
                    width: "6px",
                    cursor: "col-resize",
                    background: theme.divider,
                    transition: "background 0.15s",
                    zIndex: 1,
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.headingColor; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = theme.divider; }}
            />
            {/* ── Content area ── */}
            <div
                id={`assistant-preview-panel-${effectiveMode}`}
                role="tabpanel"
                aria-labelledby={`assistant-preview-tab-${effectiveMode}`}
                style={{ flex: 1, minWidth: 0, minHeight: 0, overflow: "hidden", marginLeft: "6px" }}
            >
                {effectiveMode === "workflow" ? (
                    <WorkflowDocPreview
                        phaseDocuments={workflowState.phaseDocuments}
                        currentPhaseID={workflowState.currentPhaseID}
                        latestDocumentPhaseID={workflowState.latestDocumentPhaseID}
                        phases={workflowState.phases}
                        workflowType={workflowState.workflowType}
                        gateResults={workflowState.gateResults}
                        lang={lang}
                        theme={docPreviewTheme}
                    />
                ) : (
                    <CodePreviewPanel
                        files={codePreviewState.files}
                        activeFilePath={codePreviewState.activeFilePath}
                        onSelectFile={selectCodeFile}
                        onClose={closeCodePreview}
                        onResizeStart={startPreviewResize}
                        theme={codeTheme}
                    />
                )}
            </div>
            {/* ── Vertical tab rail on right edge ── */}
            {availableModes.length > 1 && (
                <PreviewTabRail
                    activeMode={effectiveMode}
                    availableModes={availableModes}
                    lang={lang}
                    onClose={handleClose}
                    onSelectMode={setActiveMode}
                    theme={theme}
                />
            )}
            {/* ── Close button when only one mode (no tab rail) ── */}
            {availableModes.length === 1 && (
                <div style={{
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    padding: "8px 4px",
                    borderLeft: `1px solid ${theme.divider}`,
                    background: theme.titleBarBg,
                    flexShrink: 0,
                    width: "32px",
                }}>
                    <button
                        type="button"
                        onClick={handleClose}
                        style={{
                            width: "26px",
                            height: "26px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            background: "none",
                            border: "none",
                            cursor: "pointer",
                            fontSize: "14px",
                            padding: 0,
                            borderRadius: "4px",
                            color: theme.textMuted,
                            lineHeight: 1,
                        }}
                        title={lang === "en" ? "Close preview" : "\u5173\u95ed\u9884\u89c8"}
                        aria-label={lang === "en" ? "Close preview" : "\u5173\u95ed\u9884\u89c8"}
                    >
                        ×
                    </button>
                </div>
            )}
        </div>
    );
}

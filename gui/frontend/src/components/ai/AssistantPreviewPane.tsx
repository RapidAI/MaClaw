import React, { useEffect, useMemo, useRef, useState } from "react";
import { CodePreviewPanel, createCodePreviewTheme, maximumContrastInkOnFill } from "./CodePreviewPanel";
import { WorkflowDocPreview } from "./WorkflowDocPreview";
import { contrastingInkOnFill, type Theme } from "./aiAssistantPanelTheme";
import { AgentTaskPanel } from "./AgentTaskPanel";
import type { AgentView } from "./agentViewTypes";
import type { CodePreviewUIState } from "./useCodePreviewState";
import type { CodeFile } from "./useCodePreviewState";
import type { WorkflowUIState } from "./useWorkflowState";

interface AssistantPreviewPaneProps {
    agentView?: AgentView | null;
    codePreviewState: CodePreviewUIState;
    closeCodePreview: () => void;
    closeCodeFile?: (filePath: string) => void;
    closeOtherCodeFiles?: (keepPath: string) => void;
    closeCodeFilesToTheRight?: (fromPath: string) => void;
    closeAllCodeFiles?: () => void;
    moveCodeFile?: (fromPath: string, toIndex: number) => void;
    toggleCodeFilePinned?: (filePath: string) => void;
    closeDocPreview: () => void;
    dismissAgentView?: (viewId: string | undefined, data?: Record<string, unknown>, options?: { force?: boolean }) => void | Promise<void>;
    lang: string;
    selectCodeFile: (filePath: string) => void;
    projectPath?: string;
    openWorkspaceFile?: (file: CodeFile) => void;
    submitAgentView?: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    showCodePreview: boolean;
    showAgentView: boolean;
    showWorkflowPreview: boolean;
    /** Isolation conflict side panel (tabs with source when both active). */
    showConflict?: boolean;
    conflictContent?: React.ReactNode;
    /** Remaining conflict count — shown as CF tab badge. */
    conflictCount?: number;
    onCloseConflict?: () => void;
    splitRatio: number;
    startPreviewResize: () => void;
    onToggleMaximize?: () => void;
    theme: Theme;
    workflowState: WorkflowUIState;
}

type PreviewPaneMode = "workflow" | "code" | "agent" | "conflict";

function previewTabIcon(mode: PreviewPaneMode): string {
    if (mode === "agent") return "AG";
    if (mode === "conflict") return "CF";
    return mode === "workflow" ? "WF" : "SRC";
}

function previewTabTooltip(mode: PreviewPaneMode, lang: string): string {
    if (mode === "workflow") {
        return lang === "en" ? "Progress" : "\u6d41\u7a0b/\u8fdb\u5ea6";
    }
    if (mode === "agent") {
        return lang === "en" ? "Agent Task" : "\u667a\u80fd\u4f53\u4efb\u52a1";
    }
    if (mode === "conflict") {
        return lang === "en" ? "Conflicts" : "\u51b2\u7a81";
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
    conflictCount = 0,
}: {
    activeMode: PreviewPaneMode;
    availableModes: PreviewPaneMode[];
    lang: string;
    onClose: () => void;
    onSelectMode: (mode: PreviewPaneMode) => void;
    theme: Theme;
    conflictCount?: number;
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
                const badge = mode === "conflict" && conflictCount > 0 ? (conflictCount > 9 ? "9+" : String(conflictCount)) : "";
                const label = badge
                    ? `${previewTabTooltip(mode, lang)} (${conflictCount})`
                    : previewTabTooltip(mode, lang);
                return (
                    <button
                        key={mode}
                        type="button"
                        role="tab"
                        id={`assistant-preview-tab-${mode}`}
                        data-testid={mode === "conflict" ? "assistant-preview-tab-conflict" : undefined}
                        ref={(node) => {
                            if (node) tabRefs.current[mode] = node;
                            else delete tabRefs.current[mode];
                        }}
                        aria-selected={active}
                        aria-controls={`assistant-preview-panel-${mode}`}
                        tabIndex={active ? 0 : -1}
                        onClick={() => selectMode(mode)}
                        style={{
                            position: "relative",
                            width: "26px",
                            height: "26px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            border: `1px solid ${active ? (mode === "conflict" ? theme.errorBorder : theme.headingColor) : "transparent"}`,
                            background: active ? (mode === "conflict" ? theme.errorBg : theme.codeBg) : "transparent",
                            color: active ? (mode === "conflict" ? theme.errorText : theme.headingColor) : theme.textMuted,
                            borderRadius: "6px",
                            cursor: "pointer",
                            fontSize: "9px",
                            fontWeight: 700,
                            letterSpacing: "0",
                            lineHeight: 1,
                            padding: 0,
                        }}
                        title={label}
                        aria-label={label}
                    >
                        {previewTabIcon(mode)}
                        {badge ? (
                            <span
                                data-testid="assistant-preview-conflict-badge"
                                style={{
                                    position: "absolute",
                                    top: -3,
                                    right: -3,
                                    minWidth: 12,
                                    height: 12,
                                    padding: "0 3px",
                                    borderRadius: 999,
                                    background: theme.errorText,
                                    color: maximumContrastInkOnFill(theme.errorText),
                                    fontSize: 8,
                                    lineHeight: "12px",
                                    textAlign: "center",
                                    fontWeight: 700,
                                }}
                            >
                                {badge}
                            </span>
                        ) : null}
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
    closeCodeFile,
    closeOtherCodeFiles,
    closeCodeFilesToTheRight,
    closeAllCodeFiles,
    moveCodeFile,
    toggleCodeFilePinned,
    closeDocPreview,
    dismissAgentView,
    lang,
    selectCodeFile,
    projectPath,
    openWorkspaceFile,
    submitAgentView,
    showCodePreview,
    showAgentView,
    showWorkflowPreview,
    showConflict = false,
    conflictContent = null,
    conflictCount = 0,
    onCloseConflict,
    splitRatio,
    startPreviewResize,
    onToggleMaximize,
    theme,
    workflowState,
}: AssistantPreviewPaneProps) {
    const [activeMode, setActiveMode] = useState<PreviewPaneMode>("workflow");
    const previousShowCodeRef = useRef(showCodePreview);
    const previousShowWorkflowRef = useRef(showWorkflowPreview);
    const previousShowAgentRef = useRef(showAgentView);
    const previousShowConflictRef = useRef(showConflict);

    useEffect(() => {
        const codeOpened = showCodePreview && !previousShowCodeRef.current;
        const workflowOpened = showWorkflowPreview && !previousShowWorkflowRef.current;
        const agentOpened = showAgentView && !previousShowAgentRef.current;
        const conflictOpened = showConflict && !previousShowConflictRef.current;
        previousShowCodeRef.current = showCodePreview;
        previousShowWorkflowRef.current = showWorkflowPreview;
        previousShowAgentRef.current = showAgentView;
        previousShowConflictRef.current = showConflict;

        // Prefer newly opened conflict tab so three-way is immediately visible.
        if (conflictOpened) {
            setActiveMode("conflict");
            return;
        }
        // Prefer a newly opened agent form over code/workflow: forms need user input
        // and were previously skipped whenever source/workflow tabs were already active
        // (common on coding workflows where source preview is allowed).
        if (agentOpened) {
            setActiveMode("agent");
            return;
        }
        if (!showWorkflowPreview && !showAgentView && !showConflict && showCodePreview) {
            setActiveMode("code");
            return;
        }
        if (showWorkflowPreview && !showCodePreview && !showAgentView && !showConflict) {
            setActiveMode("workflow");
            return;
        }
        if (showAgentView && !showWorkflowPreview && !showCodePreview && !showConflict) {
            setActiveMode("agent");
            return;
        }
        if (showConflict && !showWorkflowPreview && !showCodePreview && !showAgentView) {
            setActiveMode("conflict");
            return;
        }
        // Keep an open agent form in front while source files stream in; user can
        // still switch to SRC manually. Without this, codeOpened steals focus mid-form.
        if (codeOpened && !showAgentView) {
            setActiveMode("code");
            return;
        }
        if (workflowOpened && !showAgentView) {
            setActiveMode("workflow");
            return;
        }
    }, [showAgentView, showCodePreview, showWorkflowPreview, showConflict]);

    const docPreviewTheme = useMemo(() => ({
        isDark: theme.isDark === true,
        bg: theme.bg,
        text: theme.text,
        textMuted: theme.textMuted,
        border: theme.divider,
        headerBg: theme.titleBarBg,
        accentColor: theme.btnColor,
        accentBg: `color-mix(in srgb, ${theme.btnColor} ${theme.isDark ? 12 : 8}%, ${theme.fieldBg})`,
        accentText: contrastingInkOnFill(theme.btnColor),
        successColor: theme.isDark ? "#7aa89a" : "#3f685b",
        successBg: `color-mix(in srgb, ${theme.isDark ? "#7aa89a" : "#3f685b"} ${theme.isDark ? 18 : 12}%, ${theme.fieldBg})`,
        successText: maximumContrastInkOnFill(theme.isDark ? "#7aa89a" : "#3f685b"),
        dangerColor: theme.errorText,
        dangerBg: theme.errorBg,
        dangerText: maximumContrastInkOnFill(theme.errorText),
        codeBg: theme.codeBg,
        codeText: theme.codeText,
        codeBlockBg: theme.codeBlockBg,
        codeBlockBorder: theme.codeBlockBorder,
        headingColor: theme.headingColor,
        linkColor: theme.linkColor,
        quoteBorder: theme.quoteBorder,
        quoteText: theme.quoteText,
        quoteBg: `color-mix(in srgb, ${theme.quoteBorder} ${theme.isDark ? 14 : 10}%, ${theme.fieldBg})`,
    }), [theme]);

    const codeTheme = useMemo(() => createCodePreviewTheme(theme), [theme]);

    const paneStyle: React.CSSProperties = {
        flex: Math.max(0.2, 1 - splitRatio),
        minWidth: 0,
        height: "100%",
        display: "flex",
        flexDirection: "row",
        position: "relative",
    };

    // AgentTaskPanel is now integrated into the tab system (no longer exclusive)
    // Determine which modes are available
    const availableModes: PreviewPaneMode[] = [];
    if (showConflict && conflictContent) availableModes.push("conflict");
    if (showAgentView && agentView) availableModes.push("agent");
    if (showWorkflowPreview) availableModes.push("workflow");
    if (showCodePreview) availableModes.push("code");
    if (availableModes.length === 0) return null;

    // Ensure activeMode is valid
    const effectiveMode = availableModes.includes(activeMode) ? activeMode : availableModes[0];

    const handleClose = () => {
        // Close the active tab's surface first when multiple modes are open.
        if (effectiveMode === "conflict") {
            onCloseConflict?.();
            return;
        }
        if (effectiveMode === "code") {
            closeCodePreview();
            return;
        }
        if (effectiveMode === "workflow") {
            closeDocPreview();
            return;
        }
        // Close all non-agent preview modes. Agent is closed via its own dismiss.
        if (showWorkflowPreview) closeDocPreview();
        if (showCodePreview) closeCodePreview();
        if (showConflict) onCloseConflict?.();
    };

    // Agent-only: no tab rail needed, render standalone
    if (availableModes.length === 1 && availableModes[0] === "agent") {
        return (
            <div style={paneStyle}>
                <AgentTaskPanel
                    view={agentView!}
                    onDismiss={dismissAgentView}
                    onResizeStart={startPreviewResize}
                    onToggleMaximize={onToggleMaximize}
                    onSubmit={submitAgentView}
                    theme={theme}
                    lang={lang}
                />
            </div>
        );
    }

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
            {/* ── Content area ──
                Conflict panel stays mounted (hidden) when switching to SRC so scroll / draft state
                and Esc-focus scoping survive tab switches. */}
            <div
                id={`assistant-preview-panel-${effectiveMode}`}
                role="tabpanel"
                aria-labelledby={`assistant-preview-tab-${effectiveMode}`}
                style={{ flex: 1, minWidth: 0, minHeight: 0, overflow: "hidden", marginLeft: "6px", position: "relative" }}
            >
                {showConflict && conflictContent ? (
                    <div
                        data-testid="assistant-preview-conflict-slot"
                        style={{
                            display: effectiveMode === "conflict" ? "flex" : "none",
                            flexDirection: "column",
                            height: "100%",
                            minHeight: 0,
                        }}
                        // Keep mounted for scroll/draft; hide from a11y + Esc ownership when not active.
                        aria-hidden={effectiveMode === "conflict" ? undefined : true}
                    >
                        {conflictContent}
                    </div>
                ) : null}
                {effectiveMode === "agent" && agentView ? (
                    <AgentTaskPanel
                        view={agentView}
                        onDismiss={dismissAgentView}
                        onToggleMaximize={onToggleMaximize}
                        onSubmit={submitAgentView}
                        theme={theme}
                        lang={lang}
                    />
                ) : effectiveMode === "workflow" ? (
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
                ) : effectiveMode === "code" || (effectiveMode !== "conflict" && showCodePreview) ? (
                    <CodePreviewPanel
                        files={codePreviewState.files}
                        activeFilePath={codePreviewState.activeFilePath}
                        pinnedPaths={codePreviewState.pinnedPaths}
                        mruOrder={codePreviewState.mruOrder}
                        onSelectFile={selectCodeFile}
                        projectPath={projectPath}
                        onOpenWorkspaceFile={openWorkspaceFile}
                        onCloseFile={closeCodeFile}
                        onCloseOtherFiles={closeOtherCodeFiles}
                        onCloseFilesToTheRight={closeCodeFilesToTheRight}
                        onCloseAllFiles={closeAllCodeFiles}
                        onMoveFile={moveCodeFile}
                        onTogglePinFile={toggleCodeFilePinned}
                        onClose={closeCodePreview}
                        onResizeStart={startPreviewResize}
                        onToggleMaximize={onToggleMaximize}
                        theme={codeTheme}
                        lang={lang}
                    />
                ) : null}
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
                    conflictCount={conflictCount}
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
                        X
                    </button>
                </div>
            )}
        </div>
    );
}

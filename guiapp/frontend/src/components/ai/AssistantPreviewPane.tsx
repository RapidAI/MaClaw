import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CodePreviewPanel, createCodePreviewTheme, maximumContrastInkOnFill } from "./CodePreviewPanel";
import { WorkflowDocPreview } from "./WorkflowDocPreview";
import { contrastingInkOnFill, type Theme } from "./aiAssistantPanelTheme";
import { AgentTaskPanel } from "./AgentTaskPanel";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import { usePreviewSlideLifecycle } from "./previewSlide";
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
    workspaceRefreshToken?: number;
    workspaceResetOnRefresh?: boolean;
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
    startPreviewResize: (startEvent?: MouseEvent | PointerEvent | number) => void;
    onToggleMaximize?: () => void;
    theme: Theme;
    workflowState: WorkflowUIState;
    cloudMode?: boolean;
    cloudWorkspaceName?: string;
}

type PreviewPaneMode = "workflow" | "code" | "agent" | "conflict";

function previewTabIcon(mode: PreviewPaneMode): string {
    if (mode === "agent") return "AG";
    if (mode === "conflict") return "CF";
    return mode === "workflow" ? "WF" : "SRC";
}

function previewTabTooltip(mode: PreviewPaneMode, lang: string, cloudMode = false): string {
    if (mode === "workflow") {
        return lang === "en" ? "Progress" : "\u6d41\u7a0b/\u8fdb\u5ea6";
    }
    if (mode === "agent") {
        return lang === "en" ? "Agent Task" : "\u667a\u80fd\u4f53\u4efb\u52a1";
    }
    if (mode === "conflict") {
        return lang === "en" ? "Conflicts" : "\u51b2\u7a81";
    }
    return cloudMode
        ? (lang === "en" ? "Cloud files" : "\u4e91\u7aef\u6587\u4ef6")
        : (lang === "en" ? "Source" : "\u6e90\u7801\u67e5\u770b");
}

/** Neutral preview chrome. Keep these out of the assistant scheme's blue-tinted divider. */
export const PREVIEW_SURFACE_FRAME = {
    lightBorder: "#e4e4e4",
    darkBorder: "rgba(255, 255, 255, 0.12)",
    lightBorderActive: "#c8c8c8",
    darkBorderActive: "rgba(255, 255, 255, 0.22)",
} as const;

function previewSurfaceVars(theme: Theme): React.CSSProperties {
    const dark = theme.isDark === true;
    return {
        "--mc-preview-pane-bg": theme.bg,
        "--mc-preview-surface-bg": theme.bg,
        "--mc-preview-surface-border": dark ? PREVIEW_SURFACE_FRAME.darkBorder : PREVIEW_SURFACE_FRAME.lightBorder,
        "--mc-preview-surface-border-active": dark
            ? PREVIEW_SURFACE_FRAME.darkBorderActive
            : PREVIEW_SURFACE_FRAME.lightBorderActive,
        "--mc-preview-surface-shadow": dark
            ? "0 1px 2px rgba(0, 0, 0, 0.32), 0 12px 32px -12px rgba(0, 0, 0, 0.45)"
            : "0 1px 2px rgba(0, 0, 0, 0.04), 0 12px 28px -14px rgba(0, 0, 0, 0.10)",
    } as React.CSSProperties;
}

/** Flush-right overlay surface: rounded corners only on the exposed left edge. */
const overlaySurfaceStyle: React.CSSProperties = {
    borderRadius: "12px 0 0 12px",
    borderRight: "none",
};

function previewRailStyle(theme: Theme): React.CSSProperties {
    return {
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        padding: "8px 4px",
        borderLeft: `1px solid ${theme.divider}`,
        background: theme.bg,
        flexShrink: 0,
        width: "32px",
    };
}

function previewCloseButtonStyle(theme: Theme, extra?: React.CSSProperties): React.CSSProperties {
    return {
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
        borderRadius: "8px",
        color: theme.textMuted,
        lineHeight: 1,
        transition: "background 120ms ease, color 120ms ease",
        ...extra,
    };
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
    cloudMode = false,
}: {
    activeMode: PreviewPaneMode;
    availableModes: PreviewPaneMode[];
    lang: string;
    onClose: () => void;
    onSelectMode: (mode: PreviewPaneMode) => void;
    theme: Theme;
    conflictCount?: number;
    cloudMode?: boolean;
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
            className="mc-assistant-preview-mode-tabs"
            data-testid="assistant-preview-mode-tabs"
            style={{ ...previewRailStyle(theme), gap: "4px" }}
        >
            <button
                type="button"
                onClick={onClose}
                style={previewCloseButtonStyle(theme, { marginBottom: "4px" })}
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
                    ? `${previewTabTooltip(mode, lang, cloudMode)} (${conflictCount})`
                    : previewTabTooltip(mode, lang, cloudMode);
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
                            border: `1px solid ${active ? (mode === "conflict" ? theme.errorBorder : theme.btnColor) : "transparent"}`,
                            background: active
                                ? (mode === "conflict"
                                    ? theme.errorBg
                                    : `color-mix(in srgb, ${theme.btnColor} ${theme.isDark ? 18 : 10}%, ${theme.titleBarBg})`)
                                : "transparent",
                            color: active ? (mode === "conflict" ? theme.errorText : theme.btnColor) : theme.textMuted,
                            borderRadius: "8px",
                            cursor: "pointer",
                            fontSize: "9px",
                            fontWeight: 700,
                            letterSpacing: "0.02em",
                            lineHeight: 1,
                            padding: 0,
                            transition: "background 120ms ease, color 120ms ease, border-color 120ms ease",
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
    workspaceRefreshToken,
    workspaceResetOnRefresh = false,
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
    cloudMode = false,
    cloudWorkspaceName,
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

    // AgentTaskPanel is now integrated into the tab system (no longer exclusive)
    // Determine which modes are available
    const availableModes: PreviewPaneMode[] = [];
    if (showConflict && conflictContent) availableModes.push("conflict");
    if (showAgentView && agentView) availableModes.push("agent");
    if (showWorkflowPreview) availableModes.push("workflow");
    if (showCodePreview) availableModes.push("code");
    const anyOpen = availableModes.length > 0;

    // Ensure activeMode is valid
    const effectiveMode = anyOpen
        ? (availableModes.includes(activeMode) ? activeMode : availableModes[0])
        : activeMode;

    // Overlay lifecycle: the parent keeps us mounted after the first open, so a
    // close only flips the show flags — we keep the last content on screen while
    // the panel slides back out, then drop the DOM.
    const { present, entered, closing, settled, slideMs } = usePreviewSlideLifecycle(anyOpen);
    const lastOpenRef = useRef<{ body: React.ReactNode; mode: PreviewPaneMode; widthFraction: number } | null>(null);
    // Expanded = pane covers the full window width; resets whenever the last
    // preview surface closes so the next open starts at the split width.
    const [previewExpanded, setPreviewExpanded] = useState(false);
    useEffect(() => {
        if (!anyOpen) setPreviewExpanded(false);
    }, [anyOpen]);

    const handleBackdropClose = useCallback(() => {
        // Agent forms keep their own dismiss flow; a misclick must not drop one.
        if (showCodePreview) closeCodePreview();
        if (showWorkflowPreview) closeDocPreview();
        if (showConflict) onCloseConflict?.();
    }, [showCodePreview, showWorkflowPreview, showConflict, closeCodePreview, closeDocPreview, onCloseConflict]);
    const { backdropProps } = useSafeBackdropDismiss(handleBackdropClose);

    // Freeze the width during slide-out: the parent resets splitRatio to 1 as
    // soon as the last surface closes, which would visibly shrink the panel
    // mid-animation.
    const liveFraction = previewExpanded ? 1 : Math.max(0.2, 1 - splitRatio);
    const widthFraction = anyOpen ? liveFraction : (lastOpenRef.current?.widthFraction ?? liveFraction);

    const paneStyle: React.CSSProperties = {
        position: "absolute",
        top: 0,
        right: 0,
        bottom: 0,
        width: `${Math.round(widthFraction * 100)}%`,
        maxWidth: "100%",
        height: "100%",
        display: "flex",
        flexDirection: "row",
        padding: 0,
        background: "transparent",
        zIndex: 40,
        transform: settled ? "none" : (entered && !closing ? "translateX(0)" : "translateX(105%)"),
        transition: settled ? "none" : `transform ${slideMs}ms ease`,
    };

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
    let openBody: React.ReactNode = null;
    if (anyOpen && availableModes.length === 1 && availableModes[0] === "agent") {
        openBody = (
            <div className="mc-assistant-preview-surface mc-assistant-preview-surface--agent" style={overlaySurfaceStyle}>
                <AgentTaskPanel
                    view={agentView!}
                    onDismiss={dismissAgentView}
                    onResizeStart={startPreviewResize}
                    splitRatio={splitRatio}
                    onToggleMaximize={onToggleMaximize}
                    onSubmit={submitAgentView}
                    theme={theme}
                    lang={lang}
                />
            </div>
        );
    } else if (anyOpen) {
        openBody = (
            <div className="mc-assistant-preview-surface" style={overlaySurfaceStyle}>
            {/* ── Drag handle for resizing (hidden while expanded to full width) ── */}
            {!previewExpanded && (
            <div
                className="mc-assistant-preview-resize-handle"
                data-testid="assistant-preview-resize-handle"
                role="separator"
                aria-orientation="vertical"
                aria-label={lang === "en" ? "Resize preview panel" : "调整预览面板宽度"}
                aria-valuemin={20}
                aria-valuemax={80}
                aria-valuenow={Math.round(splitRatio * 100)}
                tabIndex={0}
                onPointerDown={(e) => {
                    e.preventDefault();
                    // Pointer dragging cancels the browser's default focus step;
                    // keep the handle focused so Arrow/Home/End resizing remains
                    // available immediately after a mouse or touch drag starts.
                    e.currentTarget.focus({ preventScroll: true });
                    e.currentTarget.setPointerCapture?.(e.pointerId);
                    startPreviewResize(e.nativeEvent);
                }}
                onPointerUp={(e) => {
                    if (e.currentTarget.hasPointerCapture?.(e.pointerId)) e.currentTarget.releasePointerCapture?.(e.pointerId);
                }}
                onKeyDown={(e) => {
                    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight" && e.key !== "Home" && e.key !== "End") return;
                    e.preventDefault();
                    const delta = e.key === "ArrowLeft" ? -0.02 : e.key === "ArrowRight" ? 0.02 : 0;
                    const nextRatio = e.key === "Home" ? 0.2 : e.key === "End" ? 0.8 : Math.max(0.2, Math.min(0.8, splitRatio + delta));
                    startPreviewResize(nextRatio);
                }}
                style={{
                    position: "absolute",
                    // Keep most of the hit target inside the rounded preview
                    // surface. The surface clips overflow, so a -10px offset
                    // would leave only half of the drag zone reachable.
                    left: "-4px",
                    top: 0,
                    bottom: 0,
                    width: "24px",
                    cursor: "col-resize",
                    background: "transparent",
                    zIndex: 20,
                    touchAction: "none",
                    userSelect: "none",
                    WebkitAppRegion: "no-drag",
                    "--wails-draggable": "no-drag",
                    pointerEvents: "auto",
                } as any}
            />
            )}
            {/* ── Content area ──
                Conflict panel stays mounted (hidden) when switching to SRC so scroll / draft state
                and Esc-focus scoping survive tab switches. */}
            <div
                className="mc-assistant-preview-content"
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
                        onResizeStart={startPreviewResize}
                        onToggleMaximize={onToggleMaximize}
                        onSubmit={submitAgentView}
                        splitRatio={splitRatio}
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
                        workspaceRefreshToken={workspaceRefreshToken}
                        workspaceResetOnRefresh={workspaceResetOnRefresh}
                        onOpenWorkspaceFile={openWorkspaceFile}
                        onCloseFile={closeCodeFile}
                        onCloseOtherFiles={closeOtherCodeFiles}
                        onCloseFilesToTheRight={closeCodeFilesToTheRight}
                        onCloseAllFiles={closeAllCodeFiles}
                        onMoveFile={moveCodeFile}
                        onTogglePinFile={toggleCodeFilePinned}
                        onClose={closeCodePreview}
                        onToggleMaximize={onToggleMaximize}
                        cloudMode={cloudMode}
                        cloudWorkspaceName={cloudWorkspaceName}
                        hideHeaderClose
                        previewExpanded={previewExpanded}
                        onTogglePreviewExpand={() => setPreviewExpanded(v => !v)}
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
                    cloudMode={cloudMode}
                />
            )}
            {/* ── Close button when only one mode (no tab rail) ── */}
            {availableModes.length === 1 && (
                <div className="mc-assistant-preview-mode-tabs mc-assistant-preview-mode-tabs--single" style={previewRailStyle(theme)}>
                    <button
                        type="button"
                        onClick={handleClose}
                        style={previewCloseButtonStyle(theme)}
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
    // Cache the last open content in an effect (not during render — ref writes
    // during render can tear under concurrent rendering). The exit animation
    // replays the cached content after the show flags flip off.
    useEffect(() => {
        if (!anyOpen) return;
        lastOpenRef.current = { body: openBody, mode: effectiveMode, widthFraction: liveFraction };
    });
    const cached = lastOpenRef.current;
    if (!present || (!anyOpen && !cached)) return null;
    const renderedMode = anyOpen ? effectiveMode : cached!.mode;
    const renderedBody = anyOpen ? openBody : cached!.body;

    return (
        <>
            <div
                role="presentation"
                data-testid="assistant-preview-backdrop"
                style={{
                    position: "absolute",
                    inset: 0,
                    zIndex: 39,
                    background: "rgba(0, 0, 0, 0.45)",
                    opacity: entered && !closing ? 1 : 0,
                    transition: `opacity ${slideMs}ms ease`,
                    // Already invisible while sliding out — let clicks fall
                    // through to the chat instead of swallowing them.
                    pointerEvents: entered && !closing ? "auto" : "none",
                }}
                {...backdropProps}
            />
            <div
                className="mc-assistant-preview-pane"
                data-preview-mode={renderedMode}
                style={{ ...paneStyle, ...previewSurfaceVars(theme) }}
            >
                {renderedBody}
            </div>
        </>
    );
}

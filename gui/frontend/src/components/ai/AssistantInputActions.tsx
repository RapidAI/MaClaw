import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent } from "react";
import { localizeText } from "./aiAssistantI18n";
import {
    getComposeActionIcon,
    getComposeActionLabel,
    PLUS_MENU_ACTION_ITEMS,
    PLUS_MENU_COMPOSE_TEMPLATE_ITEMS,
    PLUS_MENU_FIRE_ITEMS,
    type ComposeAction,
    type FireSlashCommand,
    type PlusMenuActionId,
    type PlusMenuItemDef,
} from "./composeAction";
import type { UseVoiceInputResult } from "./useVoiceInput";
import { AssistantInputIcon, getInputActionButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { VoiceLevelVisualizer } from "./aiAssistantControls";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";
import { AssistantPermissionModeMenu } from "./AssistantPermissionModeMenu";

interface AssistantInputActionsProps {
    /** Whether the retained assistant panel is visible in the app shell. */
    active?: boolean;
    attachButtonTestId?: string;
    browseFile: () => void;
    canSend: boolean;
    cancelSession?: unknown;
    composeAction?: ComposeAction | null;
    handleCancel: () => void;
    handleClearInput: () => void;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: PointerEvent<HTMLButtonElement>) => void;
    finishVoicePointer: (event: PointerEvent<HTMLButtonElement>) => void;
    inputLocked: boolean;
    inputValue: string;
    permissionMode?: AssistantPermissionMode;
    showPermissionMode?: boolean;
    showWorkspacePermissionOption?: boolean;
    isBusy: boolean;
    lang: string;
    onComposeActionChange?: (action: ComposeAction | null) => void;
    onPermissionModeChange?: (mode: AssistantPermissionMode) => void;
    /** Fire a no-arg slash command immediately (e.g. /memory). */
    onFireSlashCommand?: (command: FireSlashCommand) => void;
    /** Insert a command template into the input (e.g. /loop ...). */
    onInsertTemplate?: (template: string) => void;
    /** Local UI actions from the + menu (e.g. start new conversation). */
    onPlusMenuAction?: (actionId: PlusMenuActionId) => void;
    ready: boolean;
    showBusySpinner: boolean;
    showVoiceInput?: boolean;
    sendButtonTestId?: string;
    sendButtonStyle?: CSSProperties;
    theme: Theme;
    themeMode: "light" | "dark";
    voiceInput: UseVoiceInputResult;
}

const MENU_MIN_WIDTH = 176;
const MENU_EST_HEIGHT = 280;

/** Place the + menu above the trigger when possible; flip below near the top edge. */
export function clampMenuPosition(
    triggerRect: { left: number; top: number; bottom: number; width: number },
    viewport: { width: number; height: number } = typeof window !== "undefined"
        ? { width: window.innerWidth, height: window.innerHeight }
        : { width: 1280, height: 800 },
): { left: number; top: number; openUp: boolean } {
    const pad = 8;
    const spaceAbove = triggerRect.top - pad;
    const openUp = spaceAbove >= MENU_EST_HEIGHT;
    // Keep the menu fully in view even on narrow viewports.
    const maxLeft = Math.max(pad, viewport.width - MENU_MIN_WIDTH - pad);
    const clampedLeft = Math.min(Math.max(pad, triggerRect.left), maxLeft);
    if (openUp) {
        // Anchor is the bottom edge of the menu (translateY(-100%)).
        return { left: clampedLeft, top: triggerRect.top - 6, openUp: true };
    }
    // Anchor is the top edge of the menu, just below the trigger.
    return {
        left: clampedLeft,
        top: Math.min(viewport.height - pad, triggerRect.bottom + 6),
        openUp: false,
    };
}

function menuItems(menu: HTMLElement): HTMLButtonElement[] {
    // Skip disabled items so keyboard nav never lands on busy-only actions.
    return Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)'));
}

function focusMenuItem(menu: HTMLElement, index: number) {
    const items = menuItems(menu);
    if (items.length === 0) return;
    const next = ((index % items.length) + items.length) % items.length;
    items[next]?.focus();
}

export function AssistantInputActionsLeft({
    active = true,
    attachButtonTestId,
    browseFile,
    composeAction = null,
    inputLocked,
    lang,
    onComposeActionChange,
    onPermissionModeChange,
    onFireSlashCommand,
    onInsertTemplate,
    onPlusMenuAction,
    ready,
    theme: t,
    themeMode,
    voiceInput,
    permissionMode = "request",
    showPermissionMode = true,
    showWorkspacePermissionOption = false,
    showVoiceInput = true,
    handleVoiceClick,
    handleVoicePointerDown,
    handleVoicePointerLeave,
    finishVoicePointer,
}: Pick<
    AssistantInputActionsProps,
    | "attachButtonTestId"
    | "active"
    | "browseFile"
    | "composeAction"
    | "inputLocked"
    | "lang"
    | "onComposeActionChange"
    | "onPermissionModeChange"
    | "onFireSlashCommand"
    | "onInsertTemplate"
    | "onPlusMenuAction"
    | "ready"
    | "theme"
    | "themeMode"
    | "voiceInput"
    | "showVoiceInput"
    | "handleVoiceClick"
    | "handleVoicePointerDown"
    | "handleVoicePointerLeave"
    | "finishVoicePointer"
    | "permissionMode"
    | "showPermissionMode"
    | "showWorkspacePermissionOption"
>) {
    const voiceDisabled = !ready || voiceInput.state === "transcribing" || !voiceInput.asrReady;
    const [plusMenuOpen, setPlusMenuOpen] = useState(false);
    const [plusMenuPos, setPlusMenuPos] = useState<{ left: number; top: number; openUp: boolean } | null>(null);
    const plusMenuRef = useRef<HTMLDivElement | null>(null);
    const plusButtonRef = useRef<HTMLButtonElement | null>(null);
    // Match AIAssistantPanel / WelcomeView: non-English UI uses Chinese copy.
    const isZh = !lang?.startsWith("en");
    const dark = themeMode === "dark";
    // Keep + usable while the agent is busy: /btw is designed for side queries during work.
    // Only not-ready should block the menu (input may still accept type-ahead while busy).
    const plusMenuEnabled = !!(onComposeActionChange || onFireSlashCommand || onInsertTemplate || onPlusMenuAction);
    const plusDisabled = !ready || !plusMenuEnabled;
    const composeActive = !!composeAction;


    const actionItems = useMemo(
        () => (onPlusMenuAction ? PLUS_MENU_ACTION_ITEMS : []),
        [onPlusMenuAction],
    );
    const composeTemplateItems = useMemo(
        () => PLUS_MENU_COMPOSE_TEMPLATE_ITEMS.filter((item) => {
            if (item.kind === "compose") return !!onComposeActionChange;
            if (item.kind === "template") return !!onInsertTemplate;
            return false;
        }),
        [onComposeActionChange, onInsertTemplate],
    );
    const fireItems = useMemo(
        () => (onFireSlashCommand ? PLUS_MENU_FIRE_ITEMS : []),
        [onFireSlashCommand],
    );

    const closePlusMenu = useCallback(() => {
        setPlusMenuOpen(false);
        setPlusMenuPos(null);
    }, []);

    const updatePlusMenuPosition = useCallback(() => {
        const btn = plusButtonRef.current;
        if (!btn) return;
        const rect = btn.getBoundingClientRect();
        setPlusMenuPos(clampMenuPosition(rect));
    }, []);

    const togglePlusMenu = useCallback(() => {
        setPlusMenuOpen((open) => {
            const next = !open;
            if (next) updatePlusMenuPosition();
            else setPlusMenuPos(null);
            return next;
        });
    }, [updatePlusMenuPosition]);

    useEffect(() => {
        if (!plusMenuOpen) return;
        // Focus first item for keyboard users once the menu is mounted.
        requestAnimationFrame(() => {
            if (plusMenuRef.current) focusMenuItem(plusMenuRef.current, 0);
        });
        const onPointerDown = (event: MouseEvent) => {
            if (plusMenuRef.current?.contains(event.target as Node)) return;
            if (plusButtonRef.current?.contains(event.target as Node)) return;
            closePlusMenu();
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape" || event.key === "Tab") {
                // Escape / Tab dismisses the floating menu so focus returns to the page flow.
                if (event.key === "Escape") event.preventDefault();
                closePlusMenu();
                if (event.key === "Escape") plusButtonRef.current?.focus();
                return;
            }
            const liveMenu = plusMenuRef.current;
            if (!liveMenu) return;
            const active = document.activeElement;
            // Only hijack arrow keys when focus is inside the menu (or still on the trigger).
            const focusInMenu =
                (active instanceof Node && liveMenu.contains(active))
                || (active instanceof Node && !!plusButtonRef.current?.contains(active));
            if (!focusInMenu) return;
            const items = menuItems(liveMenu);
            if (items.length === 0) return;
            const current = items.indexOf(active as HTMLButtonElement);
            if (event.key === "ArrowDown") {
                event.preventDefault();
                focusMenuItem(liveMenu, current < 0 ? 0 : current + 1);
            } else if (event.key === "ArrowUp") {
                event.preventDefault();
                focusMenuItem(liveMenu, current < 0 ? items.length - 1 : current - 1);
            } else if (event.key === "Home") {
                event.preventDefault();
                focusMenuItem(liveMenu, 0);
            } else if (event.key === "End") {
                event.preventDefault();
                focusMenuItem(liveMenu, items.length - 1);
            }
        };
        const onReposition = () => updatePlusMenuPosition();
        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        window.addEventListener("resize", onReposition);
        window.addEventListener("scroll", onReposition, true);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
            window.removeEventListener("resize", onReposition);
            window.removeEventListener("scroll", onReposition, true);
        };
    }, [closePlusMenu, plusMenuOpen, updatePlusMenuPosition]);

    // The menu is fixed-position. Close it before its owning panel becomes
    // hidden so it cannot float above System/Monitor pages.
    useEffect(() => {
        if (!active) closePlusMenu();
    }, [active, closePlusMenu]);

    const handleMenuItem = useCallback((item: PlusMenuItemDef) => {
        if (item.disableWhenBusy && inputLocked) return;
        if (item.kind === "action" && item.actionId) {
            onPlusMenuAction?.(item.actionId);
            closePlusMenu();
            return;
        }
        if (item.kind === "compose" && item.composeAction) {
            const next = composeAction === item.composeAction ? null : item.composeAction;
            onComposeActionChange?.(next);
            closePlusMenu();
            return;
        }
        if (item.kind === "template" && item.template) {
            onComposeActionChange?.(null);
            onInsertTemplate?.(item.template);
            closePlusMenu();
            return;
        }
        if (item.kind === "fire" && item.fireCommand) {
            onFireSlashCommand?.(item.fireCommand);
            closePlusMenu();
        }
    }, [closePlusMenu, composeAction, inputLocked, onComposeActionChange, onFireSlashCommand, onInsertTemplate, onPlusMenuAction]);

    const clearComposeAction = useCallback(() => {
        onComposeActionChange?.(null);
    }, [onComposeActionChange]);

    const menuBg = dark ? t.inputBarBg : t.bg;
    const menuShadow = dark ? "0 12px 32px rgba(0, 0, 0, 0.55)" : "0 12px 28px rgba(15, 23, 42, 0.16)";
    const menuItemHoverBg = dark ? "rgba(148, 163, 184, 0.14)" : "rgba(47, 111, 188, 0.08)";
    const iconColor = dark ? t.btnColor : "#2f6fbc";
    const dividerColor = dark ? "rgba(148, 163, 184, 0.18)" : "rgba(15, 23, 42, 0.08)";

    const renderMenuItem = (item: PlusMenuItemDef) => {
        const active = item.kind === "compose" && item.composeAction === composeAction;
        const itemDisabled = !!(item.disableWhenBusy && inputLocked);
        const label = isZh ? item.labelZh : item.labelEn;
        const hint = itemDisabled
            ? (isZh ? "请等待当前任务完成" : "Please wait for the current task to finish")
            : (isZh ? item.hintZh : item.hintEn);
        return (
            <button
                key={item.id}
                type="button"
                role="menuitem"
                className="ai-plus-menu-item"
                data-testid={item.testId}
                data-active={active ? "true" : "false"}
                aria-current={active ? "true" : undefined}
                aria-disabled={itemDisabled || undefined}
                disabled={itemDisabled}
                onClick={() => handleMenuItem(item)}
                style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    width: "100%",
                    border: "none",
                    background: "transparent",
                    color: t.text,
                    borderRadius: "7px",
                    padding: "8px 10px",
                    cursor: itemDisabled ? "not-allowed" : "pointer",
                    opacity: itemDisabled ? 0.45 : 1,
                    fontSize: "13px",
                    lineHeight: 1.2,
                    textAlign: "left",
                    ["--ai-plus-menu-item-hover-bg" as string]: menuItemHoverBg,
                } as CSSProperties}
                title={hint}
            >
                <span style={{ display: "inline-flex", color: iconColor, flexShrink: 0 }}>
                    <AssistantInputIcon name={item.icon} size={15} />
                </span>
                <span style={{ flex: 1, minWidth: 0 }}>{label}</span>
                {active && <span style={{ fontSize: "11px", color: t.textMuted, flexShrink: 0 }}>{isZh ? "已选" : "On"}</span>}
            </button>
        );
    };

    const hasPrimaryItems = actionItems.length > 0;
    const hasComposeTemplateItems = composeTemplateItems.length > 0;
    const hasFireItems = fireItems.length > 0;

    return (
        <>
            {plusMenuEnabled && (
                <div style={{ position: "relative", display: "inline-flex", alignItems: "center" }}>
                    <button
                        ref={plusButtonRef}
                        type="button"
                        onClick={togglePlusMenu}
                        disabled={plusDisabled}
                        style={getInputActionButtonStyle(t, themeMode, plusMenuOpen || composeActive ? "attach" : "neutral", plusDisabled)}
                        title={localizeText(lang, "Commands", "命令")}
                        aria-label={localizeText(lang, "Commands", "命令")}
                        aria-haspopup="menu"
                        aria-expanded={plusMenuOpen}
                        data-testid="ai-plus-menu-button"
                    >
                        <AssistantInputIcon name="plus" size={13} />
                    </button>
                    {plusMenuOpen && plusMenuPos && (
                        <div
                            ref={plusMenuRef}
                            role="menu"
                            aria-label={localizeText(lang, "Commands", "命令")}
                            data-testid="ai-plus-menu"
                            style={{
                                position: "fixed",
                                left: plusMenuPos.left,
                                top: plusMenuPos.top,
                                transform: plusMenuPos.openUp ? "translateY(-100%)" : "none",
                                minWidth: MENU_MIN_WIDTH,
                                maxWidth: "240px",
                                maxHeight: "min(70vh, 360px)",
                                overflowY: "auto",
                                padding: "4px",
                                borderRadius: "10px",
                                border: `1px solid ${t.inputBarBorder || t.fieldBorder}`,
                                background: menuBg,
                                boxShadow: menuShadow,
                                zIndex: 40000,
                                color: t.text,
                            }}
                        >
                            {actionItems.map(renderMenuItem)}
                            {hasPrimaryItems && (hasComposeTemplateItems || hasFireItems) && (
                                <div
                                    role="separator"
                                    aria-hidden="true"
                                    style={{ height: 1, margin: "4px 6px", background: dividerColor }}
                                />
                            )}
                            {composeTemplateItems.map(renderMenuItem)}
                            {hasComposeTemplateItems && hasFireItems && (
                                <div
                                    role="separator"
                                    aria-hidden="true"
                                    style={{ height: 1, margin: "4px 6px", background: dividerColor }}
                                />
                            )}
                            {fireItems.map(renderMenuItem)}
                        </div>
                    )}
                </div>
            )}
            {composeAction && (
                <button
                    type="button"
                    data-testid={`ai-compose-${composeAction}-chip`}
                    onClick={clearComposeAction}
                    title={localizeText(lang, "Clear mode", "取消模式")}
                    aria-label={localizeText(
                        lang,
                        `${getComposeActionLabel(composeAction, false)} mode active — click to clear`,
                        `${getComposeActionLabel(composeAction, true)}模式已启用，点击取消`,
                    )}
                    style={{
                        display: "inline-flex",
                        alignItems: "center",
                        gap: "4px",
                        height: "22px",
                        padding: "0 7px",
                        borderRadius: "999px",
                        border: `1px solid ${dark ? t.btnBorder : "rgba(47, 111, 188, 0.28)"}`,
                        background: dark ? `color-mix(in srgb, ${t.btnColor} 14%, ${t.fieldBg})` : "rgba(47, 111, 188, 0.08)",
                        color: dark ? t.btnColor : "#2f6fbc",
                        fontSize: "11px",
                        lineHeight: 1,
                        cursor: "pointer",
                        flexShrink: 0,
                    }}
                >
                    <AssistantInputIcon name={getComposeActionIcon(composeAction)} size={12} />
                    <span>{getComposeActionLabel(composeAction, isZh)}</span>
                    <span aria-hidden="true" style={{ opacity: 0.7, fontSize: "12px", marginLeft: "1px" }}>×</span>
                </button>
            )}
            <button type="button" onClick={browseFile} disabled={!ready || inputLocked} style={getInputActionButtonStyle(t, themeMode, "attach", !ready || inputLocked)} title={localizeText(lang, "Choose file", "\u9009\u62e9\u6587\u4ef6")} aria-label={localizeText(lang, "Choose file", "\u9009\u62e9\u6587\u4ef6")} data-testid={attachButtonTestId}>
                <AssistantInputIcon name="paperclip" size={13} />
            </button>
            {showVoiceInput && <button type="button" onClick={handleVoiceClick} onPointerDown={handleVoicePointerDown} onPointerUp={finishVoicePointer} onPointerCancel={finishVoicePointer} onPointerLeave={handleVoicePointerLeave} onContextMenu={(e) => e.preventDefault()} disabled={voiceDisabled} data-testid="ai-voice-input" style={{ ...getInputActionButtonStyle(t, themeMode, voiceInput.state === "listening" ? "voiceHold" : "voice", voiceDisabled), position: "relative", touchAction: "none", overflow: "hidden" }} title={!voiceInput.asrReady ? localizeText(lang, "Voice input unavailable - enable ASR model first", "\u8bed\u97f3\u8f93\u5165\u4e0d\u53ef\u7528\uff0c\u8bf7\u5148\u542f\u7528 ASR \u6a21\u578b") : voiceInput.state === "listening" ? localizeText(lang, "Listening - click to stop", "\u76d1\u542c\u4e2d\uff0c\u70b9\u51fb\u505c\u6b62") : voiceInput.state === "transcribing" ? localizeText(lang, "Transcribing...", "\u8bc6\u522b\u4e2d...") : localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")} aria-label={localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")}>
                {voiceInput.state === "transcribing" ? (
                    <span aria-hidden="true" style={{ display: "inline-block", width: "14px", height: "14px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} />
                ) : voiceInput.state === "listening" ? (
                    <VoiceLevelVisualizer onAudioLevelRef={voiceInput.onAudioLevelRef} isSpeaking={voiceInput.isSpeaking} themeColor={themeMode === "dark" ? "#ffffff" : "#334155"} speakingColor={themeMode === "dark" ? "#c7d7e8" : "#475569"} />
                ) : (
                    <AssistantInputIcon name="mic" size={13} />
                )}
            </button>}
            {showVoiceInput && voiceInput.error && <span style={{ color: t.errorText, fontSize: "11px", alignSelf: "center", maxWidth: "140px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={voiceInput.error}>{voiceInput.error}</span>}
            {showPermissionMode && onPermissionModeChange && <AssistantPermissionModeMenu active={active} lang={lang} mode={permissionMode} onChange={onPermissionModeChange} theme={t} themeMode={themeMode} showWorkspaceOption={showWorkspacePermissionOption} />}
        </>
    );
}

export function AssistantInputActionsRight({ canSend, cancelSession, handleCancel, handleClearInput, handleSend, inputValue, isBusy, lang, showBusySpinner, theme: t, themeMode, sendButtonTestId, sendButtonStyle }: Pick<AssistantInputActionsProps, "canSend" | "cancelSession" | "handleCancel" | "handleClearInput" | "handleSend" | "inputValue" | "isBusy" | "lang" | "ready" | "showBusySpinner" | "theme" | "themeMode" | "sendButtonTestId" | "sendButtonStyle">) {
    return isBusy && cancelSession ? (
        <button
            type="button"
            onClick={handleCancel}
            data-testid="ai-cancel-progress"
            style={getInputActionButtonStyle(t, themeMode, "cancel")}
            title={localizeText(
                lang,
                "Stop generation (also stops desktop Computer Use if active)",
                "停止生成（若桌面 Computer Use 进行中会一并停止）",
                "停止生成（若桌面 Computer Use 進行中會一併停止）",
            )}
            aria-label={localizeText(lang, "Cancel", "\u53d6\u6d88")}
        >
            {showBusySpinner ? <span aria-hidden="true" style={{ width: "14px", height: "14px", borderRadius: "50%", border: `2px solid ${themeMode === "dark" ? "rgba(199, 215, 232, 0.24)" : "rgba(47, 111, 188, 0.18)"}`, borderTopColor: themeMode === "dark" ? "#c7d7e8" : t.btnColor, borderRightColor: themeMode === "dark" ? "#c7d7e8" : t.btnColor, animation: "ai-spinner-spin 0.8s linear infinite" }} /> : <AssistantInputIcon name="stop" size={13} />}
            <span style={{ position: "absolute", opacity: 0, pointerEvents: "none" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88")}</span>
        </button>
    ) : (
        <>
            <button type="button" onClick={handleSend} disabled={!canSend} style={{ ...getInputActionButtonStyle(t, themeMode, canSend ? "send" : "neutral", !canSend), ...sendButtonStyle }} title={localizeText(lang, "Send", "\u53d1\u9001") + " (Enter)"} aria-label={localizeText(lang, "Send", "\u53d1\u9001")} data-testid={sendButtonTestId}>
                {isBusy ? <span style={{ width: "12px", height: "12px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} /> : <AssistantInputIcon name="cornerDownLeft" size={13} />}
            </button>
            <button type="button" onClick={handleClearInput} disabled={!inputValue} style={getInputActionButtonStyle(t, themeMode, "neutral", !inputValue)} title={localizeText(lang, "Clear input", "\u6e05\u9664\u8f93\u5165")} aria-label={localizeText(lang, "Clear input", "\u6e05\u9664\u8f93\u5165")} data-testid="ai-clear-input">
                <AssistantInputIcon name="eraser" size={13} />
            </button>
        </>
    );
}

export function AssistantInputActions(props: AssistantInputActionsProps) {
    return (<>
            <AssistantInputActionsLeft {...props} />
            <span style={{ width: "1px", height: "18px", background: props.theme.divider, margin: "0 4px", flexShrink: 0 }} aria-hidden="true" />
            <AssistantInputActionsRight {...props} />
        </>);
}

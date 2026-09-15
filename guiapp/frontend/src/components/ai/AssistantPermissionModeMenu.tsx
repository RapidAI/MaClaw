import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { localizeText } from "./aiAssistantI18n";
import { AssistantInputIcon, type Theme } from "./aiAssistantPanelTheme";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";

interface AssistantPermissionModeMenuProps {
    /** Whether the retained assistant panel is visible in the app shell. */
    active?: boolean;
    lang: string;
    mode: AssistantPermissionMode;
    onChange?: (mode: AssistantPermissionMode) => void;
    theme: Theme;
    themeMode: "light" | "dark";
    /** When true, offer session-scoped "Workspace" trust (pure coding workbench). */
    showWorkspaceOption?: boolean;
}

interface FullControlConfirmDialogProps {
    lang: string;
    onCancel: () => void;
    onConfirm: () => void;
    theme: Theme;
    themeMode: "light" | "dark";
    /** Mirrors the menu: only surfaces with a Workspace option mention it in copy. */
    showWorkspaceOption?: boolean;
}

/** Risk-warning dialog shown before switching into full-control mode. */
function FullControlConfirmDialog({ lang, onCancel, onConfirm, theme, themeMode, showWorkspaceOption = false }: FullControlConfirmDialogProps) {
    const [acknowledged, setAcknowledged] = useState(false);
    const dark = themeMode === "dark";
    const titleId = useId();
    const descId = useId();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const cancelButtonRef = useRef<HTMLButtonElement | null>(null);
    const onCancelRef = useRef(onCancel);
    onCancelRef.current = onCancel;

    useEffect(() => {
        // Default focus on Cancel so an accidental Enter does not enable full control.
        const focusTimer = window.setTimeout(() => cancelButtonRef.current?.focus(), 0);
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopImmediatePropagation();
                onCancelRef.current();
                return;
            }
            if (event.key !== "Tab") return;
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>("button:not([disabled]), input:not([disabled])");
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            items[nextIndex].focus();
        };
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                cancelButtonRef.current?.focus();
            }
        };
        window.addEventListener("keydown", handleKeyDown, true);
        document.addEventListener("focusin", handleFocusIn, true);
        return () => {
            window.clearTimeout(focusTimer);
            window.removeEventListener("keydown", handleKeyDown, true);
            document.removeEventListener("focusin", handleFocusIn, true);
        };
    }, []);

    const riskItems = [
        {
            icon: "folder" as const,
            title: localizeText(lang, "File operations", "文件操作", "文件操作"),
            desc: localizeText(lang, "Read, create, modify, upload, or delete files anywhere on this computer", "读取、创建、修改、上传或删除此计算机上任意位置的文件", "讀取、建立、修改、上傳或刪除此電腦上任意位置的檔案"),
        },
        {
            icon: "terminal" as const,
            title: localizeText(lang, "Terminal commands", "终端命令", "終端命令"),
            desc: localizeText(lang, "Run commands, install software and skills, and change system settings", "运行命令、安装软件与扩展技能，并更改系统设置", "執行命令、安裝軟體與擴充技能，並更改系統設定"),
        },
        {
            icon: "globe" as const,
            title: localizeText(lang, "Internet access", "访问互联网", "訪問網際網路"),
            desc: localizeText(lang, "Visit websites, send data, and use enabled connectors", "访问网站、发送数据并使用已启用的连接器", "訪問網站、傳送資料並使用已啟用的連接器"),
        },
    ];

    return createPortal(
        <div data-testid="ai-full-control-confirm-overlay" onPointerDown={(event) => { if (event.target === event.currentTarget) onCancel(); }} style={{ position: "fixed", inset: 0, zIndex: 50000, display: "flex", alignItems: "center", justifyContent: "center", background: dark ? "rgba(2, 6, 12, 0.6)" : "rgba(15, 23, 42, 0.45)" }}>
            <div ref={dialogRef} role="alertdialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descId} data-testid="ai-full-control-confirm-dialog" style={{ width: 420, maxWidth: "calc(100vw - 32px)", maxHeight: "calc(100vh - 48px)", overflowY: "auto", padding: "20px 20px 16px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 12, background: theme.bg, boxShadow: "0 12px 32px rgba(15, 23, 42, 0.28)", color: theme.text, fontSize: "13px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 12 }}>
                    <span style={{ color: theme.errorText || "#c43d34", display: "inline-flex", flexShrink: 0 }}><AssistantInputIcon name="alertCircle" size={19} /></span>
                    <h3 id={titleId} style={{ margin: 0, fontSize: "15px", fontWeight: 600, color: theme.headingColor || theme.text }}>{localizeText(lang, "Allow full access?", "确认允许完全访问?", "確認允許完全訪問?")}</h3>
                </div>
                <div id={descId}>
                    <p style={{ margin: "0 0 12px", lineHeight: 1.65 }}>
                        {localizeText(lang, "Once full access is enabled, all tasks skip confirmation steps and operate your computer directly. Proceed with caution, including:", "开启完全访问后，所有任务将减少确认步骤并直接操作您的电脑，请谨慎操作。包括以下内容：", "開啟完全訪問後，所有任務將減少確認步驟並直接操作您的電腦，請謹慎操作。包括以下內容：")}
                    </p>
                    <div style={{ margin: "0 0 14px", padding: "12px 14px", borderRadius: 10, background: dark ? "rgba(148, 163, 184, 0.10)" : "#f3f4f6", display: "flex", flexDirection: "column", gap: 12 }}>
                        {riskItems.map((item) => (
                            <div key={item.title} style={{ display: "flex", alignItems: "flex-start", gap: 10 }}>
                                <span style={{ color: theme.textMuted, display: "inline-flex", flexShrink: 0, marginTop: 1 }}><AssistantInputIcon name={item.icon} size={17} /></span>
                                <div style={{ minWidth: 0 }}>
                                    <div style={{ fontWeight: 600, color: theme.headingColor || theme.text, marginBottom: 2 }}>{item.title}</div>
                                    <div style={{ color: theme.textMuted, lineHeight: 1.5, fontSize: "12px" }}>{item.desc}</div>
                                </div>
                            </div>
                        ))}
                    </div>
                    <p style={{ margin: "0 0 14px", color: theme.textMuted, fontSize: "12px", lineHeight: 1.5 }}>
                        {showWorkspaceOption
                            ? localizeText(lang, "This setting is persisted globally until you switch back to Ask or Workspace trust.", "该设置全局持久生效，直到你手动切回「请求授权」或「工作区信任」。", "此設定全域持久生效，直到你手動切回「請求授權」或「工作區信任」。")
                            : localizeText(lang, "This setting is persisted globally until you switch back to Ask.", "该设置全局持久生效，直到你手动切回「请求授权」。", "此設定全域持久生效，直到你手動切回「請求授權」。")}
                    </p>
                </div>
                <label data-testid="ai-full-control-confirm-ack" style={{ display: "flex", alignItems: "center", gap: 8, margin: "0 0 16px", cursor: "pointer", userSelect: "none" }}>
                    <input
                        type="checkbox"
                        checked={acknowledged}
                        onChange={(event) => setAcknowledged(event.target.checked)}
                        style={{ width: 15, height: 15, margin: 0, accentColor: theme.btnColor || "#2f5f98", cursor: "pointer", flexShrink: 0 }}
                    />
                    <span>{localizeText(lang, "I understand the risks and take responsibility for my data security", "我已了解风险，并对自己的数据安全负责", "我已了解風險，並對自己的資料安全負責")}</span>
                </label>
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 10 }}>
                    <button ref={cancelButtonRef} type="button" data-testid="ai-full-control-confirm-cancel" onClick={onCancel} style={{ height: 30, padding: "0 16px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 8, background: theme.fieldBg, color: theme.text, fontSize: "13px", cursor: "pointer" }}>
                        {localizeText(lang, "Cancel", "取消", "取消")}
                    </button>
                    <button
                        type="button"
                        data-testid="ai-full-control-confirm-accept"
                        disabled={!acknowledged}
                        onClick={onConfirm}
                        style={{ height: 30, padding: "0 16px", border: "none", borderRadius: 8, background: theme.errorText || "#c43d34", color: "#ffffff", fontSize: "13px", fontWeight: 600, cursor: acknowledged ? "pointer" : "not-allowed", opacity: acknowledged ? 1 : 0.5 }}
                    >
                        {localizeText(lang, "Allow full access", "允许完全访问", "允許完全訪問")}
                    </button>
                </div>
            </div>
        </div>,
        document.body,
    );
}

export function AssistantPermissionModeMenu({ active: panelActive = true, lang, mode, onChange, theme, themeMode, showWorkspaceOption = false }: AssistantPermissionModeMenuProps) {
    const [open, setOpen] = useState(false);
    const [confirmFullOpen, setConfirmFullOpen] = useState(false);
    const [menuPosition, setMenuPosition] = useState<{ left: number; top: number; openUp: boolean; maxHeight: number } | null>(null);
    const rootRef = useRef<HTMLDivElement | null>(null);
    const menuRef = useRef<HTMLDivElement | null>(null);
    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const menuId = useId();
    const dark = themeMode === "dark";

    const updateMenuPosition = useCallback(() => {
        const trigger = triggerRef.current;
        if (!trigger || typeof window === "undefined") return;
        const rect = trigger.getBoundingClientRect();
        const menuWidth = 156;
        const viewportPadding = 8;
        const menuGap = 6;
        const spaceAbove = Math.max(0, rect.top - menuGap - viewportPadding);
        const spaceBelow = Math.max(0, window.innerHeight - rect.bottom - menuGap - viewportPadding);
        // Pick the roomier side, then constrain a short viewport rather than
        // letting a portal menu disappear beyond either viewport edge.
        const openUp = spaceAbove >= spaceBelow;
        const availableHeight = openUp ? spaceAbove : spaceBelow;
        const left = Math.min(
            Math.max(viewportPadding, rect.right - menuWidth),
            Math.max(viewportPadding, window.innerWidth - menuWidth - viewportPadding),
        );
        setMenuPosition({
            left,
            top: openUp ? rect.top - menuGap : rect.bottom + menuGap,
            openUp,
            maxHeight: Math.min(240, availableHeight),
        });
    }, []);

    const setMenuOpen = useCallback((next: boolean) => {
        if (next) updateMenuPosition();
        setOpen(next);
    }, [updateMenuPosition]);

    useEffect(() => {
        if (!panelActive) {
            setMenuOpen(false);
            setConfirmFullOpen(false);
        }
    }, [panelActive, setMenuOpen]);

    useEffect(() => {
        if (!open) return;
        const handlePointerDown = (event: globalThis.PointerEvent) => {
            const target = event.target as Node;
            if (!rootRef.current?.contains(target) && !menuRef.current?.contains(target)) setMenuOpen(false);
        };
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setMenuOpen(false);
                triggerRef.current?.focus();
            }
        };
        const handleReposition = () => updateMenuPosition();
        document.addEventListener("pointerdown", handlePointerDown);
        document.addEventListener("keydown", handleKeyDown);
        window.addEventListener("resize", handleReposition);
        window.addEventListener("scroll", handleReposition, true);
        return () => {
            document.removeEventListener("pointerdown", handlePointerDown);
            document.removeEventListener("keydown", handleKeyDown);
            window.removeEventListener("resize", handleReposition);
            window.removeEventListener("scroll", handleReposition, true);
        };
    }, [open, setMenuOpen, updateMenuPosition]);

    useEffect(() => {
        if (!open) return;
        const selected = menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitemradio"][aria-checked="true"]');
        selected?.focus();
    }, [open]);

    const options = [
        {
            value: "request" as const,
            icon: "shieldCheck" as const,
            label: localizeText(lang, "Ask", "请求授权", "請求授權"),
            hint: localizeText(lang, "Prompt for out-of-scope paths and high-risk shell", "越界路径与高危命令均需确认", "越界路徑與高危命令均需確認"),
        },
        ...(showWorkspaceOption
            ? [{
                value: "workspace" as const,
                icon: "folder" as const,
                label: localizeText(lang, "Workspace", "工作区信任", "工作區信任"),
                hint: localizeText(lang, "Trust project paths; still prompt for high-risk shell", "信任项目路径；高危 shell 仍需确认", "信任專案路徑；高危 shell 仍需確認"),
            }]
            : []),
        {
            value: "full" as const,
            icon: "alertTriangle" as const,
            label: localizeText(lang, "Full control", "完全控制", "完全控制"),
            hint: localizeText(lang, "Skip path and high-risk prompts (persisted)", "跳过路径与高危确认（全局持久）", "跳過路徑與高危確認（全域持久）"),
        },
    ];
    // If workspace mode is active but option is hidden, fall back to request label.
    const active = options.find((option) => option.value === mode)
        || (mode === "workspace"
            ? { value: "workspace" as const, icon: "folder" as const, label: localizeText(lang, "Workspace", "工作区信任", "工作區信任") }
            : options[0]);
    const isFullControl = mode === "full";
    const isWorkspace = mode === "workspace";
    const triggerColor = isFullControl
        ? (theme.errorText || "#c43d34")
        : isWorkspace
            ? (theme.headingColor || theme.btnColor || theme.text)
            : theme.textMuted;

    return (
        <div ref={rootRef} style={{ position: "relative", flexShrink: 0 }}>
            <button ref={triggerRef} className="ai-permission-mode-trigger" type="button" aria-label={localizeText(lang, "Permission mode", "权限模式", "權限模式")} aria-controls={open ? menuId : undefined} aria-expanded={open} aria-haspopup="menu" data-testid="ai-permission-mode" data-permission-mode={mode} onClick={() => setMenuOpen(!open)} onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setMenuOpen(true); } }} title={showWorkspaceOption
                ? localizeText(lang, "Permission: Ask / Workspace trust / Full control", "权限：请求授权 / 工作区信任 / 完全控制", "權限：請求授權 / 工作區信任 / 完全控制")
                : localizeText(lang, "Permission mode", "权限模式", "權限模式")} style={{ height: "24px", display: "inline-flex", alignItems: "center", gap: "4px", padding: "0 5px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 4, background: theme.fieldBg, color: triggerColor, fontSize: "11px", cursor: "pointer" }}>
                <AssistantInputIcon name={active.icon} size={13} />
                <span>{active.label}</span>
            </button>
            {open && menuPosition && typeof document !== "undefined" && createPortal(<div ref={menuRef} id={menuId} role="menu" aria-label={localizeText(lang, "权限模式", "权限模式", "權限模式")} data-testid="ai-permission-mode-menu" style={{ position: "fixed", left: menuPosition.left, top: menuPosition.top, transform: menuPosition.openUp ? "translateY(-100%)" : undefined, zIndex: 40000, minWidth: "156px", maxHeight: `${menuPosition.maxHeight}px`, overflowY: "auto", padding: "4px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, background: theme.bg, boxShadow: "0 4px 8px rgba(15, 23, 42, 0.14)" }}>
                {options.map((option) => {
                    const dangerous = option.value === "full";
                    const workspace = option.value === "workspace";
                    const selectedBg = mode === option.value
                        ? (dangerous
                            ? (theme.errorBg || "#fbf1f0")
                            : workspace
                                ? (dark ? "rgba(74, 222, 128, 0.12)" : "rgba(22, 163, 74, 0.1)")
                                : `color-mix(in srgb, ${theme.btnColor} 10%, transparent)`)
                        : "transparent";
                    const selectedColor = dangerous
                        ? (theme.errorText || "#c43d34")
                        : workspace
                            ? (theme.headingColor || theme.btnColor || theme.text)
                            : theme.text;
                    return <button key={option.value} className="ai-permission-mode-item" type="button" role="menuitemradio" aria-checked={mode === option.value} data-testid={`ai-permission-mode-${option.value}`} title={option.hint} onClick={() => {
                        if (option.value === "full" && mode !== "full") {
                            // Full control drops every approval prompt: confirm first.
                            setConfirmFullOpen(true);
                        } else {
                            onChange?.(option.value);
                        }
                        setMenuOpen(false);
                        triggerRef.current?.focus();
                    }} onKeyDown={(event) => { const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') || []); const index = items.indexOf(event.currentTarget); if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); items[(index + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus(); } else if (event.key === "Home") { event.preventDefault(); items[0]?.focus(); } else if (event.key === "End") { event.preventDefault(); items.at(-1)?.focus(); } }} style={{ width: "100%", minHeight: "28px", display: "flex", flexDirection: "column", alignItems: "flex-start", justifyContent: "center", gap: "1px", padding: "5px 7px", border: "none", borderRadius: 4, background: selectedBg, color: selectedColor, fontSize: "12px", textAlign: "left", cursor: "pointer" }}><span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}><AssistantInputIcon name={option.icon} size={14} /><span>{option.label}</span></span>{showWorkspaceOption && <span style={{ fontSize: 10, opacity: 0.78, paddingLeft: 21, lineHeight: 1.25 }}>{option.hint}</span>}</button>;
                })}
            </div>, document.body)}
            {confirmFullOpen && typeof document !== "undefined" && <FullControlConfirmDialog lang={lang} theme={theme} themeMode={themeMode} showWorkspaceOption={showWorkspaceOption} onCancel={() => { setConfirmFullOpen(false); triggerRef.current?.focus(); }} onConfirm={() => { setConfirmFullOpen(false); onChange?.("full"); triggerRef.current?.focus(); }} />}
        </div>
    );
}

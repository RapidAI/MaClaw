import { useEffect, useRef, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import { AssistantInputIcon, type Theme } from "./aiAssistantPanelTheme";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";

interface AssistantPermissionModeMenuProps {
    lang: string;
    mode: AssistantPermissionMode;
    onChange?: (mode: AssistantPermissionMode) => void;
    theme: Theme;
    themeMode: "light" | "dark";
    /** When true, offer session-scoped "Workspace" trust (pure coding workbench). */
    showWorkspaceOption?: boolean;
}

export function AssistantPermissionModeMenu({ lang, mode, onChange, theme, themeMode, showWorkspaceOption = false }: AssistantPermissionModeMenuProps) {
    const [open, setOpen] = useState(false);
    const menuRef = useRef<HTMLDivElement | null>(null);
    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const dark = themeMode === "dark";

    useEffect(() => {
        if (!open) return;
        const handlePointerDown = (event: globalThis.PointerEvent) => {
            if (!menuRef.current?.contains(event.target as Node)) setOpen(false);
        };
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setOpen(false);
                triggerRef.current?.focus();
            }
        };
        document.addEventListener("pointerdown", handlePointerDown);
        document.addEventListener("keydown", handleKeyDown);
        return () => {
            document.removeEventListener("pointerdown", handlePointerDown);
            document.removeEventListener("keydown", handleKeyDown);
        };
    }, [open]);

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
        <div ref={menuRef} style={{ position: "relative", flexShrink: 0 }}>
            <button ref={triggerRef} className="ai-permission-mode-trigger" type="button" aria-label={localizeText(lang, "Permission mode", "权限模式", "權限模式")} aria-expanded={open} aria-haspopup="menu" data-testid="ai-permission-mode" onClick={() => setOpen((value) => !value)} onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setOpen(true); } }} title={showWorkspaceOption
                ? localizeText(lang, "Permission: Ask / Workspace trust / Full control", "权限：请求授权 / 工作区信任 / 完全控制", "權限：請求授權 / 工作區信任 / 完全控制")
                : localizeText(lang, "Permission mode", "权限模式", "權限模式")} style={{ height: "24px", display: "inline-flex", alignItems: "center", gap: "4px", padding: "0 5px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 4, background: theme.fieldBg, color: triggerColor, fontSize: "11px", cursor: "pointer" }}>
                <AssistantInputIcon name={active.icon} size={13} />
                <span>{active.label}</span>
            </button>
            {open && <div role="menu" data-testid="ai-permission-mode-menu" style={{ position: "absolute", right: 0, bottom: "calc(100% + 6px)", zIndex: 20, minWidth: "156px", padding: "4px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, background: theme.bg, boxShadow: "0 4px 8px rgba(15, 23, 42, 0.14)" }}>
                {options.map((option) => {
                    const dangerous = option.value === "full";
                    const workspace = option.value === "workspace";
                    const selectedBg = mode === option.value
                        ? (dangerous
                            ? (theme.errorBg || "#fbf1f0")
                            : workspace
                                ? (dark ? "rgba(74, 222, 128, 0.12)" : "rgba(22, 163, 74, 0.1)")
                                : (dark ? "rgba(143, 180, 220, 0.14)" : "rgba(47, 95, 152, 0.08)"))
                        : "transparent";
                    const selectedColor = dangerous
                        ? (theme.errorText || "#c43d34")
                        : workspace
                            ? (theme.headingColor || theme.btnColor || theme.text)
                            : theme.text;
                    return <button key={option.value} className="ai-permission-mode-item" type="button" role="menuitemradio" aria-checked={mode === option.value} data-testid={`ai-permission-mode-${option.value}`} title={option.hint} onClick={() => { onChange?.(option.value); setOpen(false); triggerRef.current?.focus(); }} onKeyDown={(event) => { const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') || []); const index = items.indexOf(event.currentTarget); if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); items[(index + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus(); } else if (event.key === "Home") { event.preventDefault(); items[0]?.focus(); } else if (event.key === "End") { event.preventDefault(); items.at(-1)?.focus(); } }} style={{ width: "100%", minHeight: "28px", display: "flex", flexDirection: "column", alignItems: "flex-start", justifyContent: "center", gap: "1px", padding: "5px 7px", border: "none", borderRadius: 4, background: selectedBg, color: selectedColor, fontSize: "12px", textAlign: "left", cursor: "pointer" }}><span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}><AssistantInputIcon name={option.icon} size={14} /><span>{option.label}</span></span>{showWorkspaceOption && <span style={{ fontSize: 10, opacity: 0.78, paddingLeft: 21, lineHeight: 1.25 }}>{option.hint}</span>}</button>;
                })}
            </div>}
        </div>
    );
}

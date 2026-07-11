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
}

export function AssistantPermissionModeMenu({ lang, mode, onChange, theme, themeMode }: AssistantPermissionModeMenuProps) {
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
        { value: "request" as const, icon: "shieldCheck" as const, label: localizeText(lang, "Ask", "请求授权", "請求授權") },
        { value: "full" as const, icon: "alertTriangle" as const, label: localizeText(lang, "Full control", "完全控制", "完全控制") },
    ];
    const active = options.find((option) => option.value === mode) || options[0];
    const isFullControl = mode === "full";

    return (
        <div ref={menuRef} style={{ position: "relative", flexShrink: 0 }}>
            <button ref={triggerRef} className="ai-permission-mode-trigger" type="button" aria-label={localizeText(lang, "Permission mode", "权限模式", "權限模式")} aria-expanded={open} aria-haspopup="menu" data-testid="ai-permission-mode" onClick={() => setOpen((value) => !value)} onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setOpen(true); } }} title={localizeText(lang, "Permission mode", "权限模式", "權限模式")} style={{ height: "24px", display: "inline-flex", alignItems: "center", gap: "4px", padding: "0 5px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 4, background: theme.fieldBg, color: isFullControl ? (theme.errorText || "#c43d34") : theme.textMuted, fontSize: "11px", cursor: "pointer" }}>
                <AssistantInputIcon name={active.icon} size={13} />
                <span>{active.label}</span>
            </button>
            {open && <div role="menu" data-testid="ai-permission-mode-menu" style={{ position: "absolute", right: 0, bottom: "calc(100% + 6px)", zIndex: 20, minWidth: "142px", padding: "4px", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, background: theme.bg, boxShadow: "0 4px 8px rgba(15, 23, 42, 0.14)" }}>
                {options.map((option) => {
                    const dangerous = option.value === "full";
                    return <button key={option.value} className="ai-permission-mode-item" type="button" role="menuitemradio" aria-checked={mode === option.value} data-testid={`ai-permission-mode-${option.value}`} onClick={() => { onChange?.(option.value); setOpen(false); triggerRef.current?.focus(); }} onKeyDown={(event) => { const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') || []); const index = items.indexOf(event.currentTarget); if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); items[(index + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus(); } else if (event.key === "Home") { event.preventDefault(); items[0]?.focus(); } else if (event.key === "End") { event.preventDefault(); items.at(-1)?.focus(); } }} style={{ width: "100%", height: "28px", display: "flex", alignItems: "center", gap: "7px", padding: "0 7px", border: "none", borderRadius: 4, background: mode === option.value ? (dangerous ? (theme.errorBg || "#fbf1f0") : (dark ? "rgba(143, 180, 220, 0.14)" : "rgba(47, 95, 152, 0.08)")) : "transparent", color: dangerous ? (theme.errorText || "#c43d34") : theme.text, fontSize: "12px", textAlign: "left", cursor: "pointer" }}><AssistantInputIcon name={option.icon} size={14} /><span>{option.label}</span></button>;
                })}
            </div>}
        </div>
    );
}

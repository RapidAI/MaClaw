import { useEffect, useRef, useState } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

interface Props {
    open: boolean;
    lang: string;
    theme: Theme;
    onCancel: () => void;
    onSubmit: (name: string) => void;
}

export function ProjectSearchForkForm({ open, lang, theme: t, onCancel, onSubmit }: Props) {
    const [name, setName] = useState("");
    const inputRef = useRef<HTMLInputElement | null>(null);
    useEffect(() => { if (open) inputRef.current?.focus(); }, [open]);
    if (!open) return null;
    const cancel = () => { setName(""); onCancel(); };
    const submit = () => { const next = name.trim(); setName(""); onSubmit(next); };
    return (
        <div style={{ display: "flex", alignItems: "center", gap: "6px", padding: "0 12px 8px" }}>
            <input ref={inputRef} id="project-search-fork-name" value={name} onChange={event => setName(event.target.value)} onKeyDown={event => { if (event.key === "Enter") submit(); if (event.key === "Escape") cancel(); }} placeholder={localizeText(lang, "Task name (optional)", "\u4efb\u52a1\u540d\u79f0\uff08\u53ef\u9009\uff09")} style={{ flex: 1, minWidth: 0, border: `1px solid ${t.fieldBorder}`, borderRadius: "6px", background: t.fieldBg, color: t.text, fontSize: "12px", padding: "5px 8px", outline: "none" }} />
            <button type="button" onClick={submit} style={{ border: `1px solid ${t.btnBorder}`, borderRadius: "6px", background: t.fieldBg, color: t.actionBtnColor, cursor: "pointer", fontSize: "12px", padding: "5px 8px" }}>{localizeText(lang, "Create", "\u521b\u5efa")}</button>
            <button type="button" onClick={cancel} style={{ border: "none", background: "transparent", color: t.text, cursor: "pointer", opacity: 0.6, fontSize: "12px", padding: "5px 4px" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88")}</button>
        </div>
    );
}

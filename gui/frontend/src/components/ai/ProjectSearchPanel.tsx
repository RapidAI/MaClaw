import { useCallback, useEffect, useRef, useState } from "react";
import { HideTask, PinTask, RenameTask, ResumeProject, SearchProjects } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

interface ProjectSearchItem {
    id: string;
    name: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    tags?: string[];
    last_activity?: string;
    entry_count?: number;
    pinned?: boolean;
}

function formatWorkflowType(type: string | undefined, lang: string): string {
    if (!type) return "";
    const labels: Record<string, { en: string; zh: string }> = {
        coding: { en: "Coding", zh: "\u7f16\u7a0b" },
        product_design: { en: "Product Design", zh: "\u4ea7\u54c1\u8bbe\u8ba1" },
        research: { en: "Research", zh: "\u7814\u7a76" },
        writing: { en: "Writing", zh: "\u5199\u4f5c" },
    };
    const hit = labels[type];
    return hit ? (lang === "en" ? hit.en : hit.zh) : type.replace(/_/g, " ");
}

export function useProjectSearch(lang: string) {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [results, setResults] = useState<ProjectSearchItem[]>([]);
    const [loading, setLoading] = useState(false);
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const doSearch = useCallback((q: string) => {
        setLoading(true);
        SearchProjects(q, 10)
            .then(r => setResults((r || []) as ProjectSearchItem[]))
            .catch(() => setResults([]))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (open && query === "") doSearch("");
    }, [open, query, doSearch]);

    useEffect(() => () => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
    }, []);

    const onQueryChange = useCallback((value: string) => {
        setQuery(value);
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => doSearch(value), 250);
    }, [doSearch]);

    const close = useCallback(() => { setOpen(false); setQuery(""); }, []);
    const toggle = useCallback(() => { setOpen(v => !v); }, []);
    const refresh = useCallback(() => doSearch(query), [doSearch, query]);

    const formatTime = useCallback((iso?: string): string => {
        if (!iso) return "";
        try {
            const d = new Date(iso);
            const diffH = Math.floor((Date.now() - d.getTime()) / 3600000);
            if (diffH < 1) return localizeText(lang, "just now", "\u521a\u521a");
            if (diffH < 24) return `${diffH}${localizeText(lang, "h ago", "\u5c0f\u65f6\u524d")}`;
            const diffD = Math.floor(diffH / 24);
            if (diffD < 7) return `${diffD}${localizeText(lang, "d ago", "\u5929\u524d")}`;
            return d.toLocaleDateString();
        } catch {
            return "";
        }
    }, [lang]);

    return { open, query, results, loading, toggle, close, onQueryChange, refresh, formatTime };
}

export function ProjectSearchPanel({ search, lang, theme: t, inline, onProjectSwitch, onTaskPrefsChanged }: {
    search: ReturnType<typeof useProjectSearch>;
    lang: string;
    theme: Theme;
    inline: boolean;
    onProjectSwitch: (displayMsg: string) => Promise<void> | void;
    onTaskPrefsChanged?: () => void;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; item: ProjectSearchItem } | null>(null);
    const [renamingPath, setRenamingPath] = useState<string | null>(null);
    const [renameVal, setRenameVal] = useState("");

    useEffect(() => { if (search.open) inputRef.current?.focus(); }, [search.open]);

    useEffect(() => {
        if (!search.open) return;
        const handler = (event: MouseEvent) => {
            if (panelRef.current && !panelRef.current.contains(event.target as Node)) {
                search.close();
                setCtxMenu(null);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [search.open, search.close]);

    const refreshResults = useCallback(() => {
        search.refresh();
        onTaskPrefsChanged?.();
    }, [search, onTaskPrefsChanged]);

    const onSelect = useCallback(async (item: ProjectSearchItem) => {
        if (renamingPath) return;
        search.close();
        try {
            const msg = await ResumeProject(item.project_path);
            if (msg) await onProjectSwitch(msg);
        } catch (error) {
            console.error("[ProjectSearch] ResumeProject failed:", error);
        }
    }, [renamingPath, search, onProjectSwitch]);

    if (!search.open) return null;

    return (
        <div ref={panelRef} style={{ flexShrink: 0, borderBottom: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, zIndex: 30000, position: "relative", overflow: "visible" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "6px 12px" }}>
                <span style={{ fontSize: "13px", opacity: 0.55, flexShrink: 0 }}>{"\u{1F50D}"}</span>
                <input
                    ref={inputRef}
                    type="text"
                    value={search.query}
                    onChange={event => search.onQueryChange(event.target.value)}
                    onKeyDown={event => { if (event.key === "Escape") search.close(); }}
                    placeholder={localizeText(lang, "Search tasks...", "\u641c\u7d22\u4efb\u52a1...")}
                    style={{ flex: 1, border: "none", outline: "none", background: "transparent", color: t.text, fontSize: "13px", fontFamily: "inherit", padding: "4px 0", minWidth: 0 }}
                />
                <button
                    {...(inline ? { onMouseDown: (event: React.MouseEvent) => { event.preventDefault(); event.stopPropagation(); search.close(); } } : { onClick: () => search.close() })}
                    style={{ background: "none", border: "none", cursor: "pointer", color: t.text, opacity: 0.5, fontSize: "12px", padding: "2px 4px", lineHeight: 1, flexShrink: 0 }}
                    title={localizeText(lang, "Close", "\u5173\u95ed")}
                >{"x"}</button>
            </div>
            <div style={{ maxHeight: "320px", overflowY: "auto", padding: "0 4px 4px" }}>
                {search.loading && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{localizeText(lang, "Searching...", "\u641c\u7d22\u4e2d...")}</div>}
                {!search.loading && search.results.length === 0 && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{search.query ? localizeText(lang, "No tasks found", "\u672a\u627e\u5230\u4efb\u52a1") : localizeText(lang, "No recent tasks", "\u6682\u65e0\u6700\u8fd1\u4efb\u52a1")}</div>}
                {!search.loading && search.results.map(item => (
                    <div
                        key={item.id || item.project_path}
                        onClick={() => onSelect(item)}
                        onContextMenu={event => { event.preventDefault(); setCtxMenu({ x: event.clientX, y: event.clientY, item }); }}
                        style={{ padding: "8px 10px", cursor: "pointer", borderRadius: "6px", transition: "background 0.15s" }}
                        onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)}
                        onMouseLeave={event => (event.currentTarget.style.background = "transparent")}
                    >
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "2px" }}>
                            <span style={{ fontSize: "13px", flexShrink: 0 }}>{item.pinned ? "\u{1F4CC}" : "\u{1F516}"}</span>
                            {renamingPath === item.project_path ? (
                                <input
                                    autoFocus
                                    value={renameVal}
                                    onChange={event => setRenameVal(event.target.value)}
                                    onBlur={async () => {
                                        const trimmed = renameVal.trim();
                                        if (trimmed && trimmed !== item.name) {
                                            await RenameTask(item.project_path, trimmed);
                                            refreshResults();
                                        }
                                        setRenamingPath(null);
                                    }}
                                    onKeyDown={event => { if (event.key === "Enter") (event.target as HTMLInputElement).blur(); if (event.key === "Escape") setRenamingPath(null); }}
                                    onClick={event => event.stopPropagation()}
                                    style={{ flex: 1, fontSize: "13px", fontWeight: 600, color: t.text, background: t.codeBlockBg, border: `1px solid ${t.headingColor}`, borderRadius: "3px", padding: "2px 6px", outline: "none", minWidth: 0, fontFamily: "inherit" }}
                                />
                            ) : (
                                <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{item.name || item.project_path}</span>
                            )}
                            {item.workflow_type && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(99,102,241,0.12)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{formatWorkflowType(item.workflow_type, lang)}</span>}
                            <button type="button" onClick={event => { event.stopPropagation(); void onSelect(item); }} style={{ border: "none", background: "rgba(99,102,241,0.12)", color: t.headingColor, borderRadius: "999px", width: "22px", height: "22px", cursor: "pointer", flexShrink: 0 }} title={localizeText(lang, "Resume task", "\u7ee7\u7eed\u4efb\u52a1")}>{">"}</button>
                        </div>
                        <div style={{ fontSize: "11px", color: t.text, opacity: 0.45, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px" }}>{item.project_path}</div>
                        {item.preview && <div style={{ fontSize: "11px", color: t.text, opacity: 0.35, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px", marginTop: "1px" }}>{item.preview}</div>}
                        {item.last_activity && <div style={{ fontSize: "10px", color: t.text, opacity: 0.32, paddingLeft: "21px", marginTop: "1px" }}>{search.formatTime(item.last_activity)}</div>}
                    </div>
                ))}
            </div>
            {ctxMenu && (<>
                <div style={{ position: "fixed", inset: 0, zIndex: 9998 }} onClick={() => setCtxMenu(null)} />
                <div style={{ position: "fixed", left: ctxMenu.x, top: ctxMenu.y, zIndex: 9999, background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: "6px", boxShadow: "0 4px 12px rgba(0,0,0,0.15)", padding: "4px 0", minWidth: "120px" }}>
                    {[
                        { label: localizeText(lang, "Rename", "\u91cd\u547d\u540d"), icon: "edit", action: () => { setRenamingPath(ctxMenu.item.project_path); setRenameVal(ctxMenu.item.name || ""); setCtxMenu(null); } },
                        { label: ctxMenu.item.pinned ? localizeText(lang, "Unpin", "\u53d6\u6d88\u7f6e\u9876") : localizeText(lang, "Pin", "\u7f6e\u9876"), icon: "pin", action: async () => { await PinTask(ctxMenu.item.project_path, !ctxMenu.item.pinned); refreshResults(); setCtxMenu(null); } },
                        { label: localizeText(lang, "Remove", "\u79fb\u9664"), icon: "x", action: async () => { await HideTask(ctxMenu.item.project_path); refreshResults(); setCtxMenu(null); } },
                    ].map(item => (
                        <div key={item.label} onClick={item.action} style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 12px", cursor: "pointer", fontSize: "12px", color: t.text, transition: "background 0.1s" }} onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}>
                            <span style={{ fontSize: "13px" }}>{item.icon}</span><span>{item.label}</span>
                        </div>
                    ))}
                </div>
            </>)}
        </div>
    );
}

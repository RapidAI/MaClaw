import { useCallback, useEffect, useRef, useState } from "react";
import { ArchiveProject, GetArchivedExperience, GetProjectScene, HideTask, OpenFileOrShowInFolder, PinTask, RenameTask, ResumeTask, SearchTasks } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { ProjectSearchArchivedPanel } from "./ProjectSearchArchivedPanel";
import { ProjectSearchForkForm } from "./ProjectSearchForkForm";
import { ProjectSearchIcon } from "./ProjectSearchIcon";
import { ProjectSceneDetailPanel, type ProjectSceneDetail, type ProjectSearchArtifact } from "./ProjectSceneDetailPanel";
import { agentModeFromTaskTags, isCodingWorkflowSourceTags, isPureCodingTaskTags, remoteHostFromTaskTags } from "./codingTaskMode";

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
    archived?: boolean;
    has_output?: boolean;
    source_urls?: string[];
    recent_artifacts?: ProjectSearchArtifact[];
}

function formatArtifactSummary(item: ProjectSearchItem, lang: string, includeSource = false): string {
    const artifact = item.recent_artifacts?.find(a => a.title || a.preview || a.source_url);
    if (!artifact) return "";
    const label = artifact.title || artifact.preview || artifact.source_url || "";
    const prefix = localizeText(lang, "Latest artifact", "最近产物");
    const hint = artifact.source_hint ? "; " + artifact.source_hint : "";
    const source = includeSource && artifact.source_url ? " | " + artifact.source_url + hint : "";
    return prefix + ": " + label + source;
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
        SearchTasks(q, 20)
            .then(r => setResults(((r || []) as ProjectSearchItem[]).filter(item => item.has_output !== false)))
            .catch(() => setResults([]))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => { if (open && query === "") doSearch(""); }, [open, query, doSearch]);
    useEffect(() => () => { if (debounceRef.current) clearTimeout(debounceRef.current); }, []);

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
            return diffD < 7 ? `${diffD}${localizeText(lang, "d ago", "\u5929\u524d")}` : d.toLocaleDateString();
        } catch { return ""; }
    }, [lang]);

    return { open, query, results, loading, toggle, close, onQueryChange, refresh, formatTime };
}

export function ProjectSearchPanel({ search, lang, theme: t, inline, onProjectSwitch, onCreateProjectTab, onCloseProjectTab, onForkCurrentChat, onTaskPrefsChanged }: {
    search: ReturnType<typeof useProjectSearch>;
    lang: string;
    theme: Theme;
    inline: boolean;
    onProjectSwitch: (displayMsg: string) => Promise<void> | void;
    onCreateProjectTab?: (projectPath: string, taskTitle: string, options?: {
        autoSend?: boolean;
        agentMode?: "coding_dev" | "remote_coding_dev";
        remoteHost?: string;
        tags?: string[];
    }) => void;
    /** Close an open project tab by its project path (e.g. after archiving). */
    onCloseProjectTab?: (projectPath: string) => void;
    /** Fork current local tab conversation into a new project tab. */
    onForkCurrentChat?: (taskName: string) => void;
    onTaskPrefsChanged?: () => void;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; item: ProjectSearchItem } | null>(null);
    const [renamingPath, setRenamingPath] = useState<string | null>(null);
    const [renameVal, setRenameVal] = useState("");
    const [forkNameOpen, setForkNameOpen] = useState(false);
    const [archivedExperience, setArchivedExperience] = useState<{ name: string; content: string } | null>(null);
    const [archivedLoading, setArchivedLoading] = useState(false);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneLoadingPath, setSceneLoadingPath] = useState<string | null>(null);
    const visibleResults = search.results.filter(item => item.has_output !== false);

    useEffect(() => { if (search.open) inputRef.current?.focus(); }, [search.open]);
    useEffect(() => {
        if (!search.open && !archivedExperience) return;
        const handler = (event: MouseEvent) => {
            if (panelRef.current && !panelRef.current.contains(event.target as Node)) {
                search.close();
                setCtxMenu(null);
                setArchivedExperience(null);
                setSceneDetail(null);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [search.open, search.close, archivedExperience]);

    const refreshResults = useCallback(() => { search.refresh(); onTaskPrefsChanged?.(); }, [search, onTaskPrefsChanged]);
    const openSceneDetail = useCallback(async (item: ProjectSearchItem) => {
        setSceneLoadingPath(item.project_path);
        try {
            const detail = await GetProjectScene(item.project_path);
            setSceneDetail((detail || null) as ProjectSceneDetail | null);
        } catch (error) {
            console.error("[ProjectSearch] GetProjectScene failed:", error);
            setSceneDetail({ project_path: item.project_path, name: item.name, recent_artifacts: item.recent_artifacts || [], source_urls: item.source_urls || [], entry_count: item.entry_count });
        } finally {
            setSceneLoadingPath(null);
        }
    }, []);
    const openArchived = useCallback(async (item: ProjectSearchItem) => {
        const name = item.name || item.project_path;
        search.close();
        setArchivedExperience({ name, content: "" });
        setArchivedLoading(true);
        try {
            const experience = await GetArchivedExperience(item.project_path);
            setArchivedExperience({ name, content: experience || localizeText(lang, "No experience summary available.", "\u6682\u65e0\u7ecf\u9a8c\u6458\u8981\u3002") });
        } catch (error) {
            console.error("[ProjectSearch] GetArchivedExperience failed:", error);
            setArchivedExperience({ name, content: localizeText(lang, "Failed to load experience summary.", "\u52a0\u8f7d\u7ecf\u9a8c\u6458\u8981\u5931\u8d25\u3002") });
        } finally {
            setArchivedLoading(false);
        }
    }, [lang, search]);

    const onSelect = useCallback(async (item: ProjectSearchItem) => {
        if (renamingPath) return;
        if (item.archived) { await openArchived(item); return; }
        search.close();
        try {
            const title = item.name || item.project_path;
            const autoSend = false;
            const agentMode = agentModeFromTaskTags(item.tags);
            const remoteHost = remoteHostFromTaskTags(item.tags);
            console.info("[ProjectSearch] opened task", { taskPath: item.project_path, name: title, autoSend, agentMode: agentMode || null });
            if (onCreateProjectTab) {
                onCreateProjectTab(item.project_path, title, { autoSend, agentMode, remoteHost, tags: item.tags });
                return;
            }
            const msg = await ResumeTask(item.project_path);
            if (msg) await onProjectSwitch(msg);
        } catch (error) { console.error("[ProjectSearch] open task failed:", error); }
    }, [renamingPath, openArchived, search, onCreateProjectTab, onProjectSwitch]);

    if (!search.open && !archivedExperience) return null;
    if (archivedExperience) return <ProjectSearchArchivedPanel name={archivedExperience.name} content={archivedExperience.content} loading={archivedLoading} lang={lang} theme={t} panelRef={panelRef} onClose={() => setArchivedExperience(null)} />;

    return (
        <div ref={panelRef} style={{ flexShrink: 0, borderBottom: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg, zIndex: 30000, position: "relative", overflow: "visible" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "6px 12px" }}>
                <span style={{ color: t.textMuted, opacity: 0.8, flexShrink: 0 }}><ProjectSearchIcon name="search" /></span>
                <input ref={inputRef} type="text" value={search.query} onChange={event => search.onQueryChange(event.target.value)} onKeyDown={event => { if (event.key === "Escape") search.close(); }} placeholder={localizeText(lang, "Search tasks...", "\u641c\u7d22\u4efb\u52a1...")} style={{ flex: 1, border: "none", outline: "none", background: "transparent", color: t.text, fontSize: "13px", fontFamily: "inherit", padding: "4px 0", minWidth: 0 }} />
                {onForkCurrentChat && (
                    <button
                        type="button"
                        onClick={() => {
                            setForkNameOpen(true);
                        }}
                        style={{ background: "none", border: "none", cursor: "pointer", color: t.headingColor, fontSize: "16px", padding: "2px 4px", lineHeight: 1, flexShrink: 0, fontWeight: 700 }}
                        title={localizeText(lang, "New task from current chat", "\u4ece\u5f53\u524d\u5bf9\u8bdd\u65b0\u5efa\u4efb\u52a1")}
                    >{"+"}</button>
                )}
                <button {...(inline ? { onMouseDown: (event: React.MouseEvent) => { event.preventDefault(); event.stopPropagation(); search.close(); } } : { onClick: () => search.close() })} style={{ background: "none", border: "none", cursor: "pointer", color: t.text, opacity: 0.5, fontSize: "12px", padding: "2px 4px", lineHeight: 1, flexShrink: 0 }} title={localizeText(lang, "Close", "\u5173\u95ed")}>{"x"}</button>
            </div>
            <ProjectSearchForkForm open={forkNameOpen} lang={lang} theme={t} onCancel={() => setForkNameOpen(false)} onSubmit={name => { setForkNameOpen(false); search.close(); onForkCurrentChat?.(name); }} />
            <div style={{ maxHeight: "320px", overflowY: "auto", padding: "0 4px 4px" }}>
                {search.loading && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{localizeText(lang, "Searching...", "\u641c\u7d22\u4e2d...")}</div>}
                {!search.loading && visibleResults.length === 0 && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{search.query ? localizeText(lang, "No tasks found", "\u672a\u627e\u5230\u4efb\u52a1") : localizeText(lang, "No tasks", "\u6682\u65e0\u4efb\u52a1")}</div>}
                {!search.loading && visibleResults.map(item => <ProjectSearchRow key={item.id || item.project_path} item={item} lang={lang} theme={t} search={search} renamingPath={renamingPath} renameVal={renameVal} setRenameVal={setRenameVal} setRenamingPath={setRenamingPath} onSelect={onSelect} onShowSceneDetail={openSceneDetail} sceneLoading={sceneLoadingPath === item.project_path} refreshResults={refreshResults} setCtxMenu={setCtxMenu} />)}
            </div>
            {(sceneLoadingPath || sceneDetail) && <ProjectSceneDetailPanel detail={sceneDetail} loading={!!sceneLoadingPath} lang={lang} theme={t} formatTime={search.formatTime} onClose={() => setSceneDetail(null)} />}
            {ctxMenu && <ProjectSearchContextMenu ctxMenu={ctxMenu} lang={lang} theme={t} refreshResults={refreshResults} setCtxMenu={setCtxMenu} setRenamingPath={setRenamingPath} setRenameVal={setRenameVal} onCloseProjectTab={onCloseProjectTab} />}
        </div>
    );
}

function ProjectSearchRow({ item, lang, theme: t, search, renamingPath, renameVal, setRenameVal, setRenamingPath, onSelect, onShowSceneDetail, sceneLoading, refreshResults, setCtxMenu }: {
    item: ProjectSearchItem; lang: string; theme: Theme; search: ReturnType<typeof useProjectSearch>; renamingPath: string | null; renameVal: string; setRenameVal: (value: string) => void; setRenamingPath: (path: string | null) => void; onSelect: (item: ProjectSearchItem) => void | Promise<void>; onShowSceneDetail: (item: ProjectSearchItem) => void | Promise<void>; sceneLoading: boolean; refreshResults: () => void; setCtxMenu: (menu: { x: number; y: number; item: ProjectSearchItem } | null) => void;
}) {
    const artifact = item.recent_artifacts?.find(a => a.title || a.preview || a.source_url);
    const artifactSummary = formatArtifactSummary(item, lang);
    const artifactTooltip = formatArtifactSummary(item, lang, true);
    const pureCoding = isPureCodingTaskTags(item.tags);
    const remoteCoding = agentModeFromTaskTags(item.tags) === "remote_coding_dev";
    const remoteHost = remoteHostFromTaskTags(item.tags);
    const fromCodingWorkflow = isCodingWorkflowSourceTags(item.tags);
    const kindLabel = item.archived
        ? "ARC"
        : remoteCoding
            ? "SSH"
            : pureCoding
                ? "CODE"
                : item.pinned
                    ? "PIN"
                    : "TASK";
    return <div data-pure-coding={pureCoding ? "true" : "false"} onClick={() => void onSelect(item)} onContextMenu={event => { event.preventDefault(); setCtxMenu({ x: event.clientX, y: event.clientY, item }); }} style={{ padding: "8px 10px", cursor: "pointer", borderRadius: "6px", transition: "background 0.15s" }} onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}>
        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "2px" }}>
            <span style={{ minWidth: "26px", textAlign: "center", fontSize: "10px", fontWeight: 700, color: pureCoding ? (remoteCoding ? "#0284c7" : "#15803d") : t.textMuted, border: pureCoding ? `1px solid ${remoteCoding ? "color-mix(in srgb, #0ea5e9 48%, transparent)" : "color-mix(in srgb, #22c55e 48%, transparent)"}` : `1px solid ${t.titleBarBorder}`, borderRadius: "4px", padding: "1px 4px", flexShrink: 0 }} title={pureCoding ? (remoteCoding ? localizeText(lang, "Remote pure coding", "远程纯编程") : localizeText(lang, "Local pure coding", "本地纯编程")) : undefined}>{kindLabel}</span>
            {renamingPath === item.project_path ? <input autoFocus value={renameVal} onChange={event => setRenameVal(event.target.value)} onBlur={async () => { const trimmed = renameVal.trim(); if (trimmed && trimmed !== item.name) { await RenameTask(item.project_path, trimmed); refreshResults(); } setRenamingPath(null); }} onKeyDown={event => { if (event.key === "Enter") (event.target as HTMLInputElement).blur(); if (event.key === "Escape") setRenamingPath(null); }} onClick={event => event.stopPropagation()} style={{ flex: 1, fontSize: "13px", fontWeight: 600, color: t.text, background: t.codeBlockBg, border: `1px solid ${t.headingColor}`, borderRadius: "3px", padding: "2px 6px", outline: "none", minWidth: 0, fontFamily: "inherit" }} /> : <span style={{ fontSize: "13px", fontWeight: 600, color: t.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{item.name || item.project_path}</span>}
            {pureCoding && <span data-testid={remoteCoding ? "search-remote-coding-badge" : "search-coding-badge"} style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: remoteCoding ? "color-mix(in srgb, #0ea5e9 12%, transparent)" : "color-mix(in srgb, #22c55e 12%, transparent)", color: remoteCoding ? "#0284c7" : "#15803d", border: remoteCoding ? "1px solid color-mix(in srgb, #0ea5e9 48%, transparent)" : "1px solid color-mix(in srgb, #22c55e 48%, transparent)", flexShrink: 0, maxWidth: 140, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{remoteCoding ? (remoteHost ? `${localizeText(lang, "Remote coding", "远程编程")} · ${remoteHost}` : localizeText(lang, "Remote coding", "远程编程")) : localizeText(lang, "Pure coding", "纯编程")}</span>}
            {fromCodingWorkflow && <span data-testid="search-coding-workflow-source-badge" style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "color-mix(in srgb, #8b5cf6 12%, transparent)", color: "#7c3aed", border: "1px solid color-mix(in srgb, #8b5cf6 48%, transparent)", flexShrink: 0 }} title={localizeText(lang, "Created from coding workflow", "由编程工作流创建")}>{localizeText(lang, "Workflow", "工作流")}</span>}
            {item.workflow_type && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(47,95,152,0.10)", color: t.headingColor, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{formatWorkflowType(item.workflow_type, lang)}</span>}
            {item.archived && <span style={{ fontSize: "10px", padding: "1px 6px", borderRadius: "999px", background: "rgba(100,116,139,0.10)", color: t.textMuted, border: `1px solid ${t.titleBarBorder}`, flexShrink: 0 }}>{localizeText(lang, "Archived", "\u5df2\u5f52\u6863")}</span>}
            <button type="button" onClick={event => { event.stopPropagation(); void onShowSceneDetail(item); }} style={{ border: "none", background: "transparent", color: t.headingColor, opacity: sceneLoading ? 0.35 : 0.7, width: "20px", height: "20px", cursor: sceneLoading ? "default" : "pointer", flexShrink: 0, fontSize: "12px" }} disabled={sceneLoading} title={localizeText(lang, "Scene details", "任务证据详情")}>{sceneLoading ? "..." : <ProjectSearchIcon name="info" />}</button>
            <button type="button" onClick={event => { event.stopPropagation(); void onSelect(item); }} style={{ border: "none", background: item.archived ? "rgba(100,116,139,0.10)" : "rgba(47,95,152,0.10)", color: item.archived ? t.textMuted : t.headingColor, borderRadius: "999px", width: "22px", height: "22px", cursor: "pointer", flexShrink: 0 }} title={item.archived ? localizeText(lang, "View experience", "\u67e5\u770b\u7ecf\u9a8c") : localizeText(lang, "Resume task", "\u7ee7\u7eed\u4efb\u52a1")}><ProjectSearchIcon name="arrowRight" /></button>
        </div>
        <div style={{ fontSize: "11px", color: t.text, opacity: 0.45, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px" }}>{item.project_path}</div>
        {item.preview && <div style={{ fontSize: "11px", color: t.text, opacity: 0.35, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingLeft: "21px", marginTop: "1px" }}>{item.preview}</div>}
        {artifactSummary && <div title={artifactTooltip} style={{ display: "flex", alignItems: "center", gap: "6px", paddingLeft: "21px", marginTop: "1px", minWidth: 0 }}><span style={{ fontSize: "10px", color: t.headingColor, opacity: 0.58, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}>{artifactSummary}</span>{artifact?.source_url && <button type="button" onClick={event => { event.stopPropagation(); void OpenFileOrShowInFolder(artifact.source_url || ""); }} style={{ border: "none", background: "transparent", color: t.headingColor, opacity: 0.7, cursor: "pointer", fontSize: "11px", lineHeight: 1, padding: "1px 2px", flexShrink: 0 }} title={localizeText(lang, "Open artifact source", "打开产物来源")}><ProjectSearchIcon name="externalLink" /></button>}</div>}
        {item.last_activity && <div style={{ fontSize: "10px", color: t.text, opacity: 0.32, paddingLeft: "21px", marginTop: "1px" }}>{search.formatTime(item.last_activity)}</div>}
    </div>;
}


function ProjectSearchContextMenu({ ctxMenu, lang, theme: t, refreshResults, setCtxMenu, setRenamingPath, setRenameVal, onCloseProjectTab }: {
    ctxMenu: { x: number; y: number; item: ProjectSearchItem }; lang: string; theme: Theme; refreshResults: () => void; setCtxMenu: (menu: null) => void; setRenamingPath: (path: string | null) => void; setRenameVal: (value: string) => void; onCloseProjectTab?: (projectPath: string) => void;
}) {
    const actions = [
        { label: localizeText(lang, "Rename", "\u91cd\u547d\u540d"), icon: "edit", action: () => { setRenamingPath(ctxMenu.item.project_path); setRenameVal(ctxMenu.item.name || ""); setCtxMenu(null); } },
        { label: ctxMenu.item.pinned ? localizeText(lang, "Unpin", "\u53d6\u6d88\u7f6e\u9876") : localizeText(lang, "Pin", "\u7f6e\u9876"), icon: "pin", action: async () => { await PinTask(ctxMenu.item.project_path, !ctxMenu.item.pinned); refreshResults(); setCtxMenu(null); } },
        { label: localizeText(lang, "Remove", "\u79fb\u9664"), icon: "x", action: async () => { console.info("[ProjectSearch] removing task", { projectPath: ctxMenu.item.project_path }); await HideTask(ctxMenu.item.project_path); onCloseProjectTab?.(ctxMenu.item.project_path); refreshResults(); setCtxMenu(null); } },
        { label: localizeText(lang, "Archive", "\u5f52\u6863"), icon: "ARC", action: async () => { setCtxMenu(null); const confirmed = window.confirm(localizeText(lang, "After archiving, you will not be able to continue this task, but the experience will be preserved. Confirm archive?", "\u5f52\u6863\u540e\u5c06\u65e0\u6cd5\u7ee7\u7eed\u6b64\u4efb\u52a1\uff0c\u4f46\u7ecf\u9a8c\u4f1a\u88ab\u4fdd\u7559\u3002\u786e\u8ba4\u5f52\u6863\uff1f")); if (!confirmed) return; try { console.info("[ProjectSearch] archiving task", { projectPath: ctxMenu.item.project_path }); await ArchiveProject(ctxMenu.item.project_path); onCloseProjectTab?.(ctxMenu.item.project_path); refreshResults(); } catch (error) { console.error("[ProjectSearch] ArchiveProject failed:", error); } } },
    ];
    return <><div style={{ position: "fixed", inset: 0, zIndex: 9998 }} onClick={() => setCtxMenu(null)} /><div style={{ position: "fixed", left: ctxMenu.x, top: ctxMenu.y, zIndex: 9999, background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: "6px", boxShadow: "0 4px 12px rgba(0,0,0,0.15)", padding: "4px 0", minWidth: "120px" }}>{actions.map(item => <div key={item.label} onClick={item.action} style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 12px", cursor: "pointer", fontSize: "12px", color: t.text, transition: "background 0.1s" }} onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}><span style={{ fontSize: "13px" }}>{item.icon}</span><span>{item.label}</span></div>)}</div></>;
}

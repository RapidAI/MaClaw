import { useCallback, useEffect, useRef, useState } from "react";
import { GetArchivedExperience, GetProjectScene, ResumeTask } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { openFileLibrary } from "../../utils/fileLibraryNavigation";
import { openKnowledgeSearch } from "../../utils/knowledgeSearchNavigation";
import { openNewTaskWizard } from "./task-config/openNewTaskWizard";
import { ProjectSearchArchivedPanel } from "./ProjectSearchArchivedPanel";
import { isSearchDismissExemptTarget, searchSurfaceRootStyle } from "./projectSearchSurface";
import { ProjectSearchForkForm } from "./ProjectSearchForkForm";
import { LibrarySearchRow, SearchSectionLabel } from "./ProjectSearchLibraryRows";
import { ProjectSceneDetailPanel, type ProjectSceneDetail } from "./ProjectSceneDetailPanel";
import { agentModeFromTaskTags, isRemoteMaintenanceTaskTags, isVisibleTaskRow, remoteHostFromTaskTags } from "./codingTaskMode";
import { useProjectSearch, type ProjectSearchItem } from "./useProjectSearch";
import type { HeaderCloudWorkspaceHit } from "./cloudWorkspaceContentSearch";
import type { HeaderDataDirectoryHit } from "./dataDirectoryWorkspaceSearch";
import { clearParkedCloudWorkspaceFileOpen } from "./cloudWorkspaceFileOpen";
import { openHeaderCloudSearchHit, openHeaderDataDirectorySearchHit } from "./openHeaderWorkspaceSearchHit";
import type { HeaderExpertSearchHit, HeaderFileSearchHit, HeaderKnowledgeSearchHit } from "./unifiedHeaderSearch";
import { ProjectSearchContextMenu } from "./ProjectSearchContextMenu";
import { ProjectSearchRow } from "./ProjectSearchRow";

export { useProjectSearch } from "./useProjectSearch";

export function ProjectSearchPanel({ search, lang, theme: t, inline, active = true, onProjectSwitch, onCreateProjectTab, onCloseProjectTab, onForkCurrentChat, onTaskPrefsChanged }: {
    search: ReturnType<typeof useProjectSearch>;
    lang: string;
    theme: Theme;
    inline: boolean;
    /** Whether the assistant page is currently visible in the app shell. */
    active?: boolean;
    onProjectSwitch: (displayMsg: string) => Promise<void> | void;
    onCreateProjectTab?: (projectPath: string, taskTitle: string, options?: {
        autoSend?: boolean;
        agentMode?: "coding_dev" | "remote_coding_dev";
        remoteHost?: string;
        remoteSafety?: "diagnosis";
        tags?: string[];
    }) => void;
    /** Close an open project tab by its project path (e.g. after archiving). */
    onCloseProjectTab?: (projectPath: string) => void;
    /** Fork current local tab conversation into a new project tab. */
    onForkCurrentChat?: (taskName: string) => void;
    onTaskPrefsChanged?: () => void;
}) {
    const panelRef = useRef<HTMLDivElement>(null);
    const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; item: ProjectSearchItem } | null>(null);
    const [renamingPath, setRenamingPath] = useState<string | null>(null);
    const [renameVal, setRenameVal] = useState("");
    const [forkNameOpen, setForkNameOpen] = useState(false);
    const [archivedExperience, setArchivedExperience] = useState<{ name: string; content: string } | null>(null);
    const [archivedLoading, setArchivedLoading] = useState(false);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneLoadingPath, setSceneLoadingPath] = useState<string | null>(null);
    const activeRef = useRef(active);
    activeRef.current = active;
    const visibleResults = search.results.filter(isVisibleTaskRow);
    const fileResults = search.fileResults || [];
    const knowledgeResults = search.knowledgeResults || [];
    const expertResults = search.expertResults || [];
    const cloudResults = search.cloudResults || [];
    const dataDirResults = search.dataDirResults || [];
    const showSectionLabels = [visibleResults.length, cloudResults.length, dataDirResults.length, fileResults.length, knowledgeResults.length, expertResults.length].filter(count => count > 0).length > 1;
    const hasAnyResults = visibleResults.length > 0 || cloudResults.length > 0 || dataDirResults.length > 0 || fileResults.length > 0 || knowledgeResults.length > 0 || expertResults.length > 0;
    const pendingText = (search.pendingQuery || "").trim();
    const shownText = search.query.trim();
    const searchingLabel = pendingText && pendingText !== shownText
        ? localizeText(lang, `Searching “${pendingText}”...`, `正在搜索“${pendingText}”...`, `正在搜尋「${pendingText}」...`)
        : localizeText(lang, "Searching...", "搜索中...", "搜尋中...");

    useEffect(() => {
        if (!search.open) return;
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.stopPropagation();
            if (event.isComposing) return;
            const target = event.target instanceof HTMLElement ? event.target : null;
            const inTaskSearch = !!target?.closest(".mc-task-pane__search");
            const inPanelEditor = !!target && !!panelRef.current?.contains(target)
                && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target.isContentEditable);
            // The task field, rename box, and fork name already handle Escape.
            // Swallow the key either way so it does not also leave maximized mode or close the window.
            if (inTaskSearch || inPanelEditor || event.defaultPrevented) return;
            event.preventDefault();
            search.close();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [search.open, search.close]);
    useEffect(() => {
        if (active) return;
        search.close();
        setCtxMenu(null);
        setRenamingPath(null);
        setForkNameOpen(false);
        setArchivedExperience(null);
        setArchivedLoading(false);
        setSceneDetail(null);
        setSceneLoadingPath(null);
    }, [active, search.close]);
    useEffect(() => {
        if (!search.open && !archivedExperience) return;
        const handler = (event: MouseEvent) => {
            const target = event.target;
            if (panelRef.current && panelRef.current.contains(target as Node)) return;
            if (isSearchDismissExemptTarget(target)) return;
            search.close();
            setCtxMenu(null);
            setArchivedExperience(null);
            setSceneDetail(null);
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, [search.open, search.close, archivedExperience]);

    const refreshResults = useCallback(() => { search.refresh(); onTaskPrefsChanged?.(); }, [search, onTaskPrefsChanged]);
    const openSceneDetail = useCallback(async (item: ProjectSearchItem) => {
        setSceneLoadingPath(item.project_path);
        try {
            const detail = await GetProjectScene(item.project_path);
            if (!activeRef.current) return;
            setSceneDetail((detail || null) as ProjectSceneDetail | null);
        } catch (error) {
            console.error("[ProjectSearch] GetProjectScene failed:", error);
            if (!activeRef.current) return;
            setSceneDetail({ project_path: item.project_path, name: item.name, recent_artifacts: item.recent_artifacts || [], source_urls: item.source_urls || [], entry_count: item.entry_count });
        } finally {
            if (activeRef.current) setSceneLoadingPath(null);
        }
    }, []);
    const openArchived = useCallback(async (item: ProjectSearchItem) => {
        const name = item.name || item.project_path;
        setArchivedExperience({ name, content: "" });
        setArchivedLoading(true);
        try {
            const experience = await GetArchivedExperience(item.project_path);
            if (!activeRef.current) return;
            setArchivedExperience({ name, content: experience || localizeText(lang, "No experience summary available.", "\u6682\u65e0\u7ecf\u9a8c\u6458\u8981\u3002") });
        } catch (error) {
            console.error("[ProjectSearch] GetArchivedExperience failed:", error);
            if (!activeRef.current) return;
            setArchivedExperience({ name, content: localizeText(lang, "Failed to load experience summary.", "\u52a0\u8f7d\u7ecf\u9a8c\u6458\u8981\u5931\u8d25\u3002") });
        } finally {
            if (activeRef.current) setArchivedLoading(false);
        }
    }, [lang]);

    const onSelectCloud = useCallback((item: HeaderCloudWorkspaceHit) => {
        openHeaderCloudSearchHit(item, { close: () => search.close(), onCreateProjectTab, onProjectSwitch });
    }, [search, onCreateProjectTab, onProjectSwitch]);
    const onSelectDataDirectory = useCallback((item: HeaderDataDirectoryHit) => {
        openHeaderDataDirectorySearchHit(item, { close: () => search.close(), onCreateProjectTab, onProjectSwitch });
    }, [search, onCreateProjectTab, onProjectSwitch]);
    const onSelectFile = useCallback((item: HeaderFileSearchHit) => {
        clearParkedCloudWorkspaceFileOpen();
        search.close();
        openFileLibrary({ documentId: item.id });
    }, [search]);
    const onSelectKnowledge = useCallback((item: HeaderKnowledgeSearchHit) => {
        clearParkedCloudWorkspaceFileOpen();
        search.close();
        openKnowledgeSearch({ query: search.query.trim() || item.title });
    }, [search]);
    const onSelectExpert = useCallback((item: HeaderExpertSearchHit) => {
        clearParkedCloudWorkspaceFileOpen();
        search.close();
        openNewTaskWizard({
            expertId: item.expert.id,
            expertName: item.expert.name,
        });
    }, [search]);
    const onSelect = useCallback(async (item: ProjectSearchItem) => {
        if (renamingPath) return;
        if (item.archived) { await openArchived(item); return; }
        clearParkedCloudWorkspaceFileOpen();
        search.close();
        try {
            const title = item.name || item.project_path;
            const autoSend = false;
            const agentMode = agentModeFromTaskTags(item.tags);
            const remoteHost = remoteHostFromTaskTags(item.tags);
            const remoteSafety = agentMode === "remote_coding_dev" && isRemoteMaintenanceTaskTags(item.tags)
                ? "diagnosis"
                : undefined;
            console.info("[ProjectSearch] opened task", { taskPath: item.project_path, name: title, autoSend, agentMode: agentMode || null });
            if (onCreateProjectTab) {
                onCreateProjectTab(item.project_path, title, { autoSend, agentMode, remoteHost, remoteSafety, tags: item.tags });
                return;
            }
            const msg = await ResumeTask(item.project_path);
            if (msg) await onProjectSwitch(msg);
        } catch (error) { console.error("[ProjectSearch] open task failed:", error); }
    }, [renamingPath, openArchived, search, onCreateProjectTab, onProjectSwitch]);

    if (!active || (!search.open && !archivedExperience)) return null;
    if (archivedExperience) return <ProjectSearchArchivedPanel name={archivedExperience.name} content={archivedExperience.content} loading={archivedLoading} lang={lang} theme={t} panelRef={panelRef} onClose={() => setArchivedExperience(null)} />;

    return (
        <div ref={panelRef} id="project-search-panel" data-testid="project-search-panel" role="region" aria-label={localizeText(lang, "Search results", "搜索结果", "搜尋結果")} style={searchSurfaceRootStyle(t)} onKeyDown={event => {
            if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
            if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
            const rows = Array.from(panelRef.current?.querySelectorAll<HTMLElement>(".psp-row, .pslr-row") || []);
            if (!rows.length) return;
            const current = event.target instanceof HTMLElement ? event.target.closest<HTMLElement>(".psp-row, .pslr-row") : null;
            const index = current ? rows.indexOf(current) : -1;
            if (event.key === "ArrowUp" && index <= 0) {
                event.preventDefault();
                document.querySelector<HTMLElement>("[data-testid='task-pane-search']")?.focus();
                return;
            }
            const nextIndex = event.key === "ArrowDown" ? Math.min(rows.length - 1, index + 1) : Math.max(0, index - 1);
            if (nextIndex === index) return;
            event.preventDefault();
            const next = rows[nextIndex];
            next?.focus();
            next?.scrollIntoView?.({ block: "nearest" });
        }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", padding: "10px 12px", flexShrink: 0, borderBottom: `1px solid ${t.titleBarBorder}`, background: t.titleBarBg }}>
                <span style={{ color: t.text, fontSize: "13px", fontWeight: 650, flexShrink: 0 }}>{localizeText(lang, "Search results", "搜索结果", "搜尋結果")}</span>
                <span style={{ flex: 1, minWidth: 0, color: t.textMuted, fontSize: "12px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{search.query.trim()}</span>
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
            <div data-testid="project-search-results" className="psp-results" aria-busy={search.loading || undefined}>
                {search.loading && <div style={{ padding: hasAnyResults ? "6px 10px" : "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{searchingLabel}</div>}
                <div style={search.loading && hasAnyResults ? { opacity: 0.45 } : undefined}>
                {!search.loading && !hasAnyResults && <div style={{ padding: "16px", textAlign: "center", color: t.text, opacity: 0.45, fontSize: "12px" }}>{search.query.trim() ? localizeText(lang, "No results found", "未找到结果", "未找到結果") : localizeText(lang, "No tasks", "暂无任务", "暫無任務")}</div>}
                {showSectionLabels && visibleResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Tasks" zh="任务" zhHant="任務" />}
                {visibleResults.map(item => <ProjectSearchRow key={item.id || item.project_path} item={item} lang={lang} theme={t} formatTime={search.formatTime} renamingPath={renamingPath} renameVal={renameVal} setRenameVal={setRenameVal} setRenamingPath={setRenamingPath} onSelect={onSelect} onShowSceneDetail={openSceneDetail} sceneLoading={sceneLoadingPath === item.project_path} refreshResults={refreshResults} setCtxMenu={setCtxMenu} />)}
                {cloudResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Cloud workspace" zh="云端工作区" zhHant="雲端工作區" testId="search-cloud-section" />}
                {cloudResults.map(item => <LibrarySearchRow key={`cloud-${item.id}`} kind={localizeText(lang, "CWS", "云", "雲")} title={item.title} preview={item.preview} lang={lang} theme={t} testId="search-cloud-row" onSelect={() => onSelectCloud(item)} />)}
                {dataDirResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Data directory" zh="数据目录" zhHant="資料目錄" testId="search-data-section" />}
                {dataDirResults.map(item => <LibrarySearchRow key={`data-${item.id}`} kind={localizeText(lang, "DIR", "目录", "目錄")} title={item.title} preview={item.preview} lang={lang} theme={t} testId="search-data-row" onSelect={() => onSelectDataDirectory(item)} />)}
                {fileResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Cloud drive" zh="云盘" zhHant="雲端硬碟" testId="search-files-section" />}
                {fileResults.map(item => <LibrarySearchRow key={`file-${item.id}`} kind={item.type === "audio" ? localizeText(lang, "AUDIO", "音频", "音訊") : localizeText(lang, "FILE", "文件", "檔案")} title={item.title} preview={item.preview} lang={lang} theme={t} testId="search-file-row" onSelect={() => onSelectFile(item)} />)}
                {knowledgeResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Knowledge" zh="知识库" zhHant="知識庫" testId="search-knowledge-section" />}
                {knowledgeResults.map(item => <LibrarySearchRow key={`knowledge-${item.id}`} kind="KNOW" title={item.title} preview={item.preview || item.sourceTitle} lang={lang} theme={t} testId="search-knowledge-row" onSelect={() => onSelectKnowledge(item)} />)}
                {expertResults.length > 0 && <SearchSectionLabel lang={lang} theme={t} en="Experts" zh="AI专家" zhHant="AI專家" testId="search-experts-section" />}
                {expertResults.map(item => <LibrarySearchRow key={`expert-${item.id}`} kind="AI" title={item.title} preview={item.preview} mark={item.icon || "🤖"} lang={lang} theme={t} testId="search-expert-row" onSelect={() => onSelectExpert(item)} />)}
                </div>
            </div>
            {(sceneLoadingPath || sceneDetail) && <ProjectSceneDetailPanel detail={sceneDetail} loading={!!sceneLoadingPath} lang={lang} theme={t} formatTime={search.formatTime} onClose={() => setSceneDetail(null)} />}
            {ctxMenu && <ProjectSearchContextMenu ctxMenu={ctxMenu} lang={lang} theme={t} refreshResults={refreshResults} setCtxMenu={setCtxMenu} setRenamingPath={setRenamingPath} setRenameVal={setRenameVal} onCloseProjectTab={onCloseProjectTab} />}
        </div>
    );
}

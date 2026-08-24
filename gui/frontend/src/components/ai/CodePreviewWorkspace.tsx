import React, { useCallback, useEffect, useRef, useState } from "react";
import { GetCodingWorkbenchDirectory, GetCodingWorkbenchEntryProperties, GetCodingWorkbenchFilePreview, IsCodingWorkbenchVSCodeAvailable, OpenCodingWorkbenchFileInVSCode } from "../../../wailsjs/go/main/App";
import { localizeText, normalizeLang } from "../../i18n/langSelect";
import type { CodePreviewTheme } from "./FileTabBar";
import type { CodeFile } from "./useCodePreviewState";

type DirectoryEntry = { name: string; path: string; is_dir: boolean };
type DirectoryResponse = { root?: string; entries?: DirectoryEntry[]; truncated?: boolean };
type FilePreviewResponse = { path?: string; abs_path?: string; content?: string; language?: string; truncated?: boolean };
type EntryProperties = { name?: string; path?: string; abs_path?: string; is_dir?: boolean; size?: number; size_known?: boolean; modified_at?: number; mode?: string; extension?: string };
type ContextMenu = { entry: DirectoryEntry; x: number; y: number };
type Cache = { directories: Map<string, DirectoryResponse>; root: string; refreshedAt: Map<string, number> };

const cache = new Map<string, Cache>();
const cacheTtl = 15_000;
const cacheProjects = 12;
const cacheDirectories = 64;
const noticeAutoDismissMs = 8_000;

export function __resetWorkspaceDirectoryCacheForTests() { cache.clear(); }
function dropProjectCache(projectPath?: string) { if (projectPath) cache.delete(projectPath); }

function boundedPages(source: Map<string, DirectoryResponse>) {
    const next = new Map(source);
    while (next.size > cacheDirectories) {
        const first = next.keys().next().value;
        if (first === undefined) break;
        if (first === "") {
            const root = next.get(first);
            next.delete(first);
            if (root) next.set(first, root);
            continue;
        }
        next.delete(first);
    }
    return next;
}

function saveCache(projectPath: string, directories: Map<string, DirectoryResponse>, root: string, refreshedAt: Map<string, number>) {
    const pages = boundedPages(directories);
    const fresh = new Map<string, number>();
    for (const path of pages.keys()) {
        const at = refreshedAt.get(path);
        if (at) fresh.set(path, at);
    }
    cache.delete(projectPath);
    cache.set(projectPath, { directories: pages, root, refreshedAt: fresh });
    while (cache.size > cacheProjects) {
        const oldest = cache.keys().next().value;
        if (oldest === undefined) break;
        cache.delete(oldest);
    }
}

function takeCache(projectPath: string) {
    const found = cache.get(projectPath);
    if (!found) return undefined;
    cache.delete(projectPath);
    cache.set(projectPath, found);
    return found;
}

function label(lang: string, en: string, zhHans: string, zhHant?: string) { return localizeText(lang, en, zhHans, zhHant); }
function basename(value: string) { const clean = value.replace(/[\\/]+$/, ""); return clean.slice(Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")) + 1) || clean; }
function fileSize(bytes: number) { if (!Number.isFinite(bytes) || bytes < 0) return "-"; if (bytes < 1024) return `${bytes} B`; const units = ["KB", "MB", "GB", "TB"]; let value = bytes / 1024; let index = 0; while (value >= 1024 && index < 3) { value /= 1024; index++; } return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`; }
function modifiedAt(seconds: number, lang: string) { const normalized = normalizeLang(lang); const locale = normalized === "zh-Hant" ? "zh-TW" : normalized === "zh-Hans" ? "zh-CN" : "en-US"; return Number.isFinite(seconds) && seconds > 0 ? new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "medium" }).format(new Date(seconds * 1000)) : "-"; }
function clampMenuPosition(x: number, y: number) { return { x: Math.max(8, Math.min(x, window.innerWidth - 184)), y: Math.max(8, Math.min(y, window.innerHeight - 128)) }; }

export function workspaceFileIconKind(name: string): { badge: string; kind: "code" | "data" | "text" | "markup" | "file" } {
    const ext = name.toLowerCase().split(".").pop() || "";
    if (["ts", "tsx", "js", "jsx"].includes(ext)) return { badge: ext.startsWith("t") ? "TS" : "JS", kind: "code" };
    if (["go", "rs", "py", "java", "kt", "cs", "cpp", "c", "h", "hpp", "swift", "rb", "php"].includes(ext)) return { badge: ext === "cpp" ? "C+" : ext.slice(0, 2).toUpperCase(), kind: "code" };
    if (["json", "yaml", "yml", "toml", "xml", "ini", "env"].includes(ext)) return { badge: "{}", kind: "data" };
    if (["md", "txt", "rst", "log"].includes(ext)) return { badge: "TXT", kind: "text" };
    if (["html", "css", "scss", "svg"].includes(ext)) return { badge: "<>", kind: "markup" };
    return { badge: "FILE", kind: "file" };
}

function isHiddenBrowserName(name?: string) { return Boolean(name?.trim().startsWith(".")); }
function visibleDirectoryResponse(data?: DirectoryResponse | null): DirectoryResponse {
    const entries = (data?.entries || []).filter(entry => !isHiddenBrowserName(entry.name));
    return { ...(data || {}), entries };
}
function visiblePages(source: Map<string, DirectoryResponse>) {
    const next = new Map<string, DirectoryResponse>();
    for (const [key, value] of source) next.set(key, visibleDirectoryResponse(value));
    return next;
}
function sameWorkspaceRoot(left?: string, right?: string) {
    const normalize = (value?: string) => {
        const cleaned = (value || "").trim().replace(/[\\/]+$/, "").replace(/\\/g, "/");
        return /^[a-zA-Z]:/.test(cleaned) ? cleaned.toLowerCase() : cleaned;
    };
    const a = normalize(left);
    return a !== "" && a === normalize(right);
}
function isVSCodeSourceFile(entry: DirectoryEntry) { return !entry.is_dir && ["code", "markup"].includes(workspaceFileIconKind(entry.name).kind); }

function FileIcon({ entry, theme, open }: { entry: DirectoryEntry; theme: CodePreviewTheme; open: boolean }) {
    const icon = entry.is_dir ? { badge: "", kind: "data" as const } : workspaceFileIconKind(entry.name);
    const color = open ? theme.tabActiveText : icon.kind === "code" ? theme.syntaxKeyword : icon.kind === "text" ? theme.syntaxString : theme.syntaxNumber;
    return <svg aria-hidden="true" viewBox="0 0 20 20" width="16" height="16" style={{ flexShrink: 0 }}><path d={entry.is_dir ? "M2.5 5.5c0-1.1.9-2 2-2h3l1.4 1.7h6.6c1.1 0 2 .9 2 2v6.3c0 1.1-.9 2-2 2h-11c-1.1 0-2-.9-2-2V5.5Z" : "M4.25 2.5h7l4.5 4.5v9.25c0 .69-.56 1.25-1.25 1.25H5.5c-.69 0-1.25-.56-1.25-1.25V3.75c0-.69.56-1.25 1.25-1.25Z"} fill={color} opacity=".22" stroke={color} strokeWidth="1.2" /><text x="10" y="14" textAnchor="middle" fill={color} fontSize="5.5" fontWeight="700">{icon.badge}</text></svg>;
}

export function CodePreviewWorkspace({ projectPath, refreshToken = 0, resetOnRefresh = false, lang, theme, onOpenFile }: { projectPath?: string; refreshToken?: number; resetOnRefresh?: boolean; lang: string; theme: CodePreviewTheme; onOpenFile: (file: CodeFile) => void }) {
    const [, setDirectories] = useState<Map<string, DirectoryResponse>>(new Map());
    const [expanded, setExpanded] = useState<Set<string>>(new Set([""]));
    const [loading, setLoading] = useState<Set<string>>(new Set());
    const [errors, setErrors] = useState<Map<string, string>>(new Map());
    const [root, setRoot] = useState("");
    const [rootResolved, setRootResolved] = useState(false);
    const [notice, setNotice] = useState("");
    const [noticeIsError, setNoticeIsError] = useState(false);
    const [menu, setMenu] = useState<ContextMenu | null>(null);
    const [propertiesEntry, setPropertiesEntry] = useState<DirectoryEntry | null>(null);
    const [properties, setProperties] = useState<EntryProperties | null>(null);
    const [propertiesError, setPropertiesError] = useState("");
    const [propertiesLoading, setPropertiesLoading] = useState(false);
    const [vscodeAvailable, setVSCodeAvailable] = useState(false);
    const pagesRef = useRef(new Map<string, DirectoryResponse>());
    const refreshedRef = useRef(new Map<string, number>());
    const rootRef = useRef("");
    const menuActionRef = useRef<HTMLButtonElement | null>(null);
    const menuTriggerRef = useRef<HTMLButtonElement | null>(null);
    const lastRefreshTokenRef = useRef(refreshToken);
    const versionRef = useRef(0);
    const previewRef = useRef(0);
    const propertyRef = useRef(0);
    const vscodeOpenRef = useRef(0);
    const vscodeCheckRef = useRef(0);
    const inFlightRef = useRef(new Map<string, number>());

    const load = useCallback(async (path: string, force = false) => {
        if (!projectPath) return;
        const at = refreshedRef.current.get(path) || 0;
        if (!force && pagesRef.current.has(path) && Date.now() - at < cacheTtl) return;
        const version = versionRef.current;
        if (inFlightRef.current.get(path) === version) return;
        inFlightRef.current.set(path, version);
        setLoading(prev => new Set(prev).add(path));
        setErrors(prev => { const next = new Map(prev); next.delete(path); return next; });
        try {
            const data = await GetCodingWorkbenchDirectory(projectPath, path) as DirectoryResponse;
            if (version !== versionRef.current) return;
            const incomingRoot = path ? rootRef.current : String(data?.root || "");
            const rootChanged = !path && Boolean(rootRef.current) && Boolean(incomingRoot) && !sameWorkspaceRoot(rootRef.current, incomingRoot);
            const next = rootChanged ? new Map<string, DirectoryResponse>() : new Map(pagesRef.current);
            next.delete(path);
            next.set(path, visibleDirectoryResponse(data));
            pagesRef.current = boundedPages(next);
            if (rootChanged) {
                refreshedRef.current = new Map([["", Date.now()]]);
                setExpanded(new Set([""]));
                setErrors(new Map());
                setMenu(null);
                setPropertiesEntry(null);
            } else {
                refreshedRef.current.set(path, Date.now());
            }
            setDirectories(pagesRef.current);
            if (!path) {
                rootRef.current = incomingRoot;
                setRoot(incomingRoot);
                setRootResolved(true);
            }
            saveCache(projectPath, pagesRef.current, incomingRoot, refreshedRef.current);
        } catch (error) {
            if (version === versionRef.current) setErrors(prev => new Map(prev).set(path, error instanceof Error ? error.message : String(error)));
        } finally {
            if (inFlightRef.current.get(path) === version) inFlightRef.current.delete(path);
            if (version === versionRef.current) setLoading(prev => { const next = new Set(prev); next.delete(path); return next; });
        }
    }, [projectPath]);

    useEffect(() => {
        const saved = projectPath ? takeCache(projectPath) : undefined;
        const pages = saved ? boundedPages(visiblePages(saved.directories)) : new Map<string, DirectoryResponse>();
        pagesRef.current = pages;
        refreshedRef.current = saved ? new Map(saved.refreshedAt) : new Map();
        rootRef.current = saved?.root || "";
        lastRefreshTokenRef.current = refreshToken;
        versionRef.current++;
        inFlightRef.current.clear();
        previewRef.current++;
        propertyRef.current++;
        vscodeOpenRef.current++;
        setDirectories(pages);
        setLoading(new Set());
        setRoot(rootRef.current);
        setRootResolved(Boolean(saved));
        setExpanded(new Set([""]));
        setErrors(new Map());
        setNotice("");
        setNoticeIsError(false);
        setMenu(null);
        setPropertiesEntry(null);
        setProperties(null);
        setPropertiesLoading(false);
        if (projectPath) void load("", true);
    }, [projectPath]); // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (!projectPath || refreshToken === lastRefreshTokenRef.current) return;
        lastRefreshTokenRef.current = refreshToken;
        versionRef.current++;
        inFlightRef.current.clear();
        // Local directory changes must not keep the previous project's files on
        // screen while the new listing is in flight. SSH reconnects keep the
        // cached tree so a dropped session does not flash an empty explorer.
        if (resetOnRefresh) {
            dropProjectCache(projectPath);
            pagesRef.current = new Map();
            refreshedRef.current = new Map();
            rootRef.current = "";
            setDirectories(pagesRef.current);
            setRoot("");
            setRootResolved(false);
            setExpanded(new Set([""]));
            setErrors(new Map());
            setMenu(null);
            setPropertiesEntry(null);
        }
        setLoading(new Set());
        const refreshVersion = versionRef.current;
        // The root is the user-visible confirmation of a successful reconnect,
        // so wait for it before issuing any background child refreshes. A fully
        // expanded tree must not compete with the first useful SSH response.
        const previousRoot = rootRef.current;
        const visiblePaths = resetOnRefresh ? [] : Array.from(expanded).filter(path => path && pagesRef.current.has(path));
        void (async () => {
            await load("", true);
            if (versionRef.current !== refreshVersion) return;
            // load() already replaced the tree when the live root changed.
            // Do not refresh child paths that belonged to the previous directory.
            if (resetOnRefresh || (previousRoot && !sameWorkspaceRoot(previousRoot, rootRef.current))) {
                return;
            }
            const batchSize = 3;
            for (let index = 0; index < visiblePaths.length; index += batchSize) {
                if (versionRef.current !== refreshVersion) return;
                await Promise.all(visiblePaths.slice(index, index + batchSize).map(path => load(path, true)));
            }
        })();
    }, [projectPath, refreshToken, resetOnRefresh, load]);

    // A manual refresh is an explicit request for newer data. Give it a new
    // generation so it can supersede a slow or stuck remote directory request
    // instead of being silently ignored by the in-flight de-duplication guard.
    const refreshRoot = useCallback(() => {
        versionRef.current++;
        inFlightRef.current.clear();
        setLoading(new Set());
        void load("", true);
    }, [load]);

    const refreshVSCodeAvailability = useCallback(() => {
        const request = ++vscodeCheckRef.current;
        void Promise.resolve().then(() => IsCodingWorkbenchVSCodeAvailable()).then(value => { if (request === vscodeCheckRef.current) setVSCodeAvailable(value === true); }).catch(() => { if (request === vscodeCheckRef.current) setVSCodeAvailable(false); });
    }, []);
    useEffect(() => { refreshVSCodeAvailability(); }, [refreshVSCodeAvailability]);

    useEffect(() => {
        if (!notice || noticeIsError) return;
        const timer = window.setTimeout(() => setNotice(""), noticeAutoDismissMs);
        return () => window.clearTimeout(timer);
    }, [notice, noticeIsError]);

    const closeMenu = useCallback((restoreFocus = false) => { setMenu(null); if (restoreFocus) window.requestAnimationFrame(() => menuTriggerRef.current?.focus()); }, []);
    const openMenu = useCallback((entry: DirectoryEntry, x: number, y: number, trigger: HTMLButtonElement) => { menuTriggerRef.current = trigger; setMenu({ entry, ...clampMenuPosition(x, y) }); }, []);
    useEffect(() => {
        if (!menu) return;
        const focus = window.setTimeout(() => menuActionRef.current?.focus(), 0);
        const close = (event: MouseEvent) => { if (!(event.target instanceof Element) || !event.target.closest('[data-testid="code-preview-workspace-context-menu"]')) closeMenu(); };
        const onKeyDown = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); closeMenu(true); } };
        window.addEventListener("mousedown", close);
        window.addEventListener("keydown", onKeyDown);
        return () => { window.clearTimeout(focus); window.removeEventListener("mousedown", close); window.removeEventListener("keydown", onKeyDown); };
    }, [closeMenu, menu]);

    const toggle = useCallback((entry: DirectoryEntry) => { const willOpen = !expanded.has(entry.path); setExpanded(prev => { const next = new Set(prev); willOpen ? next.add(entry.path) : next.delete(entry.path); return next; }); if (willOpen) void load(entry.path); }, [expanded, load]);
    const openFile = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++previewRef.current; setNotice(""); setNoticeIsError(false); try { const data = await GetCodingWorkbenchFilePreview(projectPath, entry.path) as FilePreviewResponse; if (version !== versionRef.current || request !== previewRef.current) return; onOpenFile({ filePath: String(data?.path || entry.path), fileName: basename(String(data?.path || entry.path)), absPath: data?.abs_path || undefined, content: String(data?.content || ""), language: String(data?.language || "plaintext"), opType: "read", updatedAt: Date.now(), previewTruncated: data?.truncated === true }); } catch (error) { if (version === versionRef.current && request === previewRef.current) { setNoticeIsError(true); setNotice(error instanceof Error ? error.message : String(error)); } } }, [onOpenFile, projectPath]);
    const openProperties = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++propertyRef.current; setPropertiesEntry(entry); setProperties(null); setPropertiesError(""); setPropertiesLoading(true); try { const data = await GetCodingWorkbenchEntryProperties(projectPath, entry.path) as EntryProperties; if (version === versionRef.current && request === propertyRef.current) setProperties(data || {}); } catch (error) { if (version === versionRef.current && request === propertyRef.current) setPropertiesError(error instanceof Error ? error.message : String(error)); } finally { if (version === versionRef.current && request === propertyRef.current) setPropertiesLoading(false); } }, [projectPath]);
    const openVSCode = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++vscodeOpenRef.current; setNotice(""); setNoticeIsError(false); try { const localRemoteCopy = await OpenCodingWorkbenchFileInVSCode(projectPath, entry.path); if (version === versionRef.current && request === vscodeOpenRef.current && localRemoteCopy) setNotice(label(lang, "Remote file downloaded to a local temporary copy and opened in VS Code. Changes there do not sync back automatically.", "远程文件已下载到本地临时副本，并在 VS Code 中打开；其中的修改不会自动同步回远程。", "遠端檔案已下載至本機暫存副本，並在 VS Code 中開啟；其中的修改不會自動同步回遠端。")); } catch (error) { if (version === versionRef.current && request === vscodeOpenRef.current) { setNoticeIsError(true); setNotice(error instanceof Error ? error.message : String(error)); } } }, [lang, projectPath]);

    const renderEntries = (path: string, depth: number, ancestors = new Set<string>([path])): React.ReactNode => pagesRef.current.get(path)?.entries?.map(entry => <React.Fragment key={entry.path}><button type="button" data-testid={entry.is_dir ? "code-preview-workspace-directory" : "code-preview-workspace-file"} onClick={() => entry.is_dir ? toggle(entry) : void openFile(entry)} onContextMenu={event => { event.preventDefault(); if (isVSCodeSourceFile(entry)) refreshVSCodeAvailability(); openMenu(entry, event.clientX, event.clientY, event.currentTarget); }} onKeyDown={event => { if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10")) return; event.preventDefault(); if (isVSCodeSourceFile(entry)) refreshVSCodeAvailability(); const bounds = event.currentTarget.getBoundingClientRect(); openMenu(entry, bounds.left + Math.min(bounds.width / 2, 40), bounds.top + Math.min(bounds.height, 36), event.currentTarget); }} aria-expanded={entry.is_dir ? expanded.has(entry.path) : undefined} aria-haspopup="menu" title={entry.path} style={{ display: "flex", width: "100%", border: 0, padding: `4px 10px 4px ${12 + depth * 16}px`, gap: 6, background: "transparent", color: theme.text, textAlign: "left", cursor: "pointer", font: "inherit", fontSize: 12 }}><span style={{ width: 12 }}>{entry.is_dir ? (expanded.has(entry.path) ? "v" : ">") : ""}</span><FileIcon entry={entry} theme={theme} open={expanded.has(entry.path)} /><span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{entry.name}</span></button>{entry.is_dir && !ancestors.has(entry.path) && expanded.has(entry.path) ? <>{errors.get(entry.path) ? <DirectoryLoadError message={errors.get(entry.path) || ""} depth={depth} lang={lang} theme={theme} onRetry={() => void load(entry.path, true)} /> : null}{loading.has(entry.path) && !pagesRef.current.has(entry.path) ? <DirectoryLoading depth={depth} lang={lang} theme={theme} /> : renderEntries(entry.path, depth + 1, new Set([...ancestors, entry.path]))}{pagesRef.current.get(entry.path)?.truncated ? <div style={{ paddingLeft: 32 + depth * 16, color: theme.textMuted, fontSize: 11 }}>{label(lang, "Showing the first 500 items.", "仅显示前 500 个项目。", "僅顯示前 500 個項目。")}</div> : null}</> : null}</React.Fragment>);

    if (!projectPath) return <div data-testid="code-preview-workspace-status">{label(lang, "Working directory unavailable", "工作目录不可用", "工作目錄無法使用")}</div>;
    const rootError = errors.get("");
    const rootLabel = root || (rootResolved ? label(lang, "Working directory", "工作目录", "工作目錄") : label(lang, "Resolving directory...", "正在定位目录...", "正在定位目錄..."));
    return <div data-testid="code-preview-workspace" style={{ display: "flex", flexDirection: "column", height: "100%", background: theme.bg }}><div style={{ display: "flex", gap: 8, padding: "8px 12px", borderBottom: `1px solid ${theme.border}`, color: theme.textMuted, fontSize: 11 }}><strong style={{ color: theme.tabActiveText }}>{label(lang, "WORKING DIRECTORY", "工作目录", "工作目錄")}</strong><span data-testid="code-preview-workspace-root-label" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", color: rootResolved ? theme.textMuted : theme.lineNumText, fontSize: rootResolved ? 11 : 10.5, fontWeight: 400 }}>{rootLabel}</span><button type="button" aria-label={label(lang, "Refresh working directory", "刷新工作目录", "重新整理工作目錄")} title={label(lang, "Refresh", "刷新", "重新整理")} aria-busy={loading.has("")} onClick={refreshRoot} style={{ marginLeft: "auto", border: 0, borderRadius: 4, padding: "2px 5px", background: "transparent", color: theme.tabActiveText, cursor: "pointer", font: "inherit" }}>{label(lang, "Refresh", "刷新", "重新整理")}</button></div><div style={{ flex: 1, overflow: "auto", padding: "6px 0" }}>{rootError ? <WorkspaceNotice message={rootError} isError lang={lang} theme={theme} onClose={() => setErrors(prev => { const next = new Map(prev); next.delete(""); return next; })} /> : null}{notice ? <WorkspaceNotice message={notice} isError={noticeIsError} lang={lang} theme={theme} onClose={() => { setNotice(""); setNoticeIsError(false); }} /> : null}{propertiesEntry ? <Properties entry={propertiesEntry} properties={properties} loading={propertiesLoading} error={propertiesError} lang={lang} theme={theme} onClose={() => { propertyRef.current++; setPropertiesEntry(null); setPropertiesLoading(false); }} /> : null}{loading.has("") && !pagesRef.current.has("") ? <DirectoryLoading root lang={lang} theme={theme} /> : null}{rootResolved && pagesRef.current.has("") && !pagesRef.current.get("")?.entries?.length ? <EmptyDirectory lang={lang} theme={theme} /> : null}{renderEntries("", 0)}</div>{menu ? <Menu menu={menu} theme={theme} lang={lang} showVSCode={vscodeAvailable && isVSCodeSourceFile(menu.entry)} actionRef={menuActionRef} onPreview={() => { closeMenu(); menu.entry.is_dir ? void load(menu.entry.path, true) : void openFile(menu.entry); }} onVSCode={() => { closeMenu(); void openVSCode(menu.entry); }} onProperties={() => { closeMenu(); void openProperties(menu.entry); }} /> : null}</div>;
}

function Menu({ menu, theme, lang, showVSCode, actionRef, onPreview, onVSCode, onProperties }: { menu: ContextMenu; theme: CodePreviewTheme; lang: string; showVSCode: boolean; actionRef: React.RefObject<HTMLButtonElement>; onPreview: () => void; onVSCode: () => void; onProperties: () => void }) { const style: React.CSSProperties = { display: "block", width: "100%", border: 0, background: "transparent", color: theme.text, padding: "7px 12px", textAlign: "left", cursor: "pointer" }; return <div role="menu" aria-label={menu.entry.name} data-testid="code-preview-workspace-context-menu" style={{ position: "fixed", zIndex: 20, left: menu.x, top: menu.y, width: "min(176px, calc(100vw - 16px))", padding: 4, border: `1px solid ${theme.border}`, borderRadius: 6, background: theme.tabBg }}><button ref={actionRef} type="button" role="menuitem" data-testid="code-preview-workspace-context-preview" style={style} onClick={onPreview}>{menu.entry.is_dir ? label(lang, "Preview folder", "预览文件夹", "預覽資料夾") : label(lang, "Preview", "预览", "預覽")}</button>{showVSCode ? <button type="button" role="menuitem" data-testid="code-preview-workspace-context-open-vscode" style={style} onClick={onVSCode}>{label(lang, "Open with VS Code", "使用 VS Code 打开", "使用 VS Code 開啟")}</button> : null}<button type="button" role="menuitem" data-testid="code-preview-workspace-context-properties" style={style} onClick={onProperties}>{label(lang, "Properties", "属性", "屬性")}</button></div>; }

function WorkspaceNotice({ message, isError, lang, theme, onClose }: { message: string; isError: boolean; lang: string; theme: CodePreviewTheme; onClose: () => void }) {
    const foreground = isError ? theme.diffDeleteText : theme.tabActiveText;
    return <div data-testid="code-preview-workspace-notice" role={isError ? "alert" : "status"} style={{ display: "flex", alignItems: "flex-start", gap: 7, margin: "2px 10px 7px", padding: "6px 7px 6px 9px", border: `1px solid ${theme.border}`, borderRadius: 6, background: isError ? theme.diffDeleteBg : theme.tabBg, color: foreground, fontSize: isError ? 12 : 11, lineHeight: 1.4, fontWeight: 400 }}><span aria-hidden="true" style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", flex: "0 0 auto", width: 14, height: 14, marginTop: 1, border: `1px solid ${foreground}`, borderRadius: "50%", fontSize: 9, fontWeight: 700, lineHeight: 1 }}>{isError ? "!" : "i"}</span><span style={{ flex: 1, minWidth: 0, overflowWrap: "anywhere" }}>{message}</span><button type="button" aria-label={label(lang, "Dismiss message", "关闭提示", "關閉提示")} title={label(lang, "Close", "关闭", "關閉")} onClick={onClose} style={{ flex: "0 0 auto", width: 20, height: 20, marginTop: -3, border: 0, borderRadius: 4, padding: 0, background: "transparent", color: foreground, cursor: "pointer", font: "inherit", fontSize: 16, lineHeight: "20px" }}>×</button></div>;
}

function DirectoryLoadError({ message, depth, lang, theme, onRetry }: { message: string; depth: number; lang: string; theme: CodePreviewTheme; onRetry: () => void }) {
    return <div data-testid="code-preview-workspace-directory-error" role="alert" style={{ display: "flex", alignItems: "center", gap: 6, padding: `3px 10px 4px ${32 + depth * 16}px`, color: theme.diffDeleteText, background: theme.diffDeleteBg, fontSize: 11, lineHeight: 1.4 }}><span style={{ flex: 1, minWidth: 0, overflowWrap: "anywhere" }}>{message}</span><button type="button" onClick={onRetry} style={{ flex: "0 0 auto", border: `1px solid ${theme.border}`, borderRadius: 4, padding: "2px 6px", background: theme.tabBg, color: theme.tabActiveText, cursor: "pointer", font: "inherit" }}>{label(lang, "Retry", "重试", "重試")}</button></div>;
}

function DirectoryLoading({ root = false, depth = 0, lang, theme }: { root?: boolean; depth?: number; lang: string; theme: CodePreviewTheme }) {
    return <div data-testid={root ? "code-preview-workspace-root-loading" : "code-preview-workspace-directory-loading"} role="status" style={{ display: "flex", alignItems: "center", gap: 6, padding: root ? "5px 12px 7px" : `3px 10px 4px ${32 + depth * 16}px`, color: theme.textMuted, fontSize: 10.5, lineHeight: 1.35, fontWeight: 400 }}><span aria-hidden="true" style={{ width: 4, height: 4, flex: "0 0 auto", borderRadius: "50%", background: theme.textMuted, opacity: 0.65 }} /><span>{root ? label(lang, "Loading working directory...", "正在加载工作目录...", "正在載入工作目錄...") : label(lang, "Loading...", "正在加载...", "正在載入...")}</span></div>;
}

function EmptyDirectory({ lang, theme }: { lang: string; theme: CodePreviewTheme }) {
    return <div data-testid="code-preview-workspace-empty" role="status" style={{ display: "flex", alignItems: "center", gap: 6, padding: "6px 12px 8px", color: theme.textMuted, fontSize: 10.5, lineHeight: 1.4, fontWeight: 400 }}><span aria-hidden="true" style={{ width: 4, height: 4, flex: "0 0 auto", borderRadius: "50%", background: theme.textMuted, opacity: 0.5 }} /><span>{label(lang, "This working directory is empty.", "此工作目录为空。", "此工作目錄是空的。")}</span></div>;
}

function Properties({ entry, properties, loading, error, lang, theme, onClose }: { entry: DirectoryEntry; properties: EntryProperties | null; loading: boolean; error: string; lang: string; theme: CodePreviewTheme; onClose: () => void }) {
    const data = properties || {};
    const folder = data.is_dir ?? entry.is_dir;
    const rows = [[label(lang, "Name", "名称", "名稱"), data.name || entry.name], [label(lang, "Type", "类型", "類型"), `${folder ? label(lang, "Folder", "文件夹", "資料夾") : label(lang, "File", "文件", "檔案")}${data.extension ? ` (.${data.extension})` : ""}`], [label(lang, "Path", "路径", "路徑"), data.path || entry.path || "."], [label(lang, "Full path", "完整路径", "完整路徑"), data.abs_path || "-"], [label(lang, "Size", "大小", "大小"), data.size_known ? fileSize(Number(data.size || 0)) : folder ? label(lang, "Not calculated", "未计算", "未計算") : "-"], [label(lang, "Modified", "修改时间", "修改時間"), modifiedAt(Number(data.modified_at || 0), lang)], [label(lang, "Permissions", "权限", "權限"), data.mode || "-"]];
    return <section data-testid="code-preview-workspace-properties" role="region" aria-label={label(lang, "Properties", "属性", "屬性")} style={{ margin: "8px 10px 12px", border: `1px solid ${theme.border}`, borderRadius: 8, background: theme.tabBg, overflow: "hidden" }}><header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, padding: "8px 10px", borderBottom: `1px solid ${theme.border}`, color: theme.tabActiveText }}><strong style={{ fontSize: 13 }}>{label(lang, "Properties", "属性", "屬性")}</strong><button type="button" aria-label={label(lang, "Close properties", "关闭属性", "關閉屬性")} title={label(lang, "Close", "关闭", "關閉")} onClick={onClose} style={{ width: 28, height: 28, border: 0, borderRadius: 4, background: "transparent", color: theme.textMuted, cursor: "pointer", fontSize: 20, lineHeight: 1 }}>×</button></header><div style={{ display: "grid", gridTemplateColumns: "minmax(76px, max-content) minmax(0, 1fr)", gap: "6px 12px", padding: "10px", fontSize: 12, lineHeight: 1.45 }}>{rows.map(([name, value]) => <React.Fragment key={name}><span style={{ color: theme.textMuted }}>{`${name}: `}</span><span style={{ minWidth: 0, color: theme.text, overflowWrap: "anywhere" }}>{value}</span></React.Fragment>)}</div>{loading ? <div role="status" style={{ padding: "0 10px 10px", color: theme.textMuted, fontSize: 12 }}>{label(lang, "Loading properties...", "正在加载属性...", "正在載入屬性...")}</div> : null}{error ? <div role="status" style={{ padding: "0 10px 10px", color: theme.diffDeleteText, fontSize: 12, overflowWrap: "anywhere" }}>{error}</div> : null}</section>;
}

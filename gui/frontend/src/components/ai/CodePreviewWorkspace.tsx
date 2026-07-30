import React, { useCallback, useEffect, useRef, useState } from "react";
import { GetCodingWorkbenchDirectory, GetCodingWorkbenchEntryProperties, GetCodingWorkbenchFilePreview, IsCodingWorkbenchVSCodeAvailable, OpenCodingWorkbenchFileInVSCode } from "../../../wailsjs/go/main/App";
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

export function __resetWorkspaceDirectoryCacheForTests() { cache.clear(); }

function boundedPages(source: Map<string, DirectoryResponse>) {
    const next = new Map(source);
    while (next.size > cacheDirectories) {
        const first = next.keys().next().value;
        if (first === undefined) break;
        if (first === "") { const root = next.get(first); next.delete(first); if (root) next.set(first, root); continue; }
        next.delete(first);
    }
    return next;
}

function saveCache(projectPath: string, directories: Map<string, DirectoryResponse>, root: string, refreshedAt: Map<string, number>) {
    const pages = boundedPages(directories);
    const fresh = new Map<string, number>();
    for (const path of pages.keys()) { const at = refreshedAt.get(path); if (at) fresh.set(path, at); }
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
    cache.delete(projectPath); cache.set(projectPath, found);
    return found;
}

function label(lang: string, en: string, zh: string) { return lang.startsWith("zh") ? zh : en; }
function basename(value: string) { const clean = value.replace(/[\\/]+$/, ""); return clean.slice(Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")) + 1) || clean; }
function fileSize(bytes: number) { if (!Number.isFinite(bytes) || bytes < 0) return "-"; if (bytes < 1024) return `${bytes} B`; const units = ["KB", "MB", "GB", "TB"]; let value = bytes / 1024; let index = 0; while (value >= 1024 && index < 3) { value /= 1024; index++; } return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`; }
function modifiedAt(seconds: number, lang: string) { return Number.isFinite(seconds) && seconds > 0 ? new Intl.DateTimeFormat(lang.startsWith("zh") ? "zh-CN" : "en-US", { dateStyle: "medium", timeStyle: "medium" }).format(new Date(seconds * 1000)) : "-"; }

export function workspaceFileIconKind(name: string): { badge: string; kind: "code" | "data" | "text" | "markup" | "file" } {
    const ext = name.toLowerCase().split(".").pop() || "";
    if (["ts", "tsx", "js", "jsx"].includes(ext)) return { badge: ext.startsWith("t") ? "TS" : "JS", kind: "code" };
    if (["go", "rs", "py", "java", "kt", "cs", "cpp", "c", "h", "hpp", "swift", "rb", "php"].includes(ext)) return { badge: ext === "cpp" ? "C+" : ext.slice(0, 2).toUpperCase(), kind: "code" };
    if (["json", "yaml", "yml", "toml", "xml", "ini", "env"].includes(ext)) return { badge: "{}", kind: "data" };
    if (["md", "txt", "rst", "log"].includes(ext)) return { badge: "TXT", kind: "text" };
    if (["html", "css", "scss", "svg"].includes(ext)) return { badge: "<>", kind: "markup" };
    return { badge: "FILE", kind: "file" };
}

function isVSCodeSourceFile(entry: DirectoryEntry) {
    if (entry.is_dir) return false;
    const kind = workspaceFileIconKind(entry.name).kind;
    return kind === "code" || kind === "markup";
}

function FileIcon({ entry, theme, open }: { entry: DirectoryEntry; theme: CodePreviewTheme; open: boolean }) {
    const icon = entry.is_dir ? { badge: "", kind: "data" as const } : workspaceFileIconKind(entry.name);
    const color = open ? theme.tabActiveText : icon.kind === "code" ? theme.syntaxKeyword : icon.kind === "text" ? theme.syntaxString : theme.syntaxNumber;
    return <svg aria-hidden="true" viewBox="0 0 20 20" width="16" height="16" style={{ flexShrink: 0 }}><path d={entry.is_dir ? "M2.5 5.5c0-1.1.9-2 2-2h3l1.4 1.7h6.6c1.1 0 2 .9 2 2v6.3c0 1.1-.9 2-2 2h-11c-1.1 0-2-.9-2-2V5.5Z" : "M4.25 2.5h7l4.5 4.5v9.25c0 .69-.56 1.25-1.25 1.25H5.5c-.69 0-1.25-.56-1.25-1.25V3.75c0-.69.56-1.25 1.25-1.25Z"} fill={color} opacity=".22" stroke={color} strokeWidth="1.2" /><text x="10" y="14" textAnchor="middle" fill={color} fontSize="5.5" fontWeight="700">{icon.badge}</text></svg>;
}

export function CodePreviewWorkspace({ projectPath, lang, theme, onOpenFile }: { projectPath?: string; lang: string; theme: CodePreviewTheme; onOpenFile: (file: CodeFile) => void }) {
    const [directories, setDirectories] = useState<Map<string, DirectoryResponse>>(new Map());
    const [expanded, setExpanded] = useState<Set<string>>(new Set([""]));
    const [loading, setLoading] = useState<Set<string>>(new Set());
    const [errors, setErrors] = useState<Map<string, string>>(new Map());
    const [root, setRoot] = useState("");
    const [notice, setNotice] = useState("");
    const [menu, setMenu] = useState<ContextMenu | null>(null);
    const [propertiesEntry, setPropertiesEntry] = useState<DirectoryEntry | null>(null);
    const [properties, setProperties] = useState<EntryProperties | null>(null);
    const [propertiesError, setPropertiesError] = useState("");
    const [vscodeAvailable, setVSCodeAvailable] = useState(false);
    const pagesRef = useRef(new Map<string, DirectoryResponse>());
    const refreshedRef = useRef(new Map<string, number>());
    const versionRef = useRef(0); const previewRef = useRef(0); const propertyRef = useRef(0); const vscodeCheckRef = useRef(0); const inFlightRef = useRef(new Map<string, number>());

    const load = useCallback(async (path: string, force = false) => {
        if (!projectPath) return;
        const at = refreshedRef.current.get(path) || 0;
        if (!force && pagesRef.current.has(path) && Date.now() - at < cacheTtl) return;
        const version = versionRef.current;
        if (inFlightRef.current.get(path) === version) return;
        inFlightRef.current.set(path, version); setLoading(prev => new Set(prev).add(path)); setErrors(prev => { const next = new Map(prev); next.delete(path); return next; });
        try {
            const data = await GetCodingWorkbenchDirectory(projectPath, path) as DirectoryResponse;
            if (version !== versionRef.current) return;
            const next = new Map(pagesRef.current); next.delete(path); next.set(path, data || {}); pagesRef.current = boundedPages(next);
            refreshedRef.current.set(path, Date.now()); setDirectories(pagesRef.current);
            const nextRoot = path ? root : String(data?.root || ""); if (!path) setRoot(nextRoot);
            saveCache(projectPath, pagesRef.current, nextRoot, refreshedRef.current);
        } catch (error) { if (version === versionRef.current) setErrors(prev => new Map(prev).set(path, error instanceof Error ? error.message : String(error))); }
        finally { if (inFlightRef.current.get(path) === version) inFlightRef.current.delete(path); if (version === versionRef.current) setLoading(prev => { const next = new Set(prev); next.delete(path); return next; }); }
    }, [projectPath, root]);

    useEffect(() => { const saved = projectPath ? takeCache(projectPath) : undefined; const pages = saved ? boundedPages(saved.directories) : new Map<string, DirectoryResponse>(); pagesRef.current = pages; refreshedRef.current = saved ? new Map(saved.refreshedAt) : new Map(); versionRef.current++; previewRef.current++; propertyRef.current++; setDirectories(pages); setRoot(saved?.root || ""); setExpanded(new Set([""])); setErrors(new Map()); setMenu(null); setPropertiesEntry(null); setProperties(null); if (projectPath) void load("", true); }, [projectPath]); // eslint-disable-line react-hooks/exhaustive-deps
    const refreshVSCodeAvailability = useCallback(() => {
        const request = ++vscodeCheckRef.current;
        void Promise.resolve()
            .then(() => IsCodingWorkbenchVSCodeAvailable())
            .then(value => { if (request === vscodeCheckRef.current) setVSCodeAvailable(value === true); })
            .catch(() => { if (request === vscodeCheckRef.current) setVSCodeAvailable(false); });
    }, []);
    useEffect(() => { refreshVSCodeAvailability(); }, [refreshVSCodeAvailability]);
    useEffect(() => {
        if (!menu) return;
        const close = (event: MouseEvent) => {
            if (event.target instanceof Element && event.target.closest('[data-testid="code-preview-workspace-context-menu"]')) return;
            setMenu(null);
        };
        window.addEventListener("mousedown", close);
        return () => window.removeEventListener("mousedown", close);
    }, [menu]);

    const toggle = useCallback((entry: DirectoryEntry) => { const willOpen = !expanded.has(entry.path); setExpanded(prev => { const next = new Set(prev); willOpen ? next.add(entry.path) : next.delete(entry.path); return next; }); if (willOpen) void load(entry.path); }, [expanded, load]);
    const openFile = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++previewRef.current; setNotice(""); try { const data = await GetCodingWorkbenchFilePreview(projectPath, entry.path) as FilePreviewResponse; if (version !== versionRef.current || request !== previewRef.current) return; onOpenFile({ filePath: String(data?.path || entry.path), fileName: basename(String(data?.path || entry.path)), absPath: data?.abs_path || undefined, content: String(data?.content || ""), language: String(data?.language || "plaintext"), opType: "read", updatedAt: Date.now(), previewTruncated: data?.truncated === true }); } catch (error) { if (version === versionRef.current && request === previewRef.current) setNotice(error instanceof Error ? error.message : String(error)); } }, [onOpenFile, projectPath]);
    const openProperties = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++propertyRef.current; setPropertiesEntry(entry); setProperties(null); setPropertiesError(""); try { const data = await GetCodingWorkbenchEntryProperties(projectPath, entry.path) as EntryProperties; if (version === versionRef.current && request === propertyRef.current) setProperties(data || {}); } catch (error) { if (version === versionRef.current && request === propertyRef.current) setPropertiesError(error instanceof Error ? error.message : String(error)); } }, [projectPath]);
    const openVSCode = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; try { await OpenCodingWorkbenchFileInVSCode(projectPath, entry.path); } catch (error) { setNotice(error instanceof Error ? error.message : String(error)); } }, [projectPath]);
    const render = (path: string, depth: number): React.ReactNode => pagesRef.current.get(path)?.entries?.map(entry => <React.Fragment key={entry.path}><button type="button" data-testid={entry.is_dir ? "code-preview-workspace-directory" : "code-preview-workspace-file"} onClick={() => entry.is_dir ? toggle(entry) : void openFile(entry)} onContextMenu={event => { event.preventDefault(); if (isVSCodeSourceFile(entry)) refreshVSCodeAvailability(); setMenu({ entry, x: event.clientX, y: event.clientY }); }} aria-expanded={entry.is_dir ? expanded.has(entry.path) : undefined} title={entry.path} style={{ display: "flex", width: "100%", border: 0, padding: `4px 10px 4px ${12 + depth * 16}px`, gap: 6, background: "transparent", color: theme.text, textAlign: "left", cursor: "pointer", font: "inherit", fontSize: 12 }}><span style={{ width: 12 }}>{entry.is_dir ? (expanded.has(entry.path) ? "v" : ">") : ""}</span><FileIcon entry={entry} theme={theme} open={expanded.has(entry.path)} /><span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{entry.name}</span></button>{entry.is_dir && expanded.has(entry.path) ? <>{loading.has(entry.path) && !pagesRef.current.has(entry.path) ? <div style={{ paddingLeft: 32 + depth * 16, color: theme.textMuted }}>Loading...</div> : render(entry.path, depth + 1)}{pagesRef.current.get(entry.path)?.truncated ? <div style={{ paddingLeft: 32 + depth * 16, color: theme.textMuted, fontSize: 11 }}>Showing the first 500 items.</div> : null}</> : null}</React.Fragment>);
    if (!projectPath) return <div data-testid="code-preview-workspace-status">{label(lang, "Working directory unavailable", "Working directory unavailable")}</div>;
    const rootError = errors.get("");
    if (rootError && !pagesRef.current.has("")) return <div data-testid="code-preview-workspace-status" role="status">{rootError}</div>;
    return <div data-testid="code-preview-workspace" style={{ display: "flex", flexDirection: "column", height: "100%", background: theme.bg }}><div style={{ display: "flex", gap: 8, padding: "8px 12px", borderBottom: `1px solid ${theme.border}`, color: theme.textMuted, fontSize: 11 }}><strong style={{ color: theme.tabActiveText }}>WORKING DIRECTORY</strong><span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{root || "Loading directory..."}</span><button type="button" aria-label="Refresh working directory" onClick={() => void load("", true)} style={{ marginLeft: "auto", border: 0, background: "transparent", color: theme.textMuted }}>Refresh</button></div><div style={{ flex: 1, overflow: "auto", padding: "6px 0" }}>{rootError ? <div role="status">{rootError}</div> : null}{notice ? <div role="status">{notice}</div> : null}{propertiesEntry ? <Properties entry={propertiesEntry} properties={properties} error={propertiesError} lang={lang} onClose={() => { propertyRef.current++; setPropertiesEntry(null); }} /> : null}{loading.has("") && !pagesRef.current.has("") ? <div>Loading working directory...</div> : null}{pagesRef.current.has("") && !pagesRef.current.get("")?.entries?.length ? <div>This working directory is empty.</div> : null}{render("", 0)}</div>{menu ? <Menu menu={menu} theme={theme} lang={lang} showVSCode={vscodeAvailable && isVSCodeSourceFile(menu.entry)} onPreview={() => { setMenu(null); menu.entry.is_dir ? void load(menu.entry.path, true) : void openFile(menu.entry); }} onVSCode={() => { setMenu(null); void openVSCode(menu.entry); }} onProperties={() => { setMenu(null); void openProperties(menu.entry); }} /> : null}</div>;
}

function Menu({ menu, theme, lang, showVSCode, onPreview, onVSCode, onProperties }: { menu: ContextMenu; theme: CodePreviewTheme; lang: string; showVSCode: boolean; onPreview: () => void; onVSCode: () => void; onProperties: () => void }) { const style: React.CSSProperties = { display: "block", width: "100%", border: 0, background: "transparent", color: theme.text, padding: "7px 12px", textAlign: "left", cursor: "pointer" }; return <div role="menu" data-testid="code-preview-workspace-context-menu" style={{ position: "fixed", zIndex: 20, left: Math.max(8, menu.x), top: Math.max(8, menu.y), width: 176, padding: 4, border: `1px solid ${theme.border}`, borderRadius: 6, background: theme.tabBg }}><button type="button" role="menuitem" data-testid="code-preview-workspace-context-preview" style={style} onClick={onPreview}>{menu.entry.is_dir ? label(lang, "Preview folder", "Preview folder") : label(lang, "Preview", "Preview")}</button>{showVSCode ? <button type="button" role="menuitem" data-testid="code-preview-workspace-context-open-vscode" style={style} onClick={onVSCode}>{label(lang, "Open with VS Code", "\u4f7f\u7528 VS Code \u6253\u5f00")}</button> : null}<button type="button" role="menuitem" data-testid="code-preview-workspace-context-properties" style={style} onClick={onProperties}>{label(lang, "Properties", "Properties")}</button></div>; }

function Properties({ entry, properties, error, lang, onClose }: { entry: DirectoryEntry; properties: EntryProperties | null; error: string; lang: string; onClose: () => void }) { const data = properties || {}; const folder = data.is_dir ?? entry.is_dir; return <div data-testid="code-preview-workspace-properties" role="status"><strong>{label(lang, "Properties", "Properties")}</strong><button type="button" aria-label="Close properties" onClick={onClose}>x</button><div>Name: {data.name || entry.name}</div><div>Type: {folder ? "Folder" : "File"}{data.extension ? ` (.${data.extension})` : ""}</div><div>Path: {data.path || entry.path || "."}</div><div>Full path: {data.abs_path || "-"}</div><div>Size: {data.size_known ? fileSize(Number(data.size || 0)) : folder ? "Not calculated" : "-"}</div><div>Modified: {modifiedAt(Number(data.modified_at || 0), lang)}</div><div>Permissions: {data.mode || "-"}</div>{!properties && !error ? <div>Loading properties...</div> : null}{error ? <div>{error}</div> : null}</div>; }

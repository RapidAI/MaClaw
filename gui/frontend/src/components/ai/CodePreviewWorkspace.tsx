import React, { useCallback, useEffect, useRef, useState } from "react";
import { DeleteCodingWorkbenchEntry, DownloadCodingWorkbenchEntry, GetCodingWorkbenchDirectory, GetCodingWorkbenchEntryProperties, GetCodingWorkbenchFilePreview, IsCodingWorkbenchVSCodeAvailable, OpenCodingWorkbenchFileInVSCode, OpenCodingWorkbenchFileLocally } from "../../../wailsjs/go/main/App";
import { useDialog } from "../CustomDialog";
import { localizeText, normalizeLang } from "../../i18n/langSelect";
import type { CodePreviewTheme } from "./FileTabBar";
import type { CodeFile } from "./useCodePreviewState";
import { scrubCloudWorkspaceError } from "./codingTaskMode";

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
const localOpenCoalesceMs = 600;

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

const workbenchNoticeExact: Record<string, [string, string, string]> = {
    "binary files cannot be previewed": ["Binary files cannot be previewed.", "无法预览二进制文件。", "無法預覽二進位檔案。"],
    "path is a directory": ["This path is a folder and cannot be opened as a file.", "该路径是文件夹，无法作为文件预览。", "該路徑是資料夾，無法作為檔案預覽。"],
    "file path is required": ["A file path is required.", "需要文件路径。", "需要檔案路徑。"],
    "project path is required": ["A task path is required.", "需要任务路径。", "需要任務路徑。"],
    "path must be relative to the working directory": ["The path must stay inside the working directory.", "路径必须位于工作目录内。", "路徑必須位於工作目錄內。"],
    "path outside the working directory": ["The path is outside the working directory.", "路径超出工作目录。", "路徑超出工作目錄。"],
    "path resolves outside the working directory": ["The path is outside the working directory.", "路径超出工作目录。", "路徑超出工作目錄。"],
    "working directory is unavailable": ["The working directory is unavailable.", "工作目录不可用。", "工作目錄無法使用。"],
    "working directory is not a directory": ["The working directory is not a folder.", "工作目录不是文件夹。", "工作目錄不是資料夾。"],
    "path outside remote work_dir": ["The path is outside the remote working directory.", "路径超出远程工作目录。", "路徑超出遠端工作目錄。"],
    "AI assistant not initialized": ["The AI assistant is not initialized.", "AI 助手尚未初始化。", "AI 助手尚未初始化。"],
    "download is not available for remote workspaces": ["Download is not available for remote workspaces.", "远程工作区不支持此下载方式。", "遠端工作區不支援此下載方式。"],
    "open locally is not available for remote workspaces": ["Open locally is not available for remote workspaces.", "远程工作区不支持在本地打开。", "遠端工作區不支援在本機開啟。"],
    "entry cannot be opened locally": ["This item cannot be opened locally.", "无法在本地打开该项。", "無法在本機開啟該項目。"],
    "entry cannot be downloaded": ["This item cannot be downloaded.", "无法下载该项。", "無法下載該項目。"],
    "cannot save the archive inside the folder being downloaded": ["Cannot save the archive inside the folder being downloaded.", "不能将压缩包保存到正在下载的文件夹内。", "不能將壓縮包儲存到正在下載的資料夾內。"],
    "file exceeds download size limit": ["The file exceeds the download size limit.", "文件超过下载大小限制。", "檔案超過下載大小限制。"],
    "archive exceeds file count limit": ["The archive exceeds the file-count limit.", "压缩包内文件数量超过限制。", "壓縮包內檔案數量超過限制。"],
    "archive exceeds download size limit": ["The archive exceeds the download size limit.", "压缩包超过下载大小限制。", "壓縮包超過下載大小限制。"],
    "remote file path is unavailable": ["The remote file path is unavailable.", "远程文件路径不可用。", "遠端檔案路徑無法使用。"],
    "remote file changed while downloading; please try again": ["The remote file changed while downloading. Please try again.", "下载过程中远程文件已变化，请重试。", "下載過程中遠端檔案已變化，請重試。"],
    "local cache path is invalid": ["The local cache path is invalid.", "本地缓存路径无效。", "本機快取路徑無效。"],
    "remote properties response invalid": ["Remote file properties are unavailable.", "无法读取远程文件属性。", "無法讀取遠端檔案屬性。"],
    "remote SSH command failed": ["The remote SSH command failed.", "远程 SSH 命令失败。", "遠端 SSH 命令失敗。"],
    "delete is only available for cloud workspaces": ["Delete is only available for cloud workspaces.", "仅云端工作区支持删除文件。", "僅雲端工作區支援刪除檔案。"],
    "cloud workspace is read-only": ["The cloud workspace is read-only.", "云端工作区当前为只读。", "雲端工作區目前為唯讀。"],
    "entry cannot be deleted": ["This item cannot be deleted.", "无法删除该项。", "無法刪除該項目。"],
    "delete is not available for remote workspaces": ["Delete is not available for remote workspaces.", "远程工作区不支持删除。", "遠端工作區不支援刪除。"],
};

const workbenchNoticePrefixes: Array<{ prefix: string; en: string; zhHans: string; zhHant: string }> = [
    { prefix: "vs code is not available", en: "VS Code is not available.", zhHans: "未安装或无法使用 VS Code。", zhHant: "未安裝或無法使用 VS Code。" },
    { prefix: "create local vs code cache:", en: "Could not create a local VS Code cache.", zhHans: "无法创建本地 VS Code 缓存。", zhHant: "無法建立本機 VS Code 快取。" },
    { prefix: "create local vs code snapshot:", en: "Could not create a local VS Code snapshot.", zhHans: "无法创建本地 VS Code 快照。", zhHant: "無法建立本機 VS Code 快照。" },
    { prefix: "create local vs code download:", en: "Could not prepare a local VS Code download.", zhHans: "无法准备 VS Code 本地下载。", zhHant: "無法準備 VS Code 本機下載。" },
    { prefix: "prepare local vs code download:", en: "Could not prepare a local VS Code download.", zhHans: "无法准备 VS Code 本地下载。", zhHant: "無法準備 VS Code 本機下載。" },
    { prefix: "download remote file for vs code:", en: "Could not download the remote file for VS Code.", zhHans: "无法为 VS Code 下载远程文件。", zhHant: "無法為 VS Code 下載遠端檔案。" },
    { prefix: "verify local vs code download:", en: "Could not verify the local VS Code download.", zhHans: "无法校验 VS Code 本地下载。", zhHant: "無法校驗 VS Code 本機下載。" },
    { prefix: "finalize local vs code download:", en: "Could not finish the local VS Code download.", zhHans: "无法完成 VS Code 本地下载。", zhHant: "無法完成 VS Code 本機下載。" },
    { prefix: "local file deleted, but remote delete failed:", en: "The local cache copy was deleted, but the remote file could not be removed.", zhHans: "本地缓存已删除，但云端文件删除失败。", zhHant: "本機快取已刪除，但雲端檔案刪除失敗。" },
];

function workbenchErrorText(error: unknown): string {
    if (typeof error === "string") return error.trim();
    if (error instanceof Error) return String(error.message || "").trim();
    if (error && typeof error === "object") {
        const rec = error as Record<string, unknown>;
        for (const key of ["message", "Message", "error", "Error"]) {
            if (typeof rec[key] === "string" && rec[key].trim()) return rec[key].trim();
        }
    }
    const text = String(error || "").trim();
    return text === "[object Object]" ? "" : text;
}

function normalizeWorkbenchNoticeKey(raw: string): string {
    return raw.replace(/^Error:\s*/i, "").replace(/[.。!！?？]+$/u, "").trim();
}

function formatExactWorkbenchNotice(key: string, lang: string, cloudMode: boolean): string {
    const parts = workbenchNoticeExact[key];
    if (!parts) return "";
    const text = label(lang, parts[0], parts[1], parts[2]);
    if (!cloudMode || key !== "binary files cannot be previewed") return text;
    return text + label(
        lang,
        " Double-click or use Open locally on the right-click menu.",
        "可双击或右键「在本地打开」。",
        "可按兩下或右鍵「在本機開啟」。",
    );
}

function localizeWorkbenchBrowserNotice(raw: string, lang: string, cloudMode: boolean): string {
    const text = normalizeWorkbenchNoticeKey(raw);
    if (!text) return "";
    const lower = text.toLowerCase();
    if (workbenchNoticeExact[lower]) return formatExactWorkbenchNotice(lower, lang, cloudMode);
    const lastSeg = lower.split(/:\s*/).pop()?.trim() || "";
    if (lastSeg && workbenchNoticeExact[lastSeg]) return formatExactWorkbenchNotice(lastSeg, lang, cloudMode);
    for (const row of workbenchNoticePrefixes) {
        if (lower === row.prefix || lower.startsWith(row.prefix)) return label(lang, row.en, row.zhHans, row.zhHant);
    }
    const vsCodeOpenLimit = text.match(/remote file is too large to open locally with VS Code \(limit (\d+) MB\)/i);
    if (vsCodeOpenLimit) {
        const n = vsCodeOpenLimit[1];
        return label(lang, `The remote file is too large to open locally with VS Code (limit ${n} MB).`, `远程文件过大，无法用 VS Code 在本地打开（上限 ${n} MB）。`, `遠端檔案過大，無法用 VS Code 在本機開啟（上限 ${n} MB）。`);
    }
    const vsCodeDownloadLimit = text.match(/remote file exceeds the (\d+) MB VS Code download limit/i);
    if (vsCodeDownloadLimit) {
        const n = vsCodeDownloadLimit[1];
        return label(lang, `The remote file exceeds the ${n} MB VS Code download limit.`, `远程文件超过 ${n} MB 的 VS Code 下载上限。`, `遠端檔案超過 ${n} MB 的 VS Code 下載上限。`);
    }
    return text;
}

export function workspaceErrorMessage(error: unknown, cloudMode: boolean, lang: string): string {
    const localized = localizeWorkbenchBrowserNotice(workbenchErrorText(error), lang, cloudMode);
    const fallback = cloudMode
        ? label(lang, "Could not load cloud files.", "无法加载云端文件。", "無法載入雲端檔案。")
        : label(lang, "Could not load files.", "无法加载文件。", "無法載入檔案。");
    if (!localized) return fallback;
    if (!cloudMode) return localized;
    return scrubCloudWorkspaceError(localized, fallback);
}
function basename(value: string) { const clean = value.replace(/[\\/]+$/, ""); return clean.slice(Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")) + 1) || clean; }
function fileSize(bytes: number) { if (!Number.isFinite(bytes) || bytes < 0) return "-"; if (bytes < 1024) return `${bytes} B`; const units = ["KB", "MB", "GB", "TB"]; let value = bytes / 1024; let index = 0; while (value >= 1024 && index < 3) { value /= 1024; index++; } return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`; }
function modifiedAt(seconds: number, lang: string) { const normalized = normalizeLang(lang); const locale = normalized === "zh-Hant" ? "zh-TW" : normalized === "zh-Hans" ? "zh-CN" : "en-US"; return Number.isFinite(seconds) && seconds > 0 ? new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "medium" }).format(new Date(seconds * 1000)) : "-"; }
function clampMenuPosition(x: number, y: number) { return { x: Math.max(8, Math.min(x, window.innerWidth - 204)), y: Math.max(8, Math.min(y, window.innerHeight - 220)) }; }
function parentEntryPath(path: string) { const clean = path.replace(/[\\/]+$/, ""); const index = Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")); return index <= 0 ? "" : clean.slice(0, index); }
function entryPathContainedBy(path: string, ancestor: string) { if (!ancestor) return path === ancestor; return path === ancestor || path.startsWith(`${ancestor}/`) || path.startsWith(`${ancestor}\\`); }

export function workspaceFileIconKind(name: string): { badge: string; kind: "code" | "data" | "text" | "markup" | "file" } {
    const ext = name.toLowerCase().split(".").pop() || "";
    if (["ts", "tsx", "js", "jsx"].includes(ext)) return { badge: ext.startsWith("t") ? "TS" : "JS", kind: "code" };
    if (["go", "rs", "py", "java", "kt", "cs", "cpp", "c", "h", "hpp", "swift", "rb", "php"].includes(ext)) return { badge: ext === "cpp" ? "C+" : ext.slice(0, 2).toUpperCase(), kind: "code" };
    if (["json", "yaml", "yml", "toml", "xml", "ini", "env"].includes(ext)) return { badge: "{}", kind: "data" };
    if (["md", "txt", "rst", "log"].includes(ext)) return { badge: "TXT", kind: "text" };
    if (["html", "css", "scss", "svg"].includes(ext)) return { badge: "<>", kind: "markup" };
    if (ext === "pdf") return { badge: "PDF", kind: "file" };
    if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "tif", "tiff", "heic"].includes(ext)) return { badge: "IMG", kind: "file" };
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
const likelyBinaryExt = new Set(["pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "tif", "tiff", "heic", "zip", "7z", "rar", "tar", "gz", "tgz", "bz2", "mp3", "wav", "mp4", "mov", "avi", "mkv", "webm", "exe", "dll", "so", "dylib", "wasm", "bin", "iso", "dmg", "woff", "woff2", "ttf", "otf", "epub", "apk", "msi"]);
export function isLikelyBinaryName(name: string) {
    const dot = name.lastIndexOf(".");
    if (dot <= 0 || dot === name.length - 1) return false;
    return likelyBinaryExt.has(name.slice(dot + 1).toLowerCase());
}
function isBinaryPreviewError(error: unknown) {
    const text = normalizeWorkbenchNoticeKey(workbenchErrorText(error)).toLowerCase();
    if (text === "binary files cannot be previewed") return true;
    const lastSeg = text.split(/:\s*/).pop()?.trim() || "";
    return lastSeg === "binary files cannot be previewed";
}

function FileIcon({ entry, theme, open }: { entry: DirectoryEntry; theme: CodePreviewTheme; open: boolean }) {
    const icon = entry.is_dir ? { badge: "", kind: "data" as const } : workspaceFileIconKind(entry.name);
    const color = open ? theme.tabActiveText : icon.kind === "code" ? theme.syntaxKeyword : icon.kind === "text" ? theme.syntaxString : theme.syntaxNumber;
    return <svg aria-hidden="true" viewBox="0 0 20 20" width="16" height="16" style={{ flexShrink: 0 }}><path d={entry.is_dir ? "M2.5 5.5c0-1.1.9-2 2-2h3l1.4 1.7h6.6c1.1 0 2 .9 2 2v6.3c0 1.1-.9 2-2 2h-11c-1.1 0-2-.9-2-2V5.5Z" : "M4.25 2.5h7l4.5 4.5v9.25c0 .69-.56 1.25-1.25 1.25H5.5c-.69 0-1.25-.56-1.25-1.25V3.75c0-.69.56-1.25 1.25-1.25Z"} fill={color} opacity=".22" stroke={color} strokeWidth="1.2" /><text x="10" y="14" textAnchor="middle" fill={color} fontSize="5.5" fontWeight="700">{icon.badge}</text></svg>;
}

export function CodePreviewWorkspace({ projectPath, refreshToken = 0, resetOnRefresh = false, cloudMode = false, lang, theme, onOpenFile, onFileDeleted }: { projectPath?: string; refreshToken?: number; resetOnRefresh?: boolean; cloudMode?: boolean; lang: string; theme: CodePreviewTheme; onOpenFile: (file: CodeFile) => void; onFileDeleted?: (path: string) => void }) {
    const { showConfirm } = useDialog();
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
    const propertiesEntryRef = useRef(propertiesEntry);
    propertiesEntryRef.current = propertiesEntry;
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
    const downloadRef = useRef(0);
    const localOpenRef = useRef(0);
    const lastLocalOpenRef = useRef<{ path: string; at: number; inFlight: boolean } | null>(null);
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
            if (version === versionRef.current) setErrors(prev => new Map(prev).set(path, workspaceErrorMessage(error, cloudMode, lang)));
        } finally {
            if (inFlightRef.current.get(path) === version) inFlightRef.current.delete(path);
            if (version === versionRef.current) setLoading(prev => { const next = new Set(prev); next.delete(path); return next; });
        }
    }, [cloudMode, lang, projectPath]);

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
        downloadRef.current++;
        localOpenRef.current++;
        lastLocalOpenRef.current = null;
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
    useEffect(() => {
        if (cloudMode) {
            setVSCodeAvailable(false);
            return;
        }
        refreshVSCodeAvailability();
    }, [cloudMode, refreshVSCodeAvailability]);

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
    const openLocally = useCallback(async (entry: DirectoryEntry) => {
        if (!projectPath || !cloudMode || entry.is_dir) return;
        const now = Date.now();
        const recent = lastLocalOpenRef.current;
        if (recent && recent.path === entry.path && (recent.inFlight || now - recent.at < localOpenCoalesceMs)) return;
        lastLocalOpenRef.current = { path: entry.path, at: now, inFlight: true };
        const version = versionRef.current;
        const request = ++localOpenRef.current;
        previewRef.current++;
        setNotice("");
        setNoticeIsError(false);
        try {
            await OpenCodingWorkbenchFileLocally(projectPath, entry.path);
            if (request === localOpenRef.current && lastLocalOpenRef.current?.path === entry.path) {
                lastLocalOpenRef.current = { path: entry.path, at: Date.now(), inFlight: false };
            }
        } catch (error) {
            if (request === localOpenRef.current && lastLocalOpenRef.current?.path === entry.path) lastLocalOpenRef.current = null;
            if (version === versionRef.current && request === localOpenRef.current) {
                setNoticeIsError(true);
                setNotice(workspaceErrorMessage(error, cloudMode, lang));
            }
        }
    }, [cloudMode, lang, projectPath]);
    const openFile = useCallback(async (entry: DirectoryEntry) => {
        if (!projectPath) return;
        if (cloudMode && isLikelyBinaryName(entry.name)) {
            void openLocally(entry);
            return;
        }
        const version = versionRef.current;
        const request = ++previewRef.current;
        setNotice("");
        setNoticeIsError(false);
        try {
            const data = await GetCodingWorkbenchFilePreview(projectPath, entry.path) as FilePreviewResponse;
            if (version !== versionRef.current || request !== previewRef.current) return;
            onOpenFile({ filePath: String(data?.path || entry.path), fileName: basename(String(data?.path || entry.path)), absPath: cloudMode ? undefined : (data?.abs_path || undefined), content: String(data?.content || ""), language: String(data?.language || "plaintext"), opType: "read", updatedAt: Date.now(), previewTruncated: data?.truncated === true });
        } catch (error) {
            if (version !== versionRef.current || request !== previewRef.current) return;
            if (cloudMode && isBinaryPreviewError(error)) {
                void openLocally(entry);
                return;
            }
            setNoticeIsError(true);
            setNotice(workspaceErrorMessage(error, cloudMode, lang));
        }
    }, [cloudMode, lang, onOpenFile, openLocally, projectPath]);
    const openProperties = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++propertyRef.current; setPropertiesEntry(entry); setProperties(null); setPropertiesError(""); setPropertiesLoading(true); try { const data = await GetCodingWorkbenchEntryProperties(projectPath, entry.path) as EntryProperties; if (version === versionRef.current && request === propertyRef.current) setProperties(data || {}); } catch (error) { if (version === versionRef.current && request === propertyRef.current) setPropertiesError(workspaceErrorMessage(error, cloudMode, lang)); } finally { if (version === versionRef.current && request === propertyRef.current) setPropertiesLoading(false); } }, [cloudMode, lang, projectPath]);
    const openVSCode = useCallback(async (entry: DirectoryEntry) => { if (!projectPath) return; const version = versionRef.current; const request = ++vscodeOpenRef.current; setNotice(""); setNoticeIsError(false); try { const localRemoteCopy = await OpenCodingWorkbenchFileInVSCode(projectPath, entry.path); if (version === versionRef.current && request === vscodeOpenRef.current && localRemoteCopy) setNotice(label(lang, "Remote file downloaded to a local temporary copy and opened in VS Code. Changes there do not sync back automatically.", "远程文件已下载到本地临时副本，并在 VS Code 中打开；其中的修改不会自动同步回远程。", "遠端檔案已下載至本機暫存副本，並在 VS Code 中開啟；其中的修改不會自動同步回遠端。")); } catch (error) { if (version === versionRef.current && request === vscodeOpenRef.current) { setNoticeIsError(true); setNotice(workspaceErrorMessage(error, cloudMode, lang)); } } }, [cloudMode, lang, projectPath]);
    const downloadEntry = useCallback(async (entry: DirectoryEntry) => {
        if (!projectPath || !cloudMode) return;
        const request = ++downloadRef.current;
        setNotice("");
        setNoticeIsError(false);
        try {
            const dest = String(await DownloadCodingWorkbenchEntry(projectPath, entry.path) || "").trim();
            if (request !== downloadRef.current) return;
            if (!dest) return;
            setNotice(label(lang, `Saved to ${dest}`, `已保存到 ${dest}`, `已儲存到 ${dest}`));
        } catch (error) {
            if (request === downloadRef.current) {
                setNoticeIsError(true);
                setNotice(workspaceErrorMessage(error, cloudMode, lang));
            }
        }
    }, [cloudMode, lang, projectPath]);
    const forgetDeletedEntry = useCallback((entry: DirectoryEntry, taskPath: string) => {
        const parent = parentEntryPath(entry.path);
        const next = new Map(pagesRef.current);
        const page = next.get(parent);
        if (page?.entries) next.set(parent, { ...page, entries: page.entries.filter(item => item.path !== entry.path && !entryPathContainedBy(item.path, entry.path)) });
        if (entry.is_dir) {
            for (const key of [...next.keys()]) {
                if (entryPathContainedBy(key, entry.path)) next.delete(key);
            }
        }
        pagesRef.current = next;
        setDirectories(next);
        setErrors(prev => {
            let changed = false;
            const errorsNext = new Map(prev);
            for (const key of errorsNext.keys()) {
                if (entryPathContainedBy(key, entry.path)) {
                    errorsNext.delete(key);
                    changed = true;
                }
            }
            return changed ? errorsNext : prev;
        });
        setLoading(prev => {
            let changed = false;
            const loadingNext = new Set(prev);
            for (const key of loadingNext) {
                if (entryPathContainedBy(key, entry.path)) {
                    loadingNext.delete(key);
                    changed = true;
                }
            }
            return changed ? loadingNext : prev;
        });
        if (entry.is_dir) {
            setExpanded(prev => {
                const expandedNext = new Set(prev);
                for (const key of expandedNext) {
                    if (entryPathContainedBy(key, entry.path)) expandedNext.delete(key);
                }
                return expandedNext;
            });
        }
        if (taskPath) saveCache(taskPath, next, rootRef.current, refreshedRef.current);
        const currentProperties = propertiesEntryRef.current;
        if (currentProperties && entryPathContainedBy(currentProperties.path, entry.path)) {
            propertyRef.current++;
            setPropertiesEntry(null);
            setProperties(null);
            setPropertiesLoading(false);
            setPropertiesError("");
        }
    }, []);
    const deleteEntry = useCallback(async (entry: DirectoryEntry) => {
        if (!projectPath || !cloudMode) return;
        const taskPath = projectPath;
        const version = versionRef.current;
        const confirmed = await showConfirm(
            entry.is_dir
                ? label(lang, `Delete folder “${entry.name}” and all of its contents? This removes the local cache and the remote cloud files. This cannot be undone.`, `删除文件夹「${entry.name}」及其全部内容？将同时删除本地缓存和云端文件，此操作不可撤销。`, `刪除資料夾「${entry.name}」及其全部內容？將同時刪除本機快取和雲端檔案，此操作不可復原。`)
                : label(lang, `Delete “${entry.name}”? This removes the local cache copy and the remote cloud file. This cannot be undone.`, `删除「${entry.name}」？将同时删除本地缓存和云端文件，此操作不可撤销。`, `刪除「${entry.name}」？將同時刪除本機快取和雲端檔案，此操作不可復原。`),
            entry.is_dir ? label(lang, "Delete folder", "删除文件夹", "刪除資料夾") : label(lang, "Delete file", "删除文件", "刪除檔案"),
            { confirmText: label(lang, "Delete", "删除", "刪除"), cancelText: label(lang, "Cancel", "取消", "取消"), confirmVariant: "danger" },
        );
        if (!confirmed) return;
        if (version === versionRef.current) {
            setNotice("");
            setNoticeIsError(false);
            forgetDeletedEntry(entry, taskPath);
        }
        try {
            await DeleteCodingWorkbenchEntry(taskPath, entry.path);
            if (version !== versionRef.current) return;
            forgetDeletedEntry(entry, taskPath);
            onFileDeleted?.(entry.path);
        } catch (error) {
            if (version !== versionRef.current) return;
            setNoticeIsError(true);
            setNotice(workspaceErrorMessage(error, cloudMode, lang));
            void load(parentEntryPath(entry.path), true);
        }
    }, [cloudMode, forgetDeletedEntry, lang, load, onFileDeleted, projectPath, showConfirm]);

    const handleEntryDoubleClick = useCallback((entry: DirectoryEntry) => {
        if (entry.is_dir || !cloudMode) return;
        void openLocally(entry);
    }, [cloudMode, openLocally]);

    const renderEntries = (path: string, depth: number, ancestors = new Set<string>([path])): React.ReactNode => pagesRef.current.get(path)?.entries?.map(entry => <React.Fragment key={entry.path}><button type="button" data-testid={entry.is_dir ? "code-preview-workspace-directory" : "code-preview-workspace-file"} onClick={() => entry.is_dir ? toggle(entry) : void openFile(entry)} onDoubleClick={event => { event.preventDefault(); handleEntryDoubleClick(entry); }} onContextMenu={event => { event.preventDefault(); if (isVSCodeSourceFile(entry)) refreshVSCodeAvailability(); openMenu(entry, event.clientX, event.clientY, event.currentTarget); }} onKeyDown={event => { if (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10")) return; event.preventDefault(); if (isVSCodeSourceFile(entry)) refreshVSCodeAvailability(); const bounds = event.currentTarget.getBoundingClientRect(); openMenu(entry, bounds.left + Math.min(bounds.width / 2, 40), bounds.top + Math.min(bounds.height, 36), event.currentTarget); }} aria-expanded={entry.is_dir ? expanded.has(entry.path) : undefined} aria-haspopup="menu" title={cloudMode && !entry.is_dir ? `${entry.path}\n${isLikelyBinaryName(entry.name) ? label(lang, "Click to open locally", "单击在本地打开", "按一下在本機開啟") : label(lang, "Double-click to open locally", "双击在本地打开", "按兩下在本機開啟")}` : entry.path} style={{ display: "flex", width: "100%", border: 0, padding: `4px 10px 4px ${12 + depth * 16}px`, gap: 6, background: "transparent", color: theme.text, textAlign: "left", cursor: "pointer", font: "inherit", fontSize: 12 }}><span style={{ width: 12 }}>{entry.is_dir ? (expanded.has(entry.path) ? "v" : ">") : ""}</span><FileIcon entry={entry} theme={theme} open={expanded.has(entry.path)} /><span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{entry.name}</span></button>{entry.is_dir && !ancestors.has(entry.path) && expanded.has(entry.path) ? <>{errors.get(entry.path) ? <DirectoryLoadError message={errors.get(entry.path) || ""} depth={depth} lang={lang} theme={theme} onRetry={() => void load(entry.path, true)} /> : null}{loading.has(entry.path) && !pagesRef.current.has(entry.path) ? <DirectoryLoading depth={depth} lang={lang} theme={theme} /> : renderEntries(entry.path, depth + 1, new Set([...ancestors, entry.path]))}{pagesRef.current.get(entry.path)?.truncated ? <div style={{ paddingLeft: 32 + depth * 16, color: theme.textMuted, fontSize: 11 }}>{label(lang, "Showing the first 500 items.", "仅显示前 500 个项目。", "僅顯示前 500 個項目。")}</div> : null}</> : null}</React.Fragment>);

    if (!projectPath) return <div data-testid="code-preview-workspace-status">{cloudMode ? label(lang, "Cloud workspace unavailable", "云端工作区不可用", "雲端工作區無法使用") : label(lang, "Working directory unavailable", "工作目录不可用", "工作目錄無法使用")}</div>;
    const rootError = errors.get("");
    const rootLabel = cloudMode
        ? (rootResolved ? label(lang, "Cloud files", "云端文件", "雲端檔案") : label(lang, "Loading cloud files...", "正在加载云端文件...", "正在載入雲端檔案..."))
        : (root || (rootResolved ? label(lang, "Working directory", "工作目录", "工作目錄") : label(lang, "Resolving directory...", "正在定位目录...", "正在定位目錄...")));
    return <div data-testid="code-preview-workspace" data-cloud-mode={cloudMode ? "true" : undefined} style={{ display: "flex", flexDirection: "column", height: "100%", background: theme.bg }}><div style={{ display: "flex", gap: 8, padding: "8px 12px", borderBottom: `1px solid ${theme.border}`, color: theme.textMuted, fontSize: 11 }}><strong style={{ color: theme.tabActiveText }}>{cloudMode ? label(lang, "CLOUD WORKSPACE", "云端工作区", "雲端工作區") : label(lang, "WORKING DIRECTORY", "工作目录", "工作目錄")}</strong><span data-testid="code-preview-workspace-root-label" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", color: rootResolved ? theme.textMuted : theme.lineNumText, fontSize: rootResolved ? 11 : 10.5, fontWeight: 400 }}>{rootLabel}</span><button type="button" aria-label={cloudMode ? label(lang, "Refresh cloud files", "刷新云端文件", "重新整理雲端檔案") : label(lang, "Refresh working directory", "刷新工作目录", "重新整理工作目錄")} title={label(lang, "Refresh", "刷新", "重新整理")} aria-busy={loading.has("")} onClick={refreshRoot} style={{ marginLeft: "auto", border: 0, borderRadius: 4, padding: "2px 5px", background: "transparent", color: theme.tabActiveText, cursor: "pointer", font: "inherit" }}>{label(lang, "Refresh", "刷新", "重新整理")}</button></div><div style={{ flex: 1, overflow: "auto", padding: "6px 0" }}>{rootError ? <WorkspaceNotice message={rootError} isError lang={lang} theme={theme} onClose={() => setErrors(prev => { const next = new Map(prev); next.delete(""); return next; })} /> : null}{notice ? <WorkspaceNotice message={notice} isError={noticeIsError} lang={lang} theme={theme} onClose={() => { setNotice(""); setNoticeIsError(false); }} /> : null}{propertiesEntry ? <Properties entry={propertiesEntry} properties={properties} loading={propertiesLoading} error={propertiesError} lang={lang} theme={theme} hideAbsPath={cloudMode} onClose={() => { propertyRef.current++; setPropertiesEntry(null); setPropertiesLoading(false); }} /> : null}{loading.has("") && !pagesRef.current.has("") ? <DirectoryLoading root lang={lang} theme={theme} cloudMode={cloudMode} /> : null}{rootResolved && pagesRef.current.has("") && !pagesRef.current.get("")?.entries?.length ? <EmptyDirectory lang={lang} theme={theme} cloudMode={cloudMode} /> : null}{renderEntries("", 0)}</div>{menu ? <Menu menu={menu} theme={theme} lang={lang} showPreview={menu.entry.is_dir || !cloudMode || !isLikelyBinaryName(menu.entry.name)} showDownload={cloudMode} showOpenLocal={cloudMode && !menu.entry.is_dir} showVSCode={!cloudMode && vscodeAvailable && isVSCodeSourceFile(menu.entry)} actionRef={menuActionRef} onPreview={() => { closeMenu(); menu.entry.is_dir ? void load(menu.entry.path, true) : void openFile(menu.entry); }} onOpenLocal={() => { closeMenu(); void openLocally(menu.entry); }} onDownload={() => { closeMenu(); void downloadEntry(menu.entry); }} onVSCode={() => { closeMenu(); void openVSCode(menu.entry); }} onProperties={() => { closeMenu(); void openProperties(menu.entry); }} showDelete={cloudMode} onDelete={() => { closeMenu(); void deleteEntry(menu.entry); }} /> : null}</div>;
}

function Menu({ menu, theme, lang, showPreview = true, showDownload = false, showOpenLocal = false, showVSCode, showDelete = false, actionRef, onPreview, onOpenLocal, onDownload, onVSCode, onProperties, onDelete }: { menu: ContextMenu; theme: CodePreviewTheme; lang: string; showPreview?: boolean; showDownload?: boolean; showOpenLocal?: boolean; showVSCode: boolean; showDelete?: boolean; actionRef: React.RefObject<HTMLButtonElement>; onPreview: () => void; onOpenLocal: () => void; onDownload: () => void; onVSCode: () => void; onProperties: () => void; onDelete: () => void }) { const style: React.CSSProperties = { display: "block", width: "100%", border: 0, background: "transparent", color: theme.text, padding: "7px 12px", textAlign: "left", cursor: "pointer" }; const openLocalRef = !showPreview && showOpenLocal ? actionRef : undefined; return <div role="menu" aria-label={menu.entry.name} data-testid="code-preview-workspace-context-menu" style={{ position: "fixed", zIndex: 20, left: menu.x, top: menu.y, width: "min(196px, calc(100vw - 16px))", padding: 4, border: `1px solid ${theme.border}`, borderRadius: 6, background: theme.tabBg }}>{showPreview ? <button ref={actionRef} type="button" role="menuitem" data-testid="code-preview-workspace-context-preview" style={style} onClick={onPreview}>{menu.entry.is_dir ? label(lang, "Preview folder", "预览文件夹", "預覽資料夾") : label(lang, "Preview", "预览", "預覽")}</button> : null}{showOpenLocal ? <button ref={openLocalRef} type="button" role="menuitem" data-testid="code-preview-workspace-context-open-local" style={style} onClick={onOpenLocal}>{label(lang, "Open locally", "在本地打开", "在本機開啟")}</button> : null}{showDownload ? <button type="button" role="menuitem" data-testid="code-preview-workspace-context-download" style={style} onClick={onDownload}>{label(lang, "Download", "下载", "下載")}</button> : null}{showVSCode ? <button type="button" role="menuitem" data-testid="code-preview-workspace-context-open-vscode" style={style} onClick={onVSCode}>{label(lang, "Open with VS Code", "使用 VS Code 打开", "使用 VS Code 開啟")}</button> : null}<button type="button" role="menuitem" data-testid="code-preview-workspace-context-properties" style={style} onClick={onProperties}>{label(lang, "Properties", "属性", "屬性")}</button>{showDelete ? <button type="button" role="menuitem" data-testid="code-preview-workspace-context-delete" style={{ ...style, color: theme.diffDeleteText, borderTop: `1px solid ${theme.border}`, marginTop: 2 }} onClick={onDelete}>{menu.entry.is_dir ? label(lang, "Delete folder", "删除文件夹", "刪除資料夾") : label(lang, "Delete", "删除", "刪除")}</button> : null}</div>; }

function WorkspaceNotice({ message, isError, lang, theme, onClose }: { message: string; isError: boolean; lang: string; theme: CodePreviewTheme; onClose: () => void }) {
    const foreground = isError ? theme.diffDeleteText : theme.tabActiveText;
    return <div data-testid="code-preview-workspace-notice" role={isError ? "alert" : "status"} style={{ display: "flex", alignItems: "flex-start", gap: 7, margin: "2px 10px 7px", padding: "6px 7px 6px 9px", border: `1px solid ${theme.border}`, borderRadius: 6, background: isError ? theme.diffDeleteBg : theme.tabBg, color: foreground, fontSize: isError ? 12 : 11, lineHeight: 1.4, fontWeight: 400 }}><span aria-hidden="true" style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", flex: "0 0 auto", width: 14, height: 14, marginTop: 1, border: `1px solid ${foreground}`, borderRadius: "50%", fontSize: 9, fontWeight: 700, lineHeight: 1 }}>{isError ? "!" : "i"}</span><span style={{ flex: 1, minWidth: 0, overflowWrap: "anywhere" }}>{message}</span><button type="button" aria-label={label(lang, "Dismiss message", "关闭提示", "關閉提示")} title={label(lang, "Close", "关闭", "關閉")} onClick={onClose} style={{ flex: "0 0 auto", width: 20, height: 20, marginTop: -3, border: 0, borderRadius: 4, padding: 0, background: "transparent", color: foreground, cursor: "pointer", font: "inherit", fontSize: 16, lineHeight: "20px" }}>×</button></div>;
}

function DirectoryLoadError({ message, depth, lang, theme, onRetry }: { message: string; depth: number; lang: string; theme: CodePreviewTheme; onRetry: () => void }) {
    return <div data-testid="code-preview-workspace-directory-error" role="alert" style={{ display: "flex", alignItems: "center", gap: 6, padding: `3px 10px 4px ${32 + depth * 16}px`, color: theme.diffDeleteText, background: theme.diffDeleteBg, fontSize: 11, lineHeight: 1.4 }}><span style={{ flex: 1, minWidth: 0, overflowWrap: "anywhere" }}>{message}</span><button type="button" onClick={onRetry} style={{ flex: "0 0 auto", border: `1px solid ${theme.border}`, borderRadius: 4, padding: "2px 6px", background: theme.tabBg, color: theme.tabActiveText, cursor: "pointer", font: "inherit" }}>{label(lang, "Retry", "重试", "重試")}</button></div>;
}

function DirectoryLoading({ root = false, depth = 0, lang, theme, cloudMode = false }: { root?: boolean; depth?: number; lang: string; theme: CodePreviewTheme; cloudMode?: boolean }) {
    return <div data-testid={root ? "code-preview-workspace-root-loading" : "code-preview-workspace-directory-loading"} role="status" style={{ display: "flex", alignItems: "center", gap: 6, padding: root ? "5px 12px 7px" : `3px 10px 4px ${32 + depth * 16}px`, color: theme.textMuted, fontSize: 10.5, lineHeight: 1.35, fontWeight: 400 }}><span aria-hidden="true" style={{ width: 4, height: 4, flex: "0 0 auto", borderRadius: "50%", background: theme.textMuted, opacity: 0.65 }} /><span>{root ? (cloudMode ? label(lang, "Loading cloud files...", "正在加载云端文件...", "正在載入雲端檔案...") : label(lang, "Loading working directory...", "正在加载工作目录...", "正在載入工作目錄...")) : label(lang, "Loading...", "正在加载...", "正在載入...")}</span></div>;
}

function EmptyDirectory({ lang, theme, cloudMode = false }: { lang: string; theme: CodePreviewTheme; cloudMode?: boolean }) {
    return <div data-testid="code-preview-workspace-empty" role="status" style={{ display: "flex", alignItems: "center", gap: 6, padding: "6px 12px 8px", color: theme.textMuted, fontSize: 10.5, lineHeight: 1.4, fontWeight: 400 }}><span aria-hidden="true" style={{ width: 4, height: 4, flex: "0 0 auto", borderRadius: "50%", background: theme.textMuted, opacity: 0.5 }} /><span>{cloudMode ? label(lang, "This cloud workspace is empty.", "此云端工作区为空。", "此雲端工作區是空的。") : label(lang, "This working directory is empty.", "此工作目录为空。", "此工作目錄是空的。")}</span></div>;
}

function Properties({ entry, properties, loading, error, lang, theme, hideAbsPath = false, onClose }: { entry: DirectoryEntry; properties: EntryProperties | null; loading: boolean; error: string; lang: string; theme: CodePreviewTheme; hideAbsPath?: boolean; onClose: () => void }) {
    const data = properties || {};
    const folder = data.is_dir ?? entry.is_dir;
    const rows = [[label(lang, "Name", "名称", "名稱"), data.name || entry.name], [label(lang, "Type", "类型", "類型"), `${folder ? label(lang, "Folder", "文件夹", "資料夾") : label(lang, "File", "文件", "檔案")}${data.extension ? ` (.${data.extension})` : ""}`], [label(lang, "Path", "路径", "路徑"), data.path || entry.path || "."], ...(hideAbsPath ? [] : [[label(lang, "Full path", "完整路径", "完整路徑"), data.abs_path || "-"]]), [label(lang, "Size", "大小", "大小"), data.size_known ? fileSize(Number(data.size || 0)) : folder ? label(lang, "Not calculated", "未计算", "未計算") : "-"], [label(lang, "Modified", "修改时间", "修改時間"), modifiedAt(Number(data.modified_at || 0), lang)], [label(lang, "Permissions", "权限", "權限"), data.mode || "-"]];
    return <section data-testid="code-preview-workspace-properties" role="region" aria-label={label(lang, "Properties", "属性", "屬性")} style={{ margin: "8px 10px 12px", border: `1px solid ${theme.border}`, borderRadius: 8, background: theme.tabBg, overflow: "hidden" }}><header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, padding: "8px 10px", borderBottom: `1px solid ${theme.border}`, color: theme.tabActiveText }}><strong style={{ fontSize: 13 }}>{label(lang, "Properties", "属性", "屬性")}</strong><button type="button" aria-label={label(lang, "Close properties", "关闭属性", "關閉屬性")} title={label(lang, "Close", "关闭", "關閉")} onClick={onClose} style={{ width: 28, height: 28, border: 0, borderRadius: 4, background: "transparent", color: theme.textMuted, cursor: "pointer", fontSize: 20, lineHeight: 1 }}>×</button></header><div style={{ display: "grid", gridTemplateColumns: "minmax(76px, max-content) minmax(0, 1fr)", gap: "6px 12px", padding: "10px", fontSize: 12, lineHeight: 1.45 }}>{rows.map(([name, value]) => <React.Fragment key={name}><span style={{ color: theme.textMuted }}>{`${name}: `}</span><span style={{ minWidth: 0, color: theme.text, overflowWrap: "anywhere" }}>{value}</span></React.Fragment>)}</div>{loading ? <div role="status" style={{ padding: "0 10px 10px", color: theme.textMuted, fontSize: 12 }}>{label(lang, "Loading properties...", "正在加载属性...", "正在載入屬性...")}</div> : null}{error ? <div role="status" style={{ padding: "0 10px 10px", color: theme.diffDeleteText, fontSize: 12, overflowWrap: "anywhere" }}>{error}</div> : null}</section>;
}

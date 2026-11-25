import React, { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { ExportLatexSourceBundle, OpenFileOrShowInFolder, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { TaskResultUploadButton, taskResultUploadSupported } from "./TaskResultUploadButton";
import { TaskResultExportButton } from "./TaskResultExportButton";
import { dispatchPreviewTaskResult, localizeTaskResultExportError, taskResultPreviewKindFromPath } from "./taskResultPreview";
import { cloudSafePathLabel } from "./codingTaskMode";
import { chromeText } from "../../i18n/chromeText";
import {
    cloudDriveSystemIconSettled,
    cloudDriveSystemIconURL,
    ensureCloudDriveSystemIcon,
    forgetCloudDriveSystemIcon,
    subscribeCloudDriveSystemIcon,
} from "../layout/cloudDriveSystemIcons";


const LATEX_SOURCE_EXT = new Set(["tex", "ltx", "latex", "bib", "cls", "sty", "bst"]);

function artifactExt(path: string): string {
    const base = path.split(/[/\\]/).filter(Boolean).pop() || path;
    const dot = base.lastIndexOf(".");
    if (dot <= 0) return "";
    return base.slice(dot + 1).toLowerCase();
}

/** Source files stay out of the chip list. They leave as one submission zip. */
export function isLatexSourceArtifactPath(path: string): boolean {
    return LATEX_SOURCE_EXT.has(artifactExt(path));
}

/** PDF sits next to the main file. A chapter .tex would zip only that subdirectory. */
export function latexSourceBundleAnchor(paths: string[]): string {
    const files = paths.map((path) => path.trim()).filter(Boolean);
    return files.find((path) => artifactExt(path) === "pdf")
        || files.find((path) => artifactExt(path) === "tex" || artifactExt(path) === "ltx" || artifactExt(path) === "latex")
        || files.find((path) => isLatexSourceArtifactPath(path))
        || "";
}

const VISIBLE_ARTIFACTS = 2;

function artifactIdentity(path: string): string {
    return path.replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
}

/** Short badge for the icon tile: the extension when it fits, else "" (glyph). */
function artifactBadge(path: string): string {
    const ext = artifactExt(path);
    if (!ext || ext.length > 4) return "";
    return ext.toUpperCase();
}

/**
 * The icon tile for one file. The desktop shell answers with the icon the
 * operating system associates with the suffix (the same association explorer
 * uses), so a PDF shows the PDF icon instead of a badge we draw. The lookup
 * only needs the suffix and answers "" for types no application claims.
 * Badge/glyph is the fallback while unresolved, after a definitive "absent",
 * and whenever the returned image cannot be decoded.
 */
export function ArtifactFileIcon({ path }: { path: string }) {
    const ext = artifactExt(path);
    const suffix = ext ? `.${ext}` : "";
    const badge = artifactBadge(path);
    const subscribe = useCallback(
        (onStoreChange: () => void) => subscribeCloudDriveSystemIcon(suffix)(onStoreChange),
        [suffix],
    );
    const iconURL = useSyncExternalStore(
        subscribe,
        () => cloudDriveSystemIconURL(suffix),
        () => cloudDriveSystemIconURL(suffix),
    );
    useEffect(() => {
        if (!suffix || cloudDriveSystemIconSettled(suffix)) return;
        void ensureCloudDriveSystemIcon(suffix);
    }, [suffix]);
    if (iconURL) {
        return (
            <span className="mc-task-artifact__icon mc-task-artifact__icon--os" aria-hidden="true">
                <img
                    className="mc-task-artifact__img"
                    src={iconURL}
                    alt=""
                    draggable={false}
                    onError={() => { forgetCloudDriveSystemIcon(suffix); }}
                />
            </span>
        );
    }
    return (
        <span className={`mc-task-artifact__icon${badge ? "" : " mc-task-artifact__icon--glyph"}`} aria-hidden="true">{badge || "▤"}</span>
    );
}

function artifactLabel(path: string, lang: string): { title: string; name: string } {
    const title = cloudSafePathLabel(path, (chromeText(lang, "File", "文件")));
    const name = title.split(/[/\\]/).filter(Boolean).pop() || title;
    return { title, name };
}

/** Compact deliverable chips shown once a task has a saved file. */
export function TaskResultArtifacts({
    paths,
    messageId,
    lang,
    onOpen,
    latexSourceBundle = false,
}: {
    paths: string[];
    messageId: string;
    lang: string;
    onOpen: (event: React.MouseEvent, filePath: string) => void;
    /** LaTeX paper: one zip of the sources next to the PDF, not a chip per .tex. */
    latexSourceBundle?: boolean;
}) {
    const [showAll, setShowAll] = useState(false);
    const files: string[] = [];
    const seen = new Set<string>();
    for (const path of paths) {
        const trimmed = path.trim();
        const key = artifactIdentity(trimmed);
        if (!key || seen.has(key)) continue;
        seen.add(key);
        files.push(trimmed);
    }
    const bundleAnchor = latexSourceBundleAnchor(files);
    const offerBundle = !!bundleAnchor && (latexSourceBundle || files.some(isLatexSourceArtifactPath));
    const listed = files.filter((path) => !isLatexSourceArtifactPath(path));
    listed.sort((left, right) => Number(!taskResultPreviewKindFromPath(left)) - Number(!taskResultPreviewKindFromPath(right)));
    if (!listed.length && !offerBundle) return null;
    const visible = showAll ? listed : listed.slice(0, VISIBLE_ARTIFACTS);
    return (
        <div className="mc-task-artifacts" data-testid={`task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__head">
                <span className="mc-task-artifacts__glyph" aria-hidden="true">◈</span>
                <span className="mc-task-artifacts__label">{chromeText(lang, "Artifacts", "产出物", "產出物")}</span>
                {/* Keep the badge consistent with the "view all (N)" row: source-only
                    bundles have no chips, so fall back to the total then. */}
                <span className="mc-task-artifacts__count">{listed.length || files.length}</span>
            </div>
            <div className="mc-task-artifacts__row">
                {visible.map((filePath, index) => {
                    const file = artifactLabel(filePath, lang);
                    // The first file keeps the original testids; later files get -N
                    // so existing single-file tests stay untouched.
                    const suffix = index > 0 ? `-${index}` : "";
                    return (
                        <div key={`${filePath}-${index}`} className="mc-task-artifact-item">
                            <div className="mc-task-artifact-item__head">
                                <a
                                    className="mc-task-artifact"
                                    href="#"
                                    title={file.title}
                                    onClick={(event) => onOpen(event, filePath)}
                                >
                                    <ArtifactFileIcon path={filePath} />
                                    <span className="mc-task-artifact__name">{file.name}</span>
                                    <span className="mc-task-artifact__open" aria-hidden="true">↗</span>
                                </a>
                            </div>
                            <div className="mc-task-result-card__actions mc-task-artifact-item__actions">
                                {/* "fill" on purpose: a global office-repaint repaints every
                                    button[class*='primary'] with --office-accent, which would
                                    fight this card's theme-token fill. */}
                                <button type="button" className="mc-artifact-btn--fill" data-testid={`task-result-preview-btn${suffix}`} onClick={(event) => { event.stopPropagation(); dispatchPreviewTaskResult(filePath, messageId); }}>{chromeText(lang, "Preview", "预览", "預覽")}</button>
                                <button type="button" data-testid={`task-result-view-btn${suffix}`} onClick={(event) => onOpen(event, filePath)}>{chromeText(lang, "View document", "查看文档", "查看文件")}</button>
                                <TaskResultExportButton filePath={filePath} lang={lang} testIdSuffix={suffix} />
                                <button type="button" data-testid={`task-result-reveal-btn${suffix}`} onClick={(event) => { event.stopPropagation(); void ShowItemInFolder(filePath).catch(() => {}); }}>{chromeText(lang, "Reveal", "定位", "定位")}</button>
                                {taskResultUploadSupported(filePath) && <TaskResultUploadButton filePath={filePath} lang={lang} />}
                            </div>
                        </div>
                    );
                })}
            </div>
            {listed.length > VISIBLE_ARTIFACTS && (
                <button
                    type="button"
                    className="mc-task-artifacts__more"
                    data-testid="task-artifacts-more"
                    onClick={() => setShowAll((open) => !open)}
                >
                    {showAll
                        ? chromeText(lang, "Show fewer artifacts", "收起产出物", "收起產出物")
                        : chromeText(lang, `View all artifacts (${listed.length})`, `查看所有产物 (${listed.length})`, `查看所有產物 (${listed.length})`)}
                    <span aria-hidden="true">{showAll ? " ⌃" : " ›"}</span>
                </button>
            )}
            {offerBundle ? (
                <div className="mc-task-result-card__actions">
                    <LatexSourceBundleButton anchorPath={bundleAnchor} lang={lang} />
                </div>
            ) : null}
        </div>
    );
}

type RemoteArtifactState = {
    status: "idle" | "busy" | "saved" | "error";
    localPath?: string;
    detail?: string;
};

// A fetched copy survives card remounts (tab switches re-render the
// transcript), so a remote artifact is not re-downloaded on every revisit.
// Insertion-ordered FIFO bound: eviction only loses the "already fetched"
// memory — the card re-downloads on the next click.
const remoteArtifactLocalCopies = new Map<string, string>();
const REMOTE_ARTIFACT_CACHE_MAX = 200;

// Auto-fetch fan-out bound: one hub op per artifact is already concurrent
// enough at a handful per turn; beyond that the queue drains as slots free.
const REMOTE_ARTIFACT_FETCH_PARALLEL = 3;

function remoteArtifactCacheKey(messageId: string, filePath: string): string {
    return `${messageId}|${artifactIdentity(filePath)}`;
}

function rememberRemoteArtifactCopy(cacheKey: string, savedPath: string) {
    remoteArtifactLocalCopies.set(cacheKey, savedPath);
    while (remoteArtifactLocalCopies.size > REMOTE_ARTIFACT_CACHE_MAX) {
        const oldest = remoteArtifactLocalCopies.keys().next().value;
        if (oldest === undefined) break;
        remoteArtifactLocalCopies.delete(oldest);
    }
}

// A missing binding throws synchronously — the saved file must not read as a
// failed fetch just because the shell opener is unavailable.
function openLocalArtifactPath(path: string) {
    try {
        void OpenFileOrShowInFolder(path).catch(() => {});
    } catch {
        // no-op: the saved path stays on the card either way
    }
}

/**
 * Remote-control transcript card: the listed files live on the controlled
 * machine (or the cloud instance). Opening one fetches the bytes through
 * onFetch and saves them into this computer's Downloads folder. Once the
 * local copy exists the card shows the same actions a local artifact has
 * (preview / view / export / reveal / cloud-drive upload). With autoFetch
 * the download starts as soon as the turn lists the file — without opening
 * it — so the local copy is ready by the time the user clicks.
 */
export function RemoteTaskArtifacts({
    paths,
    messageId,
    lang,
    onFetch,
    autoFetch = false,
    onExportWorkspace,
}: {
    paths: string[];
    messageId: string;
    lang: string;
    /** Fetches the file from the controlled machine; resolves to the saved local path. */
    onFetch: (filePath: string) => Promise<string>;
    /** Cloud artifacts download into Downloads as soon as they are listed. */
    autoFetch?: boolean;
    /**
     * Cloud task only: packs the whole workspace into "<name>.tar.gz" — the
     * card-head 导出, same archive the task list's 打包下载 entry saves.
     * Resolves to the saved path, or "" when the dialog was cancelled.
     */
    onExportWorkspace?: () => Promise<string>;
}) {
    const [showAll, setShowAll] = useState(false);
    const [states, setStates] = useState<Record<string, RemoteArtifactState>>(() => {
        const restored: Record<string, RemoteArtifactState> = {};
        for (const path of paths) {
            const trimmed = path.trim();
            if (!trimmed) continue;
            const saved = remoteArtifactLocalCopies.get(remoteArtifactCacheKey(messageId, trimmed));
            if (saved) restored[artifactIdentity(trimmed)] = { status: "saved", localPath: saved };
        }
        return restored;
    });
    // A click races the state update: track in-flight fetches so a double
    // click cannot issue the same remote request twice.
    const inflight = useRef(new Set<string>());
    // A click during a background (autoFetch) download still wants the file
    // opened when it lands — remember the gesture instead of swallowing it.
    // The value also replays a failed reveal after a self-heal refetch.
    const pendingOpen = useRef(new Map<string, "open" | "reveal">());
    const files: string[] = [];
    const seen = new Set<string>();
    for (const path of paths) {
        const trimmed = path.trim();
        const key = artifactIdentity(trimmed);
        if (!key || seen.has(key)) continue;
        seen.add(key);
        files.push(trimmed);
    }
    // Preview-capable results first, same order the local card uses.
    files.sort((left, right) => Number(!taskResultPreviewKindFromPath(left)) - Number(!taskResultPreviewKindFromPath(right)));
    const fetchFile = useCallback((filePath: string, openAfter: boolean) => {
        const key = artifactIdentity(filePath);
        if (inflight.current.has(key)) return;
        inflight.current.add(key);
        setStates(prev => ({ ...prev, [key]: { status: "busy" } }));
        void onFetch(filePath).then(localPath => {
            const saved = String(localPath || "").trim();
            if (!saved) {
                pendingOpen.current.delete(key);
                setStates(prev => ({ ...prev, [key]: { status: "error", detail: chromeText(lang, "The file could not be fetched", "文件取回失败") } }));
                return;
            }
            rememberRemoteArtifactCopy(remoteArtifactCacheKey(messageId, filePath), saved);
            setStates(prev => ({ ...prev, [key]: { status: "saved", localPath: saved } }));
            // Consume a remembered gesture either way — otherwise an
            // explicit fetch (openAfter short-circuits the delete) would
            // leave it armed to fire on a later background download.
            const remembered = pendingOpen.current.get(key);
            pendingOpen.current.delete(key);
            const land = openAfter ? "open" : remembered;
            if (land === "reveal") void ShowItemInFolder(saved).catch(() => {});
            else if (land) openLocalArtifactPath(saved);
        }).catch((err: unknown) => {
            const detail = err instanceof Error && err.message.trim()
                ? err.message
                : String(err || "").trim() || chromeText(lang, "The file could not be fetched", "文件取回失败");
            pendingOpen.current.delete(key);
            setStates(prev => ({ ...prev, [key]: { status: "error", detail } }));
        }).finally(() => {
            inflight.current.delete(key);
        });
    }, [onFetch, messageId, lang]);
    const filesKey = files.join("\n");
    useEffect(() => {
        if (!autoFetch) return;
        let active = 0;
        for (const filePath of filesKey.split("\n").filter(Boolean)) {
            const key = artifactIdentity(filePath);
            const state = states[key];
            if (state?.status === "busy") {
                active += 1;
                continue;
            }
            // Saved and error are settled — check them before inflight: the
            // ref keeps its key until .finally runs, which can land after the
            // saved state already rendered, so counting inflight first would
            // pin a freed slot (and nothing re-renders when .finally clears).
            if (state && state.status !== "idle") continue;
            // The just-started gap: inflight has the key but the busy state
            // has not committed yet — still occupies a slot.
            if (inflight.current.has(key)) {
                active += 1;
                continue;
            }
            if (active >= REMOTE_ARTIFACT_FETCH_PARALLEL) break;
            active += 1;
            fetchFile(filePath, false);
        }
    }, [autoFetch, filesKey, states, fetchFile]);
    if (!files.length) return null;
    const runOnSavedCopy = (filePath: string, localPath: string, action: (p: string) => Promise<unknown>, reland: "open" | "reveal") => {
        // The saved copy can disappear from Downloads between visits; a
        // failed action drops the stale cache entry and re-fetches instead
        // of failing silently, then replays the gesture on the fresh copy.
        const key = artifactIdentity(filePath);
        const cacheKey = remoteArtifactCacheKey(messageId, filePath);
        const refetch = () => {
            remoteArtifactLocalCopies.delete(cacheKey);
            pendingOpen.current.set(key, reland);
            fetchFile(filePath, false);
        };
        try {
            void action(localPath).catch(refetch);
        } catch {
            // no-op: a missing binding is not a missing file — keep the
            // saved card instead of downloading on every click.
        }
    };
    const openSaved = (filePath: string, localPath: string) => {
        runOnSavedCopy(filePath, localPath, p => OpenFileOrShowInFolder(p), "open");
    };
    const revealSaved = (filePath: string, localPath: string) => {
        runOnSavedCopy(filePath, localPath, p => ShowItemInFolder(p), "reveal");
    };
    const open = (filePath: string) => {
        const key = artifactIdentity(filePath);
        const state = states[key];
        if (state?.status === "saved" && state.localPath) {
            openSaved(filePath, state.localPath);
            return;
        }
        if (state?.status === "busy" || inflight.current.has(key)) {
            pendingOpen.current.set(key, "open");
            return;
        }
        fetchFile(filePath, true);
    };
    const visible = showAll ? files : files.slice(0, VISIBLE_ARTIFACTS);
    return (
        <div className="mc-task-artifacts" data-testid={`remote-task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__head">
                <span className="mc-task-artifacts__glyph" aria-hidden="true">◈</span>
                <span className="mc-task-artifacts__label">{chromeText(lang, "Artifacts", "产出物", "產出物")}</span>
                <span className="mc-task-artifacts__count">{files.length}</span>
                {onExportWorkspace && <WorkspaceExportButton lang={lang} onExport={onExportWorkspace} />}
            </div>
            <div className="mc-task-artifacts__row">
                {visible.map((filePath, index) => {
                    const file = artifactLabel(filePath, lang);
                    const state = states[artifactIdentity(filePath)];
                    const suffix = index > 0 ? `-${index}` : "";
                    const busy = state?.status === "busy";
                    const savedPath = state?.status === "saved" ? String(state.localPath || "") : "";
                    const label = busy
                        ? chromeText(lang, "Fetching...", "取回中...", "取回中...")
                        : state?.status === "error"
                            ? chromeText(lang, "Retry", "重试", "重試")
                            : chromeText(lang, "Fetch & open", "取回并打开", "取回並打開");
                    const title = state?.status === "error" && state.detail
                        ? state.detail
                        : savedPath
                            ? savedPath
                            : file.title;
                    return (
                        <div key={`${filePath}-${index}`} className="mc-task-artifact-item">
                            <div className="mc-task-artifact-item__head">
                                <a
                                    className="mc-task-artifact"
                                    href="#"
                                    title={title}
                                    data-testid={`remote-task-artifact${suffix}`}
                                    onClick={(event) => { event.preventDefault(); event.stopPropagation(); open(filePath); }}
                                >
                                    <ArtifactFileIcon path={filePath} />
                                    <span className="mc-task-artifact__name">{file.name}</span>
                                    <span className="mc-task-artifact__open" aria-hidden="true">{busy ? "…" : "↗"}</span>
                                </a>
                            </div>
                            {savedPath ? (
                                <div className="mc-task-result-card__actions mc-task-artifact-item__actions">
                                    <button type="button" className="mc-artifact-btn--fill" data-testid={`remote-task-preview-btn${suffix}`} onClick={(event) => { event.stopPropagation(); dispatchPreviewTaskResult(savedPath, messageId); }}>{chromeText(lang, "Preview", "预览", "預覽")}</button>
                                    <button type="button" data-testid={`remote-task-view-btn${suffix}`} onClick={(event) => { event.preventDefault(); event.stopPropagation(); openSaved(filePath, savedPath); }}>{chromeText(lang, "View document", "查看文档", "查看文件")}</button>
                                    <TaskResultExportButton filePath={savedPath} lang={lang} testIdSuffix={`-remote${suffix}`} />
                                    <button type="button" data-testid={`remote-task-reveal-btn${suffix}`} onClick={(event) => { event.stopPropagation(); revealSaved(filePath, savedPath); }}>{chromeText(lang, "Reveal", "定位", "定位")}</button>
                                    {taskResultUploadSupported(savedPath) && <TaskResultUploadButton filePath={savedPath} lang={lang} />}
                                </div>
                            ) : (
                                <div className="mc-task-result-card__actions mc-task-artifact-item__actions">
                                    <button
                                        type="button"
                                        data-testid={`remote-task-fetch-btn${suffix}`}
                                        title={title}
                                        disabled={busy}
                                        onClick={(event) => { event.preventDefault(); event.stopPropagation(); open(filePath); }}
                                    >
                                        {label}
                                    </button>
                                </div>
                            )}
                            {state?.status === "error" && state.detail && (
                                <div data-testid={`remote-task-fetch-error${suffix}`} style={{ color: "var(--maclaw-error, #c0392b)", fontSize: 12, marginTop: 4, wordBreak: "break-word" }}>
                                    {state.detail}
                                </div>
                            )}
                        </div>
                    );
                })}
            </div>
            {files.length > VISIBLE_ARTIFACTS && (
                <button
                    type="button"
                    className="mc-task-artifacts__more"
                    data-testid="remote-task-artifacts-more"
                    onClick={() => setShowAll((open) => !open)}
                >
                    {showAll
                        ? chromeText(lang, "Show fewer artifacts", "收起产出物", "收起產出物")
                        : chromeText(lang, `View all artifacts (${files.length})`, `查看所有产物 (${files.length})`, `查看所有產物 (${files.length})`)}
                    <span aria-hidden="true">{showAll ? " ⌃" : " ›"}</span>
                </button>
            )}
        </div>
    );
}

// Card-head export on a cloud task: packs the whole workspace into one
// .tar.gz through the same op the task list's 打包下载 entry runs. The save
// dialog lives in the shell, so a cancelled pick resolves "" and returns the
// button to idle.
function WorkspaceExportButton({ lang, onExport }: { lang: string; onExport: () => Promise<string> }) {
    const [status, setStatus] = useState<"idle" | "busy" | "done" | "error">("idle");
    const [detail, setDetail] = useState("");
    // The pack can take a while on a big workspace: a same-tick double click
    // must not run it twice, and an unmounted card must not set state.
    const busyRef = useRef(false);
    const aliveRef = useRef(true);
    useEffect(() => {
        aliveRef.current = true;
        return () => {
            aliveRef.current = false;
        };
    }, []);
    const failLabel = chromeText(lang, "Export failed", "导出失败", "匯出失敗");
    const label = status === "busy"
        ? chromeText(lang, "Exporting...", "导出中...", "匯出中...")
        : status === "done"
            ? chromeText(lang, "Exported", "已导出", "已匯出")
            : status === "error"
                ? failLabel
                : chromeText(lang, "Export", "导出", "匯出");
    const title = status === "error"
        ? (failLabel + (detail ? `: ${detail}` : ""))
        : status === "done" && detail
            ? chromeText(lang, `Saved to ${detail}`, `已保存到 ${detail}`, `已儲存到 ${detail}`)
            : chromeText(lang, "Pack the whole workspace as .tar.gz and download it", "将整个工作目录打包为 .tar.gz 下载", "將整個工作目錄打包為 .tar.gz 下載");
    const fail = (err: unknown) => {
        busyRef.current = false;
        if (!aliveRef.current) return;
        const raw = err instanceof Error && err.message.trim() ? err.message : String(err || "").trim();
        setDetail(localizeTaskResultExportError(raw || "export failed", lang));
        setStatus("error");
    };
    const onClick = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (busyRef.current) return;
        busyRef.current = true;
        setStatus("busy");
        setDetail("");
        try {
            void onExport().then((dest) => {
                busyRef.current = false;
                if (!aliveRef.current) return;
                const saved = String(dest || "").trim();
                if (!saved) {
                    setStatus("idle");
                    return;
                }
                setDetail(saved);
                setStatus("done");
            }).catch(fail);
        } catch (err) {
            fail(err);
        }
    };
    return (
        <button
            type="button"
            className="mc-task-artifacts__export"
            data-status={status}
            data-testid="remote-task-artifacts-export"
            title={title}
            aria-busy={status === "busy"}
            aria-live="polite"
            disabled={status === "busy"}
            onClick={onClick}
        >
            {label}
        </button>
    );
}

function LatexSourceBundleButton({ anchorPath, lang }: { anchorPath: string; lang: string }) {
    const [status, setStatus] = useState<"idle" | "busy" | "done" | "error">("idle");
    const [detail, setDetail] = useState("");
    const label = status === "busy"
        ? chromeText(lang, "Exporting...", "导出中...", "匯出中...")
        : status === "done"
            ? chromeText(lang, "Source package exported", "源码包已导出", "原始碼包已匯出")
            : chromeText(lang, "Export source package", "导出源码包", "匯出原始碼包");
    const title = status === "error"
        ? detail
        : status === "done" && detail
            ? detail
            : chromeText(lang, "Zip the main file, figures and other files needed to compile", "把主文件、图片和编译需要的其它文件打成 zip", "把主檔、圖片和編譯需要的其它檔案打成 zip");
    const onClick = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (status === "busy") return;
        setStatus("busy");
        setDetail("");
        void ExportLatexSourceBundle(anchorPath).then((dest) => {
            const saved = String(dest || "").trim();
            if (!saved) {
                setStatus("idle");
                return;
            }
            setDetail(chromeText(lang, `Saved to ${saved}`, `已保存到 ${saved}`, `已儲存到 ${saved}`));
            setStatus("done");
        }).catch((error: unknown) => {
            const raw = error instanceof Error ? error.message : String(error || "");
            setDetail(localizeTaskResultExportError(raw || "export failed", lang));
            setStatus("error");
        });
    };
    return (
        <button
            type="button"
            data-testid="task-result-latex-bundle-btn"
            title={title}
            aria-busy={status === "busy"}
            disabled={status === "busy"}
            onClick={onClick}
        >
            {label}
        </button>
    );
}

import React, { useEffect, useState } from "react";
import { ExportLatexSourceBundle, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { TaskResultUploadButton, taskResultUploadSupported } from "./TaskResultUploadButton";
import { TaskResultExportButton } from "./TaskResultExportButton";
import { dispatchPreviewTaskResult, localizeTaskResultExportError, taskResultPreviewKindFromPath } from "./taskResultPreview";
import { cloudSafePathLabel } from "./codingTaskMode";
import { chromeText } from "../../i18n/chromeText";
import {
    forgetCloudDriveSystemIcon,
    loadCloudDriveSystemIcon,
    peekCloudDriveSystemIcon,
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
    const [iconURL, setIconURL] = useState<string | null>(() => peekCloudDriveSystemIcon(suffix));
    useEffect(() => {
        let active = true;
        const known = peekCloudDriveSystemIcon(suffix);
        setIconURL(known);
        if (!known) {
            void loadCloudDriveSystemIcon(suffix).then((result) => {
                if (active) setIconURL(result.state === "icon" ? result.url : null);
            });
        }
        return () => { active = false; };
    }, [suffix]);
    if (iconURL) {
        return (
            <span className="mc-task-artifact__icon mc-task-artifact__icon--os" aria-hidden="true">
                <img
                    className="mc-task-artifact__img"
                    src={iconURL}
                    alt=""
                    draggable={false}
                    onError={() => { forgetCloudDriveSystemIcon(suffix); setIconURL(null); }}
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
                                {taskResultUploadSupported(filePath) && <TaskResultUploadButton filePath={filePath} lang={lang} />}
                            </div>
                            <div className="mc-task-result-card__actions mc-task-artifact-item__actions">
                                {/* "fill" on purpose: a global office-repaint repaints every
                                    button[class*='primary'] with --office-accent, which would
                                    fight this card's theme-token fill. */}
                                <button type="button" className="mc-artifact-btn--fill" data-testid={`task-result-preview-btn${suffix}`} onClick={(event) => { event.stopPropagation(); dispatchPreviewTaskResult(filePath, messageId); }}>{chromeText(lang, "Preview", "预览", "預覽")}</button>
                                <button type="button" data-testid={`task-result-view-btn${suffix}`} onClick={(event) => onOpen(event, filePath)}>{chromeText(lang, "View document", "查看文档", "查看文件")}</button>
                                <TaskResultExportButton filePath={filePath} lang={lang} testIdSuffix={suffix} />
                                <button type="button" data-testid={`task-result-reveal-btn${suffix}`} onClick={(event) => { event.stopPropagation(); void ShowItemInFolder(filePath).catch(() => {}); }}>{chromeText(lang, "Reveal", "定位", "定位")}</button>
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

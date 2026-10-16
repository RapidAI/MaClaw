import React, { useState } from "react";
import { ExportLatexSourceBundle } from "../../../wailsjs/go/main/App";
import { TaskResultUploadButton, taskResultUploadSupported } from "./TaskResultUploadButton";
import { TaskResultExportButton } from "./TaskResultExportButton";
import { dispatchContinueEditTaskResult, dispatchPreviewTaskResult, localizeTaskResultExportError, taskResultPreviewKindFromPath } from "./taskResultPreview";
import { cloudSafePathLabel } from "./codingTaskMode";
import { localizeText } from "./aiAssistantI18n";

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

function artifactLabel(path: string, lang: string): { title: string; name: string } {
    const title = cloudSafePathLabel(path, lang === "en" ? "File" : "文件");
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
    const primary = listed[0] || "";
    const primaryName = primary ? artifactLabel(primary, lang).name : "";
    return (
        <div className="mc-task-artifacts" data-testid={`task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__label">{localizeText(lang, "Artifacts", "产出物", "產出物")}</div>
            <div className="mc-task-artifacts__row">
                {visible.map((filePath, index) => {
                    const file = artifactLabel(filePath, lang);
                    return (
                        <div key={`${filePath}-${index}`} className="mc-task-artifact-item">
                            <a
                                className="mc-task-artifact"
                                href="#"
                                title={file.title}
                                onClick={(event) => onOpen(event, filePath)}
                            >
                                <span className="mc-task-artifact__icon" aria-hidden="true">▤</span>
                                <span className="mc-task-artifact__name">{file.name}</span>
                                <span className="mc-task-artifact__open" aria-hidden="true">↗</span>
                            </a>
                            {taskResultUploadSupported(filePath) && <TaskResultUploadButton filePath={filePath} lang={lang} />}
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
                        ? localizeText(lang, "Show fewer artifacts", "收起产出物", "收起產出物")
                        : localizeText(lang, `View all artifacts (${listed.length})`, `查看所有产物 (${listed.length})`, `查看所有產物 (${listed.length})`)}
                    <span aria-hidden="true">{showAll ? " ⌃" : " ›"}</span>
                </button>
            )}
            <div className="mc-task-result-card__actions">
                {primary ? (
                    <>
                        <button type="button" data-testid="task-result-preview-btn" title={listed.length > 1 ? primaryName : undefined} onClick={(event) => { event.stopPropagation(); dispatchPreviewTaskResult(primary, messageId); }}>{localizeText(lang, "Preview", "预览", "預覽")}</button>
                        <button type="button" data-testid="task-result-view-btn" title={listed.length > 1 ? primaryName : undefined} onClick={(event) => onOpen(event, primary)}>{localizeText(lang, "View document", "查看文档", "查看文件")}</button>
                        <TaskResultExportButton filePath={primary} lang={lang} />
                        <button type="button" data-testid="task-result-continue-btn" title={listed.length > 1 ? primaryName : undefined} onClick={(event) => { event.stopPropagation(); dispatchContinueEditTaskResult(primary, messageId); }}>{localizeText(lang, "Continue editing", "继续修改", "繼續修改")}</button>
                    </>
                ) : null}
                {offerBundle ? <LatexSourceBundleButton anchorPath={bundleAnchor} lang={lang} /> : null}
            </div>
        </div>
    );
}

function LatexSourceBundleButton({ anchorPath, lang }: { anchorPath: string; lang: string }) {
    const [status, setStatus] = useState<"idle" | "busy" | "done" | "error">("idle");
    const [detail, setDetail] = useState("");
    const label = status === "busy"
        ? localizeText(lang, "Exporting...", "导出中...", "匯出中...")
        : status === "done"
            ? localizeText(lang, "Source package exported", "源码包已导出", "原始碼包已匯出")
            : localizeText(lang, "Export source package", "导出源码包", "匯出原始碼包");
    const title = status === "error"
        ? detail
        : status === "done" && detail
            ? detail
            : localizeText(lang, "Zip the main file, figures and other files needed to compile", "把主文件、图片和编译需要的其它文件打成 zip", "把主檔、圖片和編譯需要的其它檔案打成 zip");
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
            setDetail(localizeText(lang, `Saved to ${saved}`, `已保存到 ${saved}`, `已儲存到 ${saved}`));
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

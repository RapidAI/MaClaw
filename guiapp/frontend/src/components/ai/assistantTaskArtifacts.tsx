import React, { useState } from "react";
import { TaskResultUploadButton, taskResultUploadSupported } from "./TaskResultUploadButton";
import { TaskResultExportButton } from "./TaskResultExportButton";
import { dispatchContinueEditTaskResult, dispatchPreviewTaskResult, taskResultPreviewKindFromPath } from "./taskResultPreview";
import { cloudSafePathLabel } from "./codingTaskMode";
import { localizeText } from "./aiAssistantI18n";

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
}: {
    paths: string[];
    messageId: string;
    lang: string;
    onOpen: (event: React.MouseEvent, filePath: string) => void;
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
    files.sort((left, right) => Number(!taskResultPreviewKindFromPath(left)) - Number(!taskResultPreviewKindFromPath(right)));
    if (!files.length) return null;
    const visible = showAll ? files : files.slice(0, VISIBLE_ARTIFACTS);
    const primary = files[0];
    const primaryName = artifactLabel(primary, lang).name;
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
            {files.length > VISIBLE_ARTIFACTS && (
                <button
                    type="button"
                    className="mc-task-artifacts__more"
                    data-testid="task-artifacts-more"
                    onClick={() => setShowAll((open) => !open)}
                >
                    {showAll
                        ? localizeText(lang, "Show fewer artifacts", "收起产出物", "收起產出物")
                        : localizeText(lang, `View all artifacts (${files.length})`, `查看所有产物 (${files.length})`, `查看所有產物 (${files.length})`)}
                    <span aria-hidden="true">{showAll ? " ⌃" : " ›"}</span>
                </button>
            )}
            <div className="mc-task-result-card__actions">
                <button type="button" data-testid="task-result-preview-btn" title={files.length > 1 ? primaryName : undefined} onClick={(event) => { event.stopPropagation(); dispatchPreviewTaskResult(primary, messageId); }}>{localizeText(lang, "Preview", "预览", "預覽")}</button>
                <button type="button" data-testid="task-result-view-btn" title={files.length > 1 ? primaryName : undefined} onClick={(event) => onOpen(event, primary)}>{localizeText(lang, "View document", "查看文档", "查看文件")}</button>
                <TaskResultExportButton filePath={primary} lang={lang} />
                <button type="button" data-testid="task-result-continue-btn" title={files.length > 1 ? primaryName : undefined} onClick={(event) => { event.stopPropagation(); dispatchContinueEditTaskResult(primary, messageId); }}>{localizeText(lang, "Continue editing", "继续修改", "繼續修改")}</button>
            </div>
        </div>
    );
}

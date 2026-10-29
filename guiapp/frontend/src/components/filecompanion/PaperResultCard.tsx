import { useState, type MouseEvent } from "react";
import { ExportTaskResultFile, OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import { tailText } from "../../i18n/mnTail";

/** Companion paper PDF. Open and save only; preview stays hidden. */
export function PaperResultCard({ filePath, messageId, lang = "en" }: { filePath: string; messageId: string; lang?: string }) {
    const name = filePath.split(/[/\\]/).filter(Boolean).pop() || filePath;
    const [saveState, setSaveState] = useState<"idle" | "busy" | "done" | "error">("idle");
    const open = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (typeof OpenFileOrShowInFolder !== "function") return;
        void OpenFileOrShowInFolder(filePath).catch(() => undefined);
    };
    const save = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (saveState === "busy" || typeof ExportTaskResultFile !== "function") return;
        setSaveState("busy");
        void ExportTaskResultFile(filePath).then((dest) => {
            setSaveState(dest ? "done" : "idle");
        }).catch(() => {
            setSaveState("error");
        });
    };
    const saveLabel = saveState === "busy"
        ? tailText(lang, "Saving...", "保存中...")
        : saveState === "done"
            ? tailText(lang, "Saved", "已保存")
            : saveState === "error"
                ? tailText(lang, "Save failed", "保存失败")
                : tailText(lang, "Save", "保存");
    return (
        <div className="mc-task-artifacts" data-testid={`task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__label">{tailText(lang, "Artifacts", "产出物")}</div>
            <div className="mc-task-artifacts__row">
                <a className="mc-task-artifact" href="#" title={filePath} onClick={open}>
                    <span className="mc-task-artifact__name">{name}</span>
                </a>
            </div>
            <div className="mc-task-result-card__actions">
                <button type="button" data-testid="task-result-view-btn" onClick={open}>{tailText(lang, "View document", "查看文档")}</button>
                <button type="button" data-testid="file-companion-paper-save" aria-busy={saveState === "busy"} disabled={saveState === "busy"} onClick={save}>{saveLabel}</button>
            </div>
        </div>
    );
}

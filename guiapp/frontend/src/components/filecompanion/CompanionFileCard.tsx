import { useState, type MouseEvent } from "react";
import { ExportTaskResultFile, OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import { companionSay } from "../../i18n/mnCompanion";

export type CompanionFile = { label: string; path: string; size?: number };

type SaveState = "idle" | "busy" | "done" | "error";

/**
 * A file the companion produced in this answer. Open and save only; preview
 * stays hidden. The file lives in the host's own results directory, so 保存 is
 * how the reader decides where it ends up.
 */
export function CompanionFileCard({ files, messageId, lang = "en" }: { files: CompanionFile[]; messageId: string; lang?: string }) {
    const [states, setStates] = useState<Record<string, SaveState>>({});
    if (!files.length) return null;
    const open = (event: MouseEvent, path: string) => {
        event.preventDefault();
        event.stopPropagation();
        if (typeof OpenFileOrShowInFolder !== "function") return;
        void OpenFileOrShowInFolder(path).catch(() => undefined);
    };
    const save = (event: MouseEvent, path: string) => {
        event.preventDefault();
        event.stopPropagation();
        if (states[path] === "busy" || typeof ExportTaskResultFile !== "function") return;
        setStates((current) => ({ ...current, [path]: "busy" }));
        void ExportTaskResultFile(path).then((dest) => {
            setStates((current) => ({ ...current, [path]: dest ? "done" : "idle" }));
        }).catch(() => {
            setStates((current) => ({ ...current, [path]: "error" }));
        });
    };
    return (
        <div className="mc-task-artifacts" data-testid={`companion-file-card-${messageId}`}>
            <div className="mc-task-artifacts__label">{companionSay(lang, "产出物")}</div>
            {files.map((file) => {
                const name = file.label || file.path.split(/[/\\]/).filter(Boolean).pop() || file.path;
                const state = states[file.path] ?? "idle";
                const saveLabel = state === "busy"
                    ? companionSay(lang, "保存中...")
                    : state === "done"
                        ? companionSay(lang, "已保存")
                        : state === "error"
                            ? companionSay(lang, "保存失败")
                            : companionSay(lang, "保存");
                return (
                    <div key={file.path}>
                        <div className="mc-task-artifacts__row">
                            <a className="mc-task-artifact" href="#" title={file.path} onClick={(event) => open(event, file.path)}>
                                <span className="mc-task-artifact__name">{name}</span>
                            </a>
                        </div>
                        <div className="mc-task-result-card__actions">
                            <button type="button" data-testid="companion-file-view-btn" onClick={(event) => open(event, file.path)}>{companionSay(lang, "查看文档")}</button>
                            <button type="button" data-testid="companion-file-save" aria-busy={state === "busy"} disabled={state === "busy"} onClick={(event) => save(event, file.path)}>{saveLabel}</button>
                        </div>
                    </div>
                );
            })}
        </div>
    );
}
